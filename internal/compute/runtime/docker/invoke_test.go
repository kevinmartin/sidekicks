package docker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute"
)

func frame(kind byte, s string) []byte {
	b := make([]byte, 8+len(s))
	b[0] = kind
	binary.BigEndian.PutUint32(b[4:], uint32(len(s)))
	copy(b[8:], s)
	return b
}

// TestInvokeFixedAgentAndDemux verifies fixed-agent invocation and output demultiplexing.
func TestInvokeFixedAgentAndDemux(t *testing.T) {
	request := []byte(`{"method":"POST","path":"/exec","body":{"argv":["true"]}}`)
	calls := 0
	r := fake(func(req *http.Request) (*http.Response, error) {
		calls++
		switch req.URL.Path {
		case api + "/containers/" + testID + "/json":
			return jsonResponse(owned()), nil
		case api + "/containers/" + containerID + "/exec":
			var body struct {
				Cmd          []string
				User         string
				Tty          bool
				Privileged   bool
				AttachStdout bool
				AttachStderr bool
				AttachStdin  bool
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.User != "1000:1000" || body.Tty || body.Privileged || !body.AttachStdout || !body.AttachStderr || body.AttachStdin || len(body.Cmd) != 3 || body.Cmd[0] != "/usr/local/bin/sidekicks-agent" || body.Cmd[1] != "request" {
				t.Fatalf("unsafe exec %+v", body)
			}
			got, err := base64.StdEncoding.DecodeString(body.Cmd[2])
			if err != nil {
				t.Fatal("invalid envelope encoding")
			}
			var actual, expected map[string]json.RawMessage
			_ = json.Unmarshal(got, &actual)
			_ = json.Unmarshal(request, &expected)
			var identity string
			var deadline int64
			if json.Unmarshal(actual["requestId"], &identity) != nil || json.Unmarshal(actual["deadlineUnixMs"], &deadline) != nil {
				t.Fatal("missing request cancellation identity/deadline")
			}
			if raw, err := hex.DecodeString(identity); err != nil || len(raw) != 16 || identity != strings.ToLower(identity) {
				t.Fatal("invalid request ID")
			}
			if deadline <= time.Now().UnixMilli() || deadline > time.Now().Add(invokeTimeout).UnixMilli() {
				t.Fatal("invalid request deadline")
			}
			delete(actual, "requestId")
			delete(actual, "deadlineUnixMs")
			if !reflect.DeepEqual(actual, expected) {
				t.Fatal("request contents changed")
			}

			return jsonResponse(map[string]string{"Id": execID}), nil
		case api + "/exec/" + execID + "/start":
			var body map[string]bool
			_ = json.NewDecoder(req.Body).Decode(&body)
			if body["Detach"] || body["Tty"] || req.Header.Get("Content-Type") != "application/json" {
				t.Fatal(body)
			}
			return response(200, string(append(append(frame(1, `{"status":`), frame(2, "diagnostic")...), frame(1, `200}`)...))), nil
		case api + "/exec/" + execID + "/json":
			return jsonResponse(map[string]any{"Running": false, "ExitCode": 0, "ContainerID": containerID}), nil
		default:
			t.Fatalf("unexpected %s", req.URL)
			return nil, nil
		}
	})
	got, err := r.Invoke(context.Background(), testID, request)
	if err != nil || string(got) != `{"status":200}` || calls != 4 {
		t.Fatalf("%s %v calls %d", got, err, calls)
	}
}

// TestInvokeValidationAndErrors verifies invocation validation and errors.
func TestInvokeValidationAndErrors(t *testing.T) {
	for _, input := range [][]byte{nil, []byte("bad"), []byte(`[]`), []byte(`null`), []byte(`1`), bytes.Repeat([]byte("x"), MaxRequestBytes+1)} {
		r := fake(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid request reached daemon")
			return nil, nil
		})
		_, err := r.Invoke(context.Background(), testID, input)
		assertErrIs(t, err, compute.ErrInvalid)
	}
	for _, which := range []string{"foreign", "stopped", "create-error", "bad-exec-id", "start-error", "transport-error", "bad-stream", "inspect-error", "still-running", "container-changed", "exit-error"} {
		t.Run(which, func(t *testing.T) {
			r := fake(func(req *http.Request) (*http.Response, error) {
				p := req.URL.Path
				if p == api+"/containers/"+testID+"/json" {
					c := owned()
					if which == "foreign" {
						c.Config.Labels[ManagedLabel] = "false"
					}
					if which == "stopped" {
						c.State.Status = "exited"
					}
					return jsonResponse(c), nil
				}
				if strings.HasSuffix(p, "/exec") {
					if which == "create-error" {
						return response(500, "private"), nil
					}
					if which == "bad-exec-id" {
						return jsonResponse(map[string]string{"Id": "bad"}), nil
					}
					return jsonResponse(map[string]string{"Id": execID}), nil
				}
				if strings.HasSuffix(p, "/start") {
					switch which {
					case "start-error":
						return response(500, "private"), nil
					case "transport-error":
						return nil, errors.New("private")
					case "bad-stream":
						return response(200, "bad"), nil
					}
					return response(200, string(frame(1, "result"))), nil
				}
				if strings.HasSuffix(p, "/json") {
					if which == "inspect-error" {
						return response(500, "private"), nil
					}
					id := containerID
					if which == "container-changed" {
						id = "other"
					}
					code := 0
					if which == "exit-error" {
						code = 9
					}
					return jsonResponse(map[string]any{"Running": which == "still-running", "ExitCode": code, "ContainerID": id}), nil
				}
				t.Fatal(p)
				return nil, nil
			})
			_, err := r.Invoke(context.Background(), testID, []byte(`{}`))
			if err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal("expected redacted error", err)
			}
		})
	}
}

