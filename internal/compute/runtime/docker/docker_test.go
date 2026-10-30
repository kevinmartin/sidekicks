package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute"
)

const testID = "sidekicks-compute-unit"

var containerID = strings.Repeat("a", 64)
var imageID = "sha256:" + strings.Repeat("b", 64)
var execID = strings.Repeat("c", 64)

type roundTripper func(*http.Request) (*http.Response, error)

// RoundTrip calls the configured test transport function.
func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func fake(fn roundTripper) *Runtime {
	return NewWithHTTPClient(&http.Client{Transport: fn})
}
func spec() compute.Spec {
	return compute.Spec{ID: testID, Adapter: compute.Shell, Image: "sidekicks:test", Resources: compute.Resources{CPUs: 2, MemoryBytes: 256 << 20, PIDs: 128}, Network: compute.NetworkNone}
}
func owned() engineContainer {
	c := engineContainer{ID: containerID, Name: "/" + testID, Image: imageID, Created: "2026-10-30T00:00:00.123Z"}
	c.Config.Labels = map[string]string{ManagedLabel: "true", IDLabel: testID, AdapterLabel: string(compute.Shell), VersionLabel: "1"}
	c.State.Status = "running"
	c.NetworkSettings.Ports = map[string][]portBinding{}
	return c
}
func jsonResponse(v any) *http.Response {
	b, _ := json.Marshal(v)
	return response(200, string(b))
}
func assertErrIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error %v, want %v", err, target)
	}
}

// TestConstructors verifies runtime constructors.
func TestConstructors(t *testing.T) {
	for _, s := range []string{"relative.sock", "/tmp/x\x00y"} {
		if _, err := New(s); !errors.Is(err, compute.ErrInvalid) {
			t.Fatalf("accepted %q", s)
		}
	}
	r, err := New("")
	if err != nil || r.ID() != compute.Docker {
		t.Fatal(r, err)
	}
	if NewWithHTTPClient(nil).client.Timeout != 30*time.Second {
		t.Fatal("missing bounded default")
	}
	if _, err := NewWithHTTPClient(nil).Inspect(context.Background(), testID); err == nil {
		t.Fatal("nil transport must fail closed without network")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.Inspect(ctx, testID)
	assertErrIs(t, err, context.Canceled)
}

// TestValidation verifies request validation.
func TestValidation(t *testing.T) {
	if err := Validate(spec()); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*compute.Spec){
		"prefix": func(s *compute.Spec) {
			s.ID = "foreign"
		},
		"id traversal": func(s *compute.Spec) {
			s.ID = Prefix + "../a"
		},
		"adapter": func(s *compute.Spec) {
			s.Adapter = "bad"
		},
		"image": func(s *compute.Spec) {
			s.Image = "x;touch /host"
		},
		"image traversal": func(s *compute.Spec) {
			s.Image = "x/../y"
		},
		"cpu0": func(s *compute.Spec) {
			s.Resources.CPUs = 0
		},
		"cpu9": func(s *compute.Spec) {
			s.Resources.CPUs = 9
		},
		"memory low": func(s *compute.Spec) {
			s.Resources.MemoryBytes = 1
		},
		"memory high": func(s *compute.Spec) {
			s.Resources.MemoryBytes = 17 << 30
		},
		"pids low": func(s *compute.Spec) {
			s.Resources.PIDs = 1
		},
		"pids high": func(s *compute.Spec) {
			s.Resources.PIDs = 4097
		},
		"network": func(s *compute.Spec) {
			s.Network = "host"
		},
		"none ports": func(s *compute.Spec) {
			s.Ports = []compute.Port{{Guest: 80, Protocol: "tcp"}}
		},
		"port0": func(s *compute.Spec) {
			s.Network = compute.NetworkEgress
			s.Ports = []compute.Port{{Protocol: "tcp"}}
		},
		"protocol": func(s *compute.Spec) {
			s.Network = compute.NetworkEgress
			s.Ports = []compute.Port{{Guest: 80, Protocol: "sctp"}}
		},
		"duplicate guest": func(s *compute.Spec) {
			s.Network = compute.NetworkEgress
			s.Ports = []compute.Port{{Guest: 80, Protocol: "tcp"}, {Guest: 80, Protocol: "tcp"}}
		},
		"duplicate host": func(s *compute.Spec) {
			s.Network = compute.NetworkEgress
			s.Ports = []compute.Port{{Guest: 80, HostPort: 1234, Protocol: "tcp"}, {Guest: 81, HostPort: 1234, Protocol: "tcp"}}
		},
		"many ports": func(s *compute.Spec) {
			s.Network = compute.NetworkEgress
			s.Ports = make([]compute.Port, 65)
		},
		"env name": func(s *compute.Spec) {
			s.Env = map[string]string{"SIDEKICKS_=BAD": "a"}
		},
		"dangerous env": func(s *compute.Spec) {
			s.Env = map[string]string{"LD_PRELOAD": "/evil"}
		},
		"env null": func(s *compute.Spec) {
			s.Env = map[string]string{"SIDEKICKS_TOKEN": "x\x00y"}
		},
		"env long": func(s *compute.Spec) {
			s.Env = map[string]string{"SIDEKICKS_TOKEN": strings.Repeat("x", 16385)}
		},
		"env count": func(s *compute.Spec) {
			s.Env = map[string]string{}
			for i := 0; i < 33; i++ {
				s.Env[fmt.Sprintf("SIDEKICKS_%d", i)] = "x"
			}
		},
	}
	for name, edit := range tests {
		t.Run(name, func(t *testing.T) {
			s := spec()
			edit(&s)
			assertErrIs(t, Validate(s), compute.ErrInvalid)
		})
	}
	s := spec()
	s.Network = compute.NetworkEgress
	s.Ports = []compute.Port{{Guest: 80, Protocol: "tcp"}, {Guest: 80, Protocol: "udp"}}
	s.Env = map[string]string{"SIDEKICKS_TOKEN": "private", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "TZ": "UTC"}
	if err := Validate(s); err != nil {
		t.Fatal(err)
	}
}

