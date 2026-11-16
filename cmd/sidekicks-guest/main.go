package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/kevinmartin/sidekicks/internal/services/browsergateway"
)

type config struct {
	ListenPort  uint16 `env:"BROWSER_GATEWAY_LISTEN_PORT" envDefault:"8080"`
	Socket      string `env:"BROWSER_GATEWAY_SOCKET"`
	Token       string `env:"BROWSER_GATEWAY_TOKEN,required,notEmpty"`
	CDPHost     string `env:"BROWSER_CDP_HOST" envDefault:"127.0.0.1"`
	CDPPort     uint16 `env:"BROWSER_CDP_PORT" envDefault:"9222"`
	BrokerPath  string `env:"WBA_BROKER_SOCKET"`
	FixtureSPKI string `env:"WBA_FIXTURE_SPKI"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	var cfg config
	if err := env.Parse(&cfg); err != nil {
		return fmt.Errorf("load guest configuration: %w", err)
	}
	if cfg.ListenPort == 0 {
		return errors.New("BROWSER_GATEWAY_LISTEN_PORT must be nonzero")
	}
	cdpPort := strconv.Itoa(int(cfg.CDPPort))
	gateway, err := browsergateway.New(cfg.Token, net.JoinHostPort(cfg.CDPHost, cdpPort))
	if err != nil {
		return err
	}
	extensions := "/opt/sidekicks/stagehand"
	if cfg.BrokerPath != "" {
		extensions += ",/opt/sidekicks/extension"
	}
	args := []string{
		"--headless",
		"--disable-setuid-sandbox",
		"--no-first-run",
		"--no-default-browser-check",
		"--window-size=1280,800",
		"--user-data-dir=/home/browser/profile",
		"--enable-unsafe-extension-debugging",
		"--load-extension=" + extensions,
		"--remote-allow-origins=chrome-extension://" + browsergateway.StagehandExtensionID,
		"--remote-debugging-address=" + cfg.CDPHost,
		"--remote-debugging-port=" + cdpPort,
	}
	if cfg.FixtureSPKI != "" {
		args = append(args, "--ignore-certificate-errors-spki-list="+cfg.FixtureSPKI)
	}
	args = append(args, "about:blank")

	lifetime, cancel := context.WithCancel(ctx)
	defer cancel()
	childContext, cancelChild := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelChild()
	chromium := exec.CommandContext(childContext, "/usr/bin/chromium", args...)
	chromium.Stdout = os.Stdout
	chromium.Stderr = os.Stderr
	chromium.Cancel = func() error {
		return chromium.Process.Signal(syscall.SIGTERM)
	}
	chromium.WaitDelay = 5 * time.Second
	if err := chromium.Start(); err != nil {
		return fmt.Errorf("start Chromium: %w", err)
	}
	var chromiumErr error
	chromiumDone := make(chan struct{})
	go func() {
		chromiumErr = chromium.Wait()
		close(chromiumDone)
		cancel()
	}()
	defer func() {
		select {
		case <-chromiumDone:
			return
		default:
		}
		// Browser.close flushes the profile before Chromium exits.
		quit, stopQuit := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer stopQuit()
		gateway.CloseChromium(quit)
		select {
		case <-chromiumDone:
			return
		case <-quit.Done():
			cancelChild()
		}
		<-chromiumDone
	}()

	startup, endStartup := context.WithTimeout(lifetime, 60*time.Second)
	browser, err := gateway.OpenBrowser(startup)
	endStartup()
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		select {
		case <-chromiumDone:
			return errors.Join(err, chromiumErr)
		default:
			return err
		}
	}
	defer func() {
		cleanup, stopCleanup := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stopCleanup()
		if err := browser.Close(cleanup); err != nil {
			log.Printf("close Stagehand: %v", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", gateway.Health)
	mux.HandleFunc("GET /json/version", gateway.Authenticate(gateway.Discovery))
	mux.HandleFunc("GET /devtools/browser/{id}", gateway.Authenticate(gateway.WebSocket))
	mux.HandleFunc("GET /v1/tabs", gateway.Authenticate(browser.Tabs))
	mux.HandleFunc("POST /v1/command", gateway.Authenticate(browser.Command))
	mux.HandleFunc("POST /v1/cdp", gateway.Authenticate(browser.CDP))
	mux.HandleFunc("GET /v1/screenshot", gateway.Authenticate(browser.Screenshot))
	listener, err := listen(cfg)
	if err != nil {
		return fmt.Errorf("listen for guest API: %w", err)
	}
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return lifetime
		},
	}
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve(listener)
	}()
	select {
	case err := <-serverDone:
		return fmt.Errorf("serve guest API: %w", err)
	case <-lifetime.Done():
		shutdown, stopShutdown := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer stopShutdown()
		shutdownErr := server.Shutdown(shutdown)
		listenErr := <-serverDone
		if !errors.Is(listenErr, http.ErrServerClosed) {
			shutdownErr = errors.Join(shutdownErr, listenErr)
		}
		if ctx.Err() != nil {
			return shutdownErr
		}
		<-chromiumDone
		return errors.Join(shutdownErr, errors.New("browser process exited unexpectedly"), chromiumErr)
	}
}

func listen(cfg config) (net.Listener, error) {
	if cfg.Socket == "" {
		return net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(int(cfg.ListenPort))))
	}
	info, err := os.Lstat(cfg.Socket)
	if err == nil {
		if info.Mode().Type() != os.ModeSocket {
			return nil, errors.New("BROWSER_GATEWAY_SOCKET already exists and is not a socket")
		}
		if err := os.Remove(cfg.Socket); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return net.Listen("unix", cfg.Socket)
}
