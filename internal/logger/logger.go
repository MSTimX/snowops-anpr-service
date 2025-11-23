package logger

import (
	"os"

	"github.com/rs/zerolog"
)

func New(env string) zerolog.Logger {
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()

	if env == "development" {
		log = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	return log
}