// TestProvisionSecurityAndImmutableImage verifies provisioning security and immutable image selection.
func TestProvisionSecurityAndImmutableImage(t *testing.T) {
	calls := []string{}
	s := spec()
	s.Network = compute.NetworkEgress
	s.Ports = []compute.Port{{Guest: 8080, Protocol: "tcp"}, {Guest: 59001, Protocol: "udp", HostPort: 59002}}
	s.Env = map[string]string{"SIDEKICKS_TOKEN": "top-secret", "LANG": "C.UTF-8"}
	c := owned()
	c.NetworkSettings.Ports = map[string][]portBinding{"8080/tcp": {{HostIP: "127.0.0.1", HostPort: "4444"}}, "59001/udp": {{HostIP: "127.0.0.1", HostPort: "59002"}}}
	r := fake(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.Method+" "+req.URL.RequestURI())
		switch req.URL.Path {
		case api + "/images/sidekicks:test/json":
			return jsonResponse(map[string]string{"Id": imageID}), nil
		case api + "/containers/create":
			if req.URL.Query().Get("name") != testID {
				t.Fatal("wrong name")
			}
			var body createBody
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Image != imageID || body.User != "1000:1000" {
				t.Fatalf("not pinned/nonroot: %+v", body)
			}
			if body.Labels[ManagedLabel] != "true" || body.Labels[IDLabel] != testID || body.Labels[AdapterLabel] != "shell" || body.Labels[VersionLabel] != "1" {
				t.Fatal(body.Labels)
			}
			h := body.HostConfig
			if !h.ReadonlyRootfs || h.Privileged || !h.Init || h.NetworkMode != "bridge" || h.NanoCpus != 2e9 || h.Memory != 256<<20 || h.MemorySwap != h.Memory || h.PidsLimit != 128 || h.ShmSize != 512<<20 || h.RestartPolicy.Name != "no" {
				t.Fatalf("unsafe host config %+v", h)
			}
			if !reflect.DeepEqual(h.CapDrop, []string{"ALL"}) || !reflect.DeepEqual(h.SecurityOpt, []string{"no-new-privileges:true"}) {
				t.Fatal(h)
			}
			if len(h.Tmpfs) != 3 || !strings.Contains(h.Tmpfs["/workspace"], "size=536870912") {
				t.Fatal(h.Tmpfs)
			}
			if h.PortBindings["8080/tcp"][0].HostIP != "127.0.0.1" || h.PortBindings["8080/tcp"][0].HostPort != "" || h.PortBindings["59001/udp"][0].HostPort != "59002" {
				t.Fatal(h.PortBindings)
			}
			if !reflect.DeepEqual(body.Env, []string{"LANG=C.UTF-8", "SIDEKICKS_TOKEN=top-secret"}) {
				t.Fatal("unstable env order")
			}
			return jsonResponse(map[string]string{"Id": containerID}), nil
		case api + "/containers/" + testID + "/json":
			return jsonResponse(c), nil
		case api + "/containers/" + containerID + "/start":
			return response(204, ""), nil
		default:
			t.Fatalf("unexpected %s", req.URL)
			return nil, nil
		}
	})
	got, err := r.Provision(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != testID || got.Image != imageID || got.State != "running" || len(got.Endpoints) != 2 || got.CreatedAt.IsZero() {
		t.Fatalf("bad instance %+v", got)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "top-secret") {
		t.Fatal("secret exposed")
	}
	if len(calls) != 5 {
		t.Fatal(calls)
	}
}

