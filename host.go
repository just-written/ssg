// basic hosting for the build site
// NOT to be used to test for prod, use an actual server.
// (i almost want to get rid of it and make people use whatever CLI they use to test for their hosting service)
package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func ssgHost(dir string, port int) error {
	if port < 1 || port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("directory %q does not exist", dir)
		}
		return fmt.Errorf("cannot access directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", dir)
	}

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("serving %s on http://localhost%s\n", dir, addr)

	return http.ListenAndServe(addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		http.FileServer(http.Dir(dir)).ServeHTTP(rec, r)
		fmt.Printf("%s %s %d %s\n", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	}))
}
