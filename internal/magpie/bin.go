package magpie

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ErrBinNotFound reports that no magpie CLI was found.
var ErrBinNotFound = errors.New("magpie binary not found")

// FindBin returns the absolute path of the magpie CLI. A configured path wins
// and must be executable; an invalid one is an error, not a silent fallback.
// Without one it tries $PATH, then $HOME/.local/bin/magpie, because a herdr
// status bar may run with a minimal PATH.
func FindBin(configured string) (string, error) {
	if configured != "" {
		p, err := executable(configured)
		if err != nil {
			return "", fmt.Errorf("configured magpie_bin: %w", err)
		}
		return p, nil
	}
	if p, err := exec.LookPath("magpie"); err == nil {
		return filepath.Abs(p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if p, err := executable(filepath.Join(home, ".local", "bin", "magpie")); err == nil {
			return p, nil
		}
	}
	return "", ErrBinNotFound
}

// executable returns the absolute form of path when it names an executable regular file.
func executable(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s is not an executable file", abs)
	}
	return abs, nil
}
