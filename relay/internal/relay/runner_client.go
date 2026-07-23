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
)

// RunnerClient talks to the tunr-runner sidecar.
type RunnerClient struct {
	baseURL string
	secret  string
	http    *http.Client
}

// NewRunnerClient returns a client (baseURL empty → Wake is a no-op, Deploy errors).
func NewRunnerClient(baseURL, secret string) *RunnerClient {
	return &RunnerClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		http:    &http.Client{}, // no timeout: deploy streams for minutes
	}
}

// Enabled reports whether a runner is configured.
func (c *RunnerClient) Enabled() bool { return c.baseURL != "" }

// Wake implements the Waker interface used by CloudUpstream.
func (c *RunnerClient) Wake(ctx context.Context, appID string) error {
	if c.baseURL == "" {
		return nil // no runner (e.g. dev) — CloudUpstream will just probe
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/apps/"+appID+"/wake", nil)
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
		return fmt.Errorf("runner wake %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// Delete removes an app's container via the runner.
func (c *RunnerClient) Delete(ctx context.Context, appID string) error {
	if c.baseURL == "" {
		return nil
	}
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
