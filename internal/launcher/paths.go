package launcher

import (
	"os"
	"path/filepath"
	"strings"
)

// normalizeDir expands a leading ~ to home and cleans p, so that "~/x/", "/root/x"
// and "/root/x/" compare equal. An empty p stays empty.
func normalizeDir(p, home string) string {
	if p == "" {
		return ""
	}
	if home != "" {
		if p == "~" {
			p = home
		} else if rest, ok := strings.CutPrefix(p, "~/"); ok {
			p = filepath.Join(home, rest)
		}
	}
	return filepath.Clean(p)
}

// isDir reports whether path names an existing directory. It is the launcher's
// directory check; resume takes it as a parameter so tests can stand in.
func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// shortPath writes a directory with the home directory shown as ~, for display
// and for matching the filter.
func shortPath(p, home string) string {
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if home == "" {
		return p
	}
	home = filepath.Clean(home)
	if home == "/" {
		return p
	}
	switch {
	case p == home:
		return "~"
	case strings.HasPrefix(p, home+"/"):
		return "~" + p[len(home):]
	}
	return p
}
