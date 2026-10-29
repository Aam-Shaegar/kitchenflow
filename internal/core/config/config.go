package core_config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	TimeZone        *time.Location
	DatabaseURL     string
	ServerAddr      string
	ShutdownTimeout time.Duration
}

func NewConfig() (*Config, error) {
	tz := os.Getenv("TIME_ZONE")
	if tz == "" {
		tz = "UTC"
	}
	zone, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("load time zone: %s: %w", tz, err)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is missing")
	}

	serverAddr := os.Getenv("SERVER_ADDR")
	if serverAddr == "" {
		serverAddr = ":8080"
	}

	shutdownTimeout := 30 * time.Second
	if raw := os.Getenv("SHUTDOWN_TIMEOUT"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("parse shutdown timeout: %w", err)
		}
		shutdownTimeout = parsed
	}

	return &Config{
		TimeZone:        zone,
		DatabaseURL:     databaseURL,
		ServerAddr:      serverAddr,
		ShutdownTimeout: shutdownTimeout,
	}, nil
}

func NewConfigMust() *Config {
	config, err := NewConfig()
	if err != nil {
		panic(fmt.Errorf("get core config: %w", err))
	}
	return config
}
