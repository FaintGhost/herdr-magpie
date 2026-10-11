package panel

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"
)

func TestRunRequiresItsDeps(t *testing.T) {
	if err := Run(t.Context(), Deps{}); err == nil {
		t.Error("Run without Source, In and Out should fail")
	}
}

// TestRunQuitsOnQ drives the real program from a pipe, with no terminal: it draws
// from the cache, runs the refresh, and returns once q arrives.
func TestRunQuitsOnQ(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	cache := newFakeCache()
	seedCache(t, cache)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Deps{
			Source: sampleSource(),
			Cache:  cache,
			In:     r,
			Out:    &out,
			Now:    func() time.Time { return testNow },
		})
	}()
	// "2" and "q" arrive in one read, which must still switch tab and then quit.
	if _, err := w.Write([]byte("2q")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v, want nil after q", err)
		}
	case <-time.After(10 * time.Second):
		cancel()
		<-done // Run must be gone before the pipe closes under it
		t.Fatal("Run did not return after q")
	}
	if out.Len() == 0 {
		t.Error("Run drew nothing")
	}
}
