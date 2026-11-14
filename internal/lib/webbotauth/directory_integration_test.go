//go:build integration
// +build integration

package webbotauth_test

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kevinmartin/sidekicks/internal/lib/webbotauth"
)

const (
	directoryPath   = "/.well-known/http-message-signatures-directory"
	verifierTimeout = 30 * time.Second
)

var tunnelURLPattern = regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.(localhost\.run|lhr\.life)`)

func lookupDirectoryCLI() (string, error) {
	const name = "http-signature-directory"
	path, err := exec.LookPath(name)
	if err == nil {
		return path, nil
	}
	if !errors.Is(err, exec.ErrNotFound) {
		return "", fmt.Errorf("look up %s: %w", name, err)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate Cargo bin directory: %w", err)
	}
	path, err = exec.LookPath(filepath.Join(homeDir, ".cargo", "bin", name))
	if err != nil {
		return "", fmt.Errorf("look up %s on PATH or in the Cargo bin directory: %w", name, err)
	}
	return path, nil
}

func newTestDirectoryServer(ctx context.Context, t *testing.T, keyPairs []webbotauth.Ed25519KeyPair) string {
	t.Helper()

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for directory requests: %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close directory listener: %v", err)
		}
	})

	tunnelURL := startDirectoryTunnel(ctx, t, listener.Addr().String())
	handler, err := webbotauth.SignedDirectoryHandler(tunnelURL, "ai", keyPairs)
	if err != nil {
		t.Fatalf("create signed directory handler: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc(directoryPath, handler)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveDone := make(chan error, 1)
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			t.Errorf("shut down directory server: %v", err)
			if err := server.Close(); err != nil {
				t.Errorf("close directory server: %v", err)
			}
		}
		if err := <-serveDone; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("serve signed directory: %v", err)
		}
	})
	go func() {
		serveDone <- server.Serve(listener)
	}()
	return tunnelURL
}

func startDirectoryTunnel(ctx context.Context, t *testing.T, address string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)

	cmd := exec.CommandContext(
		ctx,
		"ssh",
		"-oBatchMode=yes",
		"-oExitOnForwardFailure=yes",
		"-oStrictHostKeyChecking=no",
		"-R",
		"80:"+address,
		"nokey@localhost.run",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("open SSH output pipe: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start directory tunnel: %v", err)
	}

	type startupResult struct {
		url string
		err error
	}
	startup := make(chan startupResult, 1)
	readDone := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		_ = stdout.Close()
		<-readDone
		if err := cmd.Wait(); err != nil {
			t.Logf("SSH tunnel stopped: %v", err)
		}
	})
	go func() {
		defer close(readDone)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if url := tunnelURLPattern.FindString(scanner.Text()); url != "" {
				startup <- startupResult{
					url: url,
				}
				// Keep draining the pipe until the SSH process exits.
				_, _ = io.Copy(io.Discard, stdout)
				return
			}
		}
		err := scanner.Err()
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		startup <- startupResult{
			err: err,
		}
	}()

	startupCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	select {
	case result := <-startup:
		if result.err != nil {
			t.Fatalf("read directory tunnel URL: %v", result.err)
		}
		t.Logf("directory tunnel established at %s", result.url)
		return result.url
	case <-startupCtx.Done():
		t.Fatalf("establish directory tunnel: %v", startupCtx.Err())
		return ""
	}
}

type directoryValidationResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Details struct {
		URL           string `json:"url"`
		KeysCount     int    `json:"keys_count"`
		ValidatedKeys []struct {
			Thumbprint        string `json:"thumbprint"`
			Valid             bool   `json:"valid"`
			SignatureVerified bool   `json:"signature_verified"`
			RawKeyData        *struct {
				Kty string `json:"kty"`
				Crv string `json:"crv"`
				X   string `json:"x"`
			} `json:"raw_key_data"`
			Error *string `json:"error"`
		} `json:"validated_keys"`
		Errors   []string `json:"errors"`
		Warnings []string `json:"warnings"`
	} `json:"details"`
}

func TestSignerWithDirectoryCLI(t *testing.T) {
	kp1, err := webbotauth.GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Ed25519 key pair: %v", err)
	}

	kp2, err := webbotauth.GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("Failed to generate Ed25519 key pair: %v", err)
	}

	binaryPath, err := lookupDirectoryCLI()
	if err != nil {
		t.Fatalf("Failed to locate http-signature-directory binary: %v", err)
	}

	table := []struct {
		name     string
		keyPairs []webbotauth.Ed25519KeyPair
	}{
		{
			name:     "Single Key Pair",
			keyPairs: []webbotauth.Ed25519KeyPair{kp1},
		},
		{
			name:     "Multiple Key Pairs",
			keyPairs: []webbotauth.Ed25519KeyPair{kp1, kp2},
		},
	}
	for _, tt := range table {
		t.Run(tt.name, func(t *testing.T) {
			tunnelURL := newTestDirectoryServer(t.Context(), t, tt.keyPairs)
			ctx, cancel := context.WithTimeout(t.Context(), verifierTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, binaryPath, tunnelURL+directoryPath)
			cmd.WaitDelay = 5 * time.Second

			t.Logf("Executing command: %s", strings.Join(cmd.Args, " "))

			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("wait for directory verifier: %v", ctx.Err())
			}
			if err != nil {
				t.Fatalf("run directory verifier: %v\n%s", err, output)
			}

			var validationResp directoryValidationResponse
			if err := json.Unmarshal(output, &validationResp); err != nil {
				t.Fatalf("Failed to unmarshal command output: %v, output: %s", err, string(output))
			}

			if !validationResp.Success {
				t.Fatalf("Directory validation failed: %s, details: %+v", validationResp.Message, validationResp.Details)
			}
		})
	}
}
