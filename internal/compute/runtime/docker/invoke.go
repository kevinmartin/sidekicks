package docker

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute"
)

const (
	// MaxRequestBytes limits an encoded guest request.
	MaxRequestBytes = 32 << 10
	// MaxInvokeBytes limits a multiplexed Docker agent response.
	MaxInvokeBytes = 3 << 20
	invokeTimeout  = 70 * time.Second
)

// CancellationError preserves the original transport/caller error and states
// whether the authenticated guest cancellation was acknowledged. Confirmation
// prevents future execution for this request ID and waits for normal request
// cleanup; it does not undo side effects or kill deliberately detached sessions.
type CancellationError struct {
	Cause     error
	Confirmed bool
	WasActive bool
	CancelErr error
}

// Error describes a guest request failure and its cancellation status.
func (e *CancellationError) Error() string {
	status := "unconfirmed"
	if e.Confirmed {
		status = "confirmed"
	}
	result := fmt.Sprintf("guest request failed: %v; cancellation %s", e.Cause, status)
	if e.CancelErr != nil {
		result += ": " + e.CancelErr.Error()
	}
	return result
}

// Unwrap returns the underlying guest request error.
func (e *CancellationError) Unwrap() error {
	return e.Cause
}

// Invoke reaches the guest agent even with NetworkNone. Only the fixed agent
// request command is executed. Credentials stay in the helper environment.
// Each request receives a fresh opaque identity and absolute deadline. If its
// transport fails after start is attempted, a separate bounded authenticated
// helper cancels only that request, including the cancel-before-start race.
func (r *Runtime) Invoke(ctx context.Context, id string, request []byte) ([]byte, error) {
	return r.invoke(ctx, id, request, nil)
}

// InvokeWithInstance builds and sends one request using a single observed
// running container. The result is returned only if the name, immutable ID,
// start generation and endpoint mappings still match after the request. This
// cannot prevent a later restart after return; callers must handle stale URLs.
func (r *Runtime) InvokeWithInstance(ctx context.Context, id string, build func(compute.Instance) ([]byte, error)) (compute.Instance, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, invokeTimeout)
	defer cancel()
	if build == nil {
		return compute.Instance{}, nil, fmt.Errorf("%w: request builder is required", compute.ErrInvalid)
	}
	initial, err := r.inspectOwned(ctx, id)
	if err != nil {
		return compute.Instance{}, nil, err
	}
	started, err := time.Parse(time.RFC3339Nano, initial.State.StartedAt)
	if initial.State.Status != "running" || err != nil || started.IsZero() {
		return compute.Instance{}, nil, fmt.Errorf("%w: running container generation unavailable", compute.ErrConflict)
	}
	observed, err := instance(initial, id)
	if err != nil {
		return compute.Instance{}, nil, err
	}
	request, err := build(observed)
	if err != nil {
		return compute.Instance{}, nil, err
	}
	verify := func() error {
		current, err := r.inspectOwned(ctx, id)
		if err != nil {
			return fmt.Errorf("%w: cannot verify container generation: %w", compute.ErrConflict, err)
		}
		latest, err := instance(current, id)
		if err != nil || current.ID != initial.ID || current.State.Status != "running" || current.State.StartedAt != initial.State.StartedAt || !reflect.DeepEqual(observed, latest) {
			return fmt.Errorf("%w: container generation or endpoints changed during request", compute.ErrConflict)
		}
		return nil
	}
	if err = verify(); err != nil {
		return compute.Instance{}, nil, err
	}
	output, err := r.invoke(ctx, id, request, &initial)
	if err != nil {
		return compute.Instance{}, nil, err
	}
	if err = verify(); err != nil {
		return compute.Instance{}, nil, err
	}
	return observed, output, nil
}

// invoke uses a previously owned identity when pinned is supplied, never
// resolving a reusable name a second time before executing a guest request.
func (r *Runtime) invoke(ctx context.Context, id string, request []byte, pinned *engineContainer) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, invokeTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(request) == 0 || len(request) > MaxRequestBytes {
		return nil, fmt.Errorf("%w: agent request must be a json object of at most 32 kibibytes", compute.ErrInvalid)
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(request, &envelope) != nil || envelope == nil {
		return nil, fmt.Errorf("%w: agent request must be a json object", compute.ErrInvalid)
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, errors.New("guest request identity unavailable")
	}
	requestID := hex.EncodeToString(nonce[:])
	deadline, _ := ctx.Deadline()
	envelope["requestId"], _ = json.Marshal(requestID)
	envelope["deadlineUnixMs"], _ = json.Marshal(deadline.UnixMilli())
	request, err := json.Marshal(envelope)
	if err != nil || len(request) > MaxRequestBytes {
		return nil, fmt.Errorf("%w: identified agent request exceeds 32 kibibytes", compute.ErrInvalid)
	}
	var c engineContainer
	if pinned == nil {
		c, err = r.inspectOwned(ctx, id)
		if err != nil {
			return nil, err
		}
	} else {
		c = *pinned
		if err = validateOwnership(c, id); err != nil {
			return nil, err
		}
	}
	if c.State.Status != "running" {
		return nil, fmt.Errorf("%w: guest is not running", compute.ErrConflict)
	}
	output, started, err := r.runAgentHelper(ctx, c.ID, request)
	if err == nil {
		return output, nil
	}
	if !started {
		return nil, err
	}
	confirmed, active, cancelErr := r.cancelRequest(ctx, id, c.ID, requestID)
	return nil, &CancellationError{Cause: err, Confirmed: confirmed, WasActive: active, CancelErr: cancelErr}
}

