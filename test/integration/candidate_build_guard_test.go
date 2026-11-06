package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const candidateFixtureRun = "0123456789abcdef0123456789abcdef"

// Every external executable is replaced in this fixture. It cannot contact
// Docker or change any real image; temporary files are the only side effects.
func runCandidateFixture(t *testing.T, scenario, runID string) (string, string, error) {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"scripts/compute", ".run/compute-bin", ".run/compute-web", "infrastructure/images/clean", "fake-bin"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "preexisting" {
		location := filepath.Join(root, "artifacts/compute/candidate-build", runID)
		if err := os.MkdirAll(location, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(location, "sentinel"), []byte("retain"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	script, err := os.ReadFile("../../scripts/compute/build-candidates.sh")
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := os.ReadFile("../../scripts/compute/source-fingerprint.sh")
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"scripts/compute/build-candidates.sh":    string(script),
		"scripts/compute/source-fingerprint.sh":  string(fingerprint),
		".run/compute-bin/sidekicks-agent":       "fake-agent",
		".run/compute-web/neko.js":               "fake-web",
		"infrastructure/images/clean/Dockerfile": "fake recipe",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0700); err != nil {
			t.Fatal(err)
		}
	}
	gitScript := `#!/bin/bash
set -eu
if [[ "${1:-}" == --no-pager ]]; then shift; fi
case "${1:-}" in
  rev-parse) printf '%040d\n' 1 ;;
  diff) if [[ "${CANDIDATE_TEST_CASE:-}" == bad-diff ]]; then exit 9; fi; if [[ "${CANDIDATE_TEST_CASE:-}" == drift ]] && [[ -f "$CANDIDATE_TEST_TRACE" ]] && grep -q '^BUILD shell ' "$CANDIDATE_TEST_TRACE"; then printf 'changed'; fi ;;
  ls-files) if [[ "${CANDIDATE_TEST_CASE:-}" == bad-source ]]; then exit 9; fi ;;
  status) ;;
  *) echo "unexpected fake git command" >&2; exit 2 ;;
esac
`
	dockerScript := `#!/bin/bash
set -eu
[[ "${1:-}" == --host ]] || exit 90
shift 2
case "${1:-} ${2:-}" in
  'image ls')
    if [[ "${CANDIDATE_TEST_CASE:-}" == unavailable ]]; then exit 7; fi
    if [[ "${CANDIDATE_TEST_CASE:-}" == collision ]]; then printf 'sidekicks-compute/shell:verify-%s\n' "$SIDEKICKS_CANDIDATE_RUN_ID"; fi
    if [[ "${CANDIDATE_TEST_CASE:-}" == midcollision && -f "$CANDIDATE_TEST_TRACE" ]] && grep -q '^BUILD shell ' "$CANDIDATE_TEST_TRACE"; then printf 'sidekicks-compute/browser-chromium:verify-%s\n' "$SIDEKICKS_CANDIDATE_RUN_ID"; fi
    ;;
  'image inspect')
    if [[ "${CANDIDATE_TEST_CASE:-}" == inspect-error ]]; then exit 8; fi
    if [[ "$*" == *'{{.Os}}/{{.Architecture}}'* ]]; then
      if [[ "${CANDIDATE_TEST_CASE:-}" == wrong-platform ]]; then printf 'linux/amd64\n'; else printf 'linux/arm64\n'; fi
      exit 0
    fi
    ref="${@: -1}"
    case "$ref" in
      sha256:*) printf '%s\n' "$ref" ;;
      sidekicks-compute/*:verify-*)
        adapter="${ref#sidekicks-compute/}"; adapter="${adapter%%:*}"
        if [[ "${CANDIDATE_TEST_CASE:-}" == tag-mismatch ]]; then printf 'sha256:%064d\n' 9; else cat "artifacts/compute/candidate-build/$SIDEKICKS_CANDIDATE_RUN_ID/$adapter.iid"; fi ;;
      *) exit 91 ;;
    esac
    ;;
  build*)
    target=''; tag=''; iid=''
    while [[ $# -gt 0 ]]; do
      case "$1" in
        --target) target="$2"; shift 2 ;;
        --iidfile) iid="$2"; shift 2 ;;
        -t) tag="$2"; shift 2 ;;
        *) shift ;;
      esac
    done
    printf 'BUILD %s %s\n' "$target" "$tag" >> "$CANDIDATE_TEST_TRACE"
    if [[ "${CANDIDATE_TEST_CASE:-}" == partial && "$target" == browser-chromium ]]; then exit 42; fi
    case "$target" in shell) n=1;; browser-chromium) n=2;; desktop-novnc) n=3;; desktop-neko) n=4;; *) exit 92;; esac
    if [[ "${CANDIDATE_TEST_CASE:-}" == duplicate-id && "$target" == browser-chromium ]]; then n=1; fi
    if [[ "${CANDIDATE_TEST_CASE:-}" == bad-iid ]]; then printf 'invalid\n' > "$iid"; else printf 'sha256:%064d\n' "$n" > "$iid"; fi
    ;;
  *) echo "unexpected fake docker command" >&2; exit 93 ;;
esac
`
	goScript := `#!/bin/bash
set -eu
[[ "${1:-}" == version && "${2:-}" == -m ]] || exit 94
if [[ "${CANDIDATE_TEST_CASE:-}" == wrong-agent ]]; then cpu=amd64; else cpu=arm64; fi
printf 'fake\n\tbuild\tGOOS=linux\n\tbuild\tGOARCH=%s\n\tbuild\tCGO_ENABLED=0\n' "$cpu"
if [[ "${CANDIDATE_TEST_CASE:-}" == wrong-vcs ]]; then revision=2; else revision=1; fi
printf '\tbuild\tvcs.revision=%040d\n' "$revision"
printf '\tbuild\tvcs.modified=false\n'
`
	for name, content := range map[string]string{"git": gitScript, "docker": dockerScript, "go": goScript} {
		if err := os.WriteFile(filepath.Join(root, "fake-bin", name), []byte(content), 0700); err != nil {
			t.Fatal(err)
		}
	}
	trace := filepath.Join(root, "docker-trace")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	shell := "bash"
	if scenario == "system-bash" {
		shell = "/bin/bash"
	}
	cmd := exec.CommandContext(ctx, shell, filepath.Join(root, "scripts/compute/build-candidates.sh"))
	cmd.Dir = root
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "PATH=") && !strings.HasPrefix(entry, "SIDEKICKS_") && !strings.HasPrefix(entry, "CANDIDATE_TEST_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "PATH="+filepath.Join(root, "fake-bin")+string(os.PathListSeparator)+os.Getenv("PATH"), "SIDEKICKS_IMAGE_ARCH=arm64", "SIDEKICKS_DOCKER_SOCKET=/tmp/fixture.sock", "SIDEKICKS_CANDIDATE_RUN_ID="+runID, "CANDIDATE_TEST_CASE="+scenario, "CANDIDATE_TEST_TRACE="+trace)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("candidate fixture timed out: %s", output)
	}
	return root, string(output), err
}

