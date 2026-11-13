// wba-native-host relays one Chrome native message to the launcher-provided
// Signing Broker socket. Chrome starts a fresh helper for each message.
package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

const maxMessageSize = 32 << 10

type brokerConfig struct {
	socket  string
	timeout time.Duration
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Sidekicks WBA broker exchange failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	socket := os.Getenv("WBA_BROKER_SOCKET")
	if !filepath.IsAbs(socket) {
		return errors.New("WBA_BROKER_SOCKET must be an absolute path")
	}
	timeout, err := time.ParseDuration(os.Getenv("WBA_BROKER_TIMEOUT"))
	if err != nil {
		return fmt.Errorf("parse WBA_BROKER_TIMEOUT: %w", err)
	}
	if timeout <= 0 {
		return errors.New("WBA_BROKER_TIMEOUT must be positive")
	}
	return relay(os.Stdin, os.Stdout, brokerConfig{socket: socket, timeout: timeout})
}

func relay(input io.Reader, output io.Writer, config brokerConfig) error {
	var size uint32
	if err := binary.Read(input, binary.NativeEndian, &size); err != nil {
		return fmt.Errorf("read native message size: %w", err)
	}
	if size == 0 || size > maxMessageSize {
		return errors.New("invalid native message size")
	}
	message := make([]byte, size)
	if _, err := io.ReadFull(input, message); err != nil {
		return fmt.Errorf("read native message: %w", err)
	}
	if !json.Valid(message) {
		return errors.New("invalid native message")
	}

	deadline := time.Now().Add(config.timeout)
	conn, err := net.DialTimeout("unix", config.socket, config.timeout)
	if err != nil {
		return fmt.Errorf("connect to signing broker: %w", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set broker exchange deadline: %w", err)
	}
	if _, err := conn.Write(message); err != nil {
		return fmt.Errorf("send broker request: %w", err)
	}
	if err := conn.(*net.UnixConn).CloseWrite(); err != nil {
		return fmt.Errorf("close broker request stream: %w", err)
	}
	response, err := io.ReadAll(io.LimitReader(conn, maxMessageSize+1))
	if err != nil {
		return fmt.Errorf("read broker response: %w", err)
	}
	if len(response) > maxMessageSize || !json.Valid(response) {
		return errors.New("invalid broker response")
	}
	if err := binary.Write(output, binary.NativeEndian, uint32(len(response))); err != nil {
		return fmt.Errorf("write native response size: %w", err)
	}
	if _, err := output.Write(response); err != nil {
		return fmt.Errorf("write native response: %w", err)
	}
	return nil
}
