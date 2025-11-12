package worker

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"nfe-drop/internal/config"
	"nfe-drop/internal/metrics"
	"nfe-drop/internal/nfe"
	"nfe-drop/internal/queue"
	"nfe-drop/internal/storage"
)

type Worker struct {
	cfg      *config.Config
	db       *sql.DB
	interval time.Duration

	rmq *queue.RabbitMQ
}

func New(cfg *config.Config, db *sql.DB) *Worker {
	w := &Worker{
		cfg:      cfg,
		db:       db,
		interval: 2 * time.Second,
	}

	backend := strings.ToLower(os.Getenv("NFE_DROP_QUEUE_BACKEND"))
	if backend == "rabbitmq" {
		url := os.Getenv("NFE_DROP_RABBITMQ_URL")
		if url == "" {
			url = "amqp://nfe_user:SenhaBemForte123!@localhost:5672/"
		}
		qname := os.Getenv("NFE_DROP_RABBITMQ_QUEUE")
		if qname == "" {
			qname = "nfe-drop-jobs"
		}

		rmq, err := queue.NewRabbitMQ(url, qname)
		if err != nil {
			slog.Error("erro criando cliente RabbitMQ no worker; caindo para modo polling",
				"err", err,
			)
		} else {
			w.rmq = rmq
			slog.Info("RabbitMQ habilitado no worker",
				"url", url,
				"queue", qname,
			)
		}
	} else {
		slog.Info("fila RabbitMQ desabilitada no worker (NFE_DROP_QUEUE_BACKEND != rabbitmq)")
	}

	return w
}

func (w *Worker) Run(ctx context.Context) error {
	// garante diretórios
	dirs := []string{
		w.cfg.ProcessingDir,
		w.cfg.ProcessedDir,
		w.cfg.FailedDir,
		w.cfg.TmpDir,
		w.cfg.IgnoredDir,
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	if w.rmq != nil {
		defer w.rmq.Close()
		slog.Info("worker rodando em modo fila (RabbitMQ)",
			"processing_dir", w.cfg.ProcessingDir,
		)
		return w.runQueueMode(ctx)
	}

	slog.Info("worker rodando em modo polling de diretório",
		"processing_dir", w.cfg.ProcessingDir,
	)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("contexto cancelado, encerrando worker")
			return ctx.Err()
		case <-ticker.C:
			w.processProcessingFolder()
		}
	}
}

// ----------------------------------------------------------------------
// MODO FILA (RabbitMQ)
// ----------------------------------------------------------------------

func (w *Worker) runQueueMode(ctx context.Context) error {
	return w.rmq.ConsumeJobs(ctx, func(job queue.Job) error {
		return w.handleJob(job)
	})
}

func (w *Worker) handleJob(job queue.Job) error {
	info, err := os.Stat(job.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Warn("arquivo do job não existe mais, ignorando",
				"path", job.Path,
				"filename", job.Filename,
				"kind", job.Kind,
			)
			return nil
		}
		slog.Error("erro ao stat arquivo do job",
			"path", job.Path,
			"err", err,
		)
		return nil
	}
	if info.IsDir() {
		return nil
	}

	switch strings.ToLower(job.Kind) {
	case "xml":
		w.processXML(job.Path, job.Filename)
	case "zip":
		w.processZIP(job.Path, job.Filename)
	default:
		slog.Warn("tipo de job desconhecido",
			"path", job.Path,
			"filename", job.Filename,
			"kind", job.Kind,
		)
	}

	return nil
}

// ----------------------------------------------------------------------
// MODO POLLING (legado)
// ----------------------------------------------------------------------

func (w *Worker) processProcessingFolder() {
	entries, err := os.ReadDir(w.cfg.ProcessingDir)
	if err != nil {
		slog.Error("erro lendo diretório processing", "dir", w.cfg.ProcessingDir, "err", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		srcPath := filepath.Join(w.cfg.ProcessingDir, entry.Name())
		w.handleProcessingFile(srcPath)
	}
}

func (w *Worker) handleProcessingFile(srcPath string) {
	info, err := os.Stat(srcPath)
	if err != nil {
		slog.Warn("arquivo em processing não está mais acessível, ignorando",
			"path", srcPath,
			"err", err,
		)
		return
	}
	if info.IsDir() {
		return
	}

	filename := filepath.Base(srcPath)
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".xml":
		w.processXML(srcPath, filename)
	case ".zip":
		w.processZIP(srcPath, filename)
	default:
		slog.Info("extensão não tratada em processing; movendo para processed",
			"path", srcPath,
			"ext", ext,
		)
		w.moveToProcessed(srcPath, filename)
	}
}

