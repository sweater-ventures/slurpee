package config

import (
	"log/slog"
	"os"
	"strings"

	"github.com/sweater-ventures/devslog"
	"golang.org/x/term"
)

type ContextKey string

var LoggerContextKey = ContextKey("logger")

var logLevel *slog.LevelVar

func InitLogging() {
	logLevel = new(slog.LevelVar)
	logLevel.Set(slog.LevelInfo)
	jsonLogging := false
	jsonLoggingEnv, ok := os.LookupEnv("JSON_LOGGING")
	if ok && strings.ToLower(jsonLoggingEnv) == "true" {
		jsonLogging = true
	}
	if jsonLogging || !term.IsTerminal(int(os.Stdout.Fd())) {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: logLevel,
		})))
	} else {
		logger := slog.New(devslog.NewHandler(os.Stdout, &devslog.Options{
			HandlerOptions: &slog.HandlerOptions{
				Level: logLevel,
			},
			TimeFormat:           "[ 03:04:05 PM ]",
			StringIndentation:    true,
			DisableAttributeType: true,
		}))

		// optional: set global logger
		slog.SetDefault(logger)
	}
}
