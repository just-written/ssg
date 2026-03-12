package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestDevFlags_WranglerBinDefault(t *testing.T) {
	f := DevFlags{}
	if f.wranglerBin() != "wrangler" {
		t.Errorf("expected default bin 'wrangler', got %q", f.wranglerBin())
	}
}

func TestDevFlags_WranglerBinCustom(t *testing.T) {
	f := DevFlags{WranglerBin: "/usr/local/bin/wrangler"}
	if f.wranglerBin() != "/usr/local/bin/wrangler" {
		t.Errorf("got %q", f.wranglerBin())
	}
}

func TestDevFlags_WranglerPortDefault(t *testing.T) {
	f := DevFlags{}
	if f.wranglerPort() != 8788 {
		t.Errorf("expected default port 8788, got %d", f.wranglerPort())
	}
}

func TestDevFlags_WranglerPortCustom(t *testing.T) {
	f := DevFlags{WranglerPort: 3000}
	if f.wranglerPort() != 3000 {
		t.Errorf("expected 3000, got %d", f.wranglerPort())
	}
}

func TestIsSignalError_NilError(t *testing.T) {
	if isSignalError(nil) {
		t.Error("expected false for nil error")
	}
}

func TestIsSignalError_NonExitError(t *testing.T) {
	if isSignalError(os.ErrNotExist) {
		t.Error("expected false for non-exit error")
	}
}

func TestIsSignalError_TrueForKilledProcess(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep: %v", err)
	}
	cmd.Process.Signal(syscall.SIGTERM) //nolint:errcheck
	err := cmd.Wait()
	if !isSignalError(err) {
		t.Errorf("expected isSignalError=true for SIGTERM'd process (err=%v)", err)
	}
}

func TestIsSignalError_FalseForNormalNonZeroExit(t *testing.T) {
	cmd := exec.Command("false")
	err := cmd.Run()
	if isSignalError(err) {
		t.Error("expected false for normal non-zero exit, got true")
	}
}

func TestStartWrangler_StartsAndExitsCleanly(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := newTestContext(t)
	defer cancel()

	flags := DevFlags{
		BuildFlags:  BuildFlags{BuildDir: root, Quiet: true},
		WranglerBin: fakeWrangler(t, 0, 0),
	}

	cmd, err := startWrangler(ctx, flags)
	if err != nil {
		t.Fatalf("startWrangler: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Errorf("fake wrangler exited with error: %v", err)
	}
}

func TestStartWrangler_PassesBuildDirAndPagesDev(t *testing.T) {
	root := t.TempDir()
	argsFile := filepath.Join(root, "args.txt")
	script := filepath.Join(t.TempDir(), "wrangler")
	os.WriteFile(script,
		[]byte("#!/bin/sh\necho \"$@\" > "+argsFile+"\n"),
		0755)

	ctx, cancel := newTestContext(t)
	defer cancel()

	flags := DevFlags{
		BuildFlags:  BuildFlags{BuildDir: root, Quiet: true},
		WranglerBin: script,
	}

	cmd, err := startWrangler(ctx, flags)
	if err != nil {
		t.Fatalf("startWrangler: %v", err)
	}
	cmd.Wait()

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("reading args file: %v", err)
	}
	args := string(raw)
	assertContains(t, args, "pages")
	assertContains(t, args, "dev")
	assertContains(t, args, root)
}

func TestStartWrangler_ExtraArgsForwarded(t *testing.T) {
	root := t.TempDir()
	argsFile := filepath.Join(root, "args.txt")
	script := filepath.Join(t.TempDir(), "wrangler")
	os.WriteFile(script,
		[]byte("#!/bin/sh\necho \"$@\" > "+argsFile+"\n"),
		0755)

	ctx, cancel := newTestContext(t)
	defer cancel()

	flags := DevFlags{
		BuildFlags:   BuildFlags{BuildDir: root, Quiet: true},
		WranglerBin:  script,
		WranglerArgs: []string{"--log-level", "debug"},
	}

	cmd, err := startWrangler(ctx, flags)
	if err != nil {
		t.Fatalf("startWrangler: %v", err)
	}
	cmd.Wait()

	raw, _ := os.ReadFile(argsFile)
	args := string(raw)
	assertContains(t, args, "--log-level")
	assertContains(t, args, "debug")
}

func TestStartWrangler_MissingBinaryErrors(t *testing.T) {
	ctx, cancel := newTestContext(t)
	defer cancel()

	flags := DevFlags{
		BuildFlags:  BuildFlags{BuildDir: t.TempDir(), Quiet: true},
		WranglerBin: "/nonexistent/wrangler-binary-xyz",
	}

	if _, err := startWrangler(ctx, flags); err == nil {
		t.Error("expected error for missing wrangler binary, got nil")
	}
}

func TestStartWrangler_UsesCustomPort(t *testing.T) {
	root := t.TempDir()
	argsFile := filepath.Join(root, "args.txt")
	script := filepath.Join(t.TempDir(), "wrangler")
	os.WriteFile(script,
		[]byte("#!/bin/sh\necho \"$@\" > "+argsFile+"\n"),
		0755)

	ctx, cancel := newTestContext(t)
	defer cancel()

	flags := DevFlags{
		BuildFlags:   BuildFlags{BuildDir: root, Quiet: true},
		WranglerBin:  script,
		WranglerPort: 9999,
	}

	cmd, err := startWrangler(ctx, flags)
	if err != nil {
		t.Fatalf("startWrangler: %v", err)
	}
	cmd.Wait()

	raw, _ := os.ReadFile(argsFile)
	assertContains(t, string(raw), "9999")
}

func TestSsgDev_InitialBuildFailureExitsWithError(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `{{.Unclosed`)

	flags := DevFlags{
		BuildFlags:  BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true},
		WranglerBin: fakeWrangler(t, 0, 30*time.Second),
	}

	done := make(chan error, 1)
	go func() { done <- ssgDev(flags) }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected error from failed initial build, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Error("ssgDev did not exit after initial build failure")
	}
}

func TestSsgDev_WranglerExitCausesDevToReturn(t *testing.T) {
	root := t.TempDir()
	srcDir := minimalProject(t, root)
	buildDir := filepath.Join(root, "dist")

	flags := DevFlags{
		BuildFlags:  BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true},
		WranglerBin: fakeWrangler(t, 0, 0),
	}

	done := make(chan error, 1)
	go func() { done <- ssgDev(flags) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Error("ssgDev did not return after wrangler exited")
	}
}

func TestSsgDev_WranglerReceivesBuildDirAfterSuccessfulBuild(t *testing.T) {
	root := t.TempDir()
	srcDir := minimalProject(t, root)
	buildDir := filepath.Join(root, "dist")
	argsFile := filepath.Join(root, "wrangler-args.txt")

	script := filepath.Join(t.TempDir(), "wrangler")
	os.WriteFile(script,
		[]byte("#!/bin/sh\necho \"$@\" > "+argsFile+"\n"),
		0755)

	flags := DevFlags{
		BuildFlags:  BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true},
		WranglerBin: script,
	}

	done := make(chan error, 1)
	go func() { done <- ssgDev(flags) }()

	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("ssgDev timed out")
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("wrangler args file not written: %v", err)
	}
	assertContains(t, string(raw), buildDir)
}
