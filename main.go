package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vearutop/statigz"
	"github.com/vearutop/statigz/zstd"

	"github.com/sweater-ventures/slurpee/api"
	"github.com/sweater-ventures/slurpee/app"
	"github.com/sweater-ventures/slurpee/config"
	"github.com/sweater-ventures/slurpee/middleware"
	"github.com/sweater-ventures/slurpee/views"
)

//go:embed static/*
var static embed.FS

func main() {
	config.InitLogging()
	appConfig, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Unable to load configuration!!!", err)
	}

	if appConfig == nil {
		log.Fatal("Nil AppConfig, WTF")
	}

	application, err := app.NewApp(appConfig)
	if err != nil {
		log.Fatal("Unable to initialize application", err)
	}
	defer application.Close()

	slog.Debug("Configuration",
		"DevMode", appConfig.DevMode,
		"LogLevel", appConfig.LogLevel,
	)

	router := http.NewServeMux()
	if appConfig.DevMode {
		router.Handle("/static/", http.StripPrefix("/static", http.FileServer(http.Dir("static"))))
	} else {
		router.Handle("/static/", statigz.FileServer(static, zstd.AddEncoding))
	}
	views.AddViews(application, router)
	api.AddApis(application, router)

	// Start the centralized delivery dispatcher
	api.StartDispatcher(application)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", appConfig.Port),
		Handler: middleware.AllStandardMiddleware(router),
	}

	// Listen for shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("Starting Slurpee", "port", appConfig.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-sigChan
	slog.Info("Shutdown signal received")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("HTTP server shutdown error", "error", err)
	}

	// application.Close() runs via defer:
	// 1. Closes DeliveryChan (dispatcher stops accepting)
	// 2. Dispatcher drains buffered events and waits for all workers
	// 3. DB pool closes
	slog.Info("Shutdown complete")
}