// ----------------------------------------------------------------------
// Lógica de processamento
// ----------------------------------------------------------------------

func (w *Worker) processXML(srcPath, filename string) {
	start := time.Now()
	status := "success"
	source := "xml"

	defer func() {
		metrics.ObserveNFe(status, source, time.Since(start))
	}()

	parsed, err := nfe.ParseFile(srcPath)
	if err != nil {
		status = "parse_error"
		slog.Error("erro ao validar/parsear XML",
			"path", srcPath,
			"err", err,
		)
		w.moveToFailed(srcPath, filename)
		return
	}

	w.logParsedNFe(srcPath, parsed)

	_, err = storage.SaveNFeWithRelations(w.db, parsed)
	if err != nil {
		if errors.Is(err, storage.ErrNFeAlreadyExists) {
			status = "duplicate"
			slog.Info("NFe já existia no banco, ignorando reprocessamento (XML solto)",
				"path", srcPath,
				"chave", parsed.ChaveAcesso,
			)
			w.moveToIgnored(srcPath, filename)
			return
		}

		status = "db_error"
		slog.Error("erro salvando NFe e relacionamentos no banco (XML solto)",
			"path", srcPath,
			"chave", parsed.ChaveAcesso,
			"err", err,
		)
		w.moveToFailed(srcPath, filename)
		return
	}

	// sucesso
	status = "success"
	w.moveToProcessed(srcPath, filename)
}

func (w *Worker) processZIP(srcPath, filename string) {
	slog.Info("ZIP identificado, iniciando extração e processamento",
		"path", srcPath,
	)

	ext := filepath.Ext(filename)
	baseName := strings.TrimSuffix(filename, ext)

	workDir := filepath.Join(w.cfg.TmpDir, baseName)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		slog.Error("erro criando diretório temporário para ZIP",
			"zip", srcPath,
			"work_dir", workDir,
			"err", err,
		)
		_ = os.Remove(srcPath)
		return
	}
	defer func() {
		if err := os.RemoveAll(workDir); err != nil {
			slog.Warn("falha ao remover diretório temporário",
				"work_dir", workDir,
				"err", err,
			)
		}
	}()

	zr, err := zip.OpenReader(srcPath)
	if err != nil {
		slog.Error("erro abrindo ZIP",
			"path", srcPath,
			"err", err,
		)
		_ = os.Remove(srcPath)
		return
	}
	defer zr.Close()

	if len(zr.File) == 0 {
		slog.Warn("ZIP está vazio",
			"path", srcPath,
		)
		_ = os.Remove(srcPath)
		return
	}

	var (
		xmlCount     int
		successCount int
		dupCount     int
		failCount    int
	)

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}

		name := f.Name
		lowerName := strings.ToLower(name)
		if !strings.HasSuffix(lowerName, ".xml") {
			slog.Info("arquivo dentro do ZIP ignorado (não é XML)",
				"zip", srcPath,
				"inner_name", name,
			)
			continue
		}

		xmlCount++

		rc, err := f.Open()
		if err != nil {
			slog.Error("erro abrindo entrada do ZIP",
				"zip", srcPath,
				"inner_name", name,
				"err", err,
			)
			failCount++
			continue
		}

		innerFileName := filepath.Base(name)
		innerPath := filepath.Join(workDir, innerFileName)

		out, err := os.Create(innerPath)
		if err != nil {
			slog.Error("erro criando arquivo temporário para XML extraído",
				"zip", srcPath,
				"inner_name", name,
				"dest", innerPath,
				"err", err,
			)
			rc.Close()
			failCount++
			continue
		}

		if _, err := io.Copy(out, rc); err != nil {
			slog.Error("erro copiando conteúdo do ZIP para arquivo temporário",
				"zip", srcPath,
				"inner_name", name,
				"dest", innerPath,
				"err", err,
			)
			out.Close()
			rc.Close()
			failCount++
			continue
		}

		out.Close()
		rc.Close()

		slog.Info("XML extraído do ZIP para processamento",
			"zip", srcPath,
			"inner_name", name,
			"temp_path", innerPath,
		)

		// métrica por NF-e vinda de ZIP
		start := time.Now()
		status := "success"
		source := "zip"

		parsed, err := nfe.ParseFile(innerPath)
		if err != nil {
			status = "parse_error"
			slog.Error("erro ao validar/parsear XML extraído do ZIP",
				"zip", srcPath,
				"inner_name", name,
				"temp_path", innerPath,
				"err", err,
			)
			failCount++
			w.moveToFailed(innerPath, innerFileName)
			metrics.ObserveNFe(status, source, time.Since(start))
			continue
		}

		w.logParsedNFe(innerPath, parsed)

		_, err = storage.SaveNFeWithRelations(w.db, parsed)
		if err != nil {
			if errors.Is(err, storage.ErrNFeAlreadyExists) {
				status = "duplicate"
				slog.Info("NFe já existia no banco, ignorando reprocessamento (ZIP)",
					"zip", srcPath,
					"inner_name", name,
					"chave", parsed.ChaveAcesso,
				)
				dupCount++
				w.moveToIgnored(innerPath, innerFileName)
				metrics.ObserveNFe(status, source, time.Since(start))
				continue
			}

			status = "db_error"
			slog.Error("erro salvando NFe e relacionamentos no banco (XML de ZIP)",
				"zip", srcPath,
				"inner_name", name,
				"chave", parsed.ChaveAcesso,
				"err", err,
			)
			failCount++
			w.moveToFailed(innerPath, innerFileName)
			metrics.ObserveNFe(status, source, time.Since(start))
			continue
		}

		// sucesso
		status = "success"
		successCount++
		w.moveToProcessed(innerPath, innerFileName)
		metrics.ObserveNFe(status, source, time.Since(start))
	}

	if err := os.Remove(srcPath); err != nil {
		slog.Warn("falha ao remover ZIP original após processamento",
			"path", srcPath,
			"err", err,
		)
	}

	slog.Info("processamento de ZIP concluído",
		"zip", srcPath,
		"xml_total", xmlCount,
		"success", successCount,
		"duplicatas", dupCount,
		"failed", failCount,
	)
}

