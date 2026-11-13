package main

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

type config struct {
	Token          string        `env:"BROWSER_GATEWAY_TOKEN,required,notEmpty"`
	SignedHost     string        `env:"WBA_FIXTURE_SIGNED_HOST,required"`
	UnsignedHost   string        `env:"WBA_FIXTURE_UNSIGNED_HOST,required"`
	BlockedHost    string        `env:"WBA_FIXTURE_BLOCKED_HOST,required"`
	Port           uint16        `env:"WBA_FIXTURE_PORT,required"`
	ObserverPort   uint16        `env:"WBA_OBSERVER_PORT,required"`
	Directory      string        `env:"WBA_FIXTURE_DIRECTORY,required"`
	SocketPath     string        `env:"WBA_BROKER_SOCKET,required"`
	TLSCert        string        `env:"WBA_FIXTURE_TLS_CERT,required"`
	TLSKey         string        `env:"WBA_FIXTURE_TLS_KEY,required"`
	ReadinessLease time.Duration `env:"WBA_FIXTURE_READINESS_LEASE,required"`
	BrokerTimeout  time.Duration `env:"WBA_FIXTURE_BROKER_TIMEOUT,required"`
}

func loadConfig() (config, error) {
	var cfg config
	if err := env.Parse(&cfg); err != nil {
		return config{}, fmt.Errorf("load fixture configuration: %w", err)
	}
	for _, host := range []string{cfg.SignedHost, cfg.UnsignedHost, cfg.BlockedHost} {
		if host == "" || strings.ContainsAny(host, "/:@?#% \t\r\n") {
			return config{}, errors.New("fixture origins must have valid hostnames")
		}
	}
	if cfg.SignedHost == cfg.UnsignedHost || cfg.SignedHost == cfg.BlockedHost || cfg.UnsignedHost == cfg.BlockedHost {
		return config{}, errors.New("fixture origins must have distinct hostnames")
	}
	directory, err := url.Parse(cfg.Directory)
	if err != nil || directory.Scheme != "https" || directory.Hostname() == "" || directory.User != nil {
		return config{}, errors.New("WBA_FIXTURE_DIRECTORY must be an HTTPS URL")
	}
	if !filepath.IsAbs(cfg.SocketPath) || !filepath.IsAbs(cfg.TLSCert) || !filepath.IsAbs(cfg.TLSKey) {
		return config{}, errors.New("fixture socket and TLS files must have absolute paths")
	}
	if cfg.Port == 0 || cfg.ObserverPort == 0 || cfg.Port == cfg.ObserverPort {
		return config{}, errors.New("WBA_FIXTURE_PORT and WBA_OBSERVER_PORT must be nonzero and distinct")
	}
	if lease := cfg.ReadinessLease.Milliseconds(); lease < 3 || lease > 2_147_483_647 {
		return config{}, errors.New("WBA_FIXTURE_READINESS_LEASE is outside the extension timer range")
	}
	if cfg.BrokerTimeout <= 0 {
		return config{}, errors.New("WBA_FIXTURE_BROKER_TIMEOUT must be positive")
	}
	return cfg, nil
}
