package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

func watchDirs(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		return watcher.Add(path)
	})
}

func watchNewDir(watcher *fsnotify.Watcher, path string) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return
	}
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		return watcher.Add(p)
	})
}

func ssgWatch(flags BuildFlags, onBuild func(error)) error {
	err := ssgBuild(flags)
	if onBuild != nil {
		onBuild(err)
	}
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer watcher.Close()

	if err := watchDirs(watcher, flags.SrcDir); err != nil {
		return fmt.Errorf("watching %s: %w", flags.SrcDir, err)
	}

	fmt.Printf("watching %s for changes...\n", flags.SrcDir)

	var debounce *time.Timer
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Chmod) {
				continue
			}
			if event.Has(fsnotify.Create) {
				watchNewDir(watcher, event.Name)
			}
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(100*time.Millisecond, func() {
				fmt.Println("\nchange detected, rebuilding...")
				err := ssgBuild(flags)
				if err != nil {
					fmt.Printf("build error: %v\n", err)
				}
				if onBuild != nil {
					onBuild(err)
				}
			})
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			fmt.Printf("watcher error: %v\n", err)
		}
	}
}