func (w *Worker) logParsedNFe(srcPath string, parsed *nfe.ParsedNFe) {
	summary := map[string]interface{}{
		"chave":            parsed.ChaveAcesso,
		"modelo":           parsed.Modelo,
		"serie":            parsed.Serie,
		"numero":           parsed.Numero,
		"emissao":          parsed.EmissaoDate,
		"emitente_cnpj":    parsed.EmitenteCNPJ,
		"emitente_razao":   parsed.EmitenteRazao,
		"dest_cnpj_cpf":    parsed.DestCNPJCPF,
		"dest_razao":       parsed.DestRazao,
		"valor_total_nota": parsed.ValorTotalNota,
		"itens":            len(parsed.Itens),
		"duplicatas":       len(parsed.Duplicatas),
		"pagamentos":       len(parsed.Pagamentos),
	}

	rawJSON, _ := json.Marshal(parsed)

	slog.Info("NFe parseada com sucesso",
		"path", srcPath,
		"summary", summary,
		"parsed_json", string(rawJSON),
	)
}

func (w *Worker) moveToProcessed(srcPath, filename string) {
	destPath := filepath.Join(w.cfg.ProcessedDir, filename)
	if err := os.Rename(srcPath, destPath); err != nil {
		slog.Error("erro movendo arquivo para processed",
			"src", srcPath,
			"dest", destPath,
			"err", err,
		)
		return
	}
	slog.Info("arquivo movido para processed",
		"src", srcPath,
		"dest", destPath,
	)
}

func (w *Worker) moveToFailed(srcPath, filename string) {
	destPath := filepath.Join(w.cfg.FailedDir, filename)
	if err := os.Rename(srcPath, destPath); err != nil {
		slog.Error("erro movendo arquivo para failed",
			"src", srcPath,
			"dest", destPath,
			"err", err,
		)
		return
	}
	slog.Info("arquivo movido para failed",
		"src", srcPath,
		"dest", destPath,
	)
}

func (w *Worker) moveToIgnored(srcPath, filename string) {
	destPath := filepath.Join(w.cfg.IgnoredDir, filename)
	if err := os.Rename(srcPath, destPath); err != nil {
		slog.Error("erro movendo arquivo para ignored",
			"src", srcPath,
			"dest", destPath,
			"err", err,
		)
		return
	}
	slog.Info("arquivo movido para ignored",
		"src", srcPath,
		"dest", destPath,
	)
}
