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
		fmt.Fprintln(os.Stderr, "Sidekicks WBA broker exchange failed")
		os.Exit(1)
	}
}

func relay(input io.Reader, output io.Writer) error {
	var size uint32
	if err := binary.Read(input, binary.NativeEndian, &size); err != nil {
		return err
	}
	if size == 0 || size > maxMessageSize {
		return errors.New("invalid native message size")
	}
	message := make([]byte, size)
	if _, err := io.ReadFull(input, message); err != nil {
		return err
	}
	if !json.Valid(message) {
		return errors.New("invalid native message")
	}

	deadline := time.Now().Add(brokerTimeout)
	conn, err := net.DialTimeout("unix", brokerSocket, brokerTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	if _, err := conn.Write(message); err != nil {
		return err
	}
	if err := conn.(*net.UnixConn).CloseWrite(); err != nil {
		return err
	}
	response, err := io.ReadAll(io.LimitReader(conn, maxMessageSize+1))
	if err != nil {
		return err
	}
	if len(response) > maxMessageSize || !json.Valid(response) {
		return errors.New("invalid broker response")
	}
	if err := binary.Write(output, binary.NativeEndian, uint32(len(response))); err != nil {
		return err
	}
	_, err = output.Write(response)
	return err
}