// TestCandidateBuildCreatesOnlyUniqueTagsAndImmutableManifest verifies candidate build creates only unique tags and immutable manifest.
func TestCandidateBuildCreatesOnlyUniqueTagsAndImmutableManifest(t *testing.T) {
	root, output, err := runCandidateFixture(t, "success", candidateFixtureRun)
	if err != nil {
		t.Fatalf("candidate fixture: %v\n%s", err, output)
	}
	base := filepath.Join(root, "artifacts/compute/candidate-build", candidateFixtureRun)
	raw, err := os.ReadFile(filepath.Join(base, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Status, RunID, Architecture, GitRevision, SourceFingerprintSHA256 string
		Images                                                            []struct{ Adapter, Tag, ID string }
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Status != "built-not-verified" || manifest.RunID != candidateFixtureRun || manifest.Architecture != "arm64" || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(manifest.GitRevision) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(manifest.SourceFingerprintSHA256) || len(manifest.Images) != 4 {
		t.Fatalf("invalid manifest: %+v", manifest)
	}
	wanted := map[string]bool{"shell": true, "browser-chromium": true, "desktop-novnc": true, "desktop-neko": true}
	for _, image := range manifest.Images {
		if !wanted[image.Adapter] || image.Tag != "sidekicks-compute/"+image.Adapter+":verify-"+candidateFixtureRun || !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(image.ID) {
			t.Fatalf("invalid image identity: %+v", image)
		}
		delete(wanted, image.Adapter)
	}
	trace, err := os.ReadFile(filepath.Join(root, "docker-trace"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(trace), "BUILD ") != 4 || strings.Contains(string(trace), ":clean") || strings.Contains(string(trace), ":dev") {
		t.Fatalf("unexpected image changes: %s", trace)
	}
}

// TestCandidateBuildFailsClosedBeforeOrDuringPartialBuild verifies candidate build fails closed before or during partial build.
func TestCandidateBuildFailsClosedBeforeOrDuringPartialBuild(t *testing.T) {
	for _, scenario := range []string{"collision", "unavailable", "partial", "drift", "midcollision", "bad-iid", "tag-mismatch", "inspect-error", "bad-source", "bad-diff", "wrong-platform", "wrong-agent", "wrong-vcs", "duplicate-id"} {
		t.Run(scenario, func(t *testing.T) {
			root, output, err := runCandidateFixture(t, scenario, candidateFixtureRun)
			if err == nil {
				t.Fatalf("%s unexpectedly passed: %s", scenario, output)
			}
			base := filepath.Join(root, "artifacts/compute/candidate-build", candidateFixtureRun)
			if _, err := os.Stat(filepath.Join(base, "manifest.json")); !os.IsNotExist(err) {
				t.Fatalf("%s published a manifest: %v", scenario, err)
			}
			if _, err := os.Stat(filepath.Join(base, "FAILED")); err != nil {
				t.Fatalf("%s lost failure evidence: %v", scenario, err)
			}
			trace, _ := os.ReadFile(filepath.Join(root, "docker-trace"))
			if (scenario == "collision" || scenario == "unavailable" || scenario == "bad-source" || scenario == "bad-diff" || scenario == "wrong-agent" || scenario == "wrong-vcs") && len(trace) != 0 {
				t.Fatalf("%s began a build: %s", scenario, trace)
			}
			if scenario == "partial" && (strings.Count(string(trace), "BUILD ") != 2 || strings.Contains(string(trace), "desktop-novnc")) {
				t.Fatalf("partial failure continued: %s", trace)
			}
			if scenario == "duplicate-id" && (strings.Count(string(trace), "BUILD ") != 2 || strings.Contains(string(trace), "desktop-novnc")) {
				t.Fatalf("duplicate identity was accepted: %s", trace)
			}
			if (scenario == "drift" || scenario == "midcollision" || scenario == "bad-iid" || scenario == "tag-mismatch" || scenario == "inspect-error" || scenario == "wrong-platform") && strings.Count(string(trace), "BUILD ") != 1 {
				t.Fatalf("source drift continued: %s", trace)
			}
		})
	}
}

// TestCandidateBuildWorksWithSystemBash verifies candidate build works with system bash.
func TestCandidateBuildWorksWithSystemBash(t *testing.T) {
	_, output, err := runCandidateFixture(t, "system-bash", candidateFixtureRun)
	if err != nil {
		t.Fatalf("system bash candidate fixture: %v\n%s", err, output)
	}
}

// TestCandidateBuildDoesNotOverwriteExistingEvidence verifies candidate build does not overwrite existing evidence.
func TestCandidateBuildDoesNotOverwriteExistingEvidence(t *testing.T) {
	root, _, err := runCandidateFixture(t, "preexisting", candidateFixtureRun)
	if err == nil {
		t.Fatal("existing evidence directory accepted")
	}
	raw, err := os.ReadFile(filepath.Join(root, "artifacts/compute/candidate-build", candidateFixtureRun, "sentinel"))
	if err != nil || string(raw) != "retain" {
		t.Fatalf("existing evidence changed: %q %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(root, "docker-trace")); !os.IsNotExist(err) {
		t.Fatal("existing evidence check invoked Docker")
	}
}

// TestCandidateBuildRejectsUnsafeRunID verifies candidate build rejects unsafe run id.
func TestCandidateBuildRejectsUnsafeRunID(t *testing.T) {
	root, _, err := runCandidateFixture(t, "success", "../../unsafe")
	if err == nil {
		t.Fatal("unsafe run ID accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "docker-trace")); !os.IsNotExist(err) {
		t.Fatal("unsafe run ID invoked docker")
	}
}
