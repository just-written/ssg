// Just thin wrapper. I thought about making either the build or host function do this
// but didn't want to mess with them any more than I have to.
// I also don't have any test for this for obvious reasons.
package main

import (
	"fmt"
	"os"
)

func ssgDev(flags BuildFlags, port int) error {
	dir := flags.BuildDir

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating build directory: %w", err)
		}
	}

	watchErr := make(chan error, 1)
	hostErr := make(chan error, 1)

	ready := make(chan struct{})
	var readyOnce func(error)
	readyOnce = func(err error) {
		if err == nil {
			close(ready)
		}
		readyOnce = func(_ error) {}
	}

	go func() {
		watchErr <- ssgWatch(flags, func(err error) {
			readyOnce(err)
		})
	}()

	go func() {
		hostErr <- ssgHost(dir, port)
	}()

	select {
	case err := <-watchErr:
		return fmt.Errorf("watcher stopped: %w", err)
	case err := <-hostErr:
		return fmt.Errorf("server stopped: %w", err)
	case <-ready:
		fmt.Printf("\nready: http://localhost:%d\n\n", port)
	}

	select {
	case err := <-watchErr:
		return fmt.Errorf("watcher stopped: %w", err)
	case err := <-hostErr:
		return fmt.Errorf("server stopped: %w", err)
	}
}
