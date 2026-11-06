package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

// TestParsersAndRedaction verifies parsers and redaction.
func TestParsersAndRedaction(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want int
		ok   bool
	}{{`{"config":{}}`, 0, true}, {"{\"config\":{}}\n{\"finding\":{\"osv\":\"GO-123\"}}", 1, true}, {`null`, 0, false}, {``, 0, false}, {`broken`, 0, false}} {
		n, e := goFindingCount([]byte(tt.raw))
		if (e == nil) != tt.ok || n != tt.want {
			t.Fatalf("Go parse %q=%d,%v", tt.raw, n, e)
		}
	}
	for _, tt := range []struct {
		raw  string
		want int
		ok   bool
	}{{`{"metadata":{},"vulnerabilities":{}}`, 0, true}, {`{"metadata":{},"vulnerabilities":{"a":{}}}`, 1, true}, {`{"error":{},"metadata":{},"vulnerabilities":{}}`, 0, false}, {`{}`, 0, false}, {`broken`, 0, false}} {
		n, e := npmFindingCount([]byte(tt.raw))
		if (e == nil) != tt.ok || n != tt.want {
			t.Fatalf("npm parse %q=%d,%v", tt.raw, n, e)
		}
	}
	raw := []byte(`{"SchemaVersion":2,"ArtifactName":"sha256:abc","Metadata":{"OS":{"Family":"debian"},"ImageID":"sha256:abc","ImageConfig":{"config":{"Env":["TOKEN=private-value"],"Cmd":["private-cmd"]}},"Labels":{"credential":"private-label"}},"SecretFindings":["private-file-content"],"Results":[{"Target":"debian","Class":"os-pkgs","Type":"debian","Packages":[{"Name":"bash"}]}]}`)
	safe, e := sanitizeTrivy(raw)
	if e != nil || strings.Contains(string(safe), "private-") || !strings.Contains(string(safe), "bash") {
		t.Fatalf("redaction failed:%s %v", safe, e)
	}
	if _, e = sanitizeTrivy([]byte(`broken`)); e == nil {
		t.Fatal("bad report accepted")
	}
	if _, e = sanitizeTrivy([]byte(`{"Metadata":1}`)); e == nil {
		t.Fatal("bad metadata accepted")
	}
	s := imageScan{Adapter: "desktop-neko", OS: check{Status: "UNSCANNED"}, UpstreamNeko: check{Status: "UNSCANNED"}, PackageCounts: map[string]int{}, FindingCounts: map[string]int{}}
	if e = summarizeTrivy([]byte(`{"Results":[{"Target":"debian","Class":"os-pkgs","Type":"debian","Packages":[{}],"Vulnerabilities":[{"Severity":"HIGH"},{"Severity":"LOW"}]},{"Target":"/usr/bin/neko","Class":"lang-pkgs","Type":"gobinary","Packages":[{}],"Vulnerabilities":[{"Severity":"CRITICAL"}]}]}`), &s); e != nil || s.HighCritical != 2 || s.OS.Status != "COMPLETE" || s.UpstreamNeko.Status != "COMPLETE" {
		t.Fatalf("bad summary %+v %v", s, e)
	}
	if e = summarizeTrivy([]byte(`{"Results":[]}`), &s); e == nil {
		t.Fatal("missing inventory accepted")
	}
	s.UpstreamNeko.Status = "COMPLETE"
	_ = summarizeTrivy([]byte(`{"Results":[{"Class":"os-pkgs","Packages":[{}]}]}`), &s)
	if s.UpstreamNeko.Status != "UNSCANNED" {
		t.Fatal("Neko coverage invented")
	}
}

