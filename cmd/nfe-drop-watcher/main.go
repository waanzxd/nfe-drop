package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"nfe-drop/internal/config"
	"nfe-drop/internal/logx"
	"nfe-drop/internal/metrics"
	"nfe-drop/internal/watcher"
)

func main() {
	logx.Init()
	slog.Info("[nfe-drop-watcher] iniciando...")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("erro carregando config", "err", err)
		os.Exit(1)
	}

	// inicia métricas Prometheus
	metrics.Init()
	metricsAddr := os.Getenv("NFE_DROP_METRICS_ADDR_WATCHER")
	if metricsAddr == "" {
		metricsAddr = ":9100"
	}
	metrics.StartHTTPServer(metricsAddr)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	w, err := watcher.New(cfg)
	if err != nil {
		slog.Error("erro criando watcher", "err", err)
		os.Exit(1)
	}

	if err := w.Run(ctx); err != nil && err != context.Canceled {
		slog.Error("watcher finalizou com erro", "err", err)
		os.Exit(1)
	}

	slog.Info("[nfe-drop-watcher] finalizado")
}
