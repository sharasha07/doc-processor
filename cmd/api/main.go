package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/caarlos0/env/v11"
)

type application struct {
	logger *slog.Logger
	cfg    Config
}

type Config struct {
	Port   int `env:"PORT,required"`
	Server struct {
		ReadTimeout  time.Duration `env:"SERVER_READ_TIMEOUT,required"`
		WriteTimeout time.Duration `env:"SERVER_WRITE_TIMEOUT,required"`
		IdleTimeout  time.Duration `env:"SERVER_IDLE_TIMEOUT,required"`
	}
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	exitOnError := func(err error, msg string) {
		if err != nil {
			logger.Error(msg, "err", err)
			os.Exit(1)
		}
	}

	var cfg Config
	err := env.Parse(&cfg)
	exitOnError(err, "failed to parse environment variables")

	app := &application{
		logger: logger,
		cfg:    cfg,
	}

	err = app.serve()
	exitOnError(err, "server failed")
}
