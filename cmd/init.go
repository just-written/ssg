package cmd

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"ssg/embedded"

	"github.com/spf13/cobra"
)

var projectName string

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a project",
	Args:  cobra.NoArgs,
	RunE:  initFunc,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVarP(&projectName, "name", "n", "ssg-project", "Name of the project directory")
}

func initFunc(cmd *cobra.Command, args []string) error {
	fsys := embedded.TemplateFS
	srcDir := "files/project-templates/default"
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	if _, err := fs.Stat(fsys, srcDir); err != nil {
		return fmt.Errorf("source dir not found: %w", err)
	}

	baseName := filepath.Base(srcDir)
	if projectName != "" {
		baseName = projectName
	}
	destRoot := filepath.Join(cwd, baseName)

	if _, err := os.Stat(destRoot); err == nil {
		return fmt.Errorf("destination already exists: %s", destRoot)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check destination: %w", err)
	}

	return fs.WalkDir(fsys, srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(destRoot, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		srcFile, err := fsys.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		destFile, err := os.Create(destPath)
		if err != nil {
			return err
		}
		defer destFile.Close()

		_, err = io.Copy(destFile, srcFile)
		return err
	})
}
