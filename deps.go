// content hash based dep graph
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const graphFile = ".build_graph.json"

type depGraph struct {
	Pages        map[string]map[string]string `json:"pages"`
	Assets       map[string]string            `json:"assets"`
	KnownOutputs map[string]struct{}          `json:"known_outputs"`
}

func newDepGraph() *depGraph {
	return &depGraph{
		Pages:        map[string]map[string]string{},
		Assets:       map[string]string{},
		KnownOutputs: map[string]struct{}{},
	}
}

func (g *depGraph) recordOutput(outputRel string) {
	g.KnownOutputs[filepath.ToSlash(outputRel)] = struct{}{}
}

func (g *depGraph) pruneOutputs(buildDir string, quiet bool) error {
	var toDelete []string

	err := filepath.WalkDir(buildDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(buildDir, path)
		if err != nil {
			return err
		}
		if _, ok := g.KnownOutputs[filepath.ToSlash(rel)]; !ok {
			toDelete = append(toDelete, path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, path := range toDelete {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing stale file %s: %w", path, err)
		}
		if !quiet {
			rel, _ := filepath.Rel(buildDir, path)
			fmt.Printf("removed: %s\n", rel)
		}
	}

	var dirs []string
	if err := filepath.WalkDir(buildDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != buildDir {
			dirs = append(dirs, path)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("walking build dir for empty directory cleanup: %w", err)
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		entries, err := os.ReadDir(dirs[i])
		if err == nil && len(entries) == 0 {
			os.Remove(dirs[i])
		}
	}

	return nil
}

func loadDepGraph(projectDir string) (*depGraph, error) {
	path := filepath.Join(projectDir, graphFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return newDepGraph(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	g := newDepGraph()
	if err := json.Unmarshal(raw, g); err != nil {
		return newDepGraph(), nil
	}
	return g, nil
}

func (g *depGraph) save(projectDir string) error {
	raw, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding dep graph: %w", err)
	}
	path := filepath.Join(projectDir, graphFile)
	if err := os.WriteFile(path, raw, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func (g *depGraph) record(outputRel string, deps []string) {
	hashes := make(map[string]string, len(deps))
	for _, dep := range deps {
		h, err := hashFile(dep)
		if err != nil {
			continue
		}
		hashes[dep] = h
	}
	g.Pages[outputRel] = hashes
}

func (g *depGraph) changed(outputRel, outputAbs string) (bool, error) {
	if _, err := os.Stat(outputAbs); errors.Is(err, os.ErrNotExist) {
		return true, nil
	}

	deps, ok := g.Pages[outputRel]
	if !ok {
		return true, nil
	}

	for path, savedHash := range deps {
		current, err := hashFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("hashing %s: %w", path, err)
		}
		if current != savedHash {
			return true, nil
		}
	}
	return false, nil
}

type stalePage struct {
	srcPath   string
	outputRel string
}

func (g *depGraph) allPages(pagesDir, buildDir string) ([]stalePage, error) {
	return newDepGraph().stalePages(pagesDir, buildDir)
}

func (g *depGraph) stalePages(pagesDir, buildDir string) ([]stalePage, error) {
	var stale []stalePage

	err := filepath.WalkDir(pagesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if strings.HasPrefix(name, "_") || !strings.HasSuffix(name, ".html") {
			return nil
		}

		rel, err := filepath.Rel(pagesDir, path)
		if err != nil {
			return err
		}
		outputAbs := filepath.Join(buildDir, rel)
		outputRel := filepath.ToSlash(rel)

		dirty, err := g.changed(outputRel, outputAbs)
		if err != nil {
			return err
		}
		if dirty {
			stale = append(stale, stalePage{srcPath: path, outputRel: outputRel})
		}
		return nil
	})
	return stale, err
}