// TestStreamBounds verifies stream bounds.
func TestStreamBounds(t *testing.T) {
	invalid := frame(1, "")
	invalid[2] = 1
	unsupported := frame(3, "")
	big := frame(1, "")
	binary.BigEndian.PutUint32(big[4:], MaxInvokeBytes+1)
	for _, input := range [][]byte{[]byte("short"), invalid, unsupported, big, frame(1, "a")[:8], append(frame(1, strings.Repeat("x", MaxInvokeBytes)), frame(1, "x")...)} {
		if _, _, err := readExecStream(bytes.NewReader(input)); err == nil {
			t.Fatal("accepted invalid or oversized stream")
		}
	}
	got, stderr, err := readExecStream(bytes.NewReader(frame(1, "")))
	if err != nil || len(got) != 0 || stderr != 0 {
		t.Fatal(got, stderr, err)
	}
}

// TestInvokeDeadlineFitsGuestTimeoutWithoutChangingRPCClient verifies that the invocation deadline accommodates the guest timeout without changing the RPC client.
func TestInvokeDeadlineFitsGuestTimeoutWithoutChangingRPCClient(t *testing.T) {
	for _, shortCaller := range []bool{false, true} {
		t.Run(fmt.Sprint(shortCaller), func(t *testing.T) {
			ctx := context.Background()
			var cancel context.CancelFunc
			if shortCaller {
				ctx, cancel = context.WithTimeout(ctx, time.Second)
				defer cancel()
			}
			r := fake(func(req *http.Request) (*http.Response, error) {
				p := req.URL.Path
				switch {
				case strings.HasPrefix(p, api+"/containers/") && strings.HasSuffix(p, "/json"):
					return jsonResponse(owned()), nil
				case strings.HasSuffix(p, "/exec"):
					return jsonResponse(map[string]string{"Id": execID}), nil
				case strings.HasSuffix(p, "/start"):
					deadline, ok := req.Context().Deadline()
					if !ok {
						t.Fatal("unbounded Invoke")
					}
					remaining := time.Until(deadline)
					if shortCaller {
						if remaining > time.Second {
							t.Fatalf("caller deadline extended: %v", remaining)
						}
					} else if remaining <= 60*time.Second || remaining > invokeTimeout {
						t.Fatalf("transport doesn't fit 60s guest command: %v", remaining)
					}
					return response(200, string(frame(1, `{}`))), nil
				case strings.HasPrefix(p, api+"/exec/") && strings.HasSuffix(p, "/json"):
					return jsonResponse(map[string]any{"ContainerID": containerID}), nil
				default:
					t.Fatal(p)
					return nil, nil
				}
			})
			if _, err := r.Invoke(ctx, testID, []byte(`{}`)); err != nil {
				t.Fatal(err)
			}
			if r.client.Timeout != 30*time.Second {
				t.Fatalf("Engine RPC timeout changed: %v", r.client.Timeout)
			}
		})
	}
}

