package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kevinmartin/sidekicks/internal/compute"
	docker "github.com/kevinmartin/sidekicks/internal/compute/runtime/docker"
)

func appsGuardPlan(t *testing.T) (appsLifecyclePlan, map[string]string) {
	t.Helper()
	root := t.TempDir()
	env := map[string]string{"SIDEKICKS_APPS_TEST_BROWSER": "firefox", "SIDEKICKS_APPS_IMAGE_ID": "sha256:" + strings.Repeat("a", 64), "SIDEKICKS_APPS_TEST_BINARY": filepath.Join(root, ".run/compute-bin/apps-lifecycle.test"), "SIDEKICKS_DOCKER_SOCKET": "/tmp/owned-docker.sock"}
	plan, err := appsPlan(root, func(key string) string {
		return env[key]
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan, env
}

// TestAppsSourceEvidenceRecordsRevisionAndSourceChanges verifies apps source evidence records revision and source changes.
func TestAppsSourceEvidenceRecordsRevisionAndSourceChanges(t *testing.T) {
	root := t.TempDir()
	// Prove the fixture ignores inherited signing, hooks and environment-based
	// config. The hook would fail the commit if executed.
	hostile := t.TempDir()
	hooks := filepath.Join(hostile, "hooks")
	if err := os.Mkdir(hooks, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(hostile, "global-config")
	if err := os.WriteFile(config, []byte("[commit]\n\tgpgsign = true\n[core]\n\thooksPath = "+hooks+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "commit.gpgsign")
	t.Setenv("GIT_CONFIG_VALUE_0", "true")
	emptyTemplate := t.TempDir()
	emptyHooks := t.TempDir()
	for _, dir := range []string{"internal", "cmd", "web", "test/integration"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"go.mod", "go.sum", "Makefile", "internal/sample.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("initial\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"commit", "-q", "-m", "baseline"}} {
		base := []string{"-C", root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=" + emptyHooks}
		cmd := exec.Command("git", append(base, args...)...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TEMPLATE_DIR=" + emptyTemplate, "GIT_TERMINAL_PROMPT=0"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("temporary git: %v: %s", err, out)
		}
	}
	before, err := appsSourceEvidence(root)
	if err != nil || before["dirty"] != false {
		t.Fatalf("initial source evidence: %v %v", before, err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal/sample.go"), []byte("changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := appsSourceEvidence(root)
	if err != nil || after["dirty"] != true || before["revision"] != after["revision"] || before["goSourceSHA256"] == after["goSourceSHA256"] {
		t.Fatalf("source change not recorded: %v %v %v", before, after, err)
	}
}

// TestAppsLifecyclePlanRejectsMutableImagesExistingTargetsAndUnsafeFiles verifies apps lifecycle plan rejects mutable images existing targets and unsafe files.
func TestAppsLifecyclePlanRejectsMutableImagesExistingTargetsAndUnsafeFiles(t *testing.T) {
	plan, env := appsGuardPlan(t)
	if plan.adapter != compute.DesktopNeko {
		t.Fatal("Firefox selected wrong workload")
	}
	for key, value := range map[string]string{"SIDEKICKS_APPS_TEST_BROWSER": "firefox; sh", "SIDEKICKS_APPS_IMAGE_ID": "sidekicks-compute/desktop-neko:dev", "SIDEKICKS_APPS_TEST_BINARY": "/etc/passwd", "SIDEKICKS_DOCKER_SOCKET": "tcp://remote:2375"} {
		before := env[key]
		env[key] = value
		if _, err := appsPlan(plan.root, func(key string) string {
			return env[key]
		}); err == nil {
			t.Fatal("unsafe plan accepted", key)
		}
		env[key] = before
	}
	env["SIDEKICKS_APPS_TEST_BROWSER"] = ""
	if _, err := appsPlan(plan.root, func(key string) string {
		return env[key]
	}); err == nil {
		t.Fatal("harness selected a browser implicitly")
	}
	env["SIDEKICKS_APPS_TEST_BROWSER"] = "chromium"
	chromium, err := appsPlan(plan.root, func(key string) string {
		return env[key]
	})
	if err != nil || chromium.adapter != compute.DesktopNoVNC {
		t.Fatal("original Chromium workload changed")
	}
}
func validAppsImage(p appsLifecyclePlan) appsImageInfo {
	info := appsImageInfo{ID: p.image, OS: "linux", Architecture: "arm64"}
	info.Config.Entrypoint = []string{"/usr/local/bin/sidekicks-agent"}
	info.Config.Env = []string{"SIDEKICKS_ADAPTER=" + string(p.adapter)}
	return info
}

// TestAppsLifecycleImageGuardRequiresExactCompatibleSource verifies apps lifecycle image guard requires exact compatible source.
func TestAppsLifecycleImageGuardRequiresExactCompatibleSource(t *testing.T) {
	p, _ := appsGuardPlan(t)
	if err := p.imageGuard(validAppsImage(p), "arm64"); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*appsImageInfo){func(i *appsImageInfo) {
		i.ID = "sha256:" + strings.Repeat("b", 64)
	}, func(i *appsImageInfo) {
		i.Architecture = "amd64"
	}, func(i *appsImageInfo) {
		i.OS = "darwin"
	}, func(i *appsImageInfo) {
		i.Config.Entrypoint = []string{"/usr/bin/supervisord"}
	}, func(i *appsImageInfo) {
		i.Config.Cmd = []string{"arbitrary"}
	}, func(i *appsImageInfo) {
		i.Config.Env = []string{"SIDEKICKS_ADAPTER=desktop-novnc"}
	}, func(i *appsImageInfo) {
		i.Config.Env = nil
	}, func(i *appsImageInfo) {
		i.Config.Volumes = map[string]json.RawMessage{"/host": json.RawMessage(`{}`)}
	}} {
		image := validAppsImage(p)
		mutate(&image)
		if err := p.imageGuard(image, "arm64"); err == nil {
			t.Fatal("changed/legacy image accepted")
		}
	}
}
func validAppsOwned(p appsLifecyclePlan, name string) appsOwnedInfo {
	info := appsOwnedInfo{ID: strings.Repeat("c", 64), Name: "/" + name, Image: p.image}
	info.Config.Labels = map[string]string{docker.ManagedLabel: "true", docker.IDLabel: name, docker.AdapterLabel: string(p.adapter), docker.VersionLabel: "1"}
	info.Config.Env = []string{"SIDEKICKS_DESKTOP_START_BROWSER=0"}
	info.HostConfig.NetworkMode = "none"
	info.HostConfig.ReadonlyRootfs = true
	return info
}

// TestAppsLifecycleMutationGuardCannotTargetForeignOrOccupiedGuest verifies apps lifecycle mutation guard cannot target foreign or occupied guest.
func TestAppsLifecycleMutationGuardCannotTargetForeignOrOccupiedGuest(t *testing.T) {
	p, _ := appsGuardPlan(t)
	name := "sidekicks-compute-apps-0123456789abcdef"
	owned := validAppsOwned(p, name)
	if err := p.ownedGuard(owned, name, owned.ID); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*appsOwnedInfo){func(i *appsOwnedInfo) {
		i.ID = strings.Repeat("d", 64)
	}, func(i *appsOwnedInfo) {
		i.Name = "/sidekicks-neko"
	}, func(i *appsOwnedInfo) {
		i.Image = "sha256:" + strings.Repeat("b", 64)
	}, func(i *appsOwnedInfo) {
		i.Config.Labels[docker.ManagedLabel] = "false"
	}, func(i *appsOwnedInfo) {
		i.Config.Labels[docker.IDLabel] = "sidekicks-neko"
	}, func(i *appsOwnedInfo) {
		i.Config.Env = []string{"SIDEKICKS_DESKTOP_START_BROWSER=1"}
	}, func(i *appsOwnedInfo) {
		i.Config.Env = nil
	}, func(i *appsOwnedInfo) {
		i.HostConfig.NetworkMode = "host"
	}, func(i *appsOwnedInfo) {
		i.HostConfig.Privileged = true
	}, func(i *appsOwnedInfo) {
		i.HostConfig.ReadonlyRootfs = false
	}, func(i *appsOwnedInfo) {
		i.HostConfig.Binds = []string{"/home:/host"}
	}, func(i *appsOwnedInfo) {
		i.NetworkSettings.Ports = map[string]json.RawMessage{"6080/tcp": json.RawMessage(`[{"HostIp":"127.0.0.1","HostPort":"1234"}]`)}
	}} {
		changed := validAppsOwned(p, name)
		mutate(&changed)
		if err := p.ownedGuard(changed, name, owned.ID); err == nil {
			t.Fatal("unsafe lifecycle target accepted")
		}
	}
	// Exposed metadata without a host binding is not a published listener.
	owned.NetworkSettings.Ports = map[string]json.RawMessage{"6080/tcp": json.RawMessage(`null`)}
	if err := p.ownedGuard(owned, name, owned.ID); err != nil {
		t.Fatal("unpublished image metadata was treated as exposure", err)
	}
	if err := p.ownedGuard(validAppsOwned(p, "sidekicks-neko"), "sidekicks-neko", owned.ID); err == nil {
		t.Fatal("existing POC name accepted")
	}
}

// TestAppsLifecycleExecutionPlanHasFixedUserDisplayAndExactTest verifies apps lifecycle execution plan has fixed user display and exact test.
func TestAppsLifecycleExecutionPlanHasFixedUserDisplayAndExactTest(t *testing.T) {
	args := appsExecArgs(strings.Repeat("c", 64), "firefox")
	joined := strings.Join(args, " ")
	for _, required := range []string{"--user 1000:1000", "--workdir /workspace", "SIDEKICKS_APPS_TEST_DISPLAY=:99", "SIDEKICKS_APPS_TEST_BROWSER=firefox", "DBUS_SESSION_BUS_ADDRESS=unix:path=/tmp/sidekicks-home/.run/session-bus", "-test.run=^TestRealGuestApps$", "-test.timeout=150s"} {
		if !strings.Contains(joined, required) {
			t.Fatal("lifecycle test lost its fixed scope", required)
		}
	}
	if strings.Contains(joined, "--privileged") || strings.Contains(joined, "docker.sock") || strings.Contains(joined, "--no-sandbox") {
		t.Fatal("harness weakened workload isolation")
	}
	if !reflect.DeepEqual(args[len(args)-4:], []string{"-test.run=^TestRealGuestApps$", "-test.v", "-test.timeout=150s", "-test.coverprofile=/workspace/apps.cover"}) {
		t.Fatal("different tests can execute")
	}
}

// TestAppsDerivativeRequiresIdenticalConfigAndOneExtraLayer verifies apps derivative requires identical config and one extra layer.
func TestAppsDerivativeRequiresIdenticalConfigAndOneExtraLayer(t *testing.T) {
	base := appsLayerInfo{ID: "sha256:" + strings.Repeat("a", 64), Config: map[string]any{"User": "1000:1000"}}
	base.RootFS.Type = "layers"
	base.RootFS.Layers = []string{"sha256:base"}
	derived := appsLayerInfo{ID: "sha256:" + strings.Repeat("b", 64), Config: map[string]any{"User": "1000:1000"}}
	derived.RootFS.Type = "layers"
	derived.RootFS.Layers = []string{"sha256:base", "sha256:test"}
	if err := appsDerivativeGuard(base, derived, base.ID, derived.ID); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*appsLayerInfo){
		func(i *appsLayerInfo) {
			i.Config = map[string]any{"User": "root"}
		},
		func(i *appsLayerInfo) {
			i.RootFS.Layers = []string{"sha256:other", "sha256:test"}
		},
		func(i *appsLayerInfo) {
			i.RootFS.Layers = []string{"sha256:base"}
		},
		func(i *appsLayerInfo) {
			i.ID = "sha256:" + strings.Repeat("c", 64)
		},
	} {
		changed := derived
		mutate(&changed)
		if err := appsDerivativeGuard(base, changed, base.ID, derived.ID); err == nil {
			t.Fatal("unsafe derivative accepted")
		}
	}
}

// TestAppsLifecycleBinaryGuardRejectsScriptsSymlinksAndPrivateFiles verifies apps lifecycle binary guard rejects scripts symlinks and private files.
func TestAppsLifecycleBinaryGuardRejectsScriptsSymlinksAndPrivateFiles(t *testing.T) {
	p, _ := appsGuardPlan(t)
	if err := os.MkdirAll(filepath.Dir(p.binary), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.binary, []byte("#!/bin/sh\necho not-a-Go-test\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.binaryGuard(); err == nil {
		t.Fatal("host script was accepted as a Linux test")
	}
	if err := os.Chmod(p.binary, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.binaryGuard(); err == nil {
		t.Fatal("private non-executable file accepted")
	}
	if err := os.Remove(p.binary); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", p.binary); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.binaryGuard(); err == nil {
		t.Fatal("symlink test payload accepted")
	}
}
