// wba-fixture is the dev-only Signing Broker and HTTPS verifier.
package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/kevinmartin/sidekicks/internal/lib/webbotauth"
)

type fixture struct {
	config config
	port   string
	signer *webbotauth.Signer
	public ed25519.PublicKey
	mu     sync.Mutex
	recent []observation
	ready  map[string]tabReadiness
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runFixture(ctx, cfg); err != nil {
		log.Fatal(err)
	}
}

func runFixture(ctx context.Context, cfg config) error {
	key, err := webbotauth.GenerateEd25519KeyPair()
	if err != nil {
		return fmt.Errorf("generate fixture signing key: %w", err)
	}
	signer, err := webbotauth.NewSigner(key.PublicKey, key.PrivateKey)
	if err != nil {
		return fmt.Errorf("create fixture signer: %w", err)
	}
	f := &fixture{
		config: cfg,
		port:   strconv.Itoa(int(cfg.Port)),
		signer: signer,
		public: key.PublicKey,
		ready:  make(map[string]tabReadiness),
	}
	// An emptyDir survives a container restart within the same pod.
	if err := os.Remove(cfg.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove old broker socket: %w", err)
	}
	var workers sync.WaitGroup
	defer workers.Wait()
	socket, err := (&net.ListenConfig{}).Listen(ctx, "unix", cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listen on broker socket: %w", err)
	}
	defer socket.Close()
	if err := os.Chmod(cfg.SocketPath, 0660); err != nil {
		return fmt.Errorf("set broker socket permissions: %w", err)
	}
	observer := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", strconv.Itoa(int(cfg.ObserverPort))),
		Handler:           f.observer(),
		ReadHeaderTimeout: 3 * time.Second,
	}
	defer observer.Close()
	origin := &http.Server{
		Addr:              net.JoinHostPort("", f.port),
		Handler:           f.origin(),
		ReadHeaderTimeout: 3 * time.Second,
	}
	defer origin.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	failures := make(chan error, 4)
	failFixture := func(err error) {
		failures <- err
	}
	start := func(name string, report func(error), work func() error) {
		workers.Go(func() {
			defer func() {
				if panicValue := recover(); panicValue != nil {
					report(fmt.Errorf("%s panic: %v\n%s", name, panicValue, debug.Stack()))
				}
			}()
			if err := work(); err != nil && ctx.Err() == nil {
				report(fmt.Errorf("%s: %w", name, err))
			}
		})
	}
	start("readiness cleanup", failFixture, func() error {
		ticker := time.NewTicker(cfg.ReadinessLease)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case now := <-ticker.C:
				f.expireReadiness(now)
			}
		}
	})
	start("broker listener", failFixture, func() error {
		for {
			conn, err := socket.Accept()
			if err != nil {
				return err
			}
			start(
				"broker connection",
				func(err error) {
					log.Print(err)
				},
				func() error {
					return f.serveBroker(ctx, conn)
				},
			)
		}
	})
	start("fixture observer", failFixture, observer.ListenAndServe)
	start("fixture HTTPS origin", failFixture, func() error {
		return origin.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
	})
	select {
	case <-ctx.Done():
		return nil
	case err := <-failures:
		return err
	}
}
