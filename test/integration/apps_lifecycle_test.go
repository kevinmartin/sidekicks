package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute"
	docker "github.com/kevinmartin/sidekicks/internal/compute/runtime/docker"
)

var appsExactImage = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var appsContainerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

type appsLifecyclePlan struct {
	browser, image, binary, socket, root string
	adapter                              compute.AdapterID
}

// Evidence records the current source state as well as the executable digest.
// This identifies the observed checkout; it is not a cryptographic attestation
// that an independently supplied binary was built from it.
func appsSourceEvidence(root string) (map[string]any, error) {
	hash := sha256.New()
	add := func(name string) error {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(hash, "%s\x00%d\x00", filepath.ToSlash(name), len(data))
		_, _ = hash.Write(data)
		return nil
	}
	for _, name := range []string{"go.mod", "go.sum", "Makefile"} {
		if err := add(name); err != nil {
			return nil, err
		}
	}
	for _, dir := range []string{"internal", "cmd", "web", "test/integration"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			if !entry.Type().IsRegular() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			return add(relative)
		})
		if err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rev, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return nil, fmt.Errorf("source revision: %w", err)
	}
	status, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain=v1", "--untracked-files=all").Output()
	if err != nil {
		return nil, fmt.Errorf("source status: %w", err)
	}
	statusHash := sha256.Sum256(status)
	return map[string]any{"revision": strings.TrimSpace(string(rev)), "dirty": len(status) != 0, "statusSHA256": hex.EncodeToString(statusHash[:]), "goSourceSHA256": hex.EncodeToString(hash.Sum(nil)), "scope": "go.mod, go.sum, Makefile and Go files under internal, cmd, web and test/integration"}, nil
}

func appsPlan(root string, env func(string) string) (appsLifecyclePlan, error) {
	p := appsLifecyclePlan{browser: env("SIDEKICKS_APPS_TEST_BROWSER"), image: env("SIDEKICKS_APPS_IMAGE_ID"), binary: env("SIDEKICKS_APPS_TEST_BINARY"), socket: env("SIDEKICKS_DOCKER_SOCKET"), root: root}
	switch p.browser {
	case "chromium":
		p.adapter = compute.DesktopNoVNC
	case "firefox":
		p.adapter = compute.DesktopNeko
	default:
		return p, errors.New("explicit chromium or firefox lifecycle browser is required")
	}
	if !appsExactImage.MatchString(p.image) {
		return p, errors.New("exact lowercase sha256 image id required; mutable tags are not accepted")
	}
	if !filepath.IsAbs(p.socket) || filepath.Clean(p.socket) != p.socket || strings.ContainsRune(p.socket, 0) {
		return p, errors.New("explicit clean local docker socket required")
	}
	if !filepath.IsAbs(p.binary) || filepath.Clean(p.binary) != p.binary || strings.ContainsRune(p.binary, 0) {
		return p, errors.New("absolute test binary under repository .run/compute-bin required")
	}
	relative, err := filepath.Rel(filepath.Join(root, ".run/compute-bin"), p.binary)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return p, errors.New("test binary must be within repository .run/compute-bin")
	}
	return p, nil
}

type appsImageInfo struct {
	ID           string `json:"Id"`
	OS           string `json:"Os"`
	Architecture string
	Config       struct {
		Entrypoint, Cmd, Env []string
		Volumes              map[string]json.RawMessage
	}
}

