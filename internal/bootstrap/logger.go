package bootstrap

import (
	"io"
	"log/slog"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"

	"morrow-api/internal/config"
)

// NewLogger builds the JSON logger: stdout always, plus an optional rotated file.
func NewLogger(cfg *config.Config) *slog.Logger {
	writer := io.Writer(os.Stdout)
	if cfg.LogFile != "" {
		writer = io.MultiWriter(os.Stdout, &lumberjack.Logger{
			Filename:   cfg.LogFile,
			MaxSize:    cfg.LogMaxSizeMB,
			MaxBackups: cfg.LogMaxBackups,
			MaxAge:     cfg.LogMaxAgeDays,
			Compress:   cfg.LogCompress,
		})
	}

	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		level = slog.LevelInfo
	}

	return slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level}))
}
