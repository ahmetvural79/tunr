package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ahmetvural79/tunr/internal/logger"
	"github.com/ahmetvural79/tunr/internal/term"
	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "update",
		Aliases: []string{"upgrade"},
		Short:   "Update tunr to the latest version",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := updateRepo()
			baseURL := strings.TrimRight(updateBaseURL(), "/")

			fmt.Println()
			term.Dim.Println("  Checking for updates...")

			tag, err := latestTag(baseURL, repo)
			if err != nil {
				return fmt.Errorf("failed to check latest version: %w", err)
			}
			latest := strings.TrimPrefix(tag, "v")

			if latest == Version {
				term.Green.Printf("  Already up to date (v%s)\n\n", Version)
				return nil
			}

			logger.Info("Updating v%s → v%s", Version, latest)
			fmt.Println()

			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to locate binary: %w", err)
			}
			exe, err = filepath.EvalSymlinks(exe)
			if err != nil {
				return fmt.Errorf("failed to resolve binary path: %w", err)
			}

			tmpDir, err := os.MkdirTemp("", "tunr-update-*")
			if err != nil {
				return fmt.Errorf("failed to create temp dir: %w", err)
			}
			defer os.RemoveAll(tmpDir)

			arch := runtime.GOARCH
			if arch == "amd64" {
				arch = "x86_64"
			}

			filename := fmt.Sprintf("tunr_%s_%s_%s.tar.gz", latest, runtime.GOOS, arch)
			archiveURL := fmt.Sprintf("%s/%s/releases/download/%s/%s", baseURL, repo, tag, filename)
			checksumURL := fmt.Sprintf("%s/%s/releases/download/%s/checksums.txt", baseURL, repo, tag)
			archivePath := filepath.Join(tmpDir, filename)
			binaryPath := filepath.Join(tmpDir, "tunr")

			steps := []struct {
				name string
				fn   func() error
			}{
				{"Downloading", func() error { return downloadFile(archiveURL, archivePath) }},
				{"Verifying checksum", func() error { return verifyChecksum(checksumURL, archivePath, filename) }},
				{"Extracting", func() error { return extractBinary(archivePath, binaryPath) }},
				{"Replacing binary", func() error { return replaceBinary(binaryPath, exe) }},
			}

			for _, s := range steps {
				term.Dim.Printf("  %s...", s.name)
				if err := s.fn(); err != nil {
					term.Red.Println(" failed")
					return fmt.Errorf("%s failed: %w", s.name, err)
				}
				term.Green.Println(" done")
			}

			fmt.Println()
			term.Green.Printf("  Updated to v%s\n\n", latest)
			return nil
		},
	}

	return cmd
}

func latestTag(baseURL, repo string) (string, error) {
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Head(fmt.Sprintf("%s/%s/releases/latest", strings.TrimRight(baseURL, "/"), repo))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("no redirect from releases/latest")
	}

	parts := strings.Split(loc, "/tag/")
	if len(parts) != 2 {
		return "", fmt.Errorf("unexpected redirect: %s", loc)
	}

	return parts[1], nil
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}

	return f.Close()
}

func verifyChecksum(checksumURL, filePath, filename string) error {
	resp, err := http.Get(checksumURL)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var expectedHash string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), filename) {
			fields := strings.Fields(line)
			if len(fields) >= 1 {
				expectedHash = fields[0]
				break
			}
		}
	}
	if expectedHash == "" {
		return fmt.Errorf("checksum not found for %s", filename)
	}

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actual)
	}

	return nil
}

func extractBinary(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("tunr binary not found in archive")
		}
		if err != nil {
			return err
		}

		if filepath.Base(hdr.Name) != "tunr" || hdr.Typeflag != tar.TypeReg {
			continue
		}

		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}

		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}

		return out.Close()
	}
}

func replaceBinary(srcPath, dstPath string) error {
	dir := filepath.Dir(dstPath)

	tmp, err := os.CreateTemp(dir, ".tunr-update-*")
	if err != nil {
		if !os.IsPermission(err) {
			return err
		}
		return replaceBinarySudo(srcPath, dstPath)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	in, err := os.Open(srcPath)
	if err != nil {
		tmp.Close()
		return err
	}

	if _, err := io.Copy(tmp, in); err != nil {
		in.Close()
		tmp.Close()
		return err
	}
	in.Close()

	if err := tmp.Chmod(0755); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	if err := os.Rename(tmpPath, dstPath); err != nil {
		if !os.IsPermission(err) {
			return err
		}
		return replaceBinarySudo(srcPath, dstPath)
	}

	return nil
}

func replaceBinarySudo(srcPath, dstPath string) error {
	cmd := exec.Command("sudo", "install", "-m", "0755", srcPath, dstPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
