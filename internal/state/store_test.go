package state

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type sample struct {
	Name  string         `json:"name"`
	Count int            `json:"count"`
	Seen  map[string]int `json:"seen,omitempty"`
}

func mustOpen(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOpenCreatesPrivateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "outer", "state")
	mustOpen(t, dir)
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.IsDir() || fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("mode = %v, want a directory with no group or other access", fi.Mode())
	}
}

func TestOpenRejectsEmptyDir(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("Open(\"\") succeeded, want an error")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)
	want := sample{Name: "demo", Count: 3, Seen: map[string]int{"req-1": 1}}
	if err := s.Save("sample.json", want); err != nil {
		t.Fatal(err)
	}
	var got sample
	if err := s.Load("sample.json", &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loaded %+v, want %+v", got, want)
	}
	fi, err := os.Stat(filepath.Join(dir, "sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %v, want 0600", perm)
	}
}

func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)
	for i := range 3 {
		if err := s.Save("sample.json", sample{Count: i}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "sample.json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %q, want only sample.json", names)
	}
}

func TestSaveFailureRemovesTempFile(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)
	// A non-empty directory at the target makes the final rename fail.
	blocked := filepath.Join(dir, "blocked.json")
	if err := os.MkdirAll(filepath.Join(blocked, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("blocked.json", sample{Name: "x"}); err == nil {
		t.Fatal("Save succeeded over a directory, want an error")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file %q left behind", e.Name())
		}
	}
}

func TestLoadMissingFileIsZero(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	v := sample{Name: "stale", Count: 9, Seen: map[string]int{"x": 1}}
	if err := s.Load("absent.json", &v); err != nil {
		t.Fatalf("Load of a missing file: %v", err)
	}
	if !reflect.DeepEqual(v, sample{}) {
		t.Errorf("v = %+v, want the zero value", v)
	}
}

func TestLoadCorruptJSONFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var v sample
	if err := mustOpen(t, dir).Load("bad.json", &v); err == nil {
		t.Fatal("Load of corrupt JSON succeeded, want an error")
	}
}

func TestInvalidNamesRejected(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	for _, name := range []string{"", ".", "..", "a/b", "../escape.json", "lock"} {
		if err := s.Save(name, sample{}); err == nil {
			t.Errorf("Save(%q) succeeded, want an error", name)
		}
		var v sample
		if err := s.Load(name, &v); err == nil {
			t.Errorf("Load(%q) succeeded, want an error", name)
		}
	}
}

func TestLockExcludesSecondHolder(t *testing.T) {
	dir := t.TempDir()
	a, b := mustOpen(t, dir), mustOpen(t, dir)

	unlockA, ok, err := a.Lock(true)
	if err != nil || !ok {
		t.Fatalf("first Lock: ok = %v, err = %v", ok, err)
	}
	if _, ok, err := b.Lock(true); err != nil || ok {
		t.Fatalf("second non-blocking Lock while held: ok = %v, err = %v; want ok = false", ok, err)
	}
	unlockA()
	unlockA() // a second call must be harmless

	unlockB, ok, err := b.Lock(true)
	if err != nil || !ok {
		t.Fatalf("Lock after release: ok = %v, err = %v", ok, err)
	}
	unlockB()
}

func TestBlockingLockWaitsForRelease(t *testing.T) {
	dir := t.TempDir()
	a, b := mustOpen(t, dir), mustOpen(t, dir)

	unlockA, ok, err := a.Lock(false)
	if err != nil || !ok {
		t.Fatalf("first Lock: ok = %v, err = %v", ok, err)
	}
	acquired := make(chan func(), 1)
	var wg sync.WaitGroup
	wg.Go(func() {
		unlock, _, err := b.Lock(false)
		if err != nil {
			t.Errorf("blocking Lock: %v", err)
			return
		}
		acquired <- unlock
	})

	select {
	case <-acquired:
		t.Fatal("second blocking Lock was granted while the first was held")
	case <-time.After(50 * time.Millisecond):
	}
	unlockA()
	select {
	case unlock := <-acquired:
		unlock()
	case <-time.After(5 * time.Second):
		t.Fatal("blocking Lock was not granted after release")
	}
	wg.Wait()
}