// TestProvisionNetworkNoneAndFailures verifies network-none provisioning and failures.
func TestProvisionNetworkNoneAndFailures(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		r := fake(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/images/") {
				return jsonResponse(map[string]string{"Id": imageID}), nil
			}
			if strings.HasSuffix(req.URL.Path, "/create") {
				var b createBody
				_ = json.NewDecoder(req.Body).Decode(&b)
				if b.HostConfig.NetworkMode != "none" || len(b.HostConfig.PortBindings) != 0 || len(b.ExposedPorts) != 0 {
					t.Fatal(b)
				}
				return jsonResponse(map[string]string{"Id": containerID}), nil
			}
			if strings.HasSuffix(req.URL.Path, "/json") {
				return jsonResponse(owned()), nil
			}
			return response(204, ""), nil
		})
		if _, err := r.Provision(context.Background(), spec()); err != nil {
			t.Fatal(err)
		}
	})
	for _, stage := range []string{"image-missing", "bad-image-id", "create-conflict", "bad-created-id", "verify-fail", "identity-changed", "start-fail", "cleanup-fail", "cleanup-identity-fail"} {
		t.Run(stage, func(t *testing.T) {
			deleted := false
			inspects := 0
			r := fake(func(req *http.Request) (*http.Response, error) {
				p := req.URL.Path
				if strings.Contains(p, "/images/") {
					if stage == "image-missing" {
						return response(404, "secret"), nil
					}
					if stage == "bad-image-id" {
						return jsonResponse(map[string]string{"Id": "bad"}), nil
					}
					return jsonResponse(map[string]string{"Id": imageID}), nil
				}
				if strings.HasSuffix(p, "/create") {
					if stage == "create-conflict" {
						return response(409, "secret"), nil
					}
					if stage == "bad-created-id" {
						return jsonResponse(map[string]string{"Id": "bad"}), nil
					}
					return jsonResponse(map[string]string{"Id": containerID}), nil
				}
				if strings.HasSuffix(p, "/json") {
					inspects++
					if stage == "verify-fail" {
						return response(500, "private"), nil
					}
					c := owned()
					if stage == "identity-changed" {
						c.ID = strings.Repeat("d", 64)
					}
					if stage == "cleanup-identity-fail" && inspects > 1 {
						c.Config.Labels[ManagedLabel] = "false"
					}
					return jsonResponse(c), nil
				}
				if strings.HasSuffix(p, "/start") {
					return response(500, "private"), nil
				}
				if req.Method == http.MethodDelete {
					deleted = true
					if stage == "cleanup-fail" {
						return response(500, "private"), nil
					}
					return response(204, ""), nil
				}
				t.Fatalf("unexpected %s", p)
				return nil, nil
			})
			_, err := r.Provision(context.Background(), spec())
			if err == nil {
				t.Fatal("want failure")
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
				t.Fatal("error leaked response", err)
			}
			wantDelete := stage == "start-fail" || stage == "cleanup-fail"
			if deleted != wantDelete {
				t.Fatalf("deleted %v want %v err %v", deleted, wantDelete, err)
			}
		})
	}
	r := fake(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid input reached daemon")
		return nil, nil
	})
	s := spec()
	s.ID = "foreign"
	_, err := r.Provision(context.Background(), s)
	assertErrIs(t, err, compute.ErrInvalid)
}

