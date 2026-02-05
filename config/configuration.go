package config

import (
	"log/slog"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/joho/godotenv"
)

type AppConfig struct {
	DevMode    bool   `arg:"--dev,env:DEV_MODE" default:"false"`
	Port       int    `arg:"-p,--port,env:LISTEN_PORT" default:"8005"`
	LogLevel   string `arg:"--log-level,env:LOG_LEVEL" default:"default" help:"Log level to use.  Valid values are: debug, info, and warn/warning.  If default the level will be info or debug in dev mode."`
	DBHost     string `arg:"--db-host,env:DB_HOST" default:"localhost"`
	DBName     string `arg:"--db-name,env:DB_NAME" default:"slurpee"`
	DBPort     int    `arg:"--db-port,env:DB_PORT" default:"5432"`
	DBMaxConns int    `arg:"--db-max-conns,env:DB_MAX_CONNS" default:"10"`
	DBMinConns int    `arg:"--db-min-conns,env:DB_MIN_CONNS" default:"1"`
	DBSSLMode  string `arg:"--db-ssl-mode,env:DB_SSL_MODE" default:"disable"`
	DBUsername string `arg:"--db-username,env:DB_USERNAME" default:"slurpee"`
	DBPassword string `arg:"--db-password,env:DB_PASSWORD" default:"badpassword"`
	BaseURL     string `arg:"--base-url,env:BASE_URL" default:"http://localhost:8005" help:"Base URL for the application."`
	AdminSecret string `arg:"--admin-secret,env:ADMIN_SECRET" default:"" help:"Pre-shared secret for admin API endpoints (X-Slurpee-Admin-Secret header)."`
	MaxParallel int    `arg:"--max-parallel,env:MAX_PARALLEL" default:"1" help:"Default max parallel deliveries per subscriber."`
}

func LoadConfig() (*AppConfig, error) {
	var appConfig AppConfig
	arg.MustParse(&appConfig)

	if appConfig.DevMode {
		err := godotenv.Load(".env")
		if err == nil {
			// re-parse to get env vars from .env
			slog.Info("Loaded .env")
			arg.MustParse(&appConfig)
		}
	}

	if appConfig.LogLevel == "default" {
		if appConfig.DevMode {
			logLevel.Set(slog.LevelDebug)
		} else {
			logLevel.Set(slog.LevelInfo)
		}
	} else {
		intendedLevel := strings.ToLower(appConfig.LogLevel)
		switch intendedLevel {
		case "debug":
			logLevel.Set(slog.LevelDebug)
		case "info":
			logLevel.Set(slog.LevelInfo)
		case "warn", "warning":
			logLevel.Set(slog.LevelWarn)
		default:
			slog.Error("Unable to configure log level", "level", appConfig.LogLevel)
		}
	}

	return &appConfig, nil
}
