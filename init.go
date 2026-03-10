// Did you know go lets you just embed whole fucking directories?
// I am going to abuse thks feature for sure.
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed all:embedded/templates
var templateFS embed.FS

func ssgInit(outputDir string, template string) error {
	oldDirInfo, err := os.Stat(outputDir)
	if err == nil {
		if oldDirInfo.IsDir() {
			return fmt.Errorf("directory %q already exists.", outputDir)
		}
		return fmt.Errorf("file with the same name as %q already exists.", outputDir)
	}

	templatePath := "embedded/templates/" + template

	if _, err := templateFS.Open(templatePath); err != nil {
		entries, _ := templateFS.ReadDir("embedded/templates")
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		return fmt.Errorf("unknown template %q, available: %s", template, strings.Join(names, ", "))
	}

	return fs.WalkDir(templateFS, templatePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(templatePath, path)
		if err != nil {
			return err
		}

		outPath := filepath.Join(outputDir, rel)
		if d.IsDir() {
			return os.MkdirAll(outPath, 0755)
		}

		data, err := templateFS.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(outPath, data, 0644)
	})
}
