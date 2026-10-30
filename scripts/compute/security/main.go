// Local release evidence only: no publish, image build, container start or prune.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const trivyVersion = "0.76.1"

var imagePattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// Injection seams are private to deterministic tests; the CLI always uses these defaults.
var runCommand = command
var readBuildInfo = buildinfo.ReadFile

var adapters = []string{"shell", "browser-chromium", "desktop-novnc", "desktop-neko"}

type imageRecord struct {
	ID           string                    `json:"Id"`
	OS           string                    `json:"Os"`
	Architecture string                    `json:"Architecture"`
	Size         int64                     `json:"Size"`
	RepoDigests  []string                  `json:"RepoDigests"`
	RootFS       struct{ Layers []string } `json:"RootFS"`
	Config       struct {
		User       string
		Entrypoint []string
		Cmd        []string
		WorkingDir string
		Env        []string `json:"-"`
		Volumes    map[string]any
	} `json:"Config"`
}
type check struct {
	Status   string `json:"status"`
	Coverage string `json:"coverage"`
	Artifact string `json:"artifact,omitempty"`
	Detail   string `json:"detail,omitempty"`
}
type imageScan struct {
	Adapter         string         `json:"adapter"`
	ImageID         string         `json:"imageId"`
	ObservedImageID string         `json:"observedImageId,omitempty"`
	Check           check          `json:"check"`
	OS              check          `json:"osNative"`
	UpstreamNeko    check          `json:"upstreamNekoGoBinary"`
	PackageCounts   map[string]int `json:"packageCounts,omitempty"`
	FindingCounts   map[string]int `json:"findingCounts,omitempty"`
	HighCritical    int            `json:"highCritical"`
}
type report struct {
	At                 time.Time              `json:"at"`
	Mode               string                 `json:"mode"`
	ImageSelection     string                 `json:"imageSelection"`
	GitCommit          string                 `json:"gitCommit,omitempty"`
	GitDirty           bool                   `json:"gitDirty"`
	GitUntracked       bool                   `json:"gitUntracked"`
	GitStatusAvailable bool                   `json:"gitStatusAvailable"`
	GoVersion          string                 `json:"goVersion"`
	Host               string                 `json:"host"`
	Images             map[string]imageRecord `json:"images"`
	Sources            map[string]string      `json:"sourceSHA256"`
	Inputs             map[string]string      `json:"inputSHA256"`
	AgentBuild         any                    `json:"localAgentBuildInfo,omitempty"`
	OwnedGo            check                  `json:"ownedGo"`
	NPM                check                  `json:"npm"`
	Database           check                  `json:"database"`
	DatabaseHashes     map[string]string      `json:"databaseSHA256,omitempty"`
	Scans              []imageScan            `json:"imageScans,omitempty"`
	ImagesUnchanged    bool                   `json:"imagesUnchanged"`
	ReleaseGate        string                 `json:"releaseGate"`
	Limits             []string               `json:"limits"`
}
type options struct {
	mode, socket, output, imagesJSON string
	online, imagesProvided           bool
}