func (p appsLifecyclePlan) imageGuard(info appsImageInfo, binaryArch string) error {
	if info.ID != p.image || info.OS != "linux" || info.Architecture != binaryArch {
		return errors.New("image and linux test-binary identities/architecture do not match")
	}
	if len(info.Config.Entrypoint) != 1 || info.Config.Entrypoint[0] != "/usr/local/bin/sidekicks-agent" || len(info.Config.Cmd) != 0 || len(info.Config.Volumes) != 0 {
		return errors.New("image is not a compatible private go-agent compute workload")
	}
	found := false
	for _, entry := range info.Config.Env {
		if strings.HasPrefix(entry, "SIDEKICKS_ADAPTER=") {
			if found || entry != "SIDEKICKS_ADAPTER="+string(p.adapter) {
				return errors.New("image adapter does not match requested lifecycle browser")
			}
			found = true
		}
	}
	if !found {
		return errors.New("image does not declare its compute adapter")
	}
	return nil
}
func (p appsLifecyclePlan) binaryGuard() (string, string, error) {
	st, err := os.Lstat(p.binary)
	if err != nil || !st.Mode().IsRegular() || st.Size() < 1 || st.Size() > 32<<20 || st.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 || st.Mode().Perm()&0005 != 0005 {
		return "", "", errors.New("bounded regular executable test binary required")
	}
	f, err := elf.Open(p.binary)
	if err != nil {
		return "", "", errors.New("linux elf test binary required")
	}
	defer f.Close()
	if f.Class != elf.ELFCLASS64 || f.Data != elf.ELFDATA2LSB {
		return "", "", errors.New("64-bit little-endian linux test binary required")
	}
	arch := ""
	switch f.Machine {
	case elf.EM_AARCH64:
		arch = "arm64"
	case elf.EM_X86_64:
		arch = "amd64"
	default:
		return "", "", errors.New("only arm64/amd64 test binaries are supported")
	}
	for _, program := range f.Progs {
		if program.Type == elf.PT_INTERP {
			// CGO_ENABLED is a case-sensitive build variable.
			return "", "", errors.New("static CGO_ENABLED=0 test binary required")
		}
	}
	info, err := buildinfo.ReadFile(p.binary)
	if err != nil || info.Path != "github.com/kevinmartin/sidekicks/internal/compute/desktop/apps.test" {
		return "", "", errors.New("binary is not the current module's apps test package")
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if settings["GOOS"] != "linux" || settings["GOARCH"] != arch || settings["CGO_ENABLED"] != "0" || settings["-cover"] != "true" {
		// GOARCH and CGO_ENABLED are case-sensitive build variables.
		return "", "", errors.New("build apps test binary with linux matching GOARCH, CGO_ENABLED=0 and -cover")
	}
	file, err := os.Open(p.binary)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", "", err
	}
	return arch, hex.EncodeToString(hash.Sum(nil)), nil
}

type appsOwnedInfo struct {
	ID          string `json:"Id"`
	Name, Image string
	Config      struct {
		Labels map[string]string
		Env    []string
	}
	HostConfig struct {
		NetworkMode                string
		Privileged, ReadonlyRootfs bool
		Binds                      []string
	}
	NetworkSettings struct{ Ports map[string]json.RawMessage }
}

func (p appsLifecyclePlan) ownedGuard(info appsOwnedInfo, name, expectedID string) error {
	labels := info.Config.Labels
	if !appsContainerID.MatchString(info.ID) || (expectedID != "" && info.ID != expectedID) || info.Name != "/"+name || !strings.HasPrefix(name, "sidekicks-compute-apps-") || info.Image != p.image || labels[docker.ManagedLabel] != "true" || labels[docker.IDLabel] != name || labels[docker.VersionLabel] != "1" || labels[docker.AdapterLabel] != string(p.adapter) {
		return errors.New("refusing foreign, renamed or changed lifecycle container")
	}
	if info.HostConfig.NetworkMode != "none" || info.HostConfig.Privileged || !info.HostConfig.ReadonlyRootfs || len(info.HostConfig.Binds) != 0 {
		return errors.New("lifecycle container must be private network-none, read-only and unmounted")
	}
	for _, bindings := range info.NetworkSettings.Ports {
		if value := strings.TrimSpace(string(bindings)); value != "null" && value != "[]" {
			return errors.New("lifecycle container has a published endpoint")
		}
	}
	found := false
	for _, entry := range info.Config.Env {
		if strings.HasPrefix(entry, "SIDEKICKS_DESKTOP_START_BROWSER=") {
			if found || entry != "SIDEKICKS_DESKTOP_START_BROWSER=0" {
				return errors.New("refusing a desktop with automatic browser startup")
			}
			found = true
		}
	}
	if !found {
		return errors.New("fresh no-browser desktop marker required")
	}
	return nil
}

type appsOutput struct {
	mu sync.Mutex
	bytes.Buffer
	truncated bool
}

// Write implements the test fixture's Write method.
func (b *appsOutput) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(data)
	room := (128 << 10) - b.Len()
	if len(data) > room {
		data = data[:room]
		b.truncated = true
	}
	_, _ = b.Buffer.Write(data)
	return n, nil
}
func appsDocker(ctx context.Context, p appsLifecyclePlan, config string, args ...string) ([]byte, error) {
	argv := append([]string{"--host", "unix://" + p.socket}, args...)
	command := exec.CommandContext(ctx, "docker", argv...)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "DOCKER_CONFIG=" + config, "LANG=C.UTF-8"}
	var out appsOutput
	command.Stdout = &out
	command.Stderr = &out
	err := command.Run()
	if out.truncated {
		return nil, errors.New("docker lifecycle helper output exceeded 128 kibibytes")
	}
	if err != nil {
		return bytes.Clone(out.Bytes()), fmt.Errorf("docker lifecycle helper failed: %w", err)
	}
	return bytes.Clone(out.Bytes()), nil
}

