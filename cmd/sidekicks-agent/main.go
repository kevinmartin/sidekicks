// sidekicks-agent runs inside a disposable Linux workload as an unprivileged user.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute/guest"
)

func main() {
	if err := runMain(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func runMain() error {
	if len(os.Args) > 1 && os.Args[1] == "launch" {
		return launchRequest(os.Args[2:])
	}
	if len(os.Args) > 1 {
		return runRequest(os.Args[1:], os.Getenv("SIDEKICKS_AGENT_TOKEN"), nil, os.Stdout)
	}
	if runtime.GOOS != "linux" {
		return errors.New("guest server mode requires linux; darwin is not an isolation runtime")
	}
	if os.Geteuid() == 0 {
		return errors.New("sidekicks-agent must run as an unprivileged guest user")
	}
	workspace := os.Getenv("SIDEKICKS_WORKSPACE")
	if workspace == "" {
		workspace = "/workspace"
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	extra := map[string]http.Handler{}
	var cleanup func()
	var health func() error
	var desktopInstance *desktopWorkload
	switch adapter := os.Getenv("SIDEKICKS_ADAPTER"); adapter {
	case "", "shell":
	case "browser-chromium":
		h, closeBrowser, browserHealth, err := prepareBrowser(ctx, os.Getenv("SIDEKICKS_ALLOW_UNSANDBOXED_BROWSER") == "1")
		if err != nil {
			return err
		}
		cleanup = closeBrowser
		health = browserHealth
		extra["/browser"] = h
	case "desktop-novnc", "desktop-neko":
		d, err := prepareDesktop(ctx, adapter, os.Getenv("SIDEKICKS_AGENT_TOKEN"))
		if err != nil {
			return err
		}
		desktopInstance = d
		cleanup = d.close
		for _, path := range []string{"/desktop/connect", "/desktop/state", "/desktop/launch", "/desktop/control", "/desktop/input", "/desktop/capture"} {
			extra[path] = http.HandlerFunc(d.private)
		}
	default:
		return errors.New("unsupported guest workload configuration")
	}
	if cleanup != nil {
		defer cleanup()
	}
	cfg := guest.Config{Workspace: workspace, Token: os.Getenv("SIDEKICKS_AGENT_TOKEN"), ExtraRoutes: extra, Health: health}
	if desktopInstance != nil {
		cfg.Health = desktopInstance.ready
		cfg.ExecGate = desktopInstance.execGate
	}
	handler, err := guest.New(cfg)
	if err != nil {
		return fmt.Errorf("invalid agent configuration: %w", err)
	}
	defer handler.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:8080")
	if err != nil {
		return errors.New("agent listener failed")
	}
	defer listener.Close()
	log.Print("sidekicks guest agent ready")
	return serve(ctx, listener, handler)
}

func runRequest(args []string, token string, client *http.Client, out io.Writer) error {
	if len(args) != 2 || args[0] != "request" {
		return errors.New("usage: sidekicks-agent [request base64json]")
	}
	request, err := guest.DecodeLocalRequest(args[1])
	if err != nil {
		return err
	}
	response, err := guest.CallLocal(context.Background(), client, token, request)
	if err != nil {
		return err
	}
	if err = json.NewEncoder(out).Encode(response); err != nil {
		return errors.New("could not write local response")
	}
	return nil
}

func serve(ctx context.Context, listener net.Listener, handler http.Handler) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 65 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	server.BaseContext = func(net.Listener) context.Context {
		return ctx
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if server.Shutdown(shutdown) != nil {
			_ = server.Close()
		}
	}()
	err := server.Serve(listener)
	cancel()
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