// TestEvidenceAndBoundedChildEnvironment verifies evidence and bounded child environment.
func TestEvidenceAndBoundedChildEnvironment(t *testing.T) {
	root := t.TempDir()
	if _, e := evidenceDirectory(root, filepath.Join(root, "outside")); e == nil {
		t.Fatal("outside evidence path accepted")
	}
	name := filepath.Join(root, "artifacts/compute/release/test")
	if _, e := evidenceDirectory(root, name); e != nil {
		t.Fatal(e)
	}
	if _, e := evidenceDirectory(root, name); e == nil {
		t.Fatal("existing evidence overwritten")
	}
	b := limitedBuffer{limit: 3}
	n, e := b.Write([]byte("abcdef"))
	if n != 6 || e != nil || b.String() != "abc" || !b.overflow {
		t.Fatal("unbounded writer")
	}
	t.Setenv("NPM_TOKEN", "private-test-credential")
	result := command(context.Background(), root, 5*time.Second, 1<<20, "/usr/bin/env")
	if result.err != nil {
		t.Fatal(result.err)
	}
	env := string(result.stdout)
	if strings.Contains(env, "private-test-credential") || !strings.Contains(env, "sidekicks-audit-config-") || strings.Contains(env, filepath.Join(root, ".run/compute-trivy/home")) || !strings.Contains(env, "TRIVY_DISABLE_TELEMETRY=true") {
		t.Fatal("child environment not isolated")
	}
	result = command(context.Background(), root, time.Second, 2, "/bin/sh", "-c", "printf 'large-output'")
	if result.err == nil || len(result.stdout) != 2 {
		t.Fatal("child output bound missing")
	}
	if _, e := fileSHA(filepath.Join(root, "missing")); e == nil {
		t.Fatal("missing hash accepted")
	}
}
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)
	for _, p := range []string{"go.mod", "go.sum", "Makefile", ".dockerignore", "web/package.json", "web/package-lock.json", "infrastructure/images/assets/desktop/wallpaper.png", "web/embed.go", "web/fixture.go", "web/fixture.html", "infrastructure/images/clean/Dockerfile", "infrastructure/images/desktop-neko/upstream-modules/go.mod", "infrastructure/images/desktop-neko/upstream-modules/go.sum", "infrastructure/images/desktop-neko/upstream-modules/manifest.json", "infrastructure/images/desktop-neko/upstream-modules/README.md", "cmd/a.go", "internal/a.go", "web/desktop/a.mjs", "scripts/compute/a.sh"} {
		p = filepath.Join(root, p)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte("fixture"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	return root
}
func fakeImages() []imageRecord {
	var list []imageRecord
	for i := range adapters {
		var image imageRecord
		image.ID = "sha256:" + strings.Repeat(string(rune('a'+i)), 64)
		image.OS = "linux"
		image.Architecture = "arm64"
		image.Config.User = "1000:1000"
		image.Config.Entrypoint = []string{"/usr/local/bin/sidekicks-agent"}
		list = append(list, image)
	}
	return list
}
func fakeChecks(t *testing.T, root, variant string) {
	t.Helper()
	oldCommand, oldInfo := runCommand, readBuildInfo
	t.Cleanup(func() {
		runCommand = oldCommand
		readBuildInfo = oldInfo
	})
	readBuildInfo = func(name string) (*debug.BuildInfo, error) {
		if variant == "wrong-go-tool" && strings.HasSuffix(name, "govulncheck") {
			return nil, errors.New("missing")
		}
		return &debug.BuildInfo{GoVersion: "go1.27.3", Main: debug.Module{Path: "golang.org/x/vuln", Version: "v1.9.0"}}, nil
	}
	inspections := 0
	runCommand = func(_ context.Context, _ string, _ time.Duration, _ int, name string, args ...string) commandResult {
		joined := strings.Join(args, " ")
		switch {
		case name == "git":
			if args[0] == "rev-parse" {
				return commandResult{stdout: []byte(strings.Repeat("a", 40))}
			}
			return commandResult{stdout: []byte(" M private-file-name\n?? untracked-private-name\n")}
		case name == "docker":
			inspections++
			if variant == "inspect-error" {
				return commandResult{err: errors.New("blocked")}
			}
			list := fakeImages()
			if variant == "changed" && inspections > 1 {
				list[0].ID = "sha256:" + strings.Repeat("f", 64)
			}
			if variant == "bad-image" {
				list[0].Config.User = "0"
			}
			raw, _ := json.Marshal(list)
			var metadata []map[string]any
			_ = json.Unmarshal(raw, &metadata)
			for i, entry := range metadata {
				entry["Config"].(map[string]any)["Env"] = []string{"SIDEKICKS_ADAPTER=" + adapters[i], "TOKEN=private-value"}
			}
			if variant == "default-wrong-adapter" {
				metadata[1]["Config"].(map[string]any)["Env"] = []string{"SIDEKICKS_ADAPTER=shell"}
			}
			if variant == "default-duplicate-id" {
				metadata[1]["Id"] = metadata[0]["Id"]
			}
			raw, _ = json.Marshal(metadata)
			return commandResult{stdout: raw}
		case strings.HasSuffix(name, "govulncheck"):
			if variant == "go-error" {
				return commandResult{err: errors.New("offline")}
			}
			raw := []byte(`{"config":{}}`)
			if variant == "go-finding" {
				raw = []byte(`{"finding":{"osv":"GO-1"}}`)
			}
			return commandResult{stdout: raw}
		case name == "npm":
			if variant == "npm-error" {
				return commandResult{stdout: []byte(`{"error":{}}`), err: errors.New("offline")}
			}
			raw := []byte(`{"metadata":{},"vulnerabilities":{}}`)
			if variant == "npm-finding" {
				raw = []byte(`{"metadata":{},"vulnerabilities":{"a":{}}}`)
			}
			return commandResult{stdout: raw}
		case strings.Contains(joined, "--version"):
			if variant == "wrong-trivy" {
				return commandResult{stdout: []byte("Version: wrong")}
			}
			return commandResult{stdout: []byte("Version: " + trivyVersion)}
		case strings.Contains(joined, "--download-db-only"):
			if !strings.Contains(joined, "--disable-telemetry") || strings.Contains(joined, "--server") {
				t.Fatal("unsafe DB invocation")
			}
			if variant == "db-error" {
				return commandResult{err: errors.New("offline")}
			}
			if variant != "db-missing" {
				for _, p := range []string{"db/trivy.db", "db/metadata.json"} {
					p = filepath.Join(root, ".run/compute-trivy/cache", p)
					_ = os.MkdirAll(filepath.Dir(p), 0700)
					_ = os.WriteFile(p, []byte("database"), 0600)
				}
			}
			return commandResult{}
		case strings.HasSuffix(name, "trivy"):
			if !strings.Contains(joined, "--image-src docker") || !strings.Contains(joined, "--docker-host unix:///tmp/fake.sock") || !strings.Contains(joined, "--disable-telemetry") || !strings.Contains(joined, "--skip-db-update") || !imagePattern.MatchString(args[len(args)-1]) {
				t.Fatal("unsafe scan arguments", joined)
			}
			if variant == "scan-error" {
				return commandResult{err: errors.New("failed")}
			}
			id := args[len(args)-1]
			neko := ""
			if id == fakeImages()[3].ID && variant != "missing-neko" {
				neko = `,{"Target":"usr/bin/neko","Class":"lang-pkgs","Type":"gobinary","Packages":[{}]}`
			}
			finding := ""
			if variant == "os-finding" {
				finding = `,"Vulnerabilities":[{"Severity":"HIGH"}]`
			}
			return commandResult{stdout: []byte(`{"ArtifactName":"` + id + `","Metadata":{"ImageID":"` + id + `","ImageConfig":{"Env":["private-value"]}},"Results":[{"Target":"debian","Class":"os-pkgs","Type":"debian","Packages":[{}]` + finding + `}` + neko + `]}`)}
		default:
			t.Fatal("unexpected command", name, args)
			return commandResult{}
		}
	}
}

// TestFullVerificationOrchestrationWithReadOnlyFakes verifies full verification orchestration with read only fakes.
func TestFullVerificationOrchestrationWithReadOnlyFakes(t *testing.T) {
	for _, variant := range []string{"pass", "manifest", "offline", "inspect-error", "bad-image", "default-wrong-adapter", "default-duplicate-id", "changed", "wrong-go-tool", "go-error", "go-finding", "npm-error", "npm-finding", "wrong-trivy", "db-error", "db-missing", "scan-error", "missing-neko", "os-finding"} {
		t.Run(variant, func(t *testing.T) {
			root := fixture(t)
			fakeChecks(t, root, variant)
			o := options{mode: "verify", socket: "/tmp/fake.sock", online: true}
			if variant == "manifest" {
				o.mode = "manifest"
			}
			if variant == "offline" {
				o.online = false
			}
			e := run(context.Background(), o)
			wantPass := variant == "pass" || variant == "manifest"
			if (e == nil) != wantPass {
				t.Fatalf("%s result:%v", variant, e)
			}
			files, _ := filepath.Glob(filepath.Join(root, "artifacts/compute/release/*/report.json"))
			if len(files) != 1 {
				t.Fatal("report not preserved")
			}
			raw, e := os.ReadFile(files[0])
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(raw), "private-value") || strings.Contains(string(raw), "private-file-name") || strings.Contains(string(raw), "untracked-private-name") {
				t.Fatal("private image config persisted")
			}
			var report report
			if e = json.Unmarshal(raw, &report); e != nil {
				t.Fatal(e)
			}
			if variant == "pass" && report.ReleaseGate != "PASS_CURRENT_ADVISORY_POLICY" {
				t.Fatal(report.ReleaseGate)
			}
			if variant == "missing-neko" && report.Scans[3].UpstreamNeko.Status != "UNSCANNED" {
				t.Fatal("missing binary coverage blessed")
			}
		})
	}
}

