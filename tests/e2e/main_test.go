package e2e

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sweater-ventures/slurpee/api"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/config"
	"github.com/sweater-ventures/slurpee/db"
	"golang.org/x/crypto/bcrypt"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		fmt.Println("skipping e2e tests (-short flag)")
		os.Exit(0)
	}

	postgres := embeddedpostgres.NewDatabase(
		embeddedpostgres.DefaultConfig().
			Port(15432).
			Database("slurpee_test"),
	)

	if err := postgres.Start(); err != nil {
		log.Fatalf("failed to start embedded postgres: %v", err)
	}

	pool, err := pgxpool.New(context.Background(),
		"host=localhost port=15432 user=postgres password=postgres dbname=slurpee_test sslmode=disable",
	)
	if err != nil {
		postgres.Stop()
		log.Fatalf("failed to connect to embedded postgres: %v", err)
	}

	if err := runMigrations(pool); err != nil {
		pool.Close()
		postgres.Stop()
		log.Fatalf("failed to run migrations: %v", err)
	}

	testPool = pool

	code := m.Run()

	pool.Close()
	if err := postgres.Stop(); err != nil {
		log.Printf("warning: failed to stop embedded postgres: %v", err)
	}
	os.Exit(code)
}

// runMigrations reads all schema/*.sql files and executes the -- +migrate Up sections.
func runMigrations(pool *pgxpool.Pool) error {
	schemaDir := filepath.Join("..", "..", "schema")
	entries, err := os.ReadDir(schemaDir)
	if err != nil {
		return fmt.Errorf("reading schema dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		content, err := os.ReadFile(filepath.Join(schemaDir, f))
		if err != nil {
			return fmt.Errorf("reading %s: %w", f, err)
		}

		upSQL := extractMigrateUp(string(content))
		if upSQL == "" {
			continue
		}

		if _, err := pool.Exec(context.Background(), upSQL); err != nil {
			return fmt.Errorf("executing migration %s: %w", f, err)
		}
	}
	return nil
}

// extractMigrateUp extracts the SQL between "-- +migrate Up" and "-- +migrate Down" markers.
func extractMigrateUp(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	var lines []string
	inUp := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "-- +migrate Up" {
			inUp = true
			continue
		}
		if trimmed == "-- +migrate Down" {
			break
		}
		if inUp {
			lines = append(lines, line)
		}
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// truncateAll truncates all tables in the correct FK order.
func truncateAll(t *testing.T) {
	t.Helper()
	tables := []string{
		"delivery_attempts",
		"api_secret_subscribers",
		"subscriptions",
		"subscribers",
		"api_secrets",
		"events",
		"log_config",
	}
	_, err := testPool.Exec(context.Background(),
		"TRUNCATE "+strings.Join(tables, ", ")+" CASCADE",
	)
	if err != nil {
		t.Fatalf("truncateAll: %v", err)
	}
}

// newTestApp returns an *app.Application wired to the real embedded database.
func newTestApp(t *testing.T) *app.Application {
	t.Helper()
	queries := db.New(testPool)
	return &app.Application{
		Config: config.AppConfig{
			AdminSecret:       "test-admin-secret",
			MaxRetries:        2,
			MaxBackoffSeconds: 1,
			DeliveryQueueSize: 100,
			DeliveryWorkers:   2,
			DeliveryChanSize:  100,
			MaxParallel:       1,
		},
		DB:           queries,
		DeliveryChan: make(chan db.Event, 100),
		EventBus:     app.NewEventBus(),
		Sessions:     app.NewSessionStore(),
	}
}

// newTestRouter returns an *http.ServeMux with API routes registered.
func newTestRouter(t *testing.T, slurpee *app.Application) *http.ServeMux {
	t.Helper()
	router := http.NewServeMux()
	api.AddApis(slurpee, router)
	return router
}

// newUUID returns a pgtype.UUID with a new random UUID.
func newUUID() pgtype.UUID {
	return pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
}

// seedSubscriber inserts a subscriber directly into the database.
func seedSubscriber(t *testing.T, queries db.Querier, name, endpointURL, authSecret string) db.Subscriber {
	t.Helper()
	sub, err := queries.UpsertSubscriber(context.Background(), db.UpsertSubscriberParams{
		ID:          newUUID(),
		Name:        name,
		EndpointUrl: endpointURL,
		AuthSecret:  authSecret,
		MaxParallel: 1,
	})
	if err != nil {
		t.Fatalf("seedSubscriber: %v", err)
	}
	return sub
}

// seedSubscription inserts a subscription directly into the database.
func seedSubscription(t *testing.T, queries db.Querier, subscriberID pgtype.UUID, subjectPattern string, filter []byte, maxRetries *int32) db.Subscription {
	t.Helper()
	mr := pgtype.Int4{}
	if maxRetries != nil {
		mr = pgtype.Int4{Int32: *maxRetries, Valid: true}
	}
	sub, err := queries.CreateSubscription(context.Background(), db.CreateSubscriptionParams{
		ID:             newUUID(),
		SubscriberID:   subscriberID,
		SubjectPattern: subjectPattern,
		Filter:         filter,
		MaxRetries:     mr,
	})
	if err != nil {
		t.Fatalf("seedSubscription: %v", err)
	}
	return sub
}

// seedApiSecret inserts an API secret with a bcrypt hash of the plaintext value.
func seedApiSecret(t *testing.T, queries db.Querier, name, plaintext, subjectPattern string) (db.ApiSecret, string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("seedApiSecret hash: %v", err)
	}
	secret, err := queries.InsertApiSecret(context.Background(), db.InsertApiSecretParams{
		ID:             newUUID(),
		Name:           name,
		SecretHash:     string(hash),
		SubjectPattern: subjectPattern,
	})
	if err != nil {
		t.Fatalf("seedApiSecret insert: %v", err)
	}
	return secret, plaintext
}

func TestPlaceholder(t *testing.T) {
	truncateAll(t)
	slurpee := newTestApp(t)
	if slurpee.DB == nil {
		t.Fatal("expected DB to be non-nil")
	}
}