// TestLifecycleOwnershipAndIdempotence verifies lifecycle ownership and idempotence.
func TestLifecycleOwnershipAndIdempotence(t *testing.T) {
	for _, operation := range []string{"inspect", "start", "stop", "destroy"} {
		t.Run(operation, func(t *testing.T) {
			for _, state := range []string{"owned", "missing", "foreign-label", "foreign-name", "foreign-id", "bad-version", "bad-adapter", "bad-id"} {
				t.Run(state, func(t *testing.T) {
					mutated := false
					r := fake(func(req *http.Request) (*http.Response, error) {
						if strings.HasSuffix(req.URL.Path, "/json") {
							if state == "missing" {
								return response(404, ""), nil
							}
							c := owned()
							switch state {
							case "foreign-label":
								c.Config.Labels[ManagedLabel] = "false"
							case "foreign-name":
								c.Name = "/foreign"
							case "foreign-id":
								c.Config.Labels[IDLabel] = "foreign"
							case "bad-version":
								c.Config.Labels[VersionLabel] = "2"
							case "bad-adapter":
								c.Config.Labels[AdapterLabel] = "evil"
							case "bad-id":
								c.ID = "short"
							}
							return jsonResponse(c), nil
						}
						if !strings.Contains(req.URL.Path, containerID) {
							t.Fatal("mutation not pinned to immutable ID")
						}
						mutated = true
						if operation == "start" || operation == "stop" {
							return response(304, ""), nil
						}
						return response(204, ""), nil
					})
					var err error
					switch operation {
					case "inspect":
						_, err = r.Inspect(context.Background(), testID)
					case "start":
						_, err = r.Start(context.Background(), testID)
					case "stop":
						err = r.Stop(context.Background(), testID)
					case "destroy":
						err = r.Destroy(context.Background(), testID)
					}
					if state == "owned" {
						if err != nil {
							t.Fatal(err)
						}
						if mutated != (operation != "inspect") {
							t.Fatal("missing lifecycle mutation")
						}
					} else if state == "missing" {
						if operation == "stop" || operation == "destroy" {
							if err != nil {
								t.Fatal(err)
							}
						} else {
							assertErrIs(t, err, compute.ErrNotFound)
						}
					} else {
						assertErrIs(t, err, compute.ErrConflict)
					}
					if state != "owned" && mutated {
						t.Fatal("foreign/missing object mutated")
					}
				})
			}
		})
	}
	r := fake(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid name reached engine")
		return nil, nil
	})
	_, err := r.Inspect(context.Background(), "sidekicks-neko")
	assertErrIs(t, err, compute.ErrInvalid)
}