// TestBadOptionsAndSources verifies bad options and sources.
func TestBadOptionsAndSources(t *testing.T) {
	if run(context.Background(), options{mode: "unknown"}) == nil {
		t.Fatal("unknown mode")
	}
	if run(context.Background(), options{mode: "classify", imagesProvided: true}) == nil {
		t.Fatal("explicit images accepted by classifier")
	}
	if run(context.Background(), options{mode: "manifest", imagesProvided: true}) == nil {
		t.Fatal("empty explicit selection silently fell back to clean tags")
	}
	if run(context.Background(), options{mode: "manifest", socket: "relative"}) == nil {
		t.Fatal("relative socket")
	}
	root := fixture(t)
	out := map[string]string{}
	if e := sourceHashes(root, out); e != nil || len(out) == 0 {
		t.Fatal(e)
	}
	if e := os.Symlink("a.go", filepath.Join(root, "cmd/link.go")); e != nil {
		t.Fatal(e)
	}
	if sourceHashes(root, map[string]string{}) == nil {
		t.Fatal("source symlink accepted")
	}
	s := scanImage(context.Background(), root, root, "/tmp/fake.sock", root, "shell", "mutable:tag", false)
	if s.Check.Status != "UNAVAILABLE" || !strings.Contains(s.Check.Detail, "Immutable") {
		t.Fatal("mutable tag accepted")
	}
}

