package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsDir(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "f")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "existing directory", path: root, want: true},
		{name: "regular file", path: file, want: false},
		{name: "missing path", path: filepath.Join(root, "gone"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDir(tc.path); got != tc.want {
				t.Errorf("isDir(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestNormalizeDir(t *testing.T) {
	const home = "/home/u"
	tests := []struct {
		name string
		p    string
		home string
		want string
	}{
		{name: "tilde alone", p: "~", home: home, want: home},
		{name: "tilde with trailing slash", p: "~/x/", home: home, want: "/home/u/x"},
		{name: "absolute with trailing slash", p: "/a/b/", home: home, want: "/a/b"},
		{name: "absolute without slash is unchanged", p: "/a/b", home: home, want: "/a/b"},
		{name: "empty stays empty", p: "", home: home, want: ""},
		{name: "tilde without a home is kept", p: "~/x", home: "", want: "~/x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeDir(tc.p, tc.home); got != tc.want {
				t.Errorf("normalizeDir(%q) = %q, want %q", tc.p, got, tc.want)
			}
		})
	}
}

func TestShortPath(t *testing.T) {
	tests := []struct {
		name string
		p    string
		home string
		want string
	}{
		{name: "under home", p: "/home/u/work/x/", home: "/home/u", want: "~/work/x"},
		{name: "home itself", p: "/home/u", home: "/home/u", want: "~"},
		{name: "a sibling sharing the prefix is not under home", p: "/home/user2/x", home: "/home/u", want: "/home/user2/x"},
		{name: "outside home", p: "/srv/a", home: "/home/u", want: "/srv/a"},
		{name: "empty", p: "", home: "/home/u", want: ""},
		{name: "no home known", p: "/home/u/x", home: "", want: "/home/u/x"},
		{name: "root is never shortened", p: "/home/u/x", home: "/", want: "/home/u/x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shortPath(tc.p, tc.home); got != tc.want {
				t.Errorf("shortPath(%q, %q) = %q, want %q", tc.p, tc.home, got, tc.want)
			}
		})
	}
}
