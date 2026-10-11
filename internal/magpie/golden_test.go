package magpie

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/ from the current output")

// readFixture returns the contents of testdata/name.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// checkGolden compares got, as indented JSON, with testdata/<name>.golden.json.
// Under -update it rewrites that file instead. Review the diff before committing it.
func checkGolden(t *testing.T, name string, got any) {
	t.Helper()
	data, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join("testdata", name+".golden.json")
	if *update {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (create it with go test -update)", err)
	}
	if !bytes.Equal(want, data) {
		t.Errorf("%s differs from the golden file; check the change, then run go test -update\n--- got\n%s", path, data)
	}
}
