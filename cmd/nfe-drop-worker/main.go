package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib"

	"nfe-drop/internal/config"
	"nfe-drop/internal/logx"
	"nfe-drop/internal/metrics"
	"nfe-drop/internal/worker"
)

func main() {
	logx.Init()
	slog.Info("[nfe-drop-worker] iniciando...")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("erro carregando config", "err", err)
		os.Exit(1)
	}

	db, err := sql.Open("pgx", cfg.AppDSN())
	if err != nil {
		slog.Error("erro abrindo conexão com banco da aplicação", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		slog.Error("erro no ping ao banco da aplicação", "err", err)
		os.Exit(1)
	}
	slog.Info("conectado ao banco da aplicação com sucesso")

	// inicia métricas Prometheus
	metrics.Init()
	metricsAddr := os.Getenv("NFE_DROP_METRICS_ADDR_WORKER")
	if metricsAddr == "" {
		metricsAddr = ":9101"
	}
	metrics.StartHTTPServer(metricsAddr)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	w := worker.New(cfg, db)
	if err := w.Run(ctx); err != nil && err != context.Canceled {
		slog.Error("worker finalizou com erro", "err", err)
		os.Exit(1)
	}

	slog.Info("worker finalizado")
}
