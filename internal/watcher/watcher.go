package watcher

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"nfe-drop/internal/config"
	"nfe-drop/internal/queue"
)

type Watcher struct {
	cfg     *config.Config
	watcher *fsnotify.Watcher

	stableAttempts int
	stableDelay    time.Duration

	rmq *queue.RabbitMQ
}

func New(cfg *config.Config) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	wr := &Watcher{
		cfg:            cfg,
		watcher:        w,
		stableAttempts: 5,
		stableDelay:    200 * time.Millisecond,
	}

	// Ativa RabbitMQ se configurado
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
			return nil, err
		}
		wr.rmq = rmq

		slog.Info("RabbitMQ habilitado no watcher",
			"url", url,
			"queue", qname,
		)
	} else {
		slog.Info("fila RabbitMQ desabilitada no watcher (NFE_DROP_QUEUE_BACKEND != rabbitmq)")
	}

	return wr, nil
}

func (w *Watcher) Run(ctx context.Context) error {
	defer w.watcher.Close()
	if w.rmq != nil {
		defer w.rmq.Close()
	}

	// Garante diretórios
	dirs := []string{
		w.cfg.IncomingDir,
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

	slog.Info("processando arquivos já existentes em incoming",
		"incoming_dir", w.cfg.IncomingDir,
	)
	w.processExistingFiles()

	if err := w.watcher.Add(w.cfg.IncomingDir); err != nil {
		return err
	}

	slog.Info("watching diretório de entrada",
		"incoming_dir", w.cfg.IncomingDir,
	)

	errCh := make(chan error, 1)

	go func() {
		for {
			select {
			case event, ok := <-w.watcher.Events:
				if !ok {
					errCh <- nil
					return
				}
				w.handleEvent(event)

			case err, ok := <-w.watcher.Errors:
				if !ok {
					errCh <- nil
					return
				}
				slog.Error("erro no watcher", "err", err)
			}
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("contexto cancelado, encerrando watcher")
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// ----------------------------------------------------------------------
// Scan inicial
// ----------------------------------------------------------------------

func (w *Watcher) processExistingFiles() {
	entries, err := os.ReadDir(w.cfg.IncomingDir)
	if err != nil {
		slog.Error("erro lendo diretório incoming",
			"dir", w.cfg.IncomingDir,
			"err", err,
		)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(w.cfg.IncomingDir, entry.Name())
		w.handleIncomingFile(path)
	}
}

// ----------------------------------------------------------------------
// Eventos fsnotify
// ----------------------------------------------------------------------

func (w *Watcher) handleEvent(event fsnotify.Event) {
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Chmod) == 0 {
		return
	}

	path := event.Name

	info, err := os.Stat(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Debug("arquivo não está mais acessível em evento, ignorando",
				"path", path,
				"err", err,
			)
		}
		return
	}
	if info.IsDir() {
		return
	}

	w.handleIncomingFile(path)
}

// ----------------------------------------------------------------------
// Regras de negócio: arquivo caiu em incoming
// ----------------------------------------------------------------------

func (w *Watcher) handleIncomingFile(path string) {
	filename := filepath.Base(path)

	if isZoneIdentifier(filename) {
		slog.Info("arquivo de metadata (Zone.Identifier) detectado; removendo",
			"path", path,
		)
		if err := os.Remove(path); err != nil {
			slog.Warn("falha ao remover arquivo de metadata",
				"path", path,
				"err", err,
			)
		}
		return
	}

	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".xml", ".zip":
		if !w.waitFileStable(path) {
			slog.Warn("arquivo não estabilizou, ignorando por enquanto",
				"path", path,
			)
			return
		}
		kind := strings.TrimPrefix(ext, ".") // "xml" / "zip"
		w.moveToProcessing(path, filename, kind)

	default:
		w.moveToIgnored(path, filename)
	}
}

// ----------------------------------------------------------------------
// Estabilidade de arquivo
// ----------------------------------------------------------------------

func (w *Watcher) waitFileStable(path string) bool {
	var lastSize int64 = -1

	for i := 0; i < w.stableAttempts; i++ {
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return false
			}
			slog.Debug("erro ao stat arquivo durante espera de estabilidade",
				"path", path,
				"err", err,
			)
			return false
		}

		size := info.Size()
		if size > 0 && size == lastSize {
			return true
		}

		lastSize = size
		time.Sleep(w.stableDelay)
	}

	return false
}

// ----------------------------------------------------------------------
// Movimentação de arquivos
// ----------------------------------------------------------------------

func (w *Watcher) moveToProcessing(srcPath, filename, kind string) {
	destPath := filepath.Join(w.cfg.ProcessingDir, filename)
	if err := os.Rename(srcPath, destPath); err != nil {
		slog.Error("erro movendo arquivo de incoming para processing",
			"src", srcPath,
			"dest", destPath,
			"err", err,
		)
		return
	}
	slog.Info("arquivo movido de incoming para processing",
		"src", srcPath,
		"dest", destPath,
	)

	// Se RabbitMQ estiver habilitado, publica job
	if w.rmq != nil {
		job := queue.Job{
			Path:     destPath,
			Filename: filename,
			Kind:     kind, // "xml" ou "zip"
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := w.rmq.PublishJob(ctx, job); err != nil {
			slog.Error("erro publicando job no RabbitMQ",
				"path", destPath,
				"kind", kind,
				"err", err,
			)
		} else {
			slog.Info("job publicado no RabbitMQ",
				"path", destPath,
				"kind", kind,
			)
		}
	}
}

func (w *Watcher) moveToIgnored(srcPath, filename string) {
	destPath := filepath.Join(w.cfg.IgnoredDir, filename)
	if err := os.Rename(srcPath, destPath); err != nil {
		slog.Error("erro movendo arquivo de incoming para ignored",
			"src", srcPath,
			"dest", destPath,
			"err", err,
		)
		return
	}
	slog.Info("arquivo não suportado movido para ignored",
		"src", srcPath,
		"dest", destPath,
	)
}

// ----------------------------------------------------------------------
// Utilitários
// ----------------------------------------------------------------------

func isZoneIdentifier(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "zone.identifier")
}
