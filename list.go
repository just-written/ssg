// for the list coomand I just made it so 
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func ssgList(srcDir string) error {
	pagesDir := filepath.Join(srcDir, "pages")
	if _, err := os.Stat(pagesDir); err != nil {
		return fmt.Errorf("pages directory not found: %s", pagesDir)
	}

	var count int
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
		outPath := filepath.ToSlash(rel)

		data, dataFiles, err := loadPageData(pagesDir, path)
		if err != nil {
			return fmt.Errorf("loading data for %s: %w", path, err)
		}

		fmt.Printf("page:  %s\n", outPath)

		if len(dataFiles) > 0 {
			relDataFiles := make([]string, 0, len(dataFiles))
			for _, df := range dataFiles {
				if r, err := filepath.Rel(srcDir, df); err == nil {
					relDataFiles = append(relDataFiles, filepath.ToSlash(r))
				} else {
					relDataFiles = append(relDataFiles, df)
				}
			}
			fmt.Printf("  data:  %s\n", strings.Join(relDataFiles, ", "))
		}

		if len(data) > 0 {
			keys := make([]string, 0, len(data))
			for k := range data {
				keys = append(keys, k)
			}
			fmt.Printf("  keys:  %s\n", strings.Join(keys, ", "))
		}

		count++
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Printf("\n%d page(s)\n", count)
	return nil
}