// TestInspectionRejectsUnsafeEndpoints verifies that inspection rejects unsafe endpoints.
func TestInspectionRejectsUnsafeEndpoints(t *testing.T) {
	for _, which := range []string{"time", "protocol", "protocol missing", "protocol extra", "guest0", "guestbad", "host", "port0", "portbad"} {
		t.Run(which, func(t *testing.T) {
			c := owned()
			key := "8080/tcp"
			b := portBinding{HostIP: "127.0.0.1", HostPort: "54321"}
			switch which {
			case "time":
				c.Created = "bad"
			case "protocol":
				key = "8080/sctp"
			case "protocol missing":
				key = "8080"
			case "protocol extra":
				key = "8080/tcp/udp"
			case "guest0":
				key = "0/tcp"
			case "guestbad":
				key = "x/tcp"
			case "host":
				b.HostIP = "0.0.0.0"
			case "port0":
				b.HostPort = "0"
			case "portbad":
				b.HostPort = "x"
			}
			c.NetworkSettings.Ports = map[string][]portBinding{key: {b}}
			r := fake(func(*http.Request) (*http.Response, error) {
				return jsonResponse(c), nil
			})
			if _, err := r.Inspect(context.Background(), testID); err == nil {
				t.Fatal("accepted unsafe Engine response")
			}
		})
	}
}

// TestRequestBoundsErrorsAndRedaction verifies request bounds, errors and redaction.
func TestRequestBoundsErrorsAndRedaction(t *testing.T) {
	cases := []struct {
		name   string
		code   int
		body   string
		target error
	}{
		{"404", 404, "token=secret", compute.ErrNotFound}, {"409", 409, "secret", compute.ErrConflict}, {"400", 400, "secret", compute.ErrInvalid}, {"500", 500, "secret", nil}, {"redirect", 302, "secret", nil}, {"304invalid", 304, "secret", nil}, {"bad-json", 200, "{", nil}, {"oversize", 200, strings.Repeat(" ", maxResponse+1), nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := fake(func(*http.Request) (*http.Response, error) {
				return response(tt.code, tt.body), nil
			})
			_, err := r.Inspect(context.Background(), testID)
			if err == nil {
				t.Fatal("want failure")
			}
			if tt.target != nil {
				assertErrIs(t, err, tt.target)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("leaked", err)
			}
		})
	}
	r := fake(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("secret transport /private/socket")
	})
	_, err := r.Inspect(context.Background(), testID)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.Inspect(ctx, testID)
	assertErrIs(t, err, context.Canceled)
	if err = r.request(context.Background(), http.MethodPost, "/x", func() {

	}, nil); err == nil {
		t.Fatal("want encode error")
	}
}

// TestImplicitImageVolumesRejected verifies that implicit image volumes are rejected.
func TestImplicitImageVolumesRejected(t *testing.T) {
	r := fake(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "/images/") {
			t.Fatal("volume-bearing image reached container create")
		}
		return jsonResponse(map[string]any{"Id": imageID, "Config": map[string]any{"Volumes": map[string]any{"/data": map[string]any{}}}}), nil
	})
	_, err := r.Provision(context.Background(), spec())
	assertErrIs(t, err, compute.ErrInvalid)
}

// TestLifecycleConcurrentRemovalIdempotence verifies idempotent lifecycle operations during concurrent removal.
func TestLifecycleConcurrentRemovalIdempotence(t *testing.T) {
	r := fake(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/json") {
			return jsonResponse(owned()), nil
		}
		return response(404, "removed concurrently"), nil
	})
	if err := r.Stop(context.Background(), testID); err != nil {
		t.Fatal(err)
	}
	if err := r.Destroy(context.Background(), testID); err != nil {
		t.Fatal(err)
	}
}