func main() {
	var o options
	flag.StringVar(&o.mode, "mode", "manifest", "manifest, verify or classify")
	flag.StringVar(&o.socket, "socket", os.Getenv("SIDEKICKS_DOCKER_SOCKET"), "absolute local Docker Unix socket")
	flag.StringVar(&o.output, "output", "", "new evidence directory under artifacts/compute/release")
	flag.StringVar(&o.imagesJSON, "images", "", "optional JSON object mapping all four adapters to distinct local immutable sha256 image IDs")
	flag.BoolVar(&o.online, "online", false, "explicitly query Go/npm advisory services and refresh local Trivy database")
	flag.Parse()
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "images" {
			o.imagesProvided = true
		}
	})
	if err := run(context.Background(), o); err != nil {
		fmt.Fprintln(os.Stderr, "release verification:", err)
		os.Exit(1)
	}
}
func run(ctx context.Context, o options) (runErr error) {
	if o.mode == "classify" {
		if o.imagesProvided || o.imagesJSON != "" {
			return errors.New("-images is not valid for classify")
		}
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		return classifyDirectory(root, o.output)
	}
	if o.mode != "manifest" && o.mode != "verify" {
		return errors.New("mode must be manifest, verify or classify")
	}
	selected, err := parseImageSelection(o.imagesJSON, o.imagesProvided || o.imagesJSON != "")
	if err != nil {
		return err
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if o.socket == "" {
		o.socket = "/var/run/docker.sock"
		if runtime.GOOS == "darwin" {
			home, _ := os.UserHomeDir()
			o.socket = filepath.Join(home, ".docker/run/docker.sock")
		}
	}
	if !filepath.IsAbs(o.socket) || strings.ContainsRune(o.socket, 0) {
		return errors.New("local absolute docker socket required")
	}
	if o.output == "" {
		o.output = filepath.Join(root, "artifacts/compute/release", time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	out, err := evidenceDirectory(root, o.output)
	if err != nil {
		return err
	}
	selectionMode := "clean-tags"
	if selected != nil {
		selectionMode = "explicit-ids"
	}
	r := report{At: time.Now().UTC(), Mode: o.mode, ImageSelection: selectionMode, GoVersion: runtime.Version(), Host: runtime.GOOS + "/" + runtime.GOARCH, Images: map[string]imageRecord{}, Sources: map[string]string{}, Inputs: map[string]string{}, OwnedGo: check{Status: "NOT_RUN", Coverage: "Owned Go source plus reachable standard-library/module advisories, not Debian/native libraries"}, NPM: check{Status: "NOT_RUN", Coverage: "Pinned web package-lock dependency advisories (includes development tooling; not proof that all findings ship in the image)"}, Database: check{Status: "NOT_RUN", Coverage: "Local Trivy vulnerability database, no cloud scan/upload"}, ReleaseGate: "INCOMPLETE", Limits: []string{
		"Image inspection/provenance is not a signature, publisher attestation or proof of runtime isolation.",
		"Local binary/source hashes are build-input evidence; they do not independently prove the exact image agent bytes match those inputs.",
		"Go source scans do not cover OS/native codecs, browsers or all dynamically executed paths; image Gobinary scans identify compiled Go dependencies separately.",
		"Trivy OS/native results follow vendor package advisories and version matching; absence is not proof of exploit absence. Vendored noVNC JS and opaque/minified SDK code are not fully identified by package scanning.",
		"APT is not snapshot-pinned; advisory databases are time-varying. Scan results are tied to immutable image IDs and recorded database hashes, not a permanent safety guarantee.",
		"No container was started, no image was rebuilt, no host firewall/network/security setting was changed, and no image or report was published by this tool.",
	}}
	defer func() {
		if err := writeJSON(filepath.Join(out, "report.json"), r); err != nil {
			runErr = errors.Join(runErr, errors.New("release report could not be preserved"))
		}
		fmt.Println(filepath.Join(out, "report.json"))
	}()
	commit := runCommand(ctx, root, 10*time.Second, 1<<20, "git", "rev-parse", "--verify", "HEAD")
	status := runCommand(ctx, root, 10*time.Second, 1<<20, "git", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "status", "--porcelain", "--untracked-files=normal")
	if commit.err == nil && status.err == nil {
		value := strings.TrimSpace(string(commit.stdout))
		if regexp.MustCompile(`^[a-f0-9]{40,64}$`).MatchString(value) {
			r.GitCommit = value
			r.GitStatusAvailable = true
			r.GitDirty = len(bytes.TrimSpace(status.stdout)) > 0
			for _, line := range strings.Split(string(status.stdout), "\n") {
				if strings.HasPrefix(line, "?? ") {
					r.GitUntracked = true
				}
			}
		}
	}
	if err = sourceHashes(root, r.Sources); err != nil {
		return err
	}
	for _, name := range []string{".run/compute-bin/sidekicks-agent", ".run/compute-web/neko.js", ".run/compute-tools/trivy", ".run/compute-tools/govulncheck"} {
		if h, e := fileSHA(filepath.Join(root, name)); e == nil {
			r.Inputs[name] = h
		}
	}
	if info, e := readBuildInfo(filepath.Join(root, ".run/compute-bin/sidekicks-agent")); e == nil {
		r.AgentBuild = map[string]any{"goVersion": info.GoVersion, "main": info.Main, "dependencies": info.Deps}
	}
	images, err := inspectImages(ctx, root, o.socket, selected)
	if err != nil {
		r.ReleaseGate = "INCOMPLETE_IMAGE_INSPECTION"
		return err
	}
	r.Images = images
	if err = writeJSON(filepath.Join(out, "images.json"), images); err != nil {
		return err
	}
	if o.mode == "manifest" {
		if selected != nil {
			after, inspectErr := inspectImages(ctx, root, o.socket, selected)
			if inspectErr != nil || !sameImages(images, after) {
				r.ReleaseGate = "INCOMPLETE_IMAGES_CHANGED"
				return errors.New("explicit image ids changed or became unavailable during inspection")
			}
		}
		r.ImagesUnchanged = true
		r.ReleaseGate = "MANIFEST_ONLY"
		return nil
	}
	if !o.online {
		r.ReleaseGate = "INCOMPLETE_ADVISORIES_NOT_AUTHORIZED"
		return errors.New("verify requires explicit -online; manifest was preserved")
	}
	r.OwnedGo = goAudit(ctx, root, out)
	r.NPM = npmAudit(ctx, root, out)
	trivy := filepath.Join(root, ".run/compute-tools/trivy")
	version := runCommand(ctx, root, 15*time.Second, 1<<20, trivy, "--version")
	if version.err != nil || !strings.Contains(string(version.stdout), "Version: "+trivyVersion) {
		r.Database.Status = "UNAVAILABLE"
		r.Database.Detail = "Missing/wrong pinned Trivy binary; run make release-tools"
	} else {
		_ = os.WriteFile(filepath.Join(out, "trivy-version.txt"), version.stdout, 0600)
		cache := filepath.Join(root, ".run/compute-trivy/cache")
		db := runCommand(ctx, root, 3*time.Minute, 4<<20, trivy, "image", "--cache-dir", cache, "--db-repository", "ghcr.io/aquasecurity/trivy-db:2", "--download-db-only", "--disable-telemetry", "--skip-version-check", "--timeout", "3m")
		_ = os.WriteFile(filepath.Join(out, "database.log"), append(db.stdout, db.stderr...), 0600)
		if db.err != nil {
			r.Database.Status = "UNAVAILABLE"
			r.Database.Detail = "Database refresh failed; old cache was not silently accepted"
		} else {
			r.Database.Status = "COMPLETE"
			r.Database.Artifact = "database.log"
			r.DatabaseHashes = map[string]string{}
			for _, name := range []string{"db/trivy.db", "db/metadata.json"} {
				if h, e := fileSHA(filepath.Join(cache, name)); e == nil {
					r.DatabaseHashes[name] = h
				} else {
					r.Database.Status = "UNAVAILABLE"
					r.Database.Detail = "Refreshed database metadata/hash unavailable"
				}
			}
			if r.Database.Status == "COMPLETE" {
				for _, adapter := range adapters {
					r.Scans = append(r.Scans, scanImage(ctx, root, out, o.socket, cache, adapter, images[adapter].ID, selected != nil))
				}
			}
		}
	}
	after, err := inspectImages(ctx, root, o.socket, selected)
	r.ImagesUnchanged = err == nil && sameImages(images, after)
	if !r.ImagesUnchanged {
		r.ReleaseGate = "INCOMPLETE_IMAGES_CHANGED"
		return errors.New("selected image references changed or became unavailable during immutable-id verification")
	}
	failed := r.OwnedGo.Status != "COMPLETE_NO_FINDINGS" || r.NPM.Status != "COMPLETE_NO_FINDINGS" || r.Database.Status != "COMPLETE" || len(r.Scans) != 4
	for _, s := range r.Scans {
		if s.Check.Status != "COMPLETE" || s.OS.Status != "COMPLETE" || s.HighCritical > 0 || (s.Adapter == "desktop-neko" && s.UpstreamNeko.Status != "COMPLETE") {
			failed = true
		}
	}
	if failed {
		r.ReleaseGate = "FAIL_FINDINGS_OR_GAPS"
		return errors.New("advisory findings or incomplete coverage require review; see preserved report")
	}
	r.ReleaseGate = "PASS_CURRENT_ADVISORY_POLICY"
	return nil
}
func evidenceDirectory(root, name string) (string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	parent := filepath.Join(root, "artifacts/compute/release")
	rel, err := filepath.Rel(parent, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("evidence must be a new directory beneath artifacts/compute/release")
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return "", err
	}
	if err = os.Mkdir(abs, 0700); err != nil {
		return "", errors.New("refusing to overwrite existing release evidence")
	}
	return abs, nil
}

// parseImageSelection rejects duplicate JSON keys as well as duplicate image
// IDs. The adapter set is exact and caller-provided tags are never accepted.
func parseImageSelection(raw string, provided bool) (map[string]string, error) {
	if !provided {
		return nil, nil
	}
	if len(raw) > 4096 {
		return nil, errors.New("image selection exceeds size bound")
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	start, err := dec.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errors.New("image selection must be a json object")
	}
	allowed := map[string]bool{}
	for _, adapter := range adapters {
		allowed[adapter] = true
	}
	result, ids := map[string]string{}, map[string]bool{}
	for dec.More() {
		key, e := dec.Token()
		name, ok := key.(string)
		if e != nil || !ok || !allowed[name] {
			return nil, errors.New("image selection contains an unknown adapter")
		}
		if _, exists := result[name]; exists {
			return nil, errors.New("image selection contains a duplicate adapter")
		}
		var id string
		if e = dec.Decode(&id); e != nil || !imagePattern.MatchString(id) {
			return nil, errors.New("each selected image must be an exact lowercase sha256 id")
		}
		if ids[id] {
			return nil, errors.New("image selection contains a duplicate image id")
		}
		result[name], ids[id] = id, true
	}
	end, err := dec.Token()
	if err != nil || end != json.Delim('}') || len(result) != len(adapters) {
		return nil, errors.New("image selection must contain all four adapters")
	}
	var trailing any
	if err = dec.Decode(&trailing); err != io.EOF {
		return nil, errors.New("image selection has trailing data")
	}
	return result, nil
}

func inspectImages(ctx context.Context, root, socket string, selected map[string]string) (map[string]imageRecord, error) {
	args := []string{"--host", "unix://" + socket, "image", "inspect"}
	for _, a := range adapters {
		if selected == nil {
			args = append(args, "sidekicks-compute/"+a+":clean")
		} else {
			args = append(args, selected[a])
		}
	}
	result := runCommand(ctx, root, 30*time.Second, 8<<20, "docker", args...)
	if result.err != nil {
		return nil, errors.New("read-only docker image inspection failed")
	}
	var list []imageRecord
	if json.Unmarshal(result.stdout, &list) != nil || len(list) != 4 {
		return nil, errors.New("invalid image metadata")
	}
	// Read adapter identity for every mapping, including default tags. Never persist
	// image environment values in evidence.
	var identities []struct{ Config struct{ Env []string } }
	if json.Unmarshal(result.stdout, &identities) != nil || len(identities) != len(adapters) {
		return nil, errors.New("invalid image adapter metadata")
	}
	images := map[string]imageRecord{}
	seen := map[string]bool{}
	for i, image := range list {
		if !imagePattern.MatchString(image.ID) || image.OS != "linux" || (image.Architecture != "arm64" && image.Architecture != "amd64") || image.Config.User != "1000:1000" || len(image.Config.Volumes) != 0 || len(image.Config.Entrypoint) != 1 || image.Config.Entrypoint[0] != "/usr/local/bin/sidekicks-agent" {
			return nil, errors.New("image metadata violates clean-image contract")
		}
		if seen[image.ID] {
			return nil, errors.New("image identity is shared by multiple adapters")
		}
		seen[image.ID] = true
		if selected != nil {
			if image.ID != selected[adapters[i]] {
				return nil, errors.New("inspected image differs from selected immutable id")
			}
		}
		found := false
		for _, env := range identities[i].Config.Env {
			if strings.HasPrefix(env, "SIDEKICKS_ADAPTER=") {
				if found || env != "SIDEKICKS_ADAPTER="+adapters[i] {
					return nil, errors.New("image adapter identity mismatch")
				}
				found = true
			}
		}
		if !found {
			return nil, errors.New("image adapter identity missing")
		}
		images[adapters[i]] = image
	}
	return images, nil
}
func sameImages(a, b map[string]imageRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k].ID != v.ID {
			return false
		}
	}
	return true
}
func sourceHashes(root string, out map[string]string) error {
	for _, name := range []string{"go.mod", "go.sum", "Makefile", ".dockerignore", "web/package.json", "web/package-lock.json", "infrastructure/images/assets/desktop", "infrastructure/images/clean", "infrastructure/images/desktop-neko/upstream-modules", "cmd", "internal", "web/desktop", "web/embed.go", "web/fixture.html", "scripts/compute"} {
		base := filepath.Join(root, name)
		err := filepath.WalkDir(base, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "__pycache__" || d.Name() == ".cache" || d.Name() == "artifacts" {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return errors.New("source manifest refuses symlinks")
			}
			st, err := d.Info()
			if err != nil {
				return err
			}
			if !st.Mode().IsRegular() || st.Size() > 8<<20 {
				return errors.New("unexpected source manifest input")
			}
			rel, _ := filepath.Rel(root, p)
			h, err := fileSHA(p)
			if err == nil {
				out[filepath.ToSlash(rel)] = h
			}
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
func fileSHA(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func writeJSON(p string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0600)
}

type limitedBuffer struct {
	data     bytes.Buffer
	limit    int
	overflow bool
}

// Len returns the number of buffered bytes.
func (b *limitedBuffer) Len() int {
	return b.data.Len()
}

// Bytes returns the buffered bytes.
func (b *limitedBuffer) Bytes() []byte {
	return b.data.Bytes()
}

// String returns the buffered data as a string.
func (b *limitedBuffer) String() string {
	return b.data.String()
}

// Write appends data subject to the configured byte limit.
func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	room := b.limit - b.Len()
	if len(p) > room {
		b.overflow = true
		p = p[:room]
	}
	_, _ = b.data.Write(p)
	return n, nil
}

type commandResult struct {
	stdout, stderr []byte
	err            error
}

func command(parent context.Context, root string, timeout time.Duration, limit int, name string, args ...string) commandResult {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = root
	c.WaitDelay = time.Second
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Join(root, ".run/compute-trivy/home"), "XDG_CONFIG_HOME=" + filepath.Join(root, ".run/compute-trivy/home/config"), "XDG_CACHE_HOME=" + filepath.Join(root, ".run/compute-trivy/home/cache"), "TRIVY_DISABLE_TELEMETRY=true", "DO_NOT_TRACK=1", "LANG=C", "LC_ALL=C", "GOCACHE=" + filepath.Join(root, ".run/compute-go-cache"), "GOPATH=" + filepath.Join(root, ".run/go"), "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly", "GIT_OPTIONAL_LOCKS=0", "NPM_CONFIG_USERCONFIG=" + filepath.Join(root, ".run/compute-trivy/home/npm-user.conf"), "NPM_CONFIG_GLOBALCONFIG=" + filepath.Join(root, ".run/compute-trivy/home/npm-global.conf"), "NPM_CONFIG_CACHE=" + filepath.Join(root, ".run/compute-npm-cache"), "DOCKER_CONFIG=" + filepath.Join(root, ".run/compute-trivy/docker-config")}
	_ = os.MkdirAll(filepath.Join(root, ".run/compute-trivy/home"), 0700)
	_ = os.MkdirAll(filepath.Join(root, ".run/compute-trivy/docker-config"), 0700)
	c.Env = env
	stdout := limitedBuffer{limit: limit}
	stderr := limitedBuffer{limit: 1 << 20}
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	if stdout.overflow || stderr.overflow {
		err = errors.New("verification output exceeded bound")
	}
	return commandResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), err: err}
}
func goAudit(ctx context.Context, root, out string) check {
	c := check{Status: "UNAVAILABLE", Coverage: "Owned Go source/standard-library reachable advisories; not OS/native libraries", Artifact: "owned-go.json"}
	tool := filepath.Join(root, ".run/compute-tools/govulncheck")
	info, err := readBuildInfo(tool)
	if err != nil || info.Main.Path != "golang.org/x/vuln" || info.Main.Version != "v1.9.0" {
		c.Detail = "Missing/wrong pinned govulncheck v1.9.0"
		return c
	}
	result := runCommand(ctx, root, 3*time.Minute, 16<<20, tool, "-json", "./internal/...", "./cmd/...", "./web/...")
	_ = os.WriteFile(filepath.Join(out, c.Artifact), result.stdout, 0600)
	_ = os.WriteFile(filepath.Join(out, "owned-go.log"), result.stderr, 0600)
	count, parseErr := goFindingCount(result.stdout)
	if parseErr != nil {
		c.Detail = "Invalid/incomplete Go advisory stream"
	} else if count > 0 {
		c.Status = "COMPLETE_FINDINGS"
		c.Detail = fmt.Sprintf("%d reachable findings; review raw stream", count)
	} else if result.err == nil {
		c.Status = "COMPLETE_NO_FINDINGS"
	} else {
		c.Detail = "Go advisory query/tool failed; not a clean scan"
	}
	return c
}
func goFindingCount(b []byte) (int, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	count, events := 0, 0
	for {
		var event map[string]json.RawMessage
		err := d.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
		events++
		if len(event["finding"]) > 0 {
			count++
		}
	}
	if events == 0 {
		return 0, errors.New("empty advisory result")
	}
	return count, nil
}
func npmAudit(ctx context.Context, root, out string) check {
	c := check{Status: "UNAVAILABLE", Coverage: "Web lockfile public npm advisory metadata, including development tooling", Artifact: "npm-audit.json"}
	result := runCommand(ctx, root, 90*time.Second, 16<<20, "npm", "audit", "--json", "--prefix", "web", "--package-lock-only", "--ignore-scripts", "--registry=https://registry.npmjs.org")
	_ = os.WriteFile(filepath.Join(out, c.Artifact), result.stdout, 0600)
	_ = os.WriteFile(filepath.Join(out, "npm-audit.log"), result.stderr, 0600)
	count, err := npmFindingCount(result.stdout)
	if err != nil {
		c.Detail = "npm advisory unavailable/invalid, not a clean scan"
	} else if count > 0 {
		c.Status = "COMPLETE_FINDINGS"
		c.Detail = fmt.Sprintf("%d dependency advisory records; runtime applicability requires review", count)
	} else if result.err == nil {
		c.Status = "COMPLETE_NO_FINDINGS"
	} else {
		c.Detail = "npm tool/query failed"
	}
	return c
}
func npmFindingCount(b []byte) (int, error) {
	var data struct {
		Error           json.RawMessage            `json:"error"`
		Vulnerabilities map[string]json.RawMessage `json:"vulnerabilities"`
		Metadata        json.RawMessage            `json:"metadata"`
	}
	if json.Unmarshal(b, &data) != nil || len(data.Error) > 0 || len(data.Metadata) == 0 || data.Vulnerabilities == nil {
		return 0, errors.New("invalid npm advisory result")
	}
	return len(data.Vulnerabilities), nil
}

