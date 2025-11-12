package logx

import (
	"log/slog"
	"os"
)

var Logger *slog.Logger

func Init() {
	// Saída em JSON no stdout
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	Logger = slog.New(handler)
}