// TestInvokeStreamAllowsEncodedTwoMiBHTTPEnvelope verifies that the invocation stream allows an encoded two-mebibyte HTTP envelope.
func TestInvokeStreamAllowsEncodedTwoMiBHTTPEnvelope(t *testing.T) {
	// LocalResponse.Body is base64 within JSON, so a 2MiB HTTP body can expand to
	// about 2.67MiB before Docker multiplexing. The total still has a hard3MiB cap.
	payload := strings.Repeat("x", base64.StdEncoding.EncodedLen(2<<20)+64)
	got, _, err := readExecStream(bytes.NewReader(frame(1, payload)))
	if err != nil || len(got) != len(payload) {
		t.Fatalf("valid encoded envelope rejected: bytes%d err%v", len(got), err)
	}
}

// TestInvokeWithInstancePinsGenerationAndEndpoints verifies that invocation pins the instance generation and endpoints.
func TestInvokeWithInstancePinsGenerationAndEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name         string
		changeAt     int
		mutate       func(*engineContainer)
		wantConflict bool
		wantExec     bool
	}{
		{"stable", 0, nil, false, true},
		{"replacement before request", 2, func(c *engineContainer) {
			c.ID = strings.Repeat("d", 64)
		}, true, false},
		{"replacement after request", 3, func(c *engineContainer) {
			c.ID = strings.Repeat("d", 64)
		}, true, true},
		{"restart", 3, func(c *engineContainer) {
			c.State.StartedAt = "2026-10-30T00:00:02Z"
		}, true, true},
		{"stopped", 3, func(c *engineContainer) {
			c.State.Status = "exited"
		}, true, true},
		{"endpoint change", 3, func(c *engineContainer) {
			c.NetworkSettings.Ports["6080/tcp"] = []portBinding{{HostIP: "127.0.0.1", HostPort: "4445"}}
		}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inspections := 0
			executed := false
			r := fake(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case api + "/containers/" + testID + "/json":
					inspections++
					c := owned()
					c.State.StartedAt = "2026-10-30T00:00:01Z"
					c.Config.Labels[AdapterLabel] = string(compute.DesktopNoVNC)
					c.NetworkSettings.Ports = map[string][]portBinding{"6080/tcp": {{HostIP: "127.0.0.1", HostPort: "4444"}}}
					if tc.mutate != nil && inspections >= tc.changeAt {
						tc.mutate(&c)
					}
					return jsonResponse(c), nil
				case api + "/containers/" + containerID + "/exec":
					executed = true
					return jsonResponse(map[string]string{"Id": execID}), nil
				case api + "/exec/" + execID + "/start":
					return response(200, string(frame(1, `{"status":200}`))), nil
				case api + "/exec/" + execID + "/json":
					return jsonResponse(map[string]any{"Running": false, "ExitCode": 0, "ContainerID": containerID}), nil
				default:
					t.Fatalf("request used wrong identity: %s", req.URL)
					return nil, nil
				}
			})
			var initial compute.Instance
			got, data, err := r.InvokeWithInstance(context.Background(), testID, func(i compute.Instance) ([]byte, error) {
				initial = i
				return []byte(`{"method":"POST","path":"/desktop/connect"}`), nil
			})
			if tc.wantConflict {
				if !errors.Is(err, compute.ErrConflict) || got.ID != "" || data != nil {
					t.Fatalf("expected conflict, got %+v %s %v", got, data, err)
				}
			} else if err != nil || got.ID != testID || string(data) != `{"status":200}` || !reflect.DeepEqual(initial, got) {
				t.Fatalf("unexpected stable result %+v %s %v", got, data, err)
			}
			if executed != tc.wantExec {
				t.Fatalf("request executed=%v want %v", executed, tc.wantExec)
			}
		})
	}
}

// TestInvokeWithInstanceRejectsUnavailableGeneration verifies that invocation rejects an unavailable generation.
func TestInvokeWithInstanceRejectsUnavailableGeneration(t *testing.T) {
	r := fake(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(owned()), nil
	})
	if _, _, err := r.InvokeWithInstance(context.Background(), testID, nil); !errors.Is(err, compute.ErrInvalid) {
		t.Fatalf("nil builder: %v", err)
	}
	called := false
	if _, _, err := r.InvokeWithInstance(context.Background(), testID, func(compute.Instance) ([]byte, error) {
		called = true
		return nil, nil
	}); !errors.Is(err, compute.ErrConflict) || called {
		t.Fatalf("missing generation: %v builder=%v", err, called)
	}
}
