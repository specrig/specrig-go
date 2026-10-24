package gomod

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Root() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
			return cwd, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("failed to stat go.mod: %w", err)
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return "", errors.New("failed to find go.mod")
		}
		cwd = parent
	}
}

func Path(root string) (string, error) {
	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("failed to read go.mod: %w", err)
	}
	for line := range strings.Lines(string(goMod)) {
		if p, found := strings.CutPrefix(strings.TrimSpace(line), "module "); found {
			return strings.TrimSpace(p), nil
		}
	}
	return "", errors.New("go.mod declares no module path")
}
