// Package docker implements compute isolation using a local Docker Engine.
// It never pulls images, mounts host paths, or manages unlabelled containers.
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kevinmartin/sidekicks/internal/compute"
)

const (
	api = "/v1.47"
	// Prefix identifies names reserved for compute instances.
	Prefix = "sidekicks-compute-"
	// ManagedLabel identifies containers managed by this runtime.
	ManagedLabel = "io.sidekicks.compute.managed"
	// IDLabel records the owned compute instance ID.
	IDLabel = "io.sidekicks.compute.id"
	// AdapterLabel records the workload adapter ID.
	AdapterLabel = "io.sidekicks.compute.adapter"
	// VersionLabel records the ownership-label schema version.
	VersionLabel = "io.sidekicks.compute.version"
	maxResponse  = 1 << 20
)

var (
	idPattern     = regexp.MustCompile(`^sidekicks-compute-[a-z0-9][a-z0-9-]{0,47}$`)
	imagePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]{0,254}$`)
	envPattern    = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// Runtime uses only a local Engine; no registry credentials or Docker socket
// are passed to workloads.
type Runtime struct{ client *http.Client }

type unavailableTransport struct{}

// RoundTrip returns an error for an unavailable Docker transport.
func (unavailableTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("docker transport was not configured")
}

// New constructs a Unix-socket Engine client. An empty path selects the standard
// /var/run/docker.sock. DOCKER_HOST and other process environment overrides are
// intentionally ignored; callers must explicitly select a local socket.
func New(socketPath string) (*Runtime, error) {
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}
	if !filepath.IsAbs(socketPath) || strings.ContainsRune(socketPath, '\x00') {
		return nil, fmt.Errorf("%w: docker socket must be an absolute local path", compute.ErrInvalid)
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socketPath)
		},
		DisableCompression:  true,
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     30 * time.Second,
	}
	return NewWithHTTPClient(&http.Client{Transport: transport, Timeout: 30 * time.Second}), nil
}

// NewWithHTTPClient injects an Engine transport (for example a recording fake).
// Requests use a fixed dummy authority and no user-controlled URL or redirects.
func NewWithHTTPClient(client *http.Client) *Runtime {
	if client == nil {
		client = &http.Client{Transport: unavailableTransport{}, Timeout: 30 * time.Second}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if copyClient.Timeout == 0 {
		copyClient.Timeout = 30 * time.Second
	}
	return &Runtime{client: &copyClient}
}

// ID returns the Docker runtime ID.
func (*Runtime) ID() compute.RuntimeID {
	return compute.Docker
}

// Validate rejects requests that cannot meet this backend's isolation contract.
// All limits are explicit; zero resource limits are not silently unlimited.
func Validate(s compute.Spec) error {
	invalid := func(s string) error {
		return fmt.Errorf("%w: %s", compute.ErrInvalid, s)
	}
	if !idPattern.MatchString(s.ID) {
		return invalid("instance id must use sidekicks-compute- prefix and a lowercase suffix")
	}
	switch s.Adapter {
	case compute.Shell, compute.BrowserChromium, compute.DesktopNoVNC, compute.DesktopNeko:
	default:
		return invalid("unknown adapter")
	}
	if !imagePattern.MatchString(s.Image) || strings.Contains(s.Image, "..") {
		return invalid("invalid local image reference")
	}
	if s.Resources.CPUs < 1 || s.Resources.CPUs > 8 {
		return invalid("cpus must be 1..8")
	}
	if s.Resources.MemoryBytes < 64<<20 || s.Resources.MemoryBytes > 16<<30 {
		return invalid("memory must be 64 mebibytes..16 gibibytes")
	}
	if s.Resources.PIDs < 16 || s.Resources.PIDs > 4096 {
		return invalid("pid limit must be 16..4096")
	}
	if s.Network != compute.NetworkNone && s.Network != compute.NetworkEgress {
		return invalid("network must be explicitly none or egress")
	}
	if s.Network == compute.NetworkNone && len(s.Ports) > 0 {
		return invalid("published ports require egress networking")
	}
	if len(s.Ports) > 64 {
		return invalid("too many published ports")
	}
	seen := map[string]bool{}
	host := map[string]bool{}
	for _, p := range s.Ports {
		if p.Guest == 0 || (p.Protocol != "tcp" && p.Protocol != "udp") {
			return invalid("invalid guest port/protocol")
		}
		k := portKey(p.Guest, p.Protocol)
		if seen[k] {
			return invalid("duplicate guest port")
		}
		seen[k] = true
		if p.HostPort > 0 {
			k = portKey(p.HostPort, p.Protocol)
			if host[k] {
				return invalid("duplicate host port")
			}
			host[k] = true
		}
	}
	if len(s.Env) > 32 {
		return invalid("too many environment variables")
	}
	for k, v := range s.Env {
		if !envPattern.MatchString(k) || (!strings.HasPrefix(k, "SIDEKICKS_") && k != "LANG" && k != "LC_ALL" && k != "TZ") || len(v) > 16384 || strings.ContainsRune(v, '\x00') {
			return invalid("invalid private environment configuration")
		}
	}
	return nil
}

type portBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}
type createBody struct {
	Image        string
	User         string
	Env          []string
	Labels       map[string]string
	ExposedPorts map[string]struct{}
	HostConfig   hostConfig
}
type hostConfig struct {
	NetworkMode    string
	ReadonlyRootfs bool
	Privileged     bool
	CapDrop        []string
	SecurityOpt    []string
	NanoCpus       int64
	Memory         int64
	MemorySwap     int64
	PidsLimit      int64
	ShmSize        int64
	Tmpfs          map[string]string
	PortBindings   map[string][]portBinding
	Init           bool
	RestartPolicy  struct{ Name string }
}

type engineContainer struct {
	ID              string `json:"Id"`
	Name            string
	Image           string
	Created         string
	Config          struct{ Labels map[string]string }
	State           struct{ Status, StartedAt string }
	NetworkSettings struct{ Ports map[string][]portBinding }
}

func portKey(port uint16, protocol string) string {
	return strconv.Itoa(int(port)) + "/" + protocol
}

// ProvisionError reports the safe caller-chosen instance identity and whether
// cleanup of the newly-created immutable object was confirmed. Creation errors
// without a valid returned object ID are deliberately not reconciled by name:
// an identically-named pre-existing or replacement object must never be deleted.
type ProvisionError struct {
	InstanceID    string
	Cause         error
	CleanupStatus string
	CleanupErr    error
}

// Error describes provisioning failure and cleanup status.
func (e *ProvisionError) Error() string {
	message := fmt.Sprintf("instance %s: provision failed: %v; cleanup %s", e.InstanceID, e.Cause, e.CleanupStatus)
	if e.CleanupErr != nil {
		message += ": " + e.CleanupErr.Error()
	}
	return message
}

// Unwrap returns the underlying provisioning error.
func (e *ProvisionError) Unwrap() error {
	return e.Cause
}

// Provision creates and starts a fresh labelled container from an already-local
// image. Every failure after a valid created ID triggers independently bounded,
// identity-verified cleanup of that exact object, including caller cancellation.
func (r *Runtime) Provision(ctx context.Context, s compute.Spec) (compute.Instance, error) {
	if err := Validate(s); err != nil {
		return compute.Instance{}, err
	}
	var image struct {
		ID     string `json:"Id"`
		Config struct{ Volumes map[string]json.RawMessage }
	}
	if err := r.request(ctx, http.MethodGet, "/images/"+url.PathEscape(s.Image)+"/json", nil, &image); err != nil {
		return compute.Instance{}, fmt.Errorf("local image unavailable: %w", err)
	}
	if !digestPattern.MatchString(image.ID) {
		return compute.Instance{}, errors.New("docker returned an invalid local image identity")
	}
	if len(image.Config.Volumes) != 0 {
		return compute.Instance{}, fmt.Errorf("%w: image declares implicit writable volumes", compute.ErrInvalid)
	}
	body := createBody{
		Image: image.ID, User: "1000:1000", Env: []string{},
		Labels:       map[string]string{ManagedLabel: "true", IDLabel: s.ID, AdapterLabel: string(s.Adapter), VersionLabel: "1"},
		ExposedPorts: map[string]struct{}{},
		HostConfig: hostConfig{
			NetworkMode: string(s.Network), ReadonlyRootfs: true, Privileged: false,
			CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges:true"},
			NanoCpus: int64(s.Resources.CPUs) * 1_000_000_000,
			Memory:   s.Resources.MemoryBytes, MemorySwap: s.Resources.MemoryBytes, PidsLimit: s.Resources.PIDs, ShmSize: 512 << 20,
			Tmpfs: map[string]string{
				"/tmp":       "rw,nosuid,nodev,size=268435456,mode=1777",
				"/run":       "rw,nosuid,nodev,noexec,size=16777216,mode=0755,uid=1000,gid=1000",
				"/workspace": "rw,nosuid,nodev,size=536870912,mode=0700,uid=1000,gid=1000",
			},
			PortBindings: map[string][]portBinding{}, Init: true,
		},
	}
	if s.Network == compute.NetworkEgress {
		body.HostConfig.NetworkMode = "bridge"
	}
	body.HostConfig.RestartPolicy.Name = "no"
	keys := make([]string, 0, len(s.Env))
	for k := range s.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		body.Env = append(body.Env, k+"="+s.Env[k])
	}
	for _, p := range s.Ports {
		key := portKey(p.Guest, p.Protocol)
		body.ExposedPorts[key] = struct{}{}
		hostPort := ""
		if p.HostPort != 0 {
			hostPort = strconv.Itoa(int(p.HostPort))
		}
		body.HostConfig.PortBindings[key] = []portBinding{{HostIP: "127.0.0.1", HostPort: hostPort}}
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := r.request(ctx, http.MethodPost, "/containers/create?name="+url.QueryEscape(s.ID), body, &created); err != nil {
		return compute.Instance{}, &ProvisionError{InstanceID: s.ID, Cause: err, CleanupStatus: "not attempted; creation identity unavailable"}
	}
	if !engineID(created.ID) {
		return compute.Instance{}, &ProvisionError{InstanceID: s.ID, Cause: errors.New("docker returned an invalid created container identity"), CleanupStatus: "not attempted; creation identity unavailable"}
	}
	failed := func(cause error) (compute.Instance, error) {
		status, cleanupErr := r.cleanupCreated(ctx, s.ID, created.ID, s.Adapter)
		return compute.Instance{}, &ProvisionError{InstanceID: s.ID, Cause: cause, CleanupStatus: status, CleanupErr: cleanupErr}
	}
	current, err := r.inspectOwned(ctx, s.ID)
	if err != nil {
		return failed(fmt.Errorf("verify created container: %w", err))
	}
	if current.ID != created.ID || compute.AdapterID(current.Config.Labels[AdapterLabel]) != s.Adapter {
		return failed(fmt.Errorf("%w: created container identity changed", compute.ErrConflict))
	}
	if err = r.request(ctx, http.MethodPost, "/containers/"+created.ID+"/start", nil, nil); err != nil {
		return failed(err)
	}
	current, err = r.inspectOwned(ctx, s.ID)
	if err != nil {
		return failed(fmt.Errorf("inspect started container: %w", err))
	}
	if current.ID != created.ID || compute.AdapterID(current.Config.Labels[AdapterLabel]) != s.Adapter || current.State.Status != "running" {
		return failed(fmt.Errorf("%w: started container identity changed", compute.ErrConflict))
	}
	result, err := instance(current, s.ID)
	if err != nil {
		return failed(err)
	}
	return result, nil
}

// cleanupCreated never resolves a cleanup target by name. Labels/name are
// freshly verified on the returned immutable engine ID before a force deletion.
// It is safe to kill this one newly-provisioned guest after uncertain startup;
// it is not safe to infer ownership from another object's reused display name.
func (r *Runtime) cleanupCreated(caller context.Context, name, createdID string, adapter compute.AdapterID) (string, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(caller), 5*time.Second)
	defer cancel()
	var c engineContainer
	err := r.request(ctx, http.MethodGet, "/containers/"+createdID+"/json", nil, &c)
	if errors.Is(err, compute.ErrNotFound) {
		return "confirmed absent", nil
	}
	if err != nil {
		return "unconfirmed", err
	}
	if err = validateOwnership(c, name); err != nil {
		return "unconfirmed", err
	}
	if c.ID != createdID || compute.AdapterID(c.Config.Labels[AdapterLabel]) != adapter {
		return "unconfirmed", fmt.Errorf("%w: immutable cleanup identity mismatch", compute.ErrConflict)
	}
	err = r.request(ctx, http.MethodDelete, "/containers/"+createdID+"?force=true&v=false", nil, nil)
	if errors.Is(err, compute.ErrNotFound) {
		return "confirmed absent", nil
	}
	if err != nil {
		return "unconfirmed", err
	}
	return "confirmed removed", nil
}

// Inspect returns the current state of an owned container.
func (r *Runtime) Inspect(ctx context.Context, id string) (compute.Instance, error) {
	c, err := r.inspectOwned(ctx, id)
	if err != nil {
		return compute.Instance{}, err
	}
	return instance(c, id)
}

// Start starts an owned container and verifies its identity.
func (r *Runtime) Start(ctx context.Context, id string) (compute.Instance, error) {
	c, err := r.inspectOwned(ctx, id)
	if err != nil {
		return compute.Instance{}, err
	}
	if err = r.request(ctx, http.MethodPost, "/containers/"+c.ID+"/start", nil, nil); err != nil {
		return compute.Instance{}, err
	}
	current, err := r.inspectOwned(ctx, id)
	if err != nil {
		return compute.Instance{}, err
	}
	if current.ID != c.ID || current.Config.Labels[AdapterLabel] != c.Config.Labels[AdapterLabel] || current.State.Status != "running" {
		return compute.Instance{}, fmt.Errorf("%w: started container identity or state changed", compute.ErrConflict)
	}
	return instance(current, id)
}

// Stop stops an owned container.
func (r *Runtime) Stop(ctx context.Context, id string) error {
	c, err := r.inspectOwned(ctx, id)
	if errors.Is(err, compute.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	err = r.request(ctx, http.MethodPost, "/containers/"+c.ID+"/stop?t=10", nil, nil)
	if errors.Is(err, compute.ErrNotFound) {
		return nil
	}
	return err
}

// Destroy removes a stopped owned container.
func (r *Runtime) Destroy(ctx context.Context, id string) error {
	c, err := r.inspectOwned(ctx, id)
	if errors.Is(err, compute.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	// Force=false: the caller must stop first; destroying a running workload is
	// an explicit conflict, never an implicit force kill.
	err = r.request(ctx, http.MethodDelete, "/containers/"+c.ID+"?force=false&v=false", nil, nil)
	if errors.Is(err, compute.ErrNotFound) {
		return nil
	}
	return err
}

func (r *Runtime) inspectOwned(ctx context.Context, id string) (engineContainer, error) {
	var c engineContainer
	if !idPattern.MatchString(id) {
		return c, fmt.Errorf("%w: invalid owned instance id", compute.ErrInvalid)
	}
	if err := r.request(ctx, http.MethodGet, "/containers/"+id+"/json", nil, &c); err != nil {
		return c, err
	}
	if err := validateOwnership(c, id); err != nil {
		return engineContainer{}, err
	}
	return c, nil
}

func validateOwnership(c engineContainer, id string) error {
	l := c.Config.Labels
	if !engineID(c.ID) || c.Name != "/"+id || l[ManagedLabel] != "true" || l[IDLabel] != id || l[VersionLabel] != "1" {
		return fmt.Errorf("%w: container ownership identity mismatch", compute.ErrConflict)
	}
	switch compute.AdapterID(l[AdapterLabel]) {
	case compute.Shell, compute.BrowserChromium, compute.DesktopNoVNC, compute.DesktopNeko:
	default:
		return fmt.Errorf("%w: invalid owned adapter identity", compute.ErrConflict)
	}
	return nil
}

func engineID(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func instance(c engineContainer, id string) (compute.Instance, error) {
	created, err := time.Parse(time.RFC3339Nano, c.Created)
	if err != nil {
		return compute.Instance{}, errors.New("docker returned invalid creation time")
	}
	result := compute.Instance{ID: id, Runtime: compute.Docker, Adapter: compute.AdapterID(c.Config.Labels[AdapterLabel]), State: c.State.Status, Image: c.Image, CreatedAt: created}
	keys := make([]string, 0, len(c.NetworkSettings.Ports))
	for k := range c.NetworkSettings.Ports {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		guestPort, protocol, found := strings.Cut(k, "/")
		if !found || (protocol != "tcp" && protocol != "udp") {
			return compute.Instance{}, errors.New("docker returned invalid port protocol")
		}
		guest, err := strconv.ParseUint(guestPort, 10, 16)
		if err != nil || guest == 0 {
			return compute.Instance{}, errors.New("docker returned invalid guest port")
		}
		for _, b := range c.NetworkSettings.Ports[k] {
			port, err := strconv.ParseUint(b.HostPort, 10, 16)
			if err != nil || port == 0 || b.HostIP != "127.0.0.1" {
				return compute.Instance{}, errors.New("docker returned non-loopback or invalid published endpoint")
			}
			result.Endpoints = append(result.Endpoints, compute.Endpoint{Guest: uint16(guest), Protocol: protocol, Host: b.HostIP, Port: uint16(port)})
		}
	}
	return result, nil
}

func (r *Runtime) request(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return errors.New("encode docker request")
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+api+path, reader)
	if err != nil {
		return errors.New("construct docker request")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		// Preserve cancellation semantics without printing transport errors (which
		// can contain credentials, daemon response text, or private socket paths).
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("docker engine request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Docker uses 304 for already started/stopped. It is not accepted elsewhere.
		if resp.StatusCode == http.StatusNotModified && method == http.MethodPost && (strings.HasSuffix(path, "/start") || strings.Contains(path, "/stop?")) {
			return nil
		}
		var sentinel error
		switch resp.StatusCode {
		case http.StatusNotFound:
			sentinel = compute.ErrNotFound
		case http.StatusConflict:
			sentinel = compute.ErrConflict
		case http.StatusBadRequest:
			sentinel = compute.ErrInvalid
		}
		if sentinel != nil {
			return fmt.Errorf("docker engine status %d: %w", resp.StatusCode, sentinel)
		}
		return fmt.Errorf("docker engine status %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(payload) > maxResponse {
		return errors.New("invalid or oversized docker response")
	}
	if err = json.Unmarshal(payload, out); err != nil {
		return errors.New("invalid docker json response")
	}
	return nil
}
