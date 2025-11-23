package db

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"anpr-service/internal/config"
)

func New(cfg *config.Config, log zerolog.Logger) (*gorm.DB, error) {
	dbCfg := cfg.DB
	gormLog := gormlogger.New(
		zerologWriter{logger: log},
		gormlogger.Config{
			SlowThreshold:             time.Second,
			Colorful:                  false,
			IgnoreRecordNotFoundError: true,
			LogLevel:                  selectLogLevel(cfg.Environment),
		},
	)

	database, err := gorm.Open(postgres.Open(dbCfg.DSN), &gorm.Config{
		Logger: gormLog,
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := database.DB()
	if err != nil {
		return nil, err
	}

	if dbCfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(dbCfg.MaxOpenConns)
	}
	if dbCfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(dbCfg.MaxIdleConns)
	}
	if dbCfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(dbCfg.ConnMaxLifetime)
	}

	if err := runMigrations(database); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return database, nil
}

func HealthCheck(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Exec("SELECT 1").Error
}

func selectLogLevel(env string) gormlogger.LogLevel {
	if env == "development" {
		return gormlogger.Info
	}
	return gormlogger.Warn
}

type zerologWriter struct {
	logger zerolog.Logger
}

func (w zerologWriter) Printf(msg string, args ...interface{}) {
	w.logger.Info().Msgf(msg, args...)
}

