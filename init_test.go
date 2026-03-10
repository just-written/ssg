package main

import (
	"path/filepath"
	"testing"
)

func TestSsgInit_ErrorsIfDirAlreadyExists(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "myproject")

	if err := ssgInit(target, "default"); err != nil {
		t.Fatalf("first ssgInit: %v", err)
	}
	if err := ssgInit(target, "default"); err == nil {
		t.Error("expected error on duplicate init, got nil")
	}
}

func TestSsgInit_ErrorsIfFileWithSameNameExists(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "conflict")
	writeFile(t, target, "i am a file")

	if err := ssgInit(target, "default"); err == nil {
		t.Error("expected error when file exists at target path, got nil")
	}
}

func TestSsgInit_ErrorsOnUnknownTemplate(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "myproject")

	if err := ssgInit(target, "doesnotexist"); err == nil {
		t.Error("expected error for unknown template, got nil")
	}
}

func TestSsgInit_CreatedProjectBuildsSuccessfully(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "myproject")

	if err := ssgInit(target, "default"); err != nil {
		t.Fatalf("ssgInit: %v", err)
	}

	buildDir := filepath.Join(root, "dist")
	if err := ssgBuild(BuildFlags{SrcDir: target, BuildDir: buildDir, Quiet: true}); err != nil {
		t.Fatalf("ssgBuild on fresh init: %v", err)
	}
}
