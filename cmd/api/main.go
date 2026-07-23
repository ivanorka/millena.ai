package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ivanorka/millena-ai/internal/auth"
	"github.com/ivanorka/millena-ai/internal/config"
	"github.com/ivanorka/millena-ai/internal/database"
	"github.com/ivanorka/millena-ai/internal/httpapi"
	"github.com/ivanorka/millena-ai/internal/notification"
	"github.com/ivanorka/millena-ai/internal/operations"
	"github.com/ivanorka/millena-ai/internal/social"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	startupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := database.Open(startupContext, cfg.DatabaseURL, cfg.DatabaseMaxConnections)
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	authRepository := auth.NewRepository(pool)
	if err := authRepository.EnsureMPRWorkspace(startupContext, cfg.DemoAdminEmail, cfg.DemoAdminName, cfg.DemoAdminPassword); err != nil {
		slog.Error("MPR workspace bootstrap failed", "error", err)
		os.Exit(1)
	}
	if cfg.SuperAdminEmail != "" && cfg.SuperAdminPassword != "" {
		if err := authRepository.EnsureSuperAdmin(startupContext, cfg.SuperAdminEmail, cfg.SuperAdminName, cfg.SuperAdminPassword); err != nil {
			slog.Error("super-admin bootstrap failed", "error", err)
			os.Exit(1)
		}
	}

	router := httpapi.NewRouter(httpapi.RouterOptions{
		Database:        pool,
		StaticDir:       cfg.StaticDir,
		AllowedOrigins:  cfg.CORSAllowedOrigins,
		SessionTTL:      cfg.SessionTTL,
		CookieSecure:    cfg.Environment == "production",
		AIProvider:      cfg.AIProvider,
		OllamaBaseURL:   cfg.OllamaBaseURL,
		OllamaModel:     cfg.OllamaModel,
		AITimeout:       cfg.AIRequestTimeout,
		StripeSecretKey: cfg.StripeSecretKey,
		AppBaseURL:      cfg.AppBaseURL,
	})
	workerContext, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	go social.NewWorker(social.NewRepository(pool), 2*time.Second).Run(workerContext)
	go operations.NewWorker(operations.NewRepository(pool), 2*time.Second).Run(workerContext)
	go notification.NewWorker(notification.NewRepository(pool), notification.NewSMTPMailer(notification.SMTPConfig{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
		From: cfg.EmailFrom, FromName: cfg.EmailFromName, AppURL: cfg.AppBaseURL,
	}), 15*time.Second).Run(workerContext)

	writeTimeout := 30 * time.Second
	if aiWriteTimeout := cfg.AIRequestTimeout + 5*time.Second; aiWriteTimeout > writeTimeout {
		writeTimeout = aiWriteTimeout
	}
	server := &http.Server{
		Addr:              cfg.Address(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("Millena API started", "address", cfg.Address(), "environment", cfg.Environment)
		serverErrors <- server.ListenAndServe()
	}()

	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-shutdownSignal:
		slog.Info("shutdown requested", "signal", sig.String())
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}
	stopWorker()

	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownContext); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	slog.Info("Millena API stopped")
}
