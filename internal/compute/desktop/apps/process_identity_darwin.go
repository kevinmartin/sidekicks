//go:build darwin

package apps

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

// The actual desktop guest runs on Linux. This fallback supports local tests.
func processIdentity(pid int) (string, error) {
	if pid < 1 {
		return "", errors.New("invalid process id")
	}
	output, err := exec.Command("/bin/ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	identity := strings.TrimSpace(string(output))
	if identity == "" {
		return "", errors.New("missing process start time")
	}
	return identity, nil
}
