package app

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sweater-ventures/slurpee/config"
	"github.com/sweater-ventures/slurpee/db"
)

type Application struct {
	Config config.AppConfig
	// Database access, caching, etc. would go here
	DB     *db.Queries
	dbconn *pgxpool.Pool
}

func NewApp(config *config.AppConfig) (*Application, error) {
	conn, err := connectToDB(config)
	queries := db.New(conn)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		return nil, err
	}

	return &Application{
		Config: *config,
		DB:     queries,
		dbconn: conn,
	}, nil
}
