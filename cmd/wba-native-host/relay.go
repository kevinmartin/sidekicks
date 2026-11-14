package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
)

const maxMessageSize = 32 << 10

func relay(ctx context.Context, input io.Reader, output io.Writer, cfg brokerConfig) error {
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

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", cfg.Socket)
	if err != nil {
		return fmt.Errorf("connect to signing broker: %w", err)
	}
	closeConnection := func() {
		_ = conn.Close()
	}
	defer closeConnection()
	stop := context.AfterFunc(ctx, closeConnection)
	defer stop()
	deadline, _ := ctx.Deadline()
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