// TestProvisionReconcilesEveryPostCreateFailureByImmutableID verifies that post-create failures are reconciled using the immutable ID.
func TestProvisionReconcilesEveryPostCreateFailureByImmutableID(t *testing.T) {
	cases := []struct {
		name                                                                      string
		failInitial, failFinal, cancelAfterCreate, badFinalMetadata, replaceFinal bool
	}{
		{name: "initial-inspect-error", failInitial: true},
		{name: "final-inspect-error", failFinal: true},
		{name: "caller-canceled-after-create", cancelAfterCreate: true},
		{name: "final-metadata-invalid", badFinalMetadata: true},
		{name: "final-object-replaced", replaceFinal: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			nameInspects, cleanupInspects, deletes := 0, 0, 0
			r := fake(func(req *http.Request) (*http.Response, error) {
				p := req.URL.Path
				switch {
				case strings.HasPrefix(p, api+"/images/"):
					return jsonResponse(map[string]string{"Id": imageID}), nil
				case strings.HasSuffix(p, "/create"):
					if tt.cancelAfterCreate {
						cancel()
					}
					return jsonResponse(map[string]string{"Id": containerID}), nil
				case p == api+"/containers/"+testID+"/json":
					nameInspects++
					if tt.cancelAfterCreate {
						return nil, req.Context().Err()
					}
					if tt.failInitial || tt.failFinal && nameInspects == 2 {
						return response(503, "DO-NOT-LEAK-SECRET"), nil
					}
					c := owned()
					if tt.badFinalMetadata && nameInspects == 2 {
						c.Created = "invalid"
					}
					if tt.replaceFinal && nameInspects == 2 {
						c.ID = strings.Repeat("d", 64)
					}
					return jsonResponse(c), nil
				case p == api+"/containers/"+containerID+"/json":
					cleanupInspects++
					if req.Context().Err() != nil {
						t.Fatal("cleanup inherited canceled caller")
					}
					deadline, ok := req.Context().Deadline()
					if !ok || time.Until(deadline) > 5*time.Second {
						t.Fatal("unbounded cleanup")
					}
					return jsonResponse(owned()), nil
				case strings.HasSuffix(p, "/start"):
					return response(204, ""), nil
				case req.Method == http.MethodDelete:
					if p != api+"/containers/"+containerID || req.URL.Query().Get("force") != "true" || req.URL.Query().Get("v") != "false" {
						t.Fatalf("wrong cleanup target %s", req.URL)
					}
					deletes++
					return response(204, ""), nil
				default:
					t.Fatalf("unexpected %s", req.URL)
					return nil, nil
				}
			})
			_, err := r.Provision(ctx, spec())
			var detail *ProvisionError
			if !errors.As(err, &detail) || detail.InstanceID != testID || detail.CleanupStatus != "confirmed removed" || detail.CleanupErr != nil {
				t.Fatalf("missing actionable cleanup status: %v", err)
			}
			if cleanupInspects != 1 || deletes != 1 {
				t.Fatalf("cleanup inspect%d delete%d", cleanupInspects, deletes)
			}
			if tt.cancelAfterCreate && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if strings.Contains(err.Error(), "DO-NOT-LEAK-SECRET") {
				t.Fatal("daemon secret leaked")
			}
		})
	}
}

