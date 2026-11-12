package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kevinmartin/sidekicks/internal/browsergateway"
)

func main() {
	gateway, err := browsergateway.New(os.Getenv("BROWSER_GATEWAY_TOKEN"))
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", gateway.Health)
	mux.HandleFunc("GET /json/version", gateway.Authenticate(gateway.Discovery))
	mux.HandleFunc("GET /devtools/browser/{id}", gateway.Authenticate(gateway.WebSocket))
	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownDone := make(chan error, 1)
	go func() {
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
