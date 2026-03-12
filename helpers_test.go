package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist: %s (%v)", path, err)
	}
}

func assertFileNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("expected file to NOT exist: %s", path)
	}
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected %q to contain %q", s, substr)
	}
}

func minimalProject(t *testing.T, root string) string {
	t.Helper()
	srcDir := filepath.Join(root, "src")
	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<html><body>Hello</body></html>`)
	return srcDir
}

func fakeWrangler(t *testing.T, exitCode int, delay time.Duration) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "wrangler")
	content := "#!/bin/sh\n"
	if delay > 0 {
		content += fmt.Sprintf("sleep %.3f\n", delay.Seconds())
	}
	content += fmt.Sprintf("exit %d\n", exitCode)
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatalf("writing fake wrangler: %v", err)
	}
	return script
}

func newTestContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(context.Background())
}
