package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/kevinmartin/sidekicks/internal/compute"
)

type fakeBackend struct {
	called string
	spec   compute.Spec
	req    request
	fail   bool
	view   compute.Instance
}

// ID implements the test fixture's ID method.
func (f *fakeBackend) ID() compute.RuntimeID {
	return compute.Docker
}

// Provision implements the test fixture's Provision method.
func (f *fakeBackend) Provision(_ context.Context, s compute.Spec) (compute.Instance, error) {
	f.called = "create"
	f.spec = s
	if f.fail {
		return compute.Instance{}, errors.New("failure")
	}
	return compute.Instance{ID: s.ID, State: "running"}, nil
}

// Inspect implements the test fixture's Inspect method.
func (f *fakeBackend) Inspect(_ context.Context, id string) (compute.Instance, error) {
	f.called = "inspect"
	return compute.Instance{ID: id}, nil
}

// Start implements the test fixture's Start method.
func (f *fakeBackend) Start(_ context.Context, id string) (compute.Instance, error) {
	f.called = "start"
	return compute.Instance{ID: id}, nil
}

// Stop implements the test fixture's Stop method.
func (f *fakeBackend) Stop(context.Context, string) error {
	f.called = "stop"
	return nil
}

// Destroy implements the test fixture's Destroy method.
func (f *fakeBackend) Destroy(context.Context, string) error {
	f.called = "destroy"
	return nil
}

// Invoke implements the test fixture's Invoke method.
func (f *fakeBackend) Invoke(_ context.Context, id string, b []byte) ([]byte, error) {
	f.called = "invoke"
	if err := json.Unmarshal(b, &f.req); err != nil {
		return nil, err
	}
	if f.fail {
		return json.Marshal(response{Status: 403, Body: []byte("denied")})
	}
	return json.Marshal(response{Status: 200, Body: []byte("guest-output")})
}

// InvokeWithInstance implements the test fixture's InvokeWithInstance method.
func (f *fakeBackend) InvokeWithInstance(ctx context.Context, id string, build func(compute.Instance) ([]byte, error)) (compute.Instance, []byte, error) {
	f.called = "invoke-with-instance"
	body, err := build(f.view)
	if err != nil {
		return compute.Instance{}, nil, err
	}
	if err := json.Unmarshal(body, &f.req); err != nil {
		return compute.Instance{}, nil, err
	}
	if f.fail {
		return compute.Instance{}, nil, compute.ErrConflict
	}
	out, _ := json.Marshal(response{Status: 200, Body: []byte(`{"ticket":"` + strings.Repeat("a", 43) + `"}`)})
	return f.view, out, nil
}
func invokeCLI(t *testing.T, args []string, input string, f *fakeBackend) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(context.Background(), args, strings.NewReader(input), &out, io.Discard, func(string) (backend, error) {
		return f, nil
	})
	return out.String(), err
}

// TestCreatePrivateDefaults verifies create private defaults.
func TestCreatePrivateDefaults(t *testing.T) {
	f := &fakeBackend{}
	out, err := invokeCLI(t, []string{"create", "--id", "test"}, "", f)
	if err != nil {
		t.Fatal(err)
	}
	if f.spec.ID != "sidekicks-compute-test" || f.spec.Network != compute.NetworkNone || len(f.spec.Ports) != 0 || f.spec.Adapter != compute.Shell {
		t.Fatalf("spec %+v", f.spec)
	}
	token := f.spec.Env["SIDEKICKS_AGENT_TOKEN"]
	if len(token) != 64 || strings.Contains(out, token) {
		t.Fatal("missing/leakedtoken")
	}
	if f.spec.Resources.PIDs != 128 || f.spec.Resources.MemoryBytes != 512<<20 {
		t.Fatal("resourcebounds")
	}
}

// TestCLILifecycle verifies cli lifecycle.
func TestCLILifecycle(t *testing.T) {
	for _, cmd := range []string{"inspect", "start", "stop", "destroy"} {
		t.Run(cmd, func(t *testing.T) {
			f := &fakeBackend{}
			out, err := invokeCLI(t, []string{cmd, "--id", "test"}, "", f)
			if err != nil || f.called != cmd || out == "" {
				t.Fatalf("%s %v %+v", out, err, f)
			}
		})
	}
}

