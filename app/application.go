package app

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sweater-ventures/slurpee/config"
	"github.com/sweater-ventures/slurpee/db"
)

type Application struct {
	Config       config.AppConfig
	DB           *db.Queries
	DeliveryChan chan db.Event
	dbconn       *pgxpool.Pool
	stopDelivery func()
}

func NewApp(config *config.AppConfig) (*Application, error) {
	conn, err := connectToDB(config)
	queries := db.New(conn)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		return nil, err
	}

	return &Application{
		Config:       *config,
		DB:           queries,
		DeliveryChan: make(chan db.Event, 1000),
		dbconn:       conn,
		stopDelivery: func() {},
	}, nil
}

func (a *Application) SetStopDelivery(fn func()) {
	a.stopDelivery = fn
}
