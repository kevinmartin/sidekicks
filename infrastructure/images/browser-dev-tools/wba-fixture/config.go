package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type config struct {
	signedHost     string
	unsignedHost   string
	blockedHost    string
	port           string
	observerAddr   string
	directory      string
	socketPath     string
	tlsCert        string
	tlsKey         string
	readinessLease time.Duration
	brokerTimeout  time.Duration
}

func loadConfig() (config, error) {
	cfg := config{
		signedHost:   os.Getenv("WBA_FIXTURE_SIGNED_HOST"),
		unsignedHost: os.Getenv("WBA_FIXTURE_UNSIGNED_HOST"),
		blockedHost:  os.Getenv("WBA_FIXTURE_BLOCKED_HOST"),
		directory:    os.Getenv("WBA_FIXTURE_DIRECTORY"),
		socketPath:   os.Getenv("WBA_BROKER_SOCKET"),
		tlsCert:      os.Getenv("WBA_FIXTURE_TLS_CERT"),
		tlsKey:       os.Getenv("WBA_FIXTURE_TLS_KEY"),
	}
	for _, host := range []string{cfg.signedHost, cfg.unsignedHost, cfg.blockedHost} {
		if host == "" || strings.ContainsAny(host, "/:@?#% \t\r\n") {
			return config{}, errors.New("fixture origins must have valid hostnames")
		}
	}
	if cfg.signedHost == cfg.unsignedHost || cfg.signedHost == cfg.blockedHost || cfg.unsignedHost == cfg.blockedHost {
		return config{}, errors.New("fixture origins must have distinct hostnames")
	}
	directory, err := url.Parse(cfg.directory)
	if err != nil || directory.Scheme != "https" || directory.Hostname() == "" || directory.User != nil {
		return config{}, errors.New("WBA_FIXTURE_DIRECTORY must be an HTTPS URL")
	}
	if !filepath.IsAbs(cfg.socketPath) || !filepath.IsAbs(cfg.tlsCert) || !filepath.IsAbs(cfg.tlsKey) {
		return config{}, errors.New("fixture socket and TLS files must have absolute paths")
	}
	cfg.port, err = readPort("WBA_FIXTURE_PORT")
	if err != nil {
		return config{}, err
	}
	observerPort, err := readPort("WBA_OBSERVER_PORT")
	if err != nil {
		return config{}, err
	}
	cfg.observerAddr = net.JoinHostPort("127.0.0.1", observerPort)
	cfg.readinessLease, err = readDuration("WBA_FIXTURE_READINESS_LEASE")
	if err != nil {
		return config{}, err
	}
	if cfg.readinessLease.Milliseconds() < 3 || cfg.readinessLease.Milliseconds() > 2_147_483_647 {
		return config{}, errors.New("WBA_FIXTURE_READINESS_LEASE is outside the extension timer range")
	}
	cfg.brokerTimeout, err = readDuration("WBA_FIXTURE_BROKER_TIMEOUT")
	if err != nil {
		return config{}, err
	}
	return cfg, nil
}

func readPort(name string) (string, error) {
	value := os.Getenv(name)
	if port, err := strconv.ParseUint(value, 10, 16); err != nil || port == 0 {
		return "", fmt.Errorf("%s must be a valid TCP port", name)
	}
	return value, nil
}

func readDuration(name string) (time.Duration, error) {
	value, err := time.ParseDuration(os.Getenv(name))
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return value, nil
}
