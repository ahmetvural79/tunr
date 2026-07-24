// Package runner abstracts "where tunr apps run" behind a small Driver
// interface, so the control plane never talks to Docker or Fly directly.
//
// v0 ships DockerDriver (own server, docker CLI + gVisor runtime).
// FlyDriver is a stub kept as an escape hatch / future region expansion.
//
// Design rule: the Driver knows nothing about routes, subdomains or HTTP —
// it only manages compute units and returns an Endpoint the relay can dial.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// ---------- Types ----------

type AppSpec struct {
	ID           string            // "a_x7k2..." — stable, used in container names
	Name         string            // subdomain, informational only here
	InternalPort int               // the port the app listens on inside (PORT env)
	Env          map[string]string // user env; PORT/TUNR_* are added by the driver
	CPUs         float64           // e.g. 1.0
	MemoryMB     int               // e.g. 256
	EdgeSecret   string            // HMAC key shared with relay (tunr-shim verifies)
}

type DeploySpec struct {
	App          AppSpec
	ImageRef     string // local image tag (own server) or registry ref (fly)
	DeploymentID string // "dep_..." — used for labels/telemetry
}

type Status string

const (
	StatusRunning  Status = "running"
	StatusSleeping Status = "sleeping" // paused (RAM resident) — instant resume
	StatusStopped  Status = "stopped"  // cold — start takes seconds
	StatusUnknown  Status = "unknown"
)

// Endpoint is what the relay's CloudUpstream dials.
type Endpoint struct {
	URL string // e.g. http://172.20.0.7:8080 (docker) or http://tunr-a-x.flycast:80 (fly)
}

// Driver is the only surface the control plane and relay depend on.
type Driver interface {
	EnsureApp(ctx context.Context, app AppSpec) error                 // idempotent one-time setup
	Deploy(ctx context.Context, d DeploySpec) (Endpoint, error)       // (re)start unit with image
	Wake(ctx context.Context, appID string) error                     // sleeping/stopped -> running
	Sleep(ctx context.Context, appID string) error                    // running -> sleeping
	Stop(ctx context.Context, appID string) error                     // -> stopped (frees RAM)
	Destroy(ctx context.Context, appID string) error                  // remove everything
	Status(ctx context.Context, appID string) (Status, error)
	Logs(ctx context.Context, appID string, follow bool) (io.ReadCloser, error)
}

// ---------- DockerDriver (own server, v0) ----------

// Assumptions:
//   - a dedicated bridge network exists:  docker network create --opt com.docker.network.bridge.enable_icc=false tunr-apps
//   - gVisor installed and registered as a runtime named "runsc" (fallback: "runc" behind a flag)
//   - relay runs on the same host (or same L2), so it can dial container IPs directly.
//
// Wake-on-request wiring: the relay's CloudUpstream calls Wake() when a dial
// fails; an idle sweeper (control plane goroutine) calls Sleep()/Stop() based
// on last-request timestamps that the relay records per app.
type DockerDriver struct {
	Runtime string // "runsc" (default) or "runc"
	Network string // "tunr-apps"
}

func NewDockerDriver() *DockerDriver {
	return &DockerDriver{Runtime: "runsc", Network: "tunr-apps"}
}

func (d *DockerDriver) container(appID string) string { return "tunr-app-" + appID }

// docker is a tiny exec helper; swap for the Docker SDK later if needed.
func (d *DockerDriver) docker(ctx context.Context, args ...string) (string, error) {
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func (d *DockerDriver) EnsureApp(ctx context.Context, app AppSpec) error {
	// Network is created out-of-band at server setup; nothing per-app needed in v0.
	// TODO(v1): per-app iptables egress allowlist chains keyed by container IP.
	return nil
}

func (d *DockerDriver) Deploy(ctx context.Context, dep DeploySpec) (Endpoint, error) {
	name := d.container(dep.App.ID)

	// v0 update strategy: stop & replace (relay's freeze cache covers the gap).
	_, _ = d.docker(ctx, "rm", "-f", name) // ignore "no such container"

	args := []string{
		"run", "-d",
		"--name", name,
		"--runtime", d.Runtime,
		"--network", d.Network,
		"--memory", fmt.Sprintf("%dm", dep.App.MemoryMB),
		"--cpus", fmt.Sprintf("%.2f", dep.App.CPUs),
		"--pids-limit", "256",
		"--security-opt", "no-new-privileges",
		"--read-only", "--tmpfs", "/tmp:size=64m",
		"--restart", "no", // lifecycle is ours, not dockerd's
		"--label", "tunr.app=" + dep.App.ID,
		"--label", "tunr.deployment=" + dep.DeploymentID,
		"-e", fmt.Sprintf("PORT=%d", dep.App.InternalPort),
		"-e", "TUNR_APP=" + dep.App.Name,
		"-e", "TUNR_EDGE_SECRET=" + dep.App.EdgeSecret, // consumed by tunr-shim
	}
	for k, v := range dep.App.Env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, dep.ImageRef)

	if _, err := d.docker(ctx, args...); err != nil {
		return Endpoint{}, err
	}
	ip, err := d.containerIP(ctx, name)
	if err != nil {
		return Endpoint{}, err
	}
	return Endpoint{URL: fmt.Sprintf("http://%s:%d", ip, dep.App.InternalPort)}, nil
}

