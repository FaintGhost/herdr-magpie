package magpie

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeFile creates path (and its parents) with the given mode, bypassing the umask.
func writeFile(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestFindBin(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]os.FileMode // created under the case root
		configured string                 // relative to root; empty means none
		want       string                 // expected result, relative to root
		wantErr    error                  // errors.Is target
		wantFail   bool                   // any error is acceptable
	}{
		{
			name:       "configured executable wins over PATH and HOME",
			files:      map[string]os.FileMode{"cfg/magpie": 0o755, "pathdir/magpie": 0o755, "home/.local/bin/magpie": 0o755},
			configured: "cfg/magpie",
			want:       "cfg/magpie",
		},
		{
			name:       "configured file that is not executable is an error",
			files:      map[string]os.FileMode{"cfg/magpie": 0o644, "pathdir/magpie": 0o755},
			configured: "cfg/magpie",
			wantFail:   true,
		},
		{
			name:       "configured path that does not exist is an error",
			configured: "cfg/missing",
			wantFail:   true,
		},
		{
			name:  "PATH is searched before HOME",
			files: map[string]os.FileMode{"pathdir/magpie": 0o755, "home/.local/bin/magpie": 0o755},
			want:  "pathdir/magpie",
		},
		{
			name:  "HOME fallback when PATH has no magpie",
			files: map[string]os.FileMode{"home/.local/bin/magpie": 0o755},
			want:  "home/.local/bin/magpie",
		},
		{
			name:    "HOME candidate must be executable",
			files:   map[string]os.FileMode{"home/.local/bin/magpie": 0o600},
			wantErr: ErrBinNotFound,
		},
		{
			name:    "nothing found",
			wantErr: ErrBinNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for rel, mode := range tt.files {
				writeFile(t, filepath.Join(root, rel), mode)
			}
			// PATH and HOME point inside the case root so the host's magpie is never used.
			t.Setenv("PATH", filepath.Join(root, "pathdir"))
			t.Setenv("HOME", filepath.Join(root, "home"))

			configured := ""
			if tt.configured != "" {
				configured = filepath.Join(root, tt.configured)
			}
			got, err := FindBin(configured)
			switch {
			case tt.wantFail:
				if err == nil {
					t.Fatalf("FindBin() = %q, want error", got)
				}
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("FindBin() error = %v, want %v", err, tt.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("FindBin() error = %v", err)
				}
				if want := filepath.Join(root, tt.want); got != want {
					t.Errorf("FindBin() = %q, want %q", got, want)
				}
			}
		})
	}
}
