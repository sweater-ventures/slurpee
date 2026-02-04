package main

import (
	"embed"
	"fmt"
	"log"
	"log/slog"
	"net/http"

	"github.com/vearutop/statigz"
	"github.com/vearutop/statigz/zstd"

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

	app, err := app.NewApp(appConfig)
	if err != nil {
		log.Fatal("Unable to initialize application", err)
	}
	defer app.Close()

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
	views.AddViews(app, router)

	slog.Info("Starting Slurpee", "port", appConfig.Port)
	err = http.ListenAndServe(fmt.Sprintf(":%d", appConfig.Port), middleware.AllStandardMiddleware(router))
	if err != nil {
		log.Fatal(err)
	}
}
