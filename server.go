// runs watch and wrangler pages dev concurrently.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

type DevFlags struct {
	BuildFlags
	WranglerBin  string
	WranglerPort int
	WranglerArgs []string
}

func (f *DevFlags) wranglerBin() string {
	if f.WranglerBin != "" {
		return f.WranglerBin
	}
	return "wrangler"
}

func (f *DevFlags) wranglerPort() int {
	if f.WranglerPort > 0 {
		return f.WranglerPort
	}
	return 8788
}

func ssgDev(flags DevFlags) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	buildResults := make(chan error, 1)

	watcherDone := make(chan error, 1)
	go func() {
		watcherDone <- ssgWatch(flags.BuildFlags, func(err error) {
			select {
			case buildResults <- err:
			default:
			}
		})
	}()

	var wranglerCmd *exec.Cmd
	select {
	case err := <-watcherDone:
		return fmt.Errorf("watcher exited early: %w", err)
	case err := <-buildResults:
		if err != nil {
			cancel()
			return fmt.Errorf("initial build failed: %w", err)
		}
		var startErr error
		wranglerCmd, startErr = startWrangler(ctx, flags)
		if startErr != nil {
			cancel()
			return startErr
		}
	case <-ctx.Done():
		return nil
	}

	wranglerDone := make(chan error, 1)
	go func() {
		wranglerDone <- wranglerCmd.Wait()
	}()

	select {
	case <-ctx.Done():
		<-wranglerDone
		return nil
	case err := <-wranglerDone:
		cancel()
		if err != nil && !isSignalError(err) {
			return fmt.Errorf("wrangler exited: %w", err)
		}
		return nil
	case err := <-watcherDone:
		cancel()
		if wranglerCmd.Process != nil {
			wranglerCmd.Process.Signal(os.Interrupt) //nolint:errcheck
			<-wranglerDone
		}
		if err != nil {
			return fmt.Errorf("watcher exited: %w", err)
		}
		return nil
	}
}

func startWrangler(ctx context.Context, flags DevFlags) (*exec.Cmd, error) {
	args := []string{
		"pages", "dev",
		flags.BuildDir,
		"--port", fmt.Sprintf("%d", flags.wranglerPort()),
	}
	args = append(args, flags.WranglerArgs...)

	cmd := exec.CommandContext(ctx, flags.wranglerBin(), args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting wrangler: %w", err)
	}

	fmt.Printf("wrangler started on http://localhost:%d (pid %d)\n", flags.wranglerPort(), cmd.Process.Pid)
	return cmd, nil
}

func isSignalError(err error) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.Signaled()
		}
	}
	return false
}
