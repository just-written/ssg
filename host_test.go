package main

import (
	"path/filepath"
	"testing"
)

func TestSsgHost_InvalidPort(t *testing.T) {
	dir := t.TempDir()
	for _, port := range []int{0, -1, 65536, 99999} {
		if err := ssgHost(dir, port); err == nil {
			t.Errorf("expected error for port %d, got nil", port)
		}
	}
}

func TestSsgHost_NonExistentDir(t *testing.T) {
	if err := ssgHost("/this/does/not/exist/abc123", 8080); err == nil {
		t.Error("expected error for non-existent directory, got nil")
	}
}

func TestSsgHost_FileInsteadOfDir(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "afile.txt")
	writeFile(t, filePath, "hello")

	if err := ssgHost(filePath, 8080); err == nil {
		t.Error("expected error when path is a file, got nil")
	}
}
