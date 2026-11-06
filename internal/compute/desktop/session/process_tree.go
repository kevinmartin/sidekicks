package session

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type processState struct {
	pid, ppid, uid int
	name, state    string
}

// Components returns the desktop session components.
func (systemRunner) Components(ctx context.Context, root int, names []string) error {
	return observeComponents(ctx, "/proc", root, os.Getuid(), names)
}
func observeComponents(ctx context.Context, procRoot string, root, uid int, names []string) error {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return errors.New("linux process observation is unavailable")
	}
	states := make(map[int]processState)
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid < 1 {
			continue
		}
		f, err := os.Open(filepath.Join(procRoot, entry.Name(), "status"))
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(f, 16<<10))
		_ = f.Close()
		if err != nil {
			continue
		}
		state, err := parseProcessState(pid, string(raw))
		if err == nil {
			states[pid] = state
		}
	}
	return checkComponents(states, root, uid, names)
}
func parseProcessState(pid int, raw string) (processState, error) {
	result := processState{pid: pid, ppid: -1, uid: -1}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), ":", 2)
		if len(fields) != 2 {
			continue
		}
		values := strings.Fields(fields[1])
		if len(values) == 0 {
			continue
		}
		switch fields[0] {
		case "Name":
			result.name = strings.TrimSpace(fields[1])
		case "State":
			result.state = values[0]
		case "PPid":
			result.ppid, _ = strconv.Atoi(values[0])
		case "Uid":
			result.uid, _ = strconv.Atoi(values[0])
		}
	}
	if result.name == "" || result.state == "" || result.ppid < 0 || result.uid < 0 {
		return result, errors.New("incomplete process status")
	}
	return result, nil
}
func processLive(state string) bool {
	return state != "Z" && state != "T" && state != "t" && state != "X" && state != "x"
}
func checkComponents(states map[int]processState, root, uid int, names []string) error {
	parent, ok := states[root]
	if !ok || parent.name != "xfce4-session" || parent.uid != uid || !processLive(parent.state) {
		return errors.New("owned xfce session is not live")
	}
	for _, name := range names {
		count := 0
		for pid, state := range states {
			if state.name != name || state.uid != uid || !processLive(state.state) {
				continue
			}
			seen := map[int]bool{}
			for pid > 1 && !seen[pid] {
				seen[pid] = true
				if pid == root {
					count++
					break
				}
				next, ok := states[pid]
				if !ok {
					break
				}
				pid = next.ppid
			}
		}
		if count != 1 {
			return fmt.Errorf("expected one live owned %s, found %d", name, count)
		}
	}
	return nil
}
