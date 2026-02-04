package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sweater-ventures/slurpee/config"
)

func connectToDB(config *config.AppConfig) (*pgxpool.Pool, error) {
	dbconfig, err := pgxpool.ParseConfig(
		fmt.Sprintf("host=%s user=%s password=%s port=%d sslmode=%s dbname=%s pool_max_conns=%d pool_min_conns=%d",
			config.DBHost,
			config.DBUsername,
			config.DBPassword,
			config.DBPort,
			config.DBSSLMode,
			config.DBName,
			config.DBMaxConns,
			config.DBMinConns,
		),
	)
	if err != nil {
		slog.Error("Failed to parse database configuration", "error", err)
		return nil, err
	}
	slog.Info("Database connection pool established",
		slog.String("host", config.DBHost),
		slog.Int("port", config.DBPort),
		slog.String("dbname", config.DBName),
		slog.Int("max_conns", config.DBMaxConns),
	)
	pool, err := pgxpool.NewWithConfig(context.Background(), dbconfig)
	return pool, err
}

func (a *Application) Close() {
	a.dbconn.Close()
}
