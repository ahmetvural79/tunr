package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ahmetvural79/tunr/internal/auth"
	"github.com/spf13/cobra"
)

// deploy source excludes — never uploaded (secrets, build artefacts, VCS).
var deployExcludes = map[string]bool{
	".git": true, "node_modules": true, ".next": true, "dist": true,
	"__pycache__": true, ".venv": true, "venv": true, ".DS_Store": true,
}

func isExcluded(rel string) bool {
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if deployExcludes[part] {
			return true
		}
	}
	base := filepath.Base(rel)
	if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".sqlite") {
		return true
	}
	return false
}

func newDeployCmd() *cobra.Command {
	var name string
	var port int
	var envs []string

	cmd := &cobra.Command{
		Use:   "deploy [dir]",
		Short: "Build & deploy a project to the tunr cloud (preview)",
		Long: `Package the current directory, build it on tunr (Nixpacks — no Dockerfile
required), and run it on our infrastructure. Your app sleeps when idle, wakes on
request, and keeps running after you close your laptop.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			absDir, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			if name == "" {
				name = sanitizeName(filepath.Base(absDir))
			}
			token, err := auth.GetToken()
			if err != nil || token == "" {
				return fmt.Errorf("not logged in — run: tunr login")
			}

			envMap := map[string]string{}
			for _, e := range envs {
				if k, v, ok := strings.Cut(e, "="); ok {
					envMap[strings.TrimSpace(k)] = v
				}
			}
			metaJSON, _ := json.Marshal(map[string]any{
				"name": name, "internal_port": port, "env": envMap,
			})

			// Pack the directory.
			if _, err := os.Stat(filepath.Join(absDir, ".env")); err == nil {
				fmt.Println("  ⚠ .env found — it is NOT uploaded. Pass secrets with --env KEY=VALUE.")
			}
			fmt.Printf("  ▲ Packing %s…\n", absDir)
			var tarBuf bytes.Buffer
			count, err := tarDir(absDir, &tarBuf)
			if err != nil {
				return fmt.Errorf("packing failed: %w", err)
			}
			if tarBuf.Len() > 50<<20 {
				return fmt.Errorf("upload is %s (>50MB); exclude large dirs via .gitignore/.tunrignore", human(tarBuf.Len()))
			}
			fmt.Printf("  ▲ Uploading %d files, %s\n", count, human(tarBuf.Len()))

			var body bytes.Buffer
			mw := multipart.NewWriter(&body)
			_ = mw.WriteField("meta", string(metaJSON))
			fw, _ := mw.CreateFormFile("source", "source.tar.gz")
			if _, err := io.Copy(fw, &tarBuf); err != nil {
				return err
			}
			_ = mw.Close()

			req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, relayURL()+"/v1/deploy", &body)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", mw.FormDataContentType())

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
				return fmt.Errorf("deploy failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
			}
			return streamDeploy(resp.Body)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "app name / subdomain (default: directory name)")
	cmd.Flags().IntVar(&port, "port", 0, "port the app listens on inside (default 8080)")
	cmd.Flags().StringArrayVar(&envs, "env", nil, "environment variable KEY=VALUE (repeatable)")
	return cmd
}

// streamDeploy renders the control-plane's SSE build stream.
func streamDeploy(body io.Reader) error {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &ev); err != nil {
			continue
		}
		switch ev["event"] {
		case "log":
			fmt.Printf("  │ %s\n", str(ev["line"]))
		case "queued", "extracting", "building", "releasing":
			d := str(ev["detail"])
			if d != "" {
				d = " (" + d + ")"
			}
			fmt.Printf("  ▲ %s%s\n", str(ev["event"]), d)
		case "live":
			fmt.Printf("\n  🚀 Live: %s\n     (sleeps when idle, wakes on request)\n", str(ev["url"]))
			return nil
		case "failed":
			return fmt.Errorf("%s", str(ev["error"]))
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return fmt.Errorf("deploy stream ended without a result")
}

func newAppsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "List and manage your cloud apps",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listApps(cmd)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a cloud app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteApp(cmd, args[0])
		},
	})
	return cmd
}

func listApps(cmd *cobra.Command) error {
	token, err := auth.GetToken()
	if err != nil || token == "" {
		return fmt.Errorf("not logged in — run: tunr login")
	}
	req, _ := http.NewRequestWithContext(cmd.Context(), http.MethodGet, relayURL()+"/v1/apps", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s", strings.TrimSpace(string(b)))
	}
	var out struct {
		Apps []struct {
			Name, URL, Status string
		} `json:"apps"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if len(out.Apps) == 0 {
		fmt.Println("No apps yet. Deploy one with: tunr deploy --name my-app")
		return nil
	}
	for _, a := range out.Apps {
		fmt.Printf("  %-24s %-8s %s\n", a.Name, a.Status, a.URL)
	}
	return nil
}

func deleteApp(cmd *cobra.Command, name string) error {
	token, err := auth.GetToken()
	if err != nil || token == "" {
		return fmt.Errorf("not logged in — run: tunr login")
	}
	req, _ := http.NewRequestWithContext(cmd.Context(), http.MethodDelete, relayURL()+"/v1/apps?name="+name, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s", strings.TrimSpace(string(b)))
	}
	fmt.Printf("Deleted %s\n", name)
	return nil
}

// ---------- helpers ----------

func tarDir(root string, w io.Writer) (int, error) {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	count := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if isExcluded(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err := tw.Close(); err != nil {
		return 0, err
	}
	if err := gz.Close(); err != nil {
		return 0, err
	}
	return count, nil
}

func sanitizeName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func human(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
