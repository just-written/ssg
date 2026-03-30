package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve [flags]",
	Short: "Host a dev server, rebuilds on change",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		bv, err := initConfig(cmd, "build")
		if err != nil {
			return err
		}

		bcfg := BuildConfig{
			In:       bv.GetString("in"),
			Out:      bv.GetString("out"),
			Pages:    bv.GetString("pages"),
			Static:   bv.GetString("static"),
			Partials: bv.GetString("partials"),
			BaseURL:  bv.GetString("base-url"),
		}

		sv, err := initConfig(cmd, "serve")
		if err != nil {
			return err
		}

		scfg := ServeConfig{
			Port: sv.GetUint16("port"),
		}
		return serveFunc(cmd.Context(), scfg, bcfg)
	},
}

type ServeConfig struct {
	Port uint16
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().Uint16P("port", "p", 1313, "Port to serve from")
	serveCmd.Flags().StringP("in", "i", ".", "Input project directory")
	serveCmd.Flags().StringP("out", "o", "dist/", "Output directory for built files")
	serveCmd.Flags().String("pages", "pages/", "Directory of pages to build")
	serveCmd.Flags().String("static", "static/", "Directory of static assets to copy")
	serveCmd.Flags().String("partials", "partials/", "Directory of partial templates")
	serveCmd.Flags().String("base-url", "", "Base URL for sitemap generation (e.g. https://example.com)")
}

func watchAndRebuild(ctx context.Context, watchDir string, rebuild func() error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Println("Failed to create watcher:", err)
		return
	}
	defer watcher.Close()

	if err := addDirsRecursive(watcher, watchDir); err != nil {
		log.Println("Error registering watch paths:", err)
		return
	}

	const debounceDelay = 300 * time.Millisecond
	timer := time.NewTimer(0)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := watcher.Add(event.Name); err != nil {
						log.Println("Watcher add error:", err)
					}
				}
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounceDelay)

		case <-timer.C:
			if err := rebuild(); err != nil {
				log.Println("Build error:", err)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Println("Watcher error:", err)
		}
	}
}

func addDirsRecursive(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if err := watcher.Add(path); err != nil {
				log.Println("Watcher add error:", path, err)
			}
		}
		return nil
	})
}

type spaHandler struct{ root http.FileSystem }

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f, err := h.root.Open(r.URL.Path)
	if err == nil {
		f.Close()
		http.FileServer(h.root).ServeHTTP(w, r)
		return
	}
	if !errors.Is(err, os.ErrNotExist) {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	for _, fallback := range []string{"/404.html", "/index.html"} {
		f, err := h.root.Open(fallback)
		if err == nil {
			defer f.Close()
			status := http.StatusOK
			if fallback == "/404.html" {
				status = http.StatusNotFound
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(status)
			io.Copy(w, f)
			return
		}
	}

	http.Error(w, "404 Not Found", http.StatusNotFound)
}

func serveFunc(ctx context.Context, serveConfig ServeConfig, buildConfig BuildConfig) error {
	addr := fmt.Sprintf(":%d", serveConfig.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot bind to port %d: %w", serveConfig.Port, err)
	}

	rebuild := func() error {
		fmt.Println("Change detected, rebuilding…")
		if err := buildFunc(buildConfig); err != nil {
			return err
		}
		fmt.Println("Build complete")
		return nil
	}

	if err := rebuild(); err != nil {
		return fmt.Errorf("initial build failed: %w", err)
	}

	go watchAndRebuild(ctx, buildConfig.In, rebuild)

	handler := spaHandler{root: http.Dir(buildConfig.Out)}
	server := &http.Server{Handler: handler}

	fmt.Printf("Serving %s at http://localhost:%d\n", buildConfig.Out, serveConfig.Port)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Println("Server shutdown error:", err)
		}
	}()

	if err := server.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}
