package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"anpr-service/internal/auth"
	"anpr-service/internal/config"
	"anpr-service/internal/db"
	httphandler "anpr-service/internal/http"
	"anpr-service/internal/http/middleware"
	"anpr-service/internal/logger"
	"anpr-service/internal/repository"
	"anpr-service/internal/service"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	appLogger := logger.New(cfg.Environment)

	database, err := db.New(cfg, appLogger)
	if err != nil {
		appLogger.Fatal().Err(err).Msg("failed to connect database")
	}

	anprRepo := repository.NewANPRRepository(database)
	anprService := service.NewANPRService(anprRepo, appLogger)

	tokenParser := auth.NewParser(cfg.Auth.AccessSecret)

	handler := httphandler.NewHandler(anprService, cfg, appLogger)
	authMiddleware := middleware.Auth(tokenParser)
	router := httphandler.NewRouter(handler, authMiddleware, cfg.Environment, database)

	addr := fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port)
	appLogger.Info().Str("addr", addr).Msg("starting ANPR service")

	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			appLogger.Error().Err(err).Msg("failed to start server")
			os.Exit(1)
		}
	}()

	// Запускаем фоновую задачу для очистки старых событий (каждые 6 часов)
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()

		// Выполняем первую очистку через 1 минуту после запуска
		time.Sleep(1 * time.Minute)
		if deleted, err := anprService.CleanupOldEvents(context.Background(), 3); err == nil {
			appLogger.Info().Int64("deleted", deleted).Msg("initial cleanup completed")
		}

		for range ticker.C {
			if deleted, err := anprService.CleanupOldEvents(context.Background(), 3); err != nil {
				appLogger.Error().Err(err).Msg("periodic cleanup failed")
			} else if deleted > 0 {
				appLogger.Info().Int64("deleted", deleted).Msg("periodic cleanup completed")
			}
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	appLogger.Info().Msg("shutting down server")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		appLogger.Error().Err(err).Msg("server forced to shutdown")
	}

	appLogger.Info().Msg("server exited")
}
