package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

type helperEnvelope struct {
	Method         string `json:"method"`
	Path           string `json:"path"`
	Body           []byte `json:"body"`
	RequestID      string `json:"requestId"`
	DeadlineUnixMS int64  `json:"deadlineUnixMs"`
}

func helperRequest(t *testing.T, req *http.Request) helperEnvelope {
	t.Helper()
	var command struct {
		Cmd        []string
		User       string
		Privileged bool
		Tty        bool
	}
	if json.NewDecoder(req.Body).Decode(&command) != nil || len(command.Cmd) != 3 || command.Cmd[0] != "/usr/local/bin/sidekicks-agent" || command.Cmd[1] != "request" || command.User != "1000:1000" || command.Privileged || command.Tty {
		t.Fatalf("unsafe helper command %+v", command)
	}
	raw, err := base64.StdEncoding.DecodeString(command.Cmd[2])
	if err != nil {
		t.Fatal("invalid helper encoding")
	}
	var envelope helperEnvelope
	if err = json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal("invalid helper envelope", err)
	}
	return envelope
}

// TestInvokeCancellationBeforeAndAfterGuestStart verifies invocation cancellation before and after guest start.
func TestInvokeCancellationBeforeAndAfterGuestStart(t *testing.T) {
	for _, beforeStart := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancel-before-start", false: "cancel-active"}[beforeStart], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancelID := strings.Repeat("d", 64)
			var identity string
			var deadline int64
			tombstone, active, completed := false, false, false
			cancelExecs := 0
			r := fake(func(req *http.Request) (*http.Response, error) {
				p := req.URL.Path
				switch {
				case p == api+"/containers/"+testID+"/json":
					return jsonResponse(owned()), nil
				case p == api+"/containers/"+containerID+"/json":
					if req.Context().Err() != nil {
						t.Fatal("cancellation inherited canceled context")
					}
					d, ok := req.Context().Deadline()
					if !ok || time.Until(d) > 5*time.Second {
						t.Fatal("unbounded cancellation helper")
					}
					return jsonResponse(owned()), nil
				case strings.HasSuffix(p, "/exec"):
					if p != api+"/containers/"+containerID+"/exec" {
						t.Fatal("helper target not pinned")
					}
					envelope := helperRequest(t, req)
					if envelope.Path == "/cancel" {
						cancelExecs++
						if envelope.RequestID != "" || envelope.DeadlineUnixMS != 0 || envelope.Method != "POST" {
							t.Fatal("cancel must not recursively track itself")
						}
						var body struct {
							RequestID string `json:"requestId"`
						}
						if json.Unmarshal(envelope.Body, &body) != nil || body.RequestID != identity {
							t.Fatal("cancel request ID mismatch")
						}
						return jsonResponse(map[string]string{"Id": cancelID}), nil
					}
					identity, deadline = envelope.RequestID, envelope.DeadlineUnixMS
					if len(identity) != 32 || identity == "caller-supplied-victim" || deadline <= time.Now().UnixMilli() {
						t.Fatal("untrusted caller cancellation identity/deadline reused")
					}
					if beforeStart {
						cancel()
					}
					return jsonResponse(map[string]string{"Id": execID}), nil
				case p == api+"/exec/"+execID+"/start":
					active = !beforeStart
					cancel()
					return nil, ctx.Err()
				case p == api+"/exec/"+cancelID+"/start":
					tombstone = true
					completed = active
					body, _ := json.Marshal(map[string]bool{"ok": true, "canceled": active})
					envelope, _ := json.Marshal(map[string]any{"status": 200, "body": body})
					return response(200, string(frame(1, string(envelope)))), nil
				case p == api+"/exec/"+cancelID+"/json":
					return jsonResponse(map[string]any{"ContainerID": containerID}), nil
				default:
					t.Fatalf("unexpected %s", req.URL)
					return nil, nil
				}
			})
			_, err := r.Invoke(ctx, testID, []byte(`{"method":"POST","path":"/exec","requestId":"caller-supplied-victim","deadlineUnixMs":999999999999999,"body":"e30="}`))
			var canceled *CancellationError
			if !errors.As(err, &canceled) || !errors.Is(err, context.Canceled) || !canceled.Confirmed || canceled.CancelErr != nil || canceled.WasActive == beforeStart {
				t.Fatalf("cancellation state lost: %v", err)
			}
			if cancelExecs != 1 || !tombstone || (!beforeStart && !completed) {
				t.Fatal("missing bounded request-specific cancellation")
			}
			// Guest registration uses this matching tombstone before running a delayed
			// helper. Guest's independent tests prove that atomic registration rule.
			if beforeStart && !tombstone {
				t.Fatal("late helper could start without cancellation marker")
			}
		})
	}
}

