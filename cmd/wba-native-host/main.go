// wba-native-host relays one Chrome native message to the launcher-provided
// Signing Broker socket. Chrome starts a fresh helper for each message.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caarlos0/env/v11"
)

type brokerConfig struct {
	Socket  string        `env:"WBA_BROKER_SOCKET,required"`
	Timeout time.Duration `env:"WBA_BROKER_TIMEOUT,required"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Sidekicks WBA broker exchange failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var cfg brokerConfig
	if err := env.Parse(&cfg); err != nil {
		return fmt.Errorf("load native host configuration: %w", err)
	}
	if !filepath.IsAbs(cfg.Socket) {
		return errors.New("WBA_BROKER_SOCKET must be an absolute path")
	}
	if cfg.Timeout <= 0 {
		return errors.New("WBA_BROKER_TIMEOUT must be positive")
	}
	return relay(context.Background(), os.Stdin, os.Stdout, cfg)
}
