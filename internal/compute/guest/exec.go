package guest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ExecRequest describes an executable command and its environment.
type ExecRequest struct {
	Argv      []string          `json:"argv"`
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	TimeoutMS int               `json:"timeoutMs,omitempty"`
}

// ExecResult contains bounded command output and exit information.
type ExecResult struct {
	ExitCode        int    `json:"exitCode"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutTruncated bool   `json:"stdoutTruncated"`
	StderrTruncated bool   `json:"stderrTruncated"`
	TimedOut        bool   `json:"timedOut"`
}

type boundedOutput struct {
	mu        sync.Mutex
	data      bytes.Buffer
	truncated bool
}

// Write records bounded command output and reports the consumed length.
func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	room := MaxOutputBytes - b.data.Len()
	if len(p) > room {
		p = p[:room]
		b.truncated = true
	}
	_, _ = b.data.Write(p)
	return n, nil
}
func (b *boundedOutput) result() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.ToValidUTF8(b.data.String(), "\uFFFD"), b.truncated
}

func validateExec(req ExecRequest) error {
	if len(req.Argv) == 0 || len(req.Argv) > 256 || req.Argv[0] == "" {
		return errors.New("argv must contain 1..256 arguments")
	}
	size := 0
	for _, arg := range req.Argv {
		if strings.ContainsRune(arg, 0) {
			return errors.New("argv contains null byte")
		}
		size += len(arg)
	}
	if size > 64<<10 {
		return errors.New("argv exceeds 64 kibibytes")
	}
	if req.TimeoutMS < 0 || req.TimeoutMS > 60000 || (req.TimeoutMS > 0 && req.TimeoutMS < 100) {
		return errors.New("timeout in milliseconds must be 100..60000")
	}
	if len(req.Env) > 8 {
		return errors.New("too many environment variables")
	}
	for k, v := range req.Env {
		switch k {
		case "LANG", "LC_ALL", "TZ", "TERM", "COLORTERM":
		default:
			return fmt.Errorf("environment variable %q is not allowed", k)
		}
		if len(v) > 256 || strings.ContainsAny(v, "\x00\r\n") {
			return errors.New("invalid environment value")
		}
	}
	return nil
}

func resolveExecutable(name string) (string, error) {
	if filepath.IsAbs(name) {
		return name, nil
	}
	if strings.ContainsRune(name, '/') {
		return "", errors.New("executable must be absolute or a simple name")
	}
	for _, dir := range []string{"/usr/local/bin", "/usr/bin", "/bin"} {
		p := filepath.Join(dir, name)
		st, err := os.Stat(p)
		if err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0111 != 0 {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	var req ExecRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := validateExec(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dir, err := s.openDirectory(req.Cwd)
	if err != nil {
		writeError(w, http.StatusBadRequest, "cwd must be an existing workspace directory without symlinks")
		return
	}
	defer dir.Close()
	executable, err := resolveExecutable(req.Argv[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "executable not found in fixed guest search path")
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() {
			<-s.slots
		}()
	default:
		writeError(w, http.StatusTooManyRequests, "exec concurrency limit reached")
		return
	}
	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	var result ExecResult
	var executionErr, callbackErr error
	var used, active atomic.Bool
	active.Store(true)
	callbackDone := make(chan struct{})
	gateCtx, stopGate := context.WithCancel(ctx)
	defer stopGate()
	run := func(ownedCtx context.Context) error {
		if !used.CompareAndSwap(false, true) {
			return errors.New("exec callback was already used")
		}
		defer close(callbackDone)
		if !active.Load() || ownedCtx == nil {
			callbackErr = errors.New("exec callback is no longer active")
			return callbackErr
		}
		executionCtx, stopExecution := context.WithCancel(ownedCtx)
		stopOuter := context.AfterFunc(gateCtx, stopExecution)
		defer stopExecution()
		defer stopOuter()
		if gateCtx.Err() != nil {
			stopExecution()
		}
		result, executionErr = executeCommand(executionCtx, executable, req, dir)
		result.TimedOut = result.TimedOut || errors.Is(ctx.Err(), context.DeadlineExceeded)
		if executionErr != nil {
			callbackErr = executionErr
			return callbackErr
		}
		if executionCtx.Err() != nil && ctx.Err() == nil {
			callbackErr = executionCtx.Err()
			return callbackErr
		}
		return nil
	}
	var gateErr error
	if s.execGate == nil {
		gateErr = run(gateCtx)
	} else {
		gateErr = s.execGate(gateCtx, run)
	}
	active.Store(false)
	// A gate may not retain a live command after it returns. The ordinary gate
	// waits synchronously, but this also bounds an incorrectly asynchronous gate.
	stopGate()
	called := used.Load()
	if called {
		<-callbackDone
	}
	if r.Context().Err() != nil {
		return
	}
	if !called {
		writeError(w, http.StatusConflict, "guest command ownership is unavailable or was revoked")
		return
	}
	if executionErr != nil {
		writeError(w, http.StatusUnprocessableEntity, "guest command could not start")
		return
	}
	if gateErr != nil || callbackErr != nil {
		writeError(w, http.StatusConflict, "guest command ownership is unavailable or was revoked")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func executeCommand(ctx context.Context, executable string, req ExecRequest, dir *os.File) (ExecResult, error) {
	cmd := exec.CommandContext(ctx, executable, req.Argv[1:]...)
	configureProcess(cmd, dir)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/workspace", "USER=sidekicks", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TERM=xterm-256color"}
	keys := make([]string, 0, len(req.Env))
	for k := range req.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cmd.Env = append(cmd.Env, k+"="+req.Env[k])
	}
	var stdout, stderr boundedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// A descendant holding a pipe open may not keep the HTTP request alive after
	// its immediate parent exits. Kill the owned process group on cancellation.
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Cancel = func() error {
		return killProcessGroup(cmd)
	}
	err := cmd.Run()
	// Clean up descendants even when the requested command exits successfully.
	// A command is one bounded operation, not a detached service launcher.
	_ = killProcessGroup(cmd)
	result := ExecResult{ExitCode: 0, TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded)}
	result.Stdout, result.StdoutTruncated = stdout.result()
	result.Stderr, result.StderrTruncated = stderr.result()
	if err != nil {
		var exit *exec.ExitError
		switch {
		case errors.As(err, &exit):
			result.ExitCode = exit.ExitCode()
		case errors.Is(err, exec.ErrWaitDelay):
			result.ExitCode = -1
		case ctx.Err() != nil:
			result.ExitCode = -1
		default:
			return result, err
		}
	}
	return result, nil
}
