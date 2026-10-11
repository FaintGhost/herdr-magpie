// Package state keeps small JSON files in one directory. An flock on dir/lock
// serialises writers, and Save replaces files atomically.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
)

// lockName is the lock file. It is not a valid name for Load or Save.
const lockName = "lock"

// Store is a state directory.
type Store struct {
	dir string
}

// Open creates dir with mode 0700 if needed and returns its Store.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("state: empty directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("state: create %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// Lock takes an exclusive flock on dir/lock. With nonBlocking, a lock that is
// already held returns ok=false and no error instead of waiting. The returned
// unlock is never nil and may be called more than once.
func (s *Store) Lock(nonBlocking bool) (unlock func(), ok bool, err error) {
	noop := func() {}
	f, err := os.OpenFile(filepath.Join(s.dir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return noop, false, fmt.Errorf("state: open lock: %w", err)
	}
	how := syscall.LOCK_EX
	if nonBlocking {
		how |= syscall.LOCK_NB
	}
	fd := int(f.Fd())
	err = syscall.Flock(fd, how)
	for errors.Is(err, syscall.EINTR) {
		err = syscall.Flock(fd, how)
	}
	if err != nil {
		_ = f.Close()
		if nonBlocking && errors.Is(err, syscall.EWOULDBLOCK) {
			return noop, false, nil
		}
		return noop, false, fmt.Errorf("state: flock: %w", err)
	}
	unlock = sync.OnceFunc(func() {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
	})
	return unlock, true, nil
}

// Load decodes dir/name into v. v is first reset to its zero value. A missing
// file is not an error and leaves v zero. Content that is not valid JSON for v
// is an error.
func (s *Store) Load(name string, v any) error {
	if err := checkName(name); err != nil {
		return err
	}
	zero(v)
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("state: read %s: %w", name, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("state: decode %s: %w", name, err)
	}
	return nil
}

// Save writes v as indented JSON to dir/name. The data goes to a temporary file
// in the same directory, which is fsynced and renamed over the target, so a
// reader sees either the old content or the new. Files are mode 0600.
func (s *Store) Save(name string, v any) (err error) {
	if cerr := checkName(name); cerr != nil {
		return cerr
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("state: encode %s: %w", name, err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(s.dir, "."+name+".*.tmp")
	if err != nil {
		return fmt.Errorf("state: temp file for %s: %w", name, err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("state: write %s: %w", name, err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("state: sync %s: %w", name, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("state: close %s: %w", name, err)
	}
	if err = os.Rename(tmp.Name(), filepath.Join(s.dir, name)); err != nil {
		return fmt.Errorf("state: rename %s: %w", name, err)
	}
	s.syncDir()
	return nil
}

// syncDir fsyncs the directory so a completed rename survives a crash. Errors
// are ignored: the file itself is already synced, and some filesystems reject
// directory fsync.
func (s *Store) syncDir() {
	if d, err := os.Open(s.dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// checkName accepts only plain file names that do not collide with the lock.
func checkName(name string) error {
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) || name == lockName {
		return fmt.Errorf("state: invalid file name %q", name)
	}
	return nil
}

// zero sets *v to its zero value when v is a non-nil pointer.
func zero(v any) {
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && !rv.IsNil() {
		rv.Elem().SetZero()
	}
}
