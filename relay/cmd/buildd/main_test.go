package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// makeTarGz builds an in-memory .tar.gz from name->content entries (in order).
func makeTarGz(t *testing.T, entries [][2]string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		name, content := e[0], e[1]
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestExtractTarGz_HappyPath(t *testing.T) {
	dst := t.TempDir()
	src := makeTarGz(t, [][2]string{
		{"package.json", `{"name":"x"}`},
		{"src/index.js", "console.log(1)"},
	})
	if err := extractTarGz(src, dst); err != nil {
		t.Fatalf("extract: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "src", "index.js"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "console.log(1)" {
		t.Fatalf("content = %q", string(got))
	}
}

func TestExtractTarGz_PathTraversalSkipped(t *testing.T) {
	dst := t.TempDir()
	// "../evil.txt" must never escape dst; the guard skips it.
	src := makeTarGz(t, [][2]string{
		{"../evil.txt", "pwned"},
		{"ok.txt", "fine"},
	})
	if err := extractTarGz(src, dst); err != nil {
		t.Fatalf("extract: %v", err)
	}
	// The legit file lands; the traversal file does not appear next to dst.
	if _, err := os.Stat(filepath.Join(dst, "ok.txt")); err != nil {
		t.Fatalf("expected ok.txt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dst), "evil.txt")); err == nil {
		t.Fatal("path traversal wrote a file outside dst")
	}
}

func TestExtractTarGz_AbsolutePathSkipped(t *testing.T) {
	dst := t.TempDir()
	src := makeTarGz(t, [][2]string{
		{"/etc/evil", "no"},
		{"good.txt", "yes"},
	})
	if err := extractTarGz(src, dst); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "good.txt")); err != nil {
		t.Fatalf("expected good.txt: %v", err)
	}
}
