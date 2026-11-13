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
	"time"
)

const (
	brokerSocket   = "/run/sidekicks/wba/broker.sock"
	maxMessageSize = 32 << 10
	brokerTimeout  = time.Second
)

func main() {
	if err := relay(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Sidekicks WBA broker exchange failed: %v\n", err)
		os.Exit(1)
	}
}

func relay(input io.Reader, output io.Writer) error {
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

	deadline := time.Now().Add(brokerTimeout)
	conn, err := net.DialTimeout("unix", brokerSocket, brokerTimeout)
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