// TestProvisionCleanupRefusesForeignOrReusedName verifies that provisioning cleanup refuses foreign or reused names.
func TestProvisionCleanupRefusesForeignOrReusedName(t *testing.T) {
	for _, which := range []string{"absent", "foreign-label", "renamed", "adapter-changed", "cleanup-inspect-error", "cleanup-delete-error", "cleanup-delete-absent"} {
		t.Run(which, func(t *testing.T) {
			deletes := 0
			r := fake(func(req *http.Request) (*http.Response, error) {
				p := req.URL.Path
				switch {
				case strings.HasPrefix(p, api+"/images/"):
					return jsonResponse(map[string]string{"Id": imageID}), nil
				case strings.HasSuffix(p, "/create"):
					return jsonResponse(map[string]string{"Id": containerID}), nil
				case p == api+"/containers/"+testID+"/json":
					// A different object now occupies the same name. Never mutate it.
					c := owned()
					c.ID = strings.Repeat("d", 64)
					return jsonResponse(c), nil
				case p == api+"/containers/"+containerID+"/json":
					if which == "absent" {
						return response(404, ""), nil
					}
					if which == "cleanup-inspect-error" {
						return response(503, "private"), nil
					}
					c := owned()
					switch which {
					case "foreign-label":
						c.Config.Labels[ManagedLabel] = "false"
					case "renamed":
						c.Name = "/another-name"
					case "adapter-changed":
						c.Config.Labels[AdapterLabel] = string(compute.DesktopNeko)
					}
					return jsonResponse(c), nil
				case req.Method == http.MethodDelete:
					if p != api+"/containers/"+containerID {
						t.Fatalf("deleted name collision or foreign ID: %s", p)
					}
					deletes++
					if which == "cleanup-delete-error" {
						return response(503, "private"), nil
					}
					if which == "cleanup-delete-absent" {
						return response(404, ""), nil
					}
					t.Fatal("foreign or absent cleanup must not delete")
					return nil, nil
				default:
					t.Fatalf("unexpected %s", req.URL)
					return nil, nil
				}
			})
			_, err := r.Provision(context.Background(), spec())
			var detail *ProvisionError
			if !errors.As(err, &detail) {
				t.Fatalf("missing cleanup outcome %v", err)
			}
			wantStatus := "unconfirmed"
			if which == "absent" || which == "cleanup-delete-absent" {
				wantStatus = "confirmed absent"
			}
			if detail.CleanupStatus != wantStatus {
				t.Fatalf("status %q want %q", detail.CleanupStatus, wantStatus)
			}
			if (wantStatus == "unconfirmed") != (detail.CleanupErr != nil) {
				t.Fatal("cleanup error not represented")
			}
			wantDeletes := 0
			if strings.HasPrefix(which, "cleanup-delete-") {
				wantDeletes = 1
			}
			if deletes != wantDeletes {
				t.Fatalf("deletes%d want%d", deletes, wantDeletes)
			}
		})
	}
}

// TestProvisionUncertainCreationNeverReconcilesByName verifies that uncertain creation is never reconciled by name.
func TestProvisionUncertainCreationNeverReconcilesByName(t *testing.T) {
	for _, body := range []string{"error", "{}", `{"Id":"short"}`, `{"Id":"` + containerID + `"} trailing`} {
		t.Run(body, func(t *testing.T) {
			r := fake(func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, "/images/") {
					return jsonResponse(map[string]string{"Id": imageID}), nil
				}
				if strings.HasSuffix(req.URL.Path, "/create") {
					if body == "error" {
						return response(500, "secret"), nil
					}
					return response(201, body), nil
				}
				t.Fatalf("unknown creation identity was looked up or deleted: %s", req.URL)
				return nil, nil
			})
			_, err := r.Provision(context.Background(), spec())
			var detail *ProvisionError
			if !errors.As(err, &detail) || detail.CleanupStatus != "not attempted; creation identity unavailable" || !strings.Contains(err.Error(), testID) {
				t.Fatalf("missing uncertain creation status: %v", err)
			}
		})
	}
}

// TestStartRejectsReusedNameAfterStartingOriginal verifies that start rejects a reused name after starting the original container.
func TestStartRejectsReusedNameAfterStartingOriginal(t *testing.T) {
	inspectCount := 0
	original := owned()
	replacement := owned()
	replacement.ID = strings.Repeat("d", 64)
	r := fake(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case api + "/containers/" + testID + "/json":
			inspectCount++
			if inspectCount > 1 {
				return jsonResponse(replacement), nil
			}
			return jsonResponse(original), nil
		case api + "/containers/" + containerID + "/start":
			return response(http.StatusNoContent, ""), nil
		default:
			t.Fatalf("unexpected request %s", req.URL)
			return nil, nil
		}
	})
	got, err := r.Start(context.Background(), testID)
	if !errors.Is(err, compute.ErrConflict) || got.ID != "" {
		t.Fatalf("returned replacement: %+v, %v", got, err)
	}
}