// TestInvokeFailedCancellationIsExplicitAndDoesNotTouchForeignInstance verifies that failed cancellation is explicit and does not affect a foreign instance.
func TestInvokeFailedCancellationIsExplicitAndDoesNotTouchForeignInstance(t *testing.T) {
	for _, which := range []string{"foreign", "gone", "stopped", "changed-id", "create-failed", "bad-id", "start-failed", "stream-failed", "exec-incomplete", "exec-failed", "ack-429", "ack-invalid", "ack-false", "ack-too-large"} {
		t.Run(which, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancelID := strings.Repeat("e", 64)
			var identity string
			cancelExecs := 0
			r := fake(func(req *http.Request) (*http.Response, error) {
				p := req.URL.Path
				switch {
				case p == api+"/containers/"+testID+"/json":
					return jsonResponse(owned()), nil
				case p == api+"/containers/"+containerID+"/json":
					if which == "gone" {
						return response(404, "secret"), nil
					}
					c := owned()
					if which == "foreign" {
						c.Config.Labels[ManagedLabel] = "false"
					}
					if which == "stopped" {
						c.State.Status = "exited"
					}
					if which == "changed-id" {
						c.ID = strings.Repeat("f", 64)
					}
					return jsonResponse(c), nil
				case strings.HasSuffix(p, "/exec"):
					if p != api+"/containers/"+containerID+"/exec" {
						t.Fatal("foreign instance touched")
					}
					envelope := helperRequest(t, req)
					if envelope.Path != "/cancel" {
						identity = envelope.RequestID
						return jsonResponse(map[string]string{"Id": execID}), nil
					}
					cancelExecs++
					if which == "create-failed" {
						return response(500, "secret"), nil
					}
					if which == "bad-id" {
						return jsonResponse(map[string]string{"Id": "invalid"}), nil
					}
					var body map[string]string
					_ = json.Unmarshal(envelope.Body, &body)
					if body["requestId"] != identity {
						t.Fatal("wrong request canceled")
					}
					return jsonResponse(map[string]string{"Id": cancelID}), nil
				case p == api+"/exec/"+execID+"/start":
					cancel()
					return nil, ctx.Err()
				case p == api+"/exec/"+cancelID+"/start":
					if which == "start-failed" {
						return response(500, "secret"), nil
					}
					if which == "stream-failed" {
						return response(200, "bad"), nil
					}
					status := 200
					body := []byte(`{"ok":true,"canceled":true}`)
					switch which {
					case "ack-429":
						status = 429
					case "ack-invalid":
						body = []byte(`bad`)
					case "ack-false":
						body = []byte(`{"ok":false}`)
					case "ack-too-large":
						body = []byte(strings.Repeat("x", 4097))
					}
					raw, _ := json.Marshal(map[string]any{"status": status, "body": body})
					return response(200, string(frame(1, string(raw)))), nil
				case p == api+"/exec/"+cancelID+"/json":
					code := 0
					if which == "exec-failed" {
						code = 2
					}
					return jsonResponse(map[string]any{"ContainerID": containerID, "Running": which == "exec-incomplete", "ExitCode": code}), nil
				default:
					t.Fatalf("unexpected %s", req.URL)
					return nil, nil
				}
			})
			_, err := r.Invoke(ctx, testID, []byte(`{"method":"GET","path":"/health"}`))
			var interrupted *CancellationError
			if !errors.As(err, &interrupted) || interrupted.Confirmed || interrupted.CancelErr == nil || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "unconfirmed") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("failed cancellation not represented safely: %v", err)
			}
			expected := 1
			switch which {
			case "foreign", "gone", "stopped", "changed-id":
				expected = 0
			}
			if cancelExecs != expected {
				t.Fatalf("cancel helper count%d want%d", cancelExecs, expected)
			}
		})
	}
}

// TestInvokeCanceledBeforeAnyWorkDoesNotCreateHelpers verifies that an invocation canceled before work creates no helpers.
func TestInvokeCanceledBeforeAnyWorkDoesNotCreateHelpers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := fake(func(*http.Request) (*http.Response, error) {
		t.Fatal("already-canceled call touched daemon")
		return nil, nil
	})
	_, err := r.Invoke(ctx, testID, []byte(`{}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
