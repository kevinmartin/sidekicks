//go:build !linux && !darwin

package apps

import (
	"errors"
)

var errBusy = errors.New("application launch already in progress")

// ExecStarter represents an unsupported process starter on this platform.
type ExecStarter struct{}

// Start returns ErrUnavailable because process launching is unsupported.
func (ExecStarter) Start(Command) (Process, error) {
	return Process{}, ErrUnavailable
}
func lockFile(string) (func(), error) {
	return nil, ErrUnavailable
}
func processAlive(int) bool {
	return false
}
func processIdentity(int) (string, error) {
	return "", ErrUnavailable
}