// TestExplicitImageSelectionIsExactAndRejectsAmbiguity verifies explicit image selection is exact and rejects ambiguity.
func TestExplicitImageSelectionIsExactAndRejectsAmbiguity(t *testing.T) {
	ids := fakeImages()
	valid := map[string]string{}
	for i, adapter := range adapters {
		valid[adapter] = ids[i].ID
	}
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := parseImageSelection(string(raw), true)
	if err != nil || len(selected) != 4 {
		t.Fatalf("valid selection: %v %v", selected, err)
	}
	for _, adapter := range adapters {
		if selected[adapter] != valid[adapter] {
			t.Fatal("adapter image changed", adapter)
		}
	}
	if selected, err = parseImageSelection("", false); err != nil || selected != nil {
		t.Fatal("default selection changed")
	}
	duplicateKey := `{"shell":"` + ids[0].ID + `","shell":"` + ids[0].ID + `","browser-chromium":"` + ids[1].ID + `","desktop-novnc":"` + ids[2].ID + `","desktop-neko":"` + ids[3].ID + `"}`
	for _, invalid := range []string{
		``, `null`, `[]`, `{}`, `{"shell":"` + ids[0].ID + `"}`, duplicateKey,
		strings.Replace(string(raw), ids[1].ID, ids[0].ID, 1),
		strings.Replace(string(raw), ids[0].ID, "sidekicks-compute/shell:clean", 1),
		strings.Replace(string(raw), ids[0].ID, "sha256:"+strings.Repeat("A", 64), 1),
		strings.Replace(string(raw), `"shell"`, `"unknown"`, 1),
		string(raw) + ` {}`, strings.Repeat(" ", 4097),
	} {
		if _, err := parseImageSelection(invalid, true); err == nil {
			t.Fatalf("accepted invalid image selection: %q", invalid)
		}
	}
}

