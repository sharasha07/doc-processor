package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/sharasha07/doc-processor/internal/data"
	"google.golang.org/genai"
)

type application struct {
	logger       *slog.Logger
	cfg          Config
	models       data.Models
	redisClient  *redis.Client
	geminiClient *genai.Client
}

type Config struct {
	Port   int `env:"PORT,required"`
	Server struct {
		ReadTimeout  time.Duration `env:"SERVER_READ_TIMEOUT,required"`
		WriteTimeout time.Duration `env:"SERVER_WRITE_TIMEOUT,required"`
		IdleTimeout  time.Duration `env:"SERVER_IDLE_TIMEOUT,required"`
	}
	TemplatesDir string `env:"TEMPLATES_DIR,required"`
	DBURL        string `env:"DB_URL,required"`
	Redis        struct {
		URL string        `env:"REDIS_URL,required"`
		TTL time.Duration `env:"REDIS_TTL,required"`
	}
	Gemini struct {
		Key                   string  `env:"GEMINI_KEY,required"`
		Model                 string  `env:"GEMINI_MODEL,required"`
		InputPricePerMTokens  float64 `env:"GEMINI_INPUT_PRICE_PER_MTOKENS,required"`
		OutputPricePerMTokens float64 `env:"GEMINI_OUTPUT_PRICE_PER_MTOKENS,required"`
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

	geminiClient, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  cfg.Gemini.Key,
		Backend: genai.BackendGeminiAPI,
	})
	exitOnError(err, "failed to create Gemini client")

	pool, err := connectToDB(cfg.DBURL)
	exitOnError(err, "failed to connect to database")
	defer pool.Close()
	logger.Info("connected to database")

	options, err := redis.ParseURL(cfg.Redis.URL)
	exitOnError(err, "failed to parse redis URL")
	redisClient := redis.NewClient(options)
	defer redisClient.Close()

	app := &application{
		logger:       logger,
		cfg:          cfg,
		models:       data.NewModels(pool),
		redisClient:  redisClient,
		geminiClient: geminiClient,
	}

	err = app.serve()
	exitOnError(err, "server failed")
}

func connectToDB(url string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return pool, nil
}
