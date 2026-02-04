package app

import (
	"context"
	"log/slog"

	"github.com/sweater-ventures/slurpee/config"
)

func log(ctx context.Context) *slog.Logger {
	log := ctx.Value(config.LoggerContextKey)
	if log == nil {
		return slog.Default()
	} else {
		return log.(*slog.Logger)
	}
}
