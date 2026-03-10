package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchDirs
func TestWatchDirs_RegistersAllSubdirectories(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pages", "about", "index.html"), "")
	writeFile(t, filepath.Join(root, "assets", "css", "style.css"), "")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer watcher.Close()

	if err := watchDirs(watcher, root); err != nil {
		t.Fatalf("watchDirs: %v", err)
	}

	watched := watcher.WatchList()
	watchedSet := make(map[string]bool, len(watched))
	for _, p := range watched {
		watchedSet[p] = true
	}

	for _, dir := range []string{
		root,
		filepath.Join(root, "pages"),
		filepath.Join(root, "pages", "about"),
		filepath.Join(root, "assets"),
		filepath.Join(root, "assets", "css"),
	} {
		if !watchedSet[dir] {
			t.Errorf("expected %s to be watched, but it wasn't", dir)
		}
	}
}

func TestWatchDirs_ErrorsOnMissingRoot(t *testing.T) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer watcher.Close()

	if err := watchDirs(watcher, "/nonexistent/path/xyz"); err == nil {
		t.Error("expected error for missing root, got nil")
	}
}

// watchNewDir
func TestWatchNewDir_AddsDirectoryToWatcher(t *testing.T) {
	root := t.TempDir()
	newDir := filepath.Join(root, "newdir")
	if err := os.MkdirAll(newDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer watcher.Close()

	watchNewDir(watcher, newDir)

	if !slices.Contains(watcher.WatchList(), newDir) {
		t.Errorf("expected %s to be watched after watchNewDir", newDir)
	}
}

func TestWatchNewDir_AddsNestedDirectories(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer watcher.Close()

	watchNewDir(watcher, filepath.Join(root, "a"))

	watched := watcher.WatchList()
	watchedSet := make(map[string]bool, len(watched))
	for _, p := range watched {
		watchedSet[p] = true
	}

	for _, dir := range []string{
		filepath.Join(root, "a"),
		filepath.Join(root, "a", "b"),
		filepath.Join(root, "a", "b", "c"),
	} {
		if !watchedSet[dir] {
			t.Errorf("expected %s to be watched, but it wasn't", dir)
		}
	}
}

func TestWatchNewDir_IgnoresFiles(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "style.css")
	writeFile(t, filePath, "body {}")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer watcher.Close()

	watchNewDir(watcher, filePath)

	if len(watcher.WatchList()) != 0 {
		t.Error("expected no watched paths after passing a file to watchNewDir")
	}
}

func TestWatchNewDir_IgnoresMissingPath(t *testing.T) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer watcher.Close()

	watchNewDir(watcher, "/nonexistent/path/xyz")

	if len(watcher.WatchList()) != 0 {
		t.Error("expected no watched paths for missing path")
	}
}

// ssgWatch
func TestSsgWatch_InitialBuildRuns(t *testing.T) {
	root := t.TempDir()
	srcDir := minimalProject(t, root)
	buildDir := filepath.Join(root, "dist")

	flags := BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}

	built := make(chan error, 1)
	go ssgWatch(flags, func(err error) {
		select {
		case built <- err:
		default:
		}
	})

	select {
	case err := <-built:
		if err != nil {
			t.Fatalf("initial build failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for initial build")
	}

	assertFileExists(t, filepath.Join(buildDir, "index.html"))
}

func TestSsgWatch_InitialBuildError_Exits(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	buildDir := filepath.Join(root, "dist")

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `{{.Unclosed`)

	flags := BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}

	done := make(chan error, 1)
	go func() {
		done <- ssgWatch(flags, nil)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected error from initial build failure, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Error("ssgWatch did not exit after initial build failure")
	}
}

func TestSsgWatch_RebuildOnChange(t *testing.T) {
	root := t.TempDir()
	srcDir := minimalProject(t, root)
	buildDir := filepath.Join(root, "dist")

	flags := BuildFlags{SrcDir: srcDir, BuildDir: buildDir, Quiet: true}

	built := make(chan error, 10)
	go ssgWatch(flags, func(err error) {
		built <- err
	})

	select {
	case err := <-built:
		if err != nil {
			t.Fatalf("initial build failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for initial build")
	}

	writeFile(t, filepath.Join(srcDir, "pages", "index.html"), `<html><body>Updated</body></html>`)

	select {
	case err := <-built:
		if err != nil {
			t.Fatalf("rebuild failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for rebuild")
	}

	assertContains(t, readFile(t, filepath.Join(buildDir, "index.html")), "Updated")
}
