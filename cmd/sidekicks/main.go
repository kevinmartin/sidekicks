// sidekicks manages explicitly owned compute containers. It never manages POC names.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute"
	"github.com/kevinmartin/sidekicks/internal/compute/adapter"
	docker "github.com/kevinmartin/sidekicks/internal/compute/runtime/docker"
)

type backend interface {
	compute.Runtime
	Invoke(context.Context, string, []byte) ([]byte, error)
	InvokeWithInstance(context.Context, string, func(compute.Instance) ([]byte, error)) (compute.Instance, []byte, error)
}
type request struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   []byte `json:"body,omitempty"`
}
type response struct {
	Status int    `json:"status"`
	Body   []byte `json:"body"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, func(socket string) (backend, error) {
		return docker.New(socket)
	}); err != nil {
		fmt.Fprintln(os.Stderr, "sidekicks:", err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer, newBackend func(string) (backend, error)) error {
	if len(args) == 0 {
		return errors.New("usage: sidekicks adapters|create|inspect|start|stop|destroy|health|exec|read|write [options]")
	}
	if args[0] == "adapters" {
		return json.NewEncoder(out).Encode(adapter.IDs())
	}
	cmd := args[0]
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	f.SetOutput(errOut)
	socket := f.String("socket", os.Getenv("SIDEKICKS_DOCKER_SOCKET"), "absolute local Docker socket path")
	runtimeID := f.String("runtime", "docker", "isolation backend (only docker implemented)")
	id := f.String("id", "", "instance suffix, never an existing POC name")
	workload := f.String("adapter", "shell", "workload adapter")
	image := f.String("image", "", "local image reference; never pulled implicitly")
	network := f.String("network", "none", "none or explicit egress")
	cpus := f.Int("cpus", 2, "CPU cap 1..8")
	memory := f.Int64("memory-mib", 512, "memory cap MiB")
	path := f.String("path", "", "relative guest workspace path")
	publishViewer := f.Bool("publish-viewer", false, "publish only the desktop viewer on an ephemeral loopback port; requires --network egress")
	browserRequest := f.String("request", "", "browser action JSON")
	timeout := f.Duration("timeout", 10*time.Second, "guest command deadline (100ms..60s)")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if err := adapter.ValidateRuntime(compute.RuntimeID(*runtimeID)); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("--id is required")
	}
	name := *id
	if !strings.HasPrefix(name, docker.Prefix) {
		name = docker.Prefix + name
	}
	b, err := newBackend(*socket)
	if err != nil {
		return err
	}
	encode := func(v any) error {
		return json.NewEncoder(out).Encode(v)
	}
	switch cmd {
	case "create":
		a, err := adapter.Get(compute.AdapterID(*workload))
		if err != nil {
			return err
		}
		if *image == "" {
			*image = "sidekicks-compute/" + string(a.ID()) + ":dev"
		}
		if *memory < 64 || *memory > 16384 {
			return errors.New("memory-mib must be 64..16384 before conversion")
		}
		token := make([]byte, 32)
		if _, err := rand.Read(token); err != nil {
			return err
		}
		pids := int64(128)
		if a.ID() == compute.BrowserChromium {
			pids = 256
		}
		if a.ID() == compute.DesktopNoVNC || a.ID() == compute.DesktopNeko {
			pids = 512
		}
		memoryExplicit := false
		f.Visit(func(ff *flag.Flag) {
			if ff.Name == "memory-mib" {
				memoryExplicit = true
			}
		})
		if !memoryExplicit {
			if a.ID() == compute.BrowserChromium {
				*memory = 1024
			}
			if a.ID() == compute.DesktopNoVNC || a.ID() == compute.DesktopNeko {
				*memory = 2048
			}
		}
		spec := compute.Spec{ID: name, Adapter: a.ID(), Image: *image, Resources: compute.Resources{CPUs: *cpus, MemoryBytes: *memory << 20, PIDs: pids}, Network: compute.NetworkPolicy(*network), Env: map[string]string{"SIDEKICKS_AGENT_TOKEN": hex.EncodeToString(token), "SIDEKICKS_ADAPTER": string(a.ID())}}
		if *publishViewer {
			if a.ID() != compute.DesktopNoVNC && a.ID() != compute.DesktopNeko {
				return errors.New("viewer publishing requires a desktop adapter")
			}
			spec.Ports = append(spec.Ports, compute.Port{Guest: 6080, Protocol: "tcp"})
			if a.ID() == compute.DesktopNeko {
				for p := uint16(60020); p < 60040; p++ {
					spec.Ports = append(spec.Ports, compute.Port{Guest: p, Protocol: "udp"})
				}
			}
		}
		if err := a.Validate(spec); err != nil {
			return err
		}
		instance, err := b.Provision(ctx, spec)
		if err != nil {
			return err
		}
		return encode(instance)
	case "inspect":
		v, err := b.Inspect(ctx, name)
		if err != nil {
			return err
		}
		return encode(v)
	case "start":
		v, err := b.Start(ctx, name)
		if err != nil {
			return err
		}
		return encode(v)
	case "stop":
		if err := b.Stop(ctx, name); err != nil {
			return err
		}
		return encode(map[string]bool{"ok": true})
	case "destroy":
		if err := b.Destroy(ctx, name); err != nil {
			return err
		}
		return encode(map[string]bool{"ok": true})
	case "connect":
		var port uint16
		_, data, e := b.InvokeWithInstance(ctx, name, func(instance compute.Instance) ([]byte, error) {
			ports := []map[string]uint16{}
			for _, p := range instance.Endpoints {
				if p.Guest == 6080 && p.Protocol == "tcp" {
					port = p.Port
				}
				if p.Protocol == "udp" && p.Guest >= 60020 && p.Guest < 60040 {
					ports = append(ports, map[string]uint16{"guest": p.Guest, "host": p.Port})
				}
			}
			if port == 0 {
				return nil, errors.New("no published loopback desktop viewer; create with --publish-viewer --network egress")
			}
			body, err := json.Marshal(map[string]any{"ports": ports})
			if err != nil {
				return nil, err
			}
			return json.Marshal(request{Method: "POST", Path: "/desktop/connect", Body: body})
		})
		if e != nil {
			return e
		}
		var res response
		if e = json.Unmarshal(data, &res); e != nil {
			return e
		}
		if res.Status != 200 {
			return fmt.Errorf("desktop connection rejected: http%d", res.Status)
		}
		var ticket struct {
			Ticket string `json:"ticket"`
		}
		if e = json.Unmarshal(res.Body, &ticket); e != nil || len(ticket.Ticket) != 43 {
			return errors.New("invalid desktop ticket")
		}
		return encode(map[string]any{"url": fmt.Sprintf("http://127.0.0.1:%d/#ticket=%s", port, ticket.Ticket), "expiresIn": 60})
	case "health", "exec", "read", "write", "browser", "capture", "state", "control", "input":
		req := request{Method: "GET", Path: "/health"}
		switch cmd {
		case "capture":
			req.Path = "/desktop/capture"
		case "state":
			req.Path = "/desktop/state"
		case "control", "input":
			if *browserRequest == "" || !json.Valid([]byte(*browserRequest)) {
				return errors.New("control/input requires --request json")
			}
			req.Path = "/desktop/" + cmd
			req.Method = "POST"
			req.Body = []byte(*browserRequest)
		case "browser":
			if *browserRequest == "" || !json.Valid([]byte(*browserRequest)) {
				return errors.New("browser requires --request json")
			}
			req.Method = "POST"
			req.Path = "/browser"
			req.Body = []byte(*browserRequest)
		case "exec":
			if len(f.Args()) == 0 {
				return errors.New("exec requires -- followed by guest argv")
			}
			if *timeout < 100*time.Millisecond || *timeout > 60*time.Second {
				return errors.New("timeout must be 100ms..60s")
			}
			req.Method = "POST"
			req.Path = "/exec"
			req.Body, err = json.Marshal(map[string]any{"argv": f.Args(), "timeoutMs": timeout.Milliseconds()})
		case "read", "write":
			if *path == "" {
				return errors.New("--path is required")
			}
			req.Path = "/files?path=" + urlQuery(*path)
			if cmd == "write" {
				req.Method = "PUT"
				req.Body, err = io.ReadAll(io.LimitReader(in, 20*1024+1))
				if len(req.Body) > 20*1024 {
					return errors.New("cli writes are limited to 20 kibibytes per request")
				}
			}
		}
		if err != nil {
			return err
		}
		data, err := json.Marshal(req)
		if err != nil {
			return err
		}
		raw, err := b.Invoke(ctx, name, data)
		if err != nil {
			return err
		}
		var res response
		if err = json.Unmarshal(raw, &res); err != nil {
			return errors.New("invalid guest response")
		}
		if res.Status < 200 || res.Status >= 300 {
			return fmt.Errorf("guest request failed (http%d): %s", res.Status, string(res.Body))
		}
		if cmd == "read" || cmd == "capture" {
			_, err = out.Write(res.Body)
			return err
		}
		_, err = out.Write(res.Body)
		return err
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}
func urlQuery(s string) string {
	const hexchars = "0123456789ABCDEF"
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("-_.~", rune(c)) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hexchars[c>>4])
			b.WriteByte(hexchars[c&15])
		}
	}
	return b.String()
}
