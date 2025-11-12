package metrics

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	nfeProcessed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nfe_processed_total",
			Help: "Quantidade de NF-e processadas, por status e origem (xml/zip).",
		},
		[]string{"status", "source"}, // status: success|parse_error|db_error|duplicate, source: xml|zip
	)

	nfeDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nfe_process_duration_seconds",
			Help:    "Tempo de processamento de cada NF-e em segundos.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"status", "source"},
	)
)

// Init registra as métricas no registry global.
func Init() {
	prometheus.MustRegister(nfeProcessed, nfeDuration)
}

// ObserveNFe registra o resultado de uma NF-e processada.
func ObserveNFe(status, source string, d time.Duration) {
	labels := prometheus.Labels{
		"status": status,
		"source": source,
	}
	nfeProcessed.With(labels).Inc()
	nfeDuration.With(labels).Observe(d.Seconds())
}

// StartHTTPServer sobe um /metrics na porta indicada (ex: ":9101").
func StartHTTPServer(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		slog.Info("iniciando servidor de métricas Prometheus", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("erro no servidor de métricas", "addr", addr, "err", err)
		}
	}()
}
