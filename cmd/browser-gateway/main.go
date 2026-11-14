package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/kevinmartin/sidekicks/internal/services/browsergateway"
)

type config struct {
	ListenPort uint16 `env:"BROWSER_GATEWAY_LISTEN_PORT,required"`
	Token      string `env:"BROWSER_GATEWAY_TOKEN,required,notEmpty"`
	CDPAddress string `env:"BROWSER_CDP_ADDRESS,required,notEmpty"`
}

func main() {
	var cfg config
	if err := env.Parse(&cfg); err != nil {
		log.Fatalf("load gateway configuration: %v", err)
	}
	if cfg.ListenPort == 0 {
		log.Fatal("BROWSER_GATEWAY_LISTEN_PORT must be nonzero")
	}
	gateway, err := browsergateway.New(cfg.Token, cfg.CDPAddress)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", gateway.Health)
	mux.HandleFunc("GET /json/version", gateway.Authenticate(gateway.Discovery))
	mux.HandleFunc("GET /devtools/browser/{id}", gateway.Authenticate(gateway.WebSocket))
	server := &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(int(cfg.ListenPort))),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownDone := make(chan error, 1)
	go func() {
		defer func() {
			if panicValue := recover(); panicValue != nil {
				err := fmt.Errorf("gateway shutdown panic: %v\n%s", panicValue, debug.Stack())
				_ = server.Close()
				shutdownDone <- err
			}
		}()
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownDone <- server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	if err := <-shutdownDone; err != nil {
		log.Fatal(err)
	}
}