func (d *DockerDriver) containerIP(ctx context.Context, name string) (string, error) {
	format := fmt.Sprintf(`{{.NetworkSettings.Networks.%s.IPAddress}}`, d.Network)
	ip, err := d.docker(ctx, "inspect", "-f", format, name)
	if err != nil || ip == "" || ip == "<no value>" {
		return "", fmt.Errorf("container %s has no IP on %s (err=%v)", name, d.Network, err)
	}
	return ip, nil
}

func (d *DockerDriver) Wake(ctx context.Context, appID string) error {
	name := d.container(appID)
	st, _ := d.Status(ctx, appID)
	switch st {
	case StatusSleeping:
		_, err := d.docker(ctx, "unpause", name) // cgroup freezer resume: ~instant
		return err
	case StatusStopped:
		_, err := d.docker(ctx, "start", name) // cold start: seconds; relay tolerates via wakeTimeout
		return err
	case StatusRunning:
		return nil
	default:
		return fmt.Errorf("wake %s: unknown state", appID)
	}
}

func (d *DockerDriver) Sleep(ctx context.Context, appID string) error {
	_, err := d.docker(ctx, "pause", d.container(appID)) // RAM stays resident
	return err
}

func (d *DockerDriver) Stop(ctx context.Context, appID string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := d.docker(ctx, "stop", "-t", "10", d.container(appID))
	return err
}

func (d *DockerDriver) Destroy(ctx context.Context, appID string) error {
	_, err := d.docker(ctx, "rm", "-f", d.container(appID))
	return err
}

func (d *DockerDriver) Status(ctx context.Context, appID string) (Status, error) {
	out, err := d.docker(ctx, "inspect", "-f", "{{.State.Status}} {{.State.Paused}}", d.container(appID))
	if err != nil {
		return StatusUnknown, err
	}
	switch {
	case strings.HasSuffix(out, "true"): // paused
		return StatusSleeping, nil
	case strings.HasPrefix(out, "running"):
		return StatusRunning, nil
	case strings.HasPrefix(out, "exited"), strings.HasPrefix(out, "created"):
		return StatusStopped, nil
	}
	return StatusUnknown, nil
}

func (d *DockerDriver) Logs(ctx context.Context, appID string, follow bool) (io.ReadCloser, error) {
	args := []string{"logs", "--tail", "200"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, d.container(appID))
	cmd := exec.CommandContext(ctx, "docker", args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// NOTE: caller must Close(); process reaping omitted in skeleton.
	return pipe, nil
}

// ---------- FlyDriver (stub — future escape hatch) ----------

// FlyDriver would implement the same interface against api.machines.dev.
// Key differences vs DockerDriver, kept here so the knowledge isn't lost:
//   - Endpoint.URL is the app's .flycast address (private Fly Proxy), NOT
//     .internal — only flycast traffic triggers Fly's autostart.
//   - Wake() is a NO-OP: Fly Proxy wakes machines on request by itself.
//   - Sleep() is a NO-OP: services[].autostop="suspend" handles idling.
//   - Deploy() = machines create/update with registry image ref (push required).
type FlyDriver struct{ /* OrgToken, Region string */ }

func (f *FlyDriver) Wake(ctx context.Context, appID string) error { return nil } // Fly Proxy does it
// ... remaining methods: TODO if/when multi-region expansion needs Fly.
