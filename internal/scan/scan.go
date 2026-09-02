// Package scan derives repo and project state from the filesystem.
// No manifest: what exists on disk is the truth.
package scan

import (
	"os"
	"path/filepath"
	"sort"
)

// Repos returns names of direct subdirectories of reposDir that contain
// a .git entry (regular repos and worktrees; bare repos are skipped).
func Repos(reposDir string) ([]string, error) {
	return subdirs(reposDir, func(path string) bool {
		_, err := os.Stat(filepath.Join(path, ".git"))
		return err == nil
	})
}

// Projects returns names of direct subdirectories of projectsDir. A
// missing projectsDir is treated as an empty list.
func Projects(projectsDir string) ([]string, error) {
	return subdirs(projectsDir, func(string) bool { return true })
}

// AttachedRepos returns names of subdirectories of a project directory
// that are git worktrees (contain a .git file or directory).
func AttachedRepos(projectDir string) ([]string, error) {
	return Repos(projectDir)
}

func subdirs(dir string, keep func(path string) bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if keep(filepath.Join(dir, e.Name())) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