type trivyReport struct {
	Results []struct {
		Target, Class, Type string
		Packages            []json.RawMessage
		Vulnerabilities     []struct{ VulnerabilityID, PkgName, InstalledVersion, FixedVersion, Severity, Status string }
	}
}

func scanImage(ctx context.Context, root, out, socket, cache, adapter, id string, requireObservedID bool) imageScan {
	s := imageScan{Adapter: adapter, ImageID: id, Check: check{Status: "UNAVAILABLE", Coverage: "Local immutable image scan; no registry image lookup or cloud upload", Artifact: adapter + "-trivy.json"}, OS: check{Status: "UNSCANNED", Coverage: "Debian OS/native package advisory matching"}, UpstreamNeko: check{Status: "NOT_APPLICABLE", Coverage: "Upstream /usr/bin/neko compiled Go binary (native CGO libraries covered only by OS package scan)"}, PackageCounts: map[string]int{}, FindingCounts: map[string]int{}}
	args := []string{"image", "--image-src", "docker", "--docker-host", "unix://" + socket, "--cache-dir", cache, "--skip-db-update", "--skip-java-db-update", "--disable-telemetry", "--skip-version-check", "--skip-vex-repo-update", "--offline-scan", "--parallel", "1", "--timeout", "5m", "--scanners", "vuln", "--pkg-types", "os,library", "--list-all-pkgs", "--format", "json", id}
	if !imagePattern.MatchString(id) {
		s.Check.Detail = "Immutable image digest required"
		return s
	}
	result := runCommand(ctx, root, 5*time.Minute, 32<<20, filepath.Join(root, ".run/compute-tools/trivy"), args...)
	sanitized, sanitizeErr := sanitizeTrivy(result.stdout)
	if sanitizeErr == nil {
		if err := os.WriteFile(filepath.Join(out, s.Check.Artifact), sanitized, 0600); err != nil {
			s.Check.Detail = "Could not preserve image evidence"
			return s
		}
	}
	_ = os.WriteFile(filepath.Join(out, adapter+"-trivy.log"), result.stderr, 0600)
	if result.err != nil || sanitizeErr != nil {
		s.Check.Detail = "Image scan failed/timed out; not a clean result"
		return s
	}
	if requireObservedID {
		var identity struct {
			ArtifactName string
			Metadata     struct{ ImageID string }
		}
		if json.Unmarshal(result.stdout, &identity) != nil || identity.ArtifactName != id || identity.Metadata.ImageID != id {
			s.Check.Detail = "Scanner output does not confirm the selected image ID"
			return s
		}
		s.ObservedImageID = identity.Metadata.ImageID
	}
	if err := summarizeTrivy(result.stdout, &s); err != nil {
		s.Check.Detail = "Image inventory/advisory JSON invalid"
		return s
	}
	s.Check.Status = "COMPLETE"
	return s
}
func summarizeTrivy(b []byte, s *imageScan) error {
	var result trivyReport
	if json.Unmarshal(b, &result) != nil || len(result.Results) == 0 {
		return errors.New("missing image scan results")
	}
	if s.Adapter == "desktop-neko" {
		s.UpstreamNeko.Status = "UNSCANNED"
	}
	for _, r := range result.Results {
		key := r.Class + ":" + r.Type
		s.PackageCounts[key] += len(r.Packages)
		if r.Class == "os-pkgs" && len(r.Packages) > 0 {
			s.OS.Status = "COMPLETE"
		}
		if r.Type == "gobinary" && (r.Target == "usr/bin/neko" || strings.HasSuffix(r.Target, "/usr/bin/neko")) && len(r.Packages) > 0 {
			s.UpstreamNeko.Status = "COMPLETE"
			s.UpstreamNeko.Artifact = s.Check.Artifact
		}
		for _, v := range r.Vulnerabilities {
			s.FindingCounts[v.Severity]++
			if v.Severity == "HIGH" || v.Severity == "CRITICAL" {
				s.HighCritical++
			}
		}
	}
	return nil
}

// Persist only scan metadata/package advisories, never full image config Env,
// command lines, labels, credentials or raw inspected file contents.
func sanitizeTrivy(raw []byte) ([]byte, error) {
	var data map[string]json.RawMessage
	if json.Unmarshal(raw, &data) != nil {
		return nil, errors.New("invalid scanner json")
	}
	safe := map[string]json.RawMessage{}
	for _, key := range []string{"SchemaVersion", "CreatedAt", "ArtifactName", "ArtifactType", "Results"} {
		if value, ok := data[key]; ok {
			safe[key] = value
		}
	}
	var metadata map[string]json.RawMessage
	if value, ok := data["Metadata"]; ok {
		if json.Unmarshal(value, &metadata) != nil {
			return nil, errors.New("invalid image metadata")
		}
	}
	selected := map[string]json.RawMessage{}
	for _, key := range []string{"OS", "ImageID", "DiffIDs", "RepoTags", "RepoDigests", "Size"} {
		if value, ok := metadata[key]; ok {
			selected[key] = value
		}
	}
	safe["Metadata"], _ = json.Marshal(selected)
	return json.MarshalIndent(safe, "", "  ")
}
