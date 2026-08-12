package relay

// runner_client.go — the relay/control-plane's HTTP client to the tunr-runner
// sidecar. The relay stays scratch/Docker-free: waking and deploying app
// containers happen through this client, not by touching Docker directly.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// lifecycleTimeout bounds wake/sleep/stop/status/host calls.
//
// This is not belt-and-braces, it is load-bearing. These calls are made from the
// sweeper's single goroutine, so one request that never returns doesn't just
// fail — it wedges the sweeper permanently. Scale-to-zero silently stops for
// every app on the box, and any app the runner had already paused is left
// marked "awake", so the next request to it is proxied into a frozen process
// and hangs too. A slow runner must degrade into a retried operation, never
// into a stuck goroutine.
const lifecycleTimeout = 20 * time.Second

// RunnerClient talks to the tunr-runner sidecar.
type RunnerClient struct {
	baseURL string
	secret  string
	// http has no client timeout because Deploy streams build output for
	// minutes. Every non-deploy call must therefore impose its own deadline —
	// see withTimeout.
	http *http.Client
}

// NewRunnerClient returns a client (baseURL empty → Wake is a no-op, Deploy errors).
func NewRunnerClient(baseURL, secret string) *RunnerClient {
	return &RunnerClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		http:    &http.Client{}, // no timeout: deploy streams for minutes
	}
}

// withTimeout caps a lifecycle call, respecting a caller deadline that is
// already tighter.
func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < lifecycleTimeout {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, lifecycleTimeout)
}

// Enabled reports whether a runner is configured.
func (c *RunnerClient) Enabled() bool { return c.baseURL != "" }

// Wake implements the Waker interface used by CloudUpstream. It returns the
// app's current IP (the container may have moved across a cold stop→start).
func (c *RunnerClient) Wake(ctx context.Context, appID string) (string, error) {
	if c.baseURL == "" {
		return "", nil // no runner (e.g. dev) — CloudUpstream will just probe
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/apps/"+appID+"/wake", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("runner wake %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		IP string `json:"ip"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.IP, nil
}

// Delete removes an app's container via the runner.
func (c *RunnerClient) Delete(ctx context.Context, appID string) error {
	if c.baseURL == "" {
		return nil
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/v1/apps/"+appID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// HostSample fetches the runner's whole-box telemetry (PSI + meminfo). The
// sweeper polls this to decide whether to shed warm apps early, so it hits the
// cheap /v1/host endpoint rather than the full per-app /v1/stats.
//
// An error here means "no telemetry", not "no pressure" — the caller must not
// read a failure as an all-clear.
func (c *RunnerClient) HostSample(ctx context.Context) (HostSample, error) {
	var out HostSample
	if c.baseURL == "" {
		return out, fmt.Errorf("no runner configured")
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/host", nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	resp, err := c.http.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("runner host %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

// Status reports an app's actual lifecycle state on the node.
//
// Needed because the relay's belief about an app can be wrong in one specific,
// dangerous way: after a relay restart every upstream starts out marked "awake",
// but the containers themselves may well be paused. A paused container still
// completes the TCP handshake in the kernel, so probing cannot detect it — the
// relay would happily proxy into a frozen process and the request would hang
// until the response timeout. Reconciling against the node closes that window.
func (c *RunnerClient) Status(ctx context.Context, appID string) (string, error) {
	if c.baseURL == "" {
		return "", fmt.Errorf("no runner configured")
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/apps/"+appID+"/status", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("runner status %d", resp.StatusCode)
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&out); err != nil {
		return "", err
	}
	return out.Status, nil
}

// Logs streams an app's output from the node.
//
// Deliberately does NOT use withTimeout: `follow` is a long-lived stream, and
// the 20s lifecycle cap would sever it mid-line every time. The caller's ctx
// (the CLI's HTTP request) is the lifetime that matters here.
func (c *RunnerClient) Logs(ctx context.Context, appID string, tail int, follow bool) (io.ReadCloser, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("no runner configured")
	}
	u := fmt.Sprintf("%s/v1/apps/%s/logs?tail=%d", c.baseURL, appID, tail)
	if follow {
		u += "&follow=1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("runner logs %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return resp.Body, nil
}

// Sleep moves an idle app into the WARM state (pause + reclaim pages into zram);
// Stop cold-stops it, freeing RAM entirely.
func (c *RunnerClient) Sleep(ctx context.Context, appID string) error {
	return c.postApp(ctx, appID, "sleep")
}
func (c *RunnerClient) Stop(ctx context.Context, appID string) error {
	return c.postApp(ctx, appID, "stop")
}

func (c *RunnerClient) postApp(ctx context.Context, appID, action string) error {
	if c.baseURL == "" {
		return nil
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/apps/"+appID+"/"+action, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("runner %s %d: %s", action, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// Deploy uploads source to the runner and streams its SSE events to onEvent.
// It returns the app's dialable endpoint (from the "ready" event) on success.
func (c *RunnerClient) Deploy(ctx context.Context, metaJSON []byte, source io.Reader, onEvent func(map[string]any)) (string, error) {
	if c.baseURL == "" {
		return "", fmt.Errorf("no runner configured (set RUNNER_URL)")
	}

	// Stream the multipart body so we don't buffer the whole tarball.
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	contentType := mw.FormDataContentType()
	go func() {
		var ferr error
		defer func() { _ = pw.CloseWithError(ferr) }()
		if ferr = mw.WriteField("meta", string(metaJSON)); ferr != nil {
			return
		}
		fw, err := mw.CreateFormFile("source", "source.tar.gz")
		if err != nil {
			ferr = err
			return
		}
		if _, err := io.Copy(fw, source); err != nil {
			ferr = err
			return
		}
		ferr = mw.Close()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/deploy", pr)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	req.Header.Set("Content-Type", contentType)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("runner deploy %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	// Parse the SSE stream: lines of `data: {json}`.
	endpoint := ""
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var ev map[string]any
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		if onEvent != nil {
			onEvent(ev)
		}
		switch ev["event"] {
		case "ready":
			if s, ok := ev["endpoint"].(string); ok {
				endpoint = s
			}
		case "failed":
			msg, _ := ev["error"].(string)
			return "", fmt.Errorf("build failed: %s", msg)
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	if endpoint == "" {
		return "", fmt.Errorf("runner did not report an endpoint")
	}
	return endpoint, nil
}