// TestExplicitImagesInspectScanAndReinspectExactIdentities verifies explicit images inspect scan and reinspect exact identities.
func TestExplicitImagesInspectScanAndReinspectExactIdentities(t *testing.T) {
	for _, variant := range []string{"pass", "manifest", "wrong-id", "wrong-adapter", "missing-adapter", "duplicate-adapter", "changed", "scanner-id", "scanner-missing-id"} {
		t.Run(variant, func(t *testing.T) {
			root := fixture(t)
			fakeChecks(t, root, "pass")
			base := runCommand
			ids := fakeImages()
			selection := map[string]string{}
			for i, adapter := range adapters {
				selection[adapter] = ids[i].ID
			}
			selectionJSON, _ := json.Marshal(selection)
			inspections, scans := 0, 0
			runCommand = func(ctx context.Context, dir string, timeout time.Duration, limit int, name string, args ...string) commandResult {
				result := base(ctx, dir, timeout, limit, name, args...)
				if name == "docker" {
					inspections++
					if len(args) != 8 || args[0] != "--host" || args[2] != "image" || args[3] != "inspect" {
						t.Fatal("unexpected inspect arguments", args)
					}
					for i, adapter := range adapters {
						if args[4+i] != selection[adapter] {
							t.Fatal("inspection used non-selected image", args)
						}
					}
					var list []map[string]any
					if json.Unmarshal(result.stdout, &list) != nil {
						t.Fatal("invalid mock image JSON")
					}
					for i, entry := range list {
						config := entry["Config"].(map[string]any)
						config["Env"] = []string{"SIDEKICKS_ADAPTER=" + adapters[i], "TOKEN=private-value"}
					}
					if variant == "wrong-id" || (variant == "changed" && inspections == 2) {
						list[0]["Id"] = "sha256:" + strings.Repeat("f", 64)
					}
					config := list[0]["Config"].(map[string]any)
					switch variant {
					case "wrong-adapter":
						config["Env"] = []string{"SIDEKICKS_ADAPTER=desktop-neko"}
					case "missing-adapter":
						config["Env"] = []string{"TOKEN=private-value"}
					case "duplicate-adapter":
						config["Env"] = []string{"SIDEKICKS_ADAPTER=shell", "SIDEKICKS_ADAPTER=shell"}
					}
					result.stdout, _ = json.Marshal(list)
				}
				joined := strings.Join(args, " ")
				if strings.HasSuffix(name, "trivy") && !strings.Contains(joined, "--version") && !strings.Contains(joined, "--download-db-only") {
					id := args[len(args)-1]
					if scans >= 4 || id != selection[adapters[scans]] {
						t.Fatal("scan used wrong identity", args)
					}
					scans++
					var obj map[string]any
					if json.Unmarshal(result.stdout, &obj) != nil {
						t.Fatal("invalid mock scan")
					}
					metadata := obj["Metadata"].(map[string]any)
					obj["ArtifactName"], metadata["ImageID"] = id, id
					if variant == "scanner-id" && scans == 1 {
						obj["ArtifactName"] = "sha256:" + strings.Repeat("f", 64)
					}
					if variant == "scanner-missing-id" && scans == 1 {
						delete(metadata, "ImageID")
					}
					result.stdout, _ = json.Marshal(obj)
				}
				return result
			}
			o := options{mode: "verify", socket: "/tmp/fake.sock", online: true, imagesJSON: string(selectionJSON)}
			if variant == "manifest" {
				o.mode = "manifest"
			}
			err := run(context.Background(), o)
			if (err == nil) != (variant == "pass" || variant == "manifest") {
				t.Fatalf("%s: %v", variant, err)
			}
			files, _ := filepath.Glob(filepath.Join(root, "artifacts/compute/release/*/report.json"))
			if len(files) != 1 {
				t.Fatal("report not preserved")
			}
			raw, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "private-value") {
				t.Fatal("private env leaked")
			}
			var report report
			if json.Unmarshal(raw, &report) != nil || report.ImageSelection != "explicit-ids" {
				t.Fatal("explicit selection not reported")
			}
			if variant == "pass" {
				if inspections != 2 || scans != 4 || !report.ImagesUnchanged {
					t.Fatal("incomplete immutable verification", inspections, scans)
				}
				for i, scan := range report.Scans {
					if scan.ImageID != selection[adapters[i]] || scan.ObservedImageID != scan.ImageID || scan.Check.Status != "COMPLETE" {
						t.Fatal("scanner identity not confirmed", scan)
					}
				}
			}
			if variant == "manifest" && (inspections != 2 || scans != 0 || !report.ImagesUnchanged) {
				t.Fatal("manifest did not re-inspect immutable IDs")
			}
			if variant == "changed" && report.ReleaseGate != "INCOMPLETE_IMAGES_CHANGED" {
				t.Fatal("changed explicit image was not an incomplete result", report.ReleaseGate)
			}
			if variant == "scanner-id" || variant == "scanner-missing-id" {
				if report.Scans[0].Check.Status != "UNAVAILABLE" || report.Scans[0].ObservedImageID != "" {
					t.Fatal("unconfirmed scanner identity accepted")
				}
			}
		})
	}
}

// TestSourceManifestIncludesReviewedNekoModuleOverlay verifies source manifest includes reviewed neko module overlay.
func TestSourceManifestIncludesReviewedNekoModuleOverlay(t *testing.T) {
	root := fixture(t)
	const overlay = "infrastructure/images/desktop-neko/upstream-modules/"
	initial := map[string]string{}
	if err := sourceHashes(root, initial); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go.mod", "go.sum", "manifest.json", "README.md"} {
		key := overlay + name
		expected, err := fileSHA(filepath.Join(root, filepath.FromSlash(key)))
		if err != nil || initial[key] != expected || initial[key] == "" {
			t.Fatalf("reviewed overlay input omitted: %s", key)
		}
	}
	key := overlay + "go.mod"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(key)), []byte("reviewed module overlay update"), 0600); err != nil {
		t.Fatal(err)
	}
	changed := map[string]string{}
	if err := sourceHashes(root, changed); err != nil {
		t.Fatal(err)
	}
	if changed[key] == initial[key] {
		t.Fatal("overlay module change absent from provenance")
	}
}
