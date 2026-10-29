package core_logger

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

type LoggerConfig struct {
	Level  string `envconfig:"LEVEL" default:"DEBUG"`
	Folder string `envconfig:"FOLDER" required:"true"`
}

func NewLoggerConfig() (LoggerConfig, error) {
	var config LoggerConfig
	if err := envconfig.Process("LOGGER", &config); err != nil {
		return LoggerConfig{}, fmt.Errorf("process envconfig: %w", err)
	}
	return config, nil
}

func NewConfigMust() LoggerConfig {
	config, err := NewLoggerConfig()
	if err != nil {
		err = fmt.Errorf("get Logger config: %w", err)
		panic(err)
	}
	return config
}