// runAgentHelper executes one fixed unprivileged agent command. The returned
// flag means an exec-start request was attempted, so guest work may be running
// even when the HTTP response itself was lost. No shell or bearer argv is used.
func (r *Runtime) runAgentHelper(ctx context.Context, containerID string, request []byte) ([]byte, bool, error) {
	body := struct {
		AttachStdin  bool
		AttachStdout bool
		AttachStderr bool
		Tty          bool
		Privileged   bool
		User         string
		Cmd          []string
	}{AttachStdout: true, AttachStderr: true, User: "1000:1000", Cmd: []string{"/usr/local/bin/sidekicks-agent", "request", base64.StdEncoding.EncodeToString(request)}}
	var created struct {
		ID string `json:"Id"`
	}
	if err := r.request(ctx, http.MethodPost, "/containers/"+containerID+"/exec", body, &created); err != nil {
		return nil, false, err
	}
	if !engineID(created.ID) {
		return nil, false, errors.New("docker returned invalid exec identity")
	}
	payload := bytes.NewBufferString(`{"Detach":false,"Tty":false}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker"+api+"/exec/"+created.ID+"/start", payload)
	if err != nil {
		return nil, false, errors.New("construct docker exec request")
	}
	req.Header.Set("Content-Type", "application/json")
	invokeClient := *r.client
	invokeClient.Timeout = invokeTimeout
	resp, err := invokeClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, true, ctx.Err()
		}
		return nil, true, errors.New("docker agent transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, true, fmt.Errorf("docker agent transport status %d", resp.StatusCode)
	}
	output, stderr, err := readExecStream(resp.Body)
	if err != nil {
		if ctx.Err() != nil {
			return nil, true, ctx.Err()
		}
		return nil, true, err
	}
	var status struct {
		Running     bool
		ExitCode    int
		ContainerID string
	}
	if err = r.request(ctx, http.MethodGet, "/exec/"+created.ID+"/json", nil, &status); err != nil {
		return nil, true, err
	}
	if status.ContainerID != containerID || status.Running {
		return nil, true, errors.New("docker agent execution completion could not be verified")
	}
	if status.ExitCode != 0 {
		return nil, true, fmt.Errorf("guest agent request failed with exit code %d (stderr bytes %d)", status.ExitCode, stderr)
	}
	return output, true, nil
}

func (r *Runtime) cancelRequest(caller context.Context, name, containerID, requestID string) (bool, bool, error) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(caller), 5*time.Second)
	defer cancel()
	// Revalidate the original immutable container, never a newly reused name.
	var current engineContainer
	if err := r.request(cleanup, http.MethodGet, "/containers/"+containerID+"/json", nil, &current); err != nil {
		return false, false, err
	}
	if err := validateOwnership(current, name); err != nil {
		return false, false, err
	}
	if current.ID != containerID || current.State.Status != "running" {
		return false, false, errors.New("guest cancellation target is no longer the running owned instance")
	}
	body, _ := json.Marshal(struct {
		RequestID string `json:"requestId"`
	}{requestID})
	request, _ := json.Marshal(struct {
		Method string `json:"method"`
		Path   string `json:"path"`
		Body   []byte `json:"body"`
	}{Method: http.MethodPost, Path: "/cancel", Body: body})
	output, _, err := r.runAgentHelper(cleanup, containerID, request)
	if err != nil {
		return false, false, err
	}
	var response struct {
		Status int    `json:"status"`
		Body   []byte `json:"body"`
	}
	if json.Unmarshal(output, &response) != nil || response.Status != http.StatusOK || len(response.Body) > 4096 {
		return false, false, errors.New("guest cancellation acknowledgement unavailable")
	}
	var ack struct {
		OK       bool `json:"ok"`
		Canceled bool `json:"canceled"`
	}
	if json.Unmarshal(response.Body, &ack) != nil || !ack.OK {
		return false, false, errors.New("invalid guest cancellation acknowledgement")
	}
	return true, ack.Canceled, nil
}

// readExecStream validates Docker's non-TTY multiplexing instead of assuming
// stdout-only raw bytes. Neither daemon stderr nor partial output leaks on error.
func readExecStream(reader io.Reader) ([]byte, int, error) {
	var output bytes.Buffer
	stderr, total := 0, 0
	var header [8]byte
	for {
		n, err := io.ReadFull(reader, header[:])
		if errors.Is(err, io.EOF) && n == 0 {
			return output.Bytes(), stderr, nil
		}
		if err != nil {
			return nil, 0, errors.New("truncated docker agent stream header")
		}
		if header[1] != 0 || header[2] != 0 || header[3] != 0 || (header[0] != 1 && header[0] != 2) {
			return nil, 0, errors.New("invalid docker agent stream header")
		}
		size := int(binary.BigEndian.Uint32(header[4:]))
		if size > MaxInvokeBytes-total {
			return nil, 0, errors.New("docker agent output exceeded 3 mebibytes limit")
		}
		total += size
		payload := make([]byte, size)
		if _, err = io.ReadFull(reader, payload); err != nil {
			return nil, 0, errors.New("truncated docker agent stream frame")
		}
		if header[0] == 1 {
			output.Write(payload)
		} else {
			stderr += size
		}
	}
}
