package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// scanEntry is a candidate project found by walking a directory.
type scanEntry struct {
	Name string
	Path string
}

func absPath(p string) (string, error) {
	return filepath.Abs(p)
}

// readScanDir lists immediate subdirectories of dir that contain a .git
// folder. Hidden dirs (".*") are skipped.
func readScanDir(dir string) ([]scanEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading scan dir: %w", err)
	}
	out := make([]scanEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		gitInfo, err := os.Stat(filepath.Join(path, ".git"))
		if err != nil || !gitInfo.IsDir() {
			continue
		}
		out = append(out, scanEntry{Name: name, Path: path})
	}
	return out, nil
}
