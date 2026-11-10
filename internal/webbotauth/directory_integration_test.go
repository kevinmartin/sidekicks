//go:build integration
// +build integration

package webbotauth_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kevinmartin/sidekicks/internal/webbotauth"
)

func lookupDirectoryCLI() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %v", err)
	}

	var (
		cargoBinDir	= filepath.Join(homeDir, ".cargo", "bin")
		oldPath		= os.Getenv("PATH")
		newPath		= fmt.Sprintf("%s%c%s", oldPath, os.PathListSeparator, cargoBinDir)
	)
	os.Setenv("PATH", newPath)
	defer os.Setenv("PATH", oldPath)

	binaryPath, err := exec.LookPath("http-signature-directory")
	if err != nil {
		return "", fmt.Errorf("failed to find http-signature-directory binary in PATH: %v", err)
	}

	return binaryPath, nil
}

func newTestDirectoryServer(t *testing.T, keyPairs []webbotauth.Ed25519KeyPair) (string, func()) {
	mux := http.NewServeMux()

	server := &http.Server{
		Handler: mux,
	}

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		defer listener.Close()

		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Fatalf("Failed to serve: %v", err)
		}

		t.Logf("Test directory server stopped")
	}()

	ctx, cancel := context.WithCancel(context.Background())

	t.Logf("Starting SSH tunnel to localhost.run on port %d", port)
	t.Log("Using SSH command: ", "ssh", "-o", "StrictHostKeyChecking=no", "-R", "80:localhost:" + strconv.Itoa(port), "nokey@localhost.run")

	cmd := exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-R", "80:localhost:" + strconv.Itoa(port), "nokey@localhost.run")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to get stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start command: %v", err)
	}

	urlChan := make(chan string, 1)
	go func() {
		var (
			scanner	= bufio.NewScanner(stdout)
			re	= regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.(localhost\.run|lhr\.life)`)
		)
		for scanner.Scan() {
			line := scanner.Text()
			if match := re.FindString(line); match != "" {
				urlChan <- match
				return
			}
		}
	}()

	var tunnelURL string
	select {
	case tunnelURL = <-urlChan:
		t.Logf("Tunnel established at: %s", tunnelURL)
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("Timeout: Failed to establish tunnel within 10 seconds")
	}

	handler, err := webbotauth.SignedDirectoryHandler(tunnelURL, "ai", keyPairs)
	if err != nil {
		t.Fatalf("Failed to create signed directory handler: %v", err)
	}

	mux.HandleFunc("/.well-known/http-message-signatures-directory", handler)

	return tunnelURL, func() {
		cancel()
		if err := cmd.Wait(); err != nil {
			t.Logf("Command finished with error: %v", err)
		}
		if err := server.Shutdown(ctx); err != nil {
			t.Fatalf("Failed to shutdown server: %v", err)
		}
	}
}

type DirectoryValidationResponse struct {
	Success bool `json:"success"`
	Message string `json:"message"`
	Details struct {
		URL string `json:"url"`
		KeysCount int `json:"keys_count"`
		ValidatedKeys []struct {
			Thumbprint string `json:"thumbprint"`
			Valid bool `json:"valid"`
			SignatureVerified bool `json:"signature_verified"`
			RawKeyData *struct {
				Kty string `json:"kty"`
				Crv string `json:"crv"`
				X string `json:"x"`
			} `json:"raw_key_data"`
			Error *string `json:"error"`
		} `json:"validated_keys"`
		Errors []string `json:"errors"`
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
		name string
		keyPairs []webbotauth.Ed25519KeyPair
	}{
		{
			name: "Single Key Pair",
			keyPairs: []webbotauth.Ed25519KeyPair{kp1},
		},
		{
			name: "Multiple Key Pairs",
			keyPairs: []webbotauth.Ed25519KeyPair{kp1, kp2},
		},
	}
	for _, tt := range table {
		t.Run(tt.name, func(t *testing.T) {
			tunnelURL, cleanup := newTestDirectoryServer(t, tt.keyPairs)
			defer cleanup()

			cmd := exec.Command(binaryPath, tunnelURL + "/.well-known/http-message-signatures-directory")

			t.Logf("Executing command: %s", strings.Join(cmd.Args, " "))

			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Failed to execute command: %v, output: %s", err, string(output))
			}

			var validationResp DirectoryValidationResponse
			if err := json.Unmarshal(output, &validationResp); err != nil {
				t.Fatalf("Failed to unmarshal command output: %v, output: %s", err, string(output))
			}

			if !validationResp.Success {
				t.Fatalf("Directory validation failed: %s, details: %+v", validationResp.Message, validationResp.Details)
			}
		})
	}
}