const appsGuestBinary = "/usr/local/bin/sidekicks-apps-lifecycle-test"

type appsLayerInfo struct {
	ID       string `json:"Id"`
	Config   map[string]any
	RepoTags []string
	RootFS   struct {
		Type   string
		Layers []string
	}
}

func appsDerivativeGuard(base, derived appsLayerInfo, expectedBase, expectedDerived string) error {
	if base.ID != expectedBase || derived.ID != expectedDerived || !appsExactImage.MatchString(derived.ID) || derived.ID == base.ID || base.RootFS.Type != "layers" || derived.RootFS.Type != "layers" {
		return errors.New("derived image identity does not match the selected base")
	}
	if len(base.RootFS.Layers) == 0 || len(derived.RootFS.Layers) != len(base.RootFS.Layers)+1 || !reflect.DeepEqual(base.RootFS.Layers, derived.RootFS.Layers[:len(base.RootFS.Layers)]) || !reflect.DeepEqual(base.Config, derived.Config) {
		return errors.New("derived image must preserve base config and add exactly one layer")
	}
	return nil
}

// The guest workspace is noexec and the root is read-only. Build an untagged,
// test-only derivative from the exact local image with a single binary COPY;
// preserve the original image and record the derivative as a separate identity.
func appsBuildDerivative(ctx context.Context, p appsLifecyclePlan, config, dir, expectedSHA string, baseRaw []byte) (string, error) {
	var base appsLayerInfo
	if json.Unmarshal(baseRaw, &base) != nil || base.ID != p.image {
		return "", errors.New("invalid base image metadata")
	}
	allowedTag := regexp.MustCompile(`^sidekicks-compute/(desktop-neko|desktop-novnc):[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	var reference string
	for _, tag := range base.RepoTags {
		if allowedTag.MatchString(tag) {
			reference = tag
			break
		}
	}
	if reference == "" {
		return "", errors.New("exact base image needs an existing local sidekicks desktop tag for derivative build")
	}
	checkReference := func() error {
		raw, err := appsDocker(ctx, p, config, "image", "inspect", "--format", "{{json .}}", reference)
		var found appsLayerInfo
		if err != nil || json.Unmarshal(raw, &found) != nil || found.ID != p.image {
			return errors.New("local base tag no longer resolves to requested image")
		}
		return nil
	}
	if err := checkReference(); err != nil {
		return "", err
	}
	contextDir := filepath.Join(dir, "context")
	if err := os.Mkdir(contextDir, 0700); err != nil {
		return "", err
	}
	src, err := os.Open(p.binary)
	if err != nil {
		return "", err
	}
	dst, err := os.OpenFile(filepath.Join(contextDir, "testbin"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		src.Close()
		return "", err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(dst, io.TeeReader(io.LimitReader(src, (32<<20)+1), hash))
	closeErr := dst.Close()
	src.Close()
	if copyErr != nil || closeErr != nil || n > 32<<20 || hex.EncodeToString(hash.Sum(nil)) != expectedSHA {
		return "", errors.New("test binary changed while preparing derivative")
	}
	file := "FROM " + reference + "\nCOPY --chmod=0555 testbin " + appsGuestBinary + "\n"
	if err = os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte(file), 0600); err != nil {
		return "", err
	}
	iidfile := filepath.Join(dir, "image-id")
	out, err := appsDocker(ctx, p, config, "build", "--network", "none", "--pull=false", "--iidfile", iidfile, contextDir)
	if err != nil {
		return "", fmt.Errorf("build test-only derivative: %w: %s", err, out)
	}
	if err = checkReference(); err != nil {
		return "", err
	}
	iid, err := os.ReadFile(iidfile)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(iid))
	if !appsExactImage.MatchString(id) {
		return "", errors.New("derivative build returned invalid image identity")
	}
	raw, err := appsDocker(ctx, p, config, "image", "inspect", "--format", "{{json .}}", id)
	if err != nil {
		return "", err
	}
	var derived appsLayerInfo
	if json.Unmarshal(raw, &derived) != nil {
		return "", errors.New("invalid image metadata")
	}
	if err = appsDerivativeGuard(base, derived, p.image, id); err != nil {
		return "", err
	}
	return id, nil
}
func appsExecArgs(container, browser string) []string {
	return []string{"exec", "--user", "1000:1000", "--workdir", "/workspace", "-e", "HOME=/tmp/sidekicks-home", "-e", "DISPLAY=:99", "-e", "DBUS_SESSION_BUS_ADDRESS=unix:path=/tmp/sidekicks-home/.run/session-bus", "-e", "SIDEKICKS_APPS_TEST_DISPLAY=:99", "-e", "SIDEKICKS_APPS_TEST_HOME=/tmp/sidekicks-home", "-e", "SIDEKICKS_APPS_TEST_HOME_URL=http://127.0.0.1:8081/home", "-e", "SIDEKICKS_APPS_TEST_TITLE=Sidekicks", "-e", "SIDEKICKS_APPS_TEST_BROWSER=" + browser, container, appsGuestBinary, "-test.run=^TestRealGuestApps$", "-test.v", "-test.timeout=150s", "-test.coverprofile=/workspace/apps.cover"}
}

// TestIsolatedAppsLifecycle is never part of ordinary unit execution. It creates
// its own immutable-image guest; accepting an existing container/display is not
// an option. Real test code uses XGB/EWMH, not the published human viewer.
func TestIsolatedAppsLifecycle(t *testing.T) {
	if os.Getenv("SIDEKICKS_APPS_LIFECYCLE") != "1" {
		t.Skip("requires explicit immutable-image app lifecycle handoff")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	p, err := appsPlan(root, os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	arch, binarySHA, err := p.binaryGuard()
	if err != nil {
		t.Fatal(err)
	}
	sourceBefore, err := appsSourceEvidence(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dockerConfig := t.TempDir()
	imageRaw, err := appsDocker(ctx, p, dockerConfig, "image", "inspect", "--format", "{{json .}}", p.image)
	if err != nil {
		t.Fatal(err)
	}
	var image appsImageInfo
	if json.Unmarshal(imageRaw, &image) != nil {
		t.Fatal("invalid local image metadata")
	}
	if err = p.imageGuard(image, arch); err != nil {
		t.Fatal(err)
	}
	baseImage := p.image
	derivedImage, err := appsBuildDerivative(ctx, p, dockerConfig, t.TempDir(), binarySHA, imageRaw)
	if err != nil {
		t.Fatal(err)
	}
	p.image = derivedImage
	rt, err := docker.New(p.socket)
	if err != nil {
		t.Fatal(err)
	}
	var random [8]byte
	if _, err = rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	name := "sidekicks-compute-apps-" + hex.EncodeToString(random[:])
	token := make([]byte, 32)
	if _, err = rand.Read(token); err != nil {
		t.Fatal(err)
	}
	spec := compute.Spec{ID: name, Adapter: p.adapter, Image: p.image, Resources: compute.Resources{CPUs: 2, MemoryBytes: 2 << 30, PIDs: 512}, Network: compute.NetworkNone, Env: map[string]string{"SIDEKICKS_AGENT_TOKEN": hex.EncodeToString(token), "SIDEKICKS_ADAPTER": string(p.adapter), "SIDEKICKS_DESKTOP_START_BROWSER": "0"}}
	if _, err = rt.Provision(ctx, spec); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 25*time.Second)
		defer stop()
		if err := rt.Stop(cleanup, name); err != nil {
			t.Errorf("stop owned lifecycle guest: %v", err)
		}
		if err := rt.Destroy(cleanup, name); err != nil {
			t.Errorf("destroy owned lifecycle guest: %v", err)
		}
	})
	inspect := func(expected string) appsOwnedInfo {
		t.Helper()
		if current, e := rt.Inspect(ctx, name); e != nil || current.Image != p.image {
			t.Fatal("owned runtime identity changed", e)
		}
		raw, e := appsDocker(ctx, p, dockerConfig, "inspect", "--format", "{{json .}}", name)
		if e != nil {
			t.Fatal(e)
		}
		var info appsOwnedInfo
		if json.Unmarshal(raw, &info) != nil {
			t.Fatal("invalid container identity metadata")
		}
		if e = p.ownedGuard(info, name, expected); e != nil {
			t.Fatal(e)
		}
		return info
	}
	owned := inspect("")
	for deadline := time.Now().Add(30 * time.Second); ; {
		if _, _, err = callGuest(ctx, rt, name, "GET", "/health", nil); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fresh guest did not become ready", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	var state struct {
		Ready   bool `json:"ready"`
		Control struct{ Owner string }
	}
	for deadline := time.Now().Add(30 * time.Second); ; {
		status, body, stateErr := callGuest(ctx, rt, name, "GET", "/desktop/state", nil)
		if stateErr == nil && status == 200 && json.Unmarshal(body, &state) == nil {
			if state.Control.Owner != "" {
				t.Fatal("refusing an already-owned desktop")
			}
			if state.Ready {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("fresh desktop did not report current readiness", stateErr)
		}
		time.Sleep(100 * time.Millisecond)
	}
	inspect(owned.ID)
	digest, err := appsDocker(ctx, p, dockerConfig, "exec", "--user", "1000:1000", owned.ID, "/usr/bin/sha256sum", appsGuestBinary)
	if err != nil {
		t.Fatalf("read test binary hash: %v: %s", err, digest)
	}
	fields := strings.Fields(string(digest))
	if len(fields) != 2 || fields[0] != binarySHA || fields[1] != appsGuestBinary {
		t.Fatal("derived guest binary differs from guarded host binary")
	}
	evidence := filepath.Join(root, "artifacts/compute/apps-lifecycle", name)
	if err = os.MkdirAll(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	inspect(owned.ID)
	output, err := appsDocker(ctx, p, dockerConfig, appsExecArgs(owned.ID, p.browser)...)
	if writeErr := os.WriteFile(filepath.Join(evidence, "lifecycle.txt"), output, 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	sourceAfter, sourceErr := appsSourceEvidence(root)
	summary, _ := json.MarshalIndent(map[string]any{"baseImageId": baseImage, "testDerivativeImageId": p.image, "browser": p.browser, "binarySHA256": binarySHA, "containerId": owned.ID, "architecture": arch, "guestCommandSucceeded": err == nil, "sourceBefore": sourceBefore, "sourceAfter": sourceAfter, "sourceEvidenceError": fmt.Sprint(sourceErr), "scope": "test-only one-layer derivative of exact base; new network-none no-port guest; XGB/EWMH lifecycle only; cleanup failures reported by host test"}, "", "  ")
	if writeErr := os.WriteFile(filepath.Join(evidence, "summary.json"), append(summary, '\n'), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if err != nil {
		if _, screenshot, captureErr := callGuest(ctx, rt, name, "GET", "/files?path=apps-focus-failure.png", nil); captureErr == nil && len(screenshot) > 0 {
			if writeErr := os.WriteFile(filepath.Join(evidence, "focus-failure.png"), screenshot, 0600); writeErr != nil {
				t.Logf("save focus capture: %v", writeErr)
			}
		}
		diagnostic, diagnosticErr := appsDocker(ctx, p, dockerConfig, "exec", "--user", "1000:1000", owned.ID, "/bin/sh", "-c", "id; stat -c '%A %u %g %n' /workspace /usr/local/bin/sidekicks-apps-lifecycle-test; awk '$2 == \"/workspace\" {print}' /proc/mounts")
		t.Fatalf("actual app lifecycle failed: %v; guest execution diagnostics (%v): %s", err, diagnosticErr, diagnostic)
	}
	if sourceErr != nil || sourceBefore["goSourceSHA256"] != sourceAfter["goSourceSHA256"] || sourceBefore["revision"] != sourceAfter["revision"] {
		t.Fatal("source revision or Go source changed during lifecycle test", sourceErr)
	}
	if !bytes.Contains(output, []byte("--- PASS: TestRealGuestApps")) || !bytes.Contains(output, []byte("close/reopen app=files")) {
		t.Fatal("test command did not prove the full app lifecycle matrix")
	}
	t.Logf("baseimage=%s testderivative=%s browser=%s binarySHA256=%s container=%s\n%s", baseImage, p.image, p.browser, binarySHA, owned.ID, output)
	_, profile, err := callGuest(ctx, rt, name, "GET", "/files?path=apps.cover", nil)
	if err != nil || !bytes.HasPrefix(profile, []byte("mode:")) {
		t.Fatal("bounded guest coverage profile unavailable", err)
	}
	if err = os.WriteFile(filepath.Join(evidence, "guest.cover"), profile, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(evidence, "lifecycle.txt"), output, 0600); err != nil {
		t.Fatal(err)
	}
	inspect(owned.ID)
}
