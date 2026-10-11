package magpie

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// testKey is a made-up gateway key for these tests. It is not a real key.
const testKey = "synthetic-key-for-tests"

// gatewayLog records what a fake gateway received: each request path and its Authorization header.
type gatewayLog struct {
	mu    sync.Mutex
	paths []string
	auth  []string
}

func (l *gatewayLog) record(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.paths = append(l.paths, r.URL.Path)
	l.auth = append(l.auth, r.Header.Get("Authorization"))
}

func (l *gatewayLog) seen() (paths, auth []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.paths), slices.Clone(l.auth)
}

// fakeGateway answers /api/hello with hello, and /v1/magpie/usage with usage only
// when the request carries testKey. Anything else is a 404.
func fakeGateway(t *testing.T, hello, usage string) (*httptest.Server, *gatewayLog) {
	t.Helper()
	log := &gatewayLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		log.record(req)
		switch req.URL.Path {
		case helloPath:
			io.WriteString(w, hello)
		case usagePath:
			if req.Header.Get("Authorization") != "Bearer "+testKey {
				http.Error(w, `{"error":{"message":"no API key was sent"}}`, http.StatusUnauthorized)
				return
			}
			io.WriteString(w, usage)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, log
}

// writeKey writes a gateway key file the way the plugin keeps it: one line, mode 0600.
func writeKey(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gateway-key")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUsageSendsKeyAndMapsSummary(t *testing.T) {
	srv, log := fakeGateway(t, "{}", string(readFixture(t, "usage_edge.json")))
	c := APIClient{BaseURL: srv.URL + "/", KeyFile: writeKey(t, testKey+"\n"), HTTP: srv.Client()}
	got, err := c.Usage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "usage_edge", got)
	paths, auth := log.seen()
	if !slices.Equal(paths, []string{usagePath}) || !slices.Equal(auth, []string{"Bearer " + testKey}) {
		t.Errorf("requests = %q, auth = %q; want one usage call with the key", paths, auth)
	}
}

func TestUsageWithoutKeyFile(t *testing.T) {
	srv, log := fakeGateway(t, "{}", "{}")
	c := APIClient{BaseURL: srv.URL, KeyFile: filepath.Join(t.TempDir(), "missing"), HTTP: srv.Client()}
	got, err := c.Usage(t.Context())
	if err != nil || got != nil {
		t.Errorf("Usage() = %v, %v; want nil, nil", got, err)
	}
	if paths, _ := log.seen(); len(paths) != 0 {
		t.Errorf("sent %q without a key file", paths)
	}
}

func TestUsageEmptyKeyFile(t *testing.T) {
	srv, log := fakeGateway(t, "{}", "{}")
	c := APIClient{BaseURL: srv.URL, KeyFile: writeKey(t, "\n"), HTTP: srv.Client()}
	got, err := c.Usage(t.Context())
	if err != nil || got != nil {
		t.Errorf("Usage() = %v, %v; want nil, nil", got, err)
	}
	if paths, _ := log.seen(); len(paths) != 0 {
		t.Errorf("sent %q with an empty key", paths)
	}
}

func TestUsageRefusedErrorOmitsKey(t *testing.T) {
	const wrong = "not-the-right-key"
	srv, _ := fakeGateway(t, "{}", "{}")
	c := APIClient{BaseURL: srv.URL, KeyFile: writeKey(t, wrong), HTTP: srv.Client()}
	_, err := c.Usage(t.Context())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want an HTTP 401 error", err)
	}
	if strings.Contains(err.Error(), wrong) {
		t.Errorf("error %q carries the key", err)
	}
}

func TestHelloSendsNoKey(t *testing.T) {
	srv, log := fakeGateway(t, `{"name":"magpie","version":"0.0.0-test"}`, "{}")
	c := APIClient{BaseURL: srv.URL, KeyFile: writeKey(t, testKey+"\n"), HTTP: srv.Client()}
	name, version, err := c.Hello(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if name != "magpie" || version != "0.0.0-test" {
		t.Errorf("Hello() = %q, %q", name, version)
	}
	paths, auth := log.seen()
	if !slices.Equal(paths, []string{helloPath}) || !slices.Equal(auth, []string{""}) {
		t.Errorf("requests = %q, auth = %q; want one hello call without a key", paths, auth)
	}
}

func TestHelloNotMagpie(t *testing.T) {
	srv, _ := fakeGateway(t, `{"name":"other","version":"1"}`, "{}")
	c := APIClient{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, _, err := c.Hello(t.Context()); err == nil {
		t.Error("Hello succeeded for a server that does not name magpie")
	}
}

func TestHelloUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Error(w, `{"error":{"message":"send a magpie gateway key"}}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	c := APIClient{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, _, err := c.Hello(t.Context()); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v, want an HTTP 401 error", err)
	}
}

func TestAPIClientNeedsBaseURL(t *testing.T) {
	if _, _, err := (APIClient{}).Hello(t.Context()); err == nil {
		t.Error("Hello with no base URL succeeded")
	}
	if _, err := (APIClient{KeyFile: writeKey(t, testKey)}).Usage(t.Context()); err == nil {
		t.Error("Usage with no base URL succeeded")
	}
}
