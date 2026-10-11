package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testGatewayKey is a made-up gateway key for these tests. It is not a real key.
const testGatewayKey = "synthetic-gateway-key-for-tests"

// keyFileWithMode writes content to a gateway-key file in a temporary directory and
// sets its mode, which WriteFile alone would leave to the umask.
func keyFileWithMode(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gateway-key")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDoctorGatewayKey(t *testing.T) {
	// The fake gateway accepts only testGatewayKey on /api/hello.
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/hello" {
			http.NotFound(w, req)
			return
		}
		if req.Header.Get("Authorization") != "Bearer "+testGatewayKey {
			http.Error(w, `{"error":{"message":"no API key was sent"}}`, http.StatusUnauthorized)
			return
		}
		io.WriteString(w, `{"name":"magpie","version":"0.1.1201"}`)
	}))
	t.Cleanup(gateway.Close)

	missing := filepath.Join(t.TempDir(), "gateway-key")
	tests := []struct {
		name      string
		keyPath   string
		gatewayUp bool
		wantFails int
		wantWarn  bool
		wantText  string
	}{
		{name: "missing key is a warning", keyPath: missing, gatewayUp: true, wantWarn: true, wantText: "not set"},
		{name: "wrong mode fails", keyPath: keyFileWithMode(t, testGatewayKey+"\n", 0o644), gatewayUp: true, wantFails: 1, wantText: "mode 0644, want 0600"},
		{name: "empty key is a warning", keyPath: keyFileWithMode(t, "", 0o600), gatewayUp: true, wantWarn: true, wantText: "is empty"},
		{name: "gateway down skips the call", keyPath: keyFileWithMode(t, testGatewayKey+"\n", 0o600), gatewayUp: false, wantWarn: true, wantText: "gateway is down"},
		{name: "accepted key passes", keyPath: keyFileWithMode(t, testGatewayKey+"\n", 0o600), gatewayUp: true, wantText: "accepted by /api/hello (magpie 0.1.1201)"},
		{name: "refused key fails", keyPath: keyFileWithMode(t, "some-other-key\n", 0o600), gatewayUp: true, wantFails: 1, wantText: "HTTP 401"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			r := &reporter{out: &out}
			doctorGatewayKey(t.Context(), r, tc.keyPath, gateway.URL, tc.gatewayUp, gateway.Client())
			if r.failures != tc.wantFails {
				t.Errorf("failures = %d, want %d; output:\n%s", r.failures, tc.wantFails, out.String())
			}
			text := out.String()
			if !strings.Contains(text, tc.wantText) {
				t.Errorf("output lacks %q:\n%s", tc.wantText, text)
			}
			if got := strings.Contains(text, "warn"); got != tc.wantWarn {
				t.Errorf("warning printed = %t, want %t:\n%s", got, tc.wantWarn, text)
			}
			for _, secret := range []string{testGatewayKey, "some-other-key"} {
				if strings.Contains(text, secret) {
					t.Errorf("output carries the key %q:\n%s", secret, text)
				}
			}
		})
	}
}

func TestGatewayKeyPathBesideConfig(t *testing.T) {
	got := gatewayKeyPath("/cfg/faintghost.magpie/config.json")
	if want := "/cfg/faintghost.magpie/gateway-key"; got != want {
		t.Errorf("gatewayKeyPath = %q, want %q", got, want)
	}
}
