package core_database

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	URL     string        `envconfig:"DATABASE_URL" required:"true"`
	Timeout time.Duration `envconfig:"DB_TIMEOUT" required:"true"`
}

func NewConfig() (Config, error) {
	var config Config
	if err := envconfig.Process("", &config); err != nil {
		return Config{}, fmt.Errorf("process envconfig: %w", err)
	}
	return config, nil
}

func NewConfigMust() Config {
	config, err := NewConfig()
	if err != nil {
		panic(fmt.Errorf("get database config: %w", err))
	}
	return config
}