// TestCLIRequestContract verifies cli request contract.
func TestCLIRequestContract(t *testing.T) {
	for _, tc := range []struct {
		args                []string
		input, method, path string
		body                bool
	}{{[]string{"health", "--id", "x"}, "", "GET", "/health", false}, {[]string{"read", "--id", "x", "--path", "a +✓.txt"}, "", "GET", "/files?path=a%20%2B%E2%9C%93.txt", false}, {[]string{"write", "--id", "x", "--path", "a"}, "exact", "PUT", "/files?path=a", true}, {[]string{"exec", "--id", "x", "--timeout", "1s", "--", "/bin/echo", "$HOME"}, "", "POST", "/exec", true}} {
		f := &fakeBackend{}
		out, err := invokeCLI(t, tc.args, tc.input, f)
		if err != nil {
			t.Fatal(err)
		}
		if out != "guest-output" || f.req.Method != tc.method || f.req.Path != tc.path {
			t.Fatalf("%s %+v", out, f.req)
		}
		if tc.body && len(f.req.Body) == 0 {
			t.Fatal("missingbody")
		}
		if strings.Contains(strings.Join(tc.args, " "), "exec") {
			var b struct {
				Argv []string `json:"argv"`
			}
			if err = json.Unmarshal(f.req.Body, &b); err != nil || b.Argv[1] != "$HOME" {
				t.Fatal("argv expanded")
			}
		}
	}
}

// TestCLIRejectsInvalid verifies cli rejects invalid.
func TestCLIRejectsInvalid(t *testing.T) {
	for _, args := range [][]string{nil, {"create"}, {"create", "--id", "x", "--runtime", "apple-vz"}, {"create", "--id", "x", "--adapter", "unknown"}, {"exec", "--id", "x"}, {"exec", "--id", "x", "--timeout", "1h", "--", "true"}, {"write", "--id", "x"}, {"wat", "--id", "x"}, {"create", "--unknown"}, {"create", "--id", "x", "--memory-mib", "-1"}, {"create", "--id", "x", "--memory-mib", "17592186044928"}} {
		f := &fakeBackend{}
		_, err := invokeCLI(t, args, "", f)
		if err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
	f := &fakeBackend{}
	if _, err := invokeCLI(t, []string{"write", "--id", "x", "--path", "x"}, strings.Repeat("x", 20*1024+1), f); err == nil {
		t.Fatal("largewrite")
	}
}

// TestCLIErrorsAndDiscovery verifies cli errors and discovery.
func TestCLIErrorsAndDiscovery(t *testing.T) {
	f := &fakeBackend{fail: true}
	if _, err := invokeCLI(t, []string{"health", "--id", "x"}, "", f); err == nil {
		t.Fatal("guestdenial")
	}
	if _, err := invokeCLI(t, []string{"create", "--id", "x"}, "", f); err == nil {
		t.Fatal("createerror")
	}
	out, err := invokeCLI(t, []string{"adapters"}, "", f)
	if err != nil || !strings.Contains(out, "desktop-neko") {
		t.Fatal(out, err)
	}
	err = run(context.Background(), []string{"inspect", "--id", "x"}, strings.NewReader(""), io.Discard, io.Discard, func(string) (backend, error) {
		return nil, errors.New("socket")
	})
	if err == nil {
		t.Fatal("socketerror")
	}
}

// TestConnectUsesOneObservedInstanceForRequestAndURL verifies connect uses one observed instance for request and url.
func TestConnectUsesOneObservedInstanceForRequestAndURL(t *testing.T) {
	f := &fakeBackend{view: compute.Instance{Endpoints: []compute.Endpoint{
		{Guest: 6080, Protocol: "tcp", Host: "127.0.0.1", Port: 12345},
		{Guest: 60020, Protocol: "udp", Host: "127.0.0.1", Port: 54321},
	}}}
	out, err := invokeCLI(t, []string{"connect", "--id", "x"}, "", f)
	if err != nil || !strings.Contains(out, "http://127.0.0.1:12345/#ticket=") || f.called != "invoke-with-instance" {
		t.Fatalf("%q %v %+v", out, err, f)
	}
	var requestBody struct {
		Ports []struct{ Guest, Host uint16 }
	}
	if err := json.Unmarshal(f.req.Body, &requestBody); err != nil || len(requestBody.Ports) != 1 || requestBody.Ports[0].Guest != 60020 || requestBody.Ports[0].Host != 54321 {
		t.Fatalf("wrong ports: %+v %v", requestBody, err)
	}
	f.fail = true
	if out, err := invokeCLI(t, []string{"connect", "--id", "x"}, "", f); !errors.Is(err, compute.ErrConflict) || out != "" {
		t.Fatalf("conflicting generation returned URL: %q %v", out, err)
	}
	f.view.Endpoints = nil
	if out, err := invokeCLI(t, []string{"connect", "--id", "x"}, "", f); err == nil || out != "" {
		t.Fatalf("missing viewer returned URL: %q %v", out, err)
	}
}
