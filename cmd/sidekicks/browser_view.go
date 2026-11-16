package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/coder/websocket"
	"github.com/kevinmartin/sidekicks/internal/services/browserviewer"
	"github.com/spf13/cobra"
)

func (o *browserOptions) viewCommand() *cobra.Command {
	var tab string
	var noOpen bool
	command := &cobra.Command{
		Use:   "view",
		Short: "View and interact with a browser tab",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := o.client(cmd)
			if err != nil {
				return err
			}
			tabs, err := client.Tabs(cmd.Context())
			if err != nil {
				return err
			}
			selected := tab
			if selected == "" {
				selected = tabs.Primary
			}
			for _, candidate := range tabs.Tabs {
				if candidate.ID == selected {
					return serveBrowserViewer(cmd, noOpen, func(ctx context.Context) (*websocket.Conn, error) {
						return client.OpenViewer(ctx, selected)
					})
				}
			}
			return fmt.Errorf("tab unavailable: %q; use browser tabs to select a live tab", selected)
		},
	}
	command.Flags().StringVar(&tab, "tab", "", "Target tab ID (default is the primary tab)")
	command.Flags().BoolVar(&noOpen, "no-open", false, "Print the viewer URL without opening a browser")
	return command
}

func serveBrowserViewer(cmd *cobra.Command, noOpen bool, dial func(context.Context) (*websocket.Conn, error)) error {
	var config net.ListenConfig
	listener, err := config.Listen(cmd.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for browser viewer: %w", err)
	}
	defer func() {
		_ = listener.Close()
	}()
	viewer, err := browserviewer.New(listener.Addr().String(), dial)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stream", viewer.Stream)
	mux.HandleFunc("GET /", viewer.Assets)
	lifetime, stop := context.WithCancel(cmd.Context())
	defer stop()
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return lifetime
		},
	}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(listener)
	}()
	defer func() {
		_ = server.Close()
	}()
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), viewer.URL()); err != nil {
		return err
	}
	if !noOpen {
		opener := "xdg-open"
		if runtime.GOOS == "darwin" {
			opener = "open"
		}
		result := exec.CommandContext(lifetime, opener, viewer.URL())
		if err := result.Start(); err != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Open the viewer URL in your browser.")
		} else {
			go func() {
				if err := result.Wait(); err != nil && lifetime.Err() == nil {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Open the viewer URL in your browser.")
				}
			}()
		}
	}
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve browser viewer: %w", err)
		}
	case <-lifetime.Done():
	}
	return nil
}
