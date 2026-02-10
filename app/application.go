package app

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sweater-ventures/slurpee/config"
	"github.com/sweater-ventures/slurpee/db"
)

type Application struct {
	Config         config.AppConfig
	DB             db.Querier
	DeliveryChan   chan db.Event
	EventBus       *EventBus
	Sessions       *SessionStore
	SecretCache    *Cache[pgtype.UUID, db.ApiSecret]
	LogConfigCache *Cache[string, db.LogConfig]
	dbconn         *pgxpool.Pool
	stopDelivery   func()
}

func NewApp(config *config.AppConfig) (*Application, error) {
	conn, err := connectToDB(config)
	queries := db.New(conn)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		return nil, err
	}

	return &Application{
		Config:         *config,
		DB:             queries,
		DeliveryChan:   make(chan db.Event, config.DeliveryChanSize),
		EventBus:       NewEventBus(),
		Sessions:       NewSessionStore(),
		SecretCache:    NewCache[pgtype.UUID, db.ApiSecret](),
		LogConfigCache: NewCache[string, db.LogConfig](),
		dbconn:         conn,
		stopDelivery:   func() {},
	}, nil
}

func (slurpee *Application) SetStopDelivery(fn func()) {
	slurpee.stopDelivery = fn
}
