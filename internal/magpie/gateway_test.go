package magpie

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	sysKey  = "systemctl show magpie.service -p Environment"
	userKey = "systemctl --user show magpie.service -p Environment"
)

// envFunc serves environment lookups from a map. A nil map means an empty environment.
func envFunc(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// unitRunner returns the given unit outputs; a failing unit query returns an error.
func unitRunner(system, user string, systemErr, userErr bool) *fakeRunner {
	r := &fakeRunner{
		out:  map[string]string{sysKey: system, userKey: user},
		errs: map[string]error{},
	}
	if systemErr {
		r.errs[sysKey] = errors.New("unit query failed")
	}
	if userErr {
		r.errs[userKey] = errors.New("unit query failed")
	}
	return r
}

// The addresses below are TEST-NET-1 documentation addresses, not real hosts.
func TestDiscover(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		env        map[string]string
		system     string
		systemErr  bool
		user       string
		userErr    bool
		wantURL    string
		wantSource string
		wantCalls  []string
	}{
		{
			name:       "config wins over everything",
			config:     "192.0.2.1:3425/",
			env:        map[string]string{"MAGPIE_PUBLIC_URL": "192.0.2.2:3425"},
			wantURL:    "http://192.0.2.1:3425",
			wantSource: "config",
		},
		{
			name:       "invalid config is skipped",
			config:     "ftp://192.0.2.1",
			env:        map[string]string{"MAGPIE_ADDR": "192.0.2.3:3425"},
			wantURL:    "http://192.0.2.3:3425",
			wantSource: "env:MAGPIE_ADDR",
		},
		{
			name:       "MAGPIE_GATEWAY_URL keeps its scheme",
			env:        map[string]string{"MAGPIE_GATEWAY_URL": "https://gw.example.test/"},
			wantURL:    "https://gw.example.test",
			wantSource: "env:MAGPIE_GATEWAY_URL",
		},
		{
			name:       "MAGPIE_PUBLIC_URL beats MAGPIE_ADDR",
			env:        map[string]string{"MAGPIE_PUBLIC_URL": "http://192.0.2.4:3425/", "MAGPIE_ADDR": "192.0.2.5:3425"},
			wantURL:    "http://192.0.2.4:3425",
			wantSource: "env:MAGPIE_PUBLIC_URL",
		},
		{
			name:       "unusable MAGPIE_PUBLIC_URL falls back to MAGPIE_ADDR",
			env:        map[string]string{"MAGPIE_PUBLIC_URL": ":3425", "MAGPIE_ADDR": "192.0.2.5:3425"},
			wantURL:    "http://192.0.2.5:3425",
			wantSource: "env:MAGPIE_ADDR",
		},
		{
			name:       "system unit Environment",
			system:     "Environment=HOME=/root PATH=/usr/bin MAGPIE_ADDR=192.0.2.6:3425 MAGPIE_PUBLIC_URL=http://192.0.2.6:3425",
			wantURL:    "http://192.0.2.6:3425",
			wantSource: "systemd",
			wantCalls:  []string{sysKey},
		},
		{
			name:       "system unit prefers MAGPIE_PUBLIC_URL over MAGPIE_ADDR",
			system:     "Environment=MAGPIE_ADDR=192.0.2.7:3425 MAGPIE_PUBLIC_URL=http://192.0.2.8:3425",
			wantURL:    "http://192.0.2.8:3425",
			wantSource: "systemd",
			wantCalls:  []string{sysKey},
		},
		{
			name:       "double-quoted entries",
			system:     `Environment="SCRATCH_NOTE=two words" MAGPIE_PUBLIC_URL="http://192.0.2.9:3425"`,
			wantURL:    "http://192.0.2.9:3425",
			wantSource: "systemd",
			wantCalls:  []string{sysKey},
		},
		{
			name:       "backslash-escaped space",
			system:     `Environment=SCRATCH_NOTE=two\ words MAGPIE_ADDR=192.0.2.10:3425`,
			wantURL:    "http://192.0.2.10:3425",
			wantSource: "systemd",
			wantCalls:  []string{sysKey},
		},
		{
			name:       "single-quoted entry",
			system:     `Environment='MAGPIE_ADDR=192.0.2.11:3425' HOME=/root`,
			wantURL:    "http://192.0.2.11:3425",
			wantSource: "systemd",
			wantCalls:  []string{sysKey},
		},
		{
			name:       "user unit is used when the system unit has no gateway",
			system:     "Environment=HOME=/root PATH=/usr/bin",
			user:       "Environment=MAGPIE_PUBLIC_URL=http://192.0.2.12:3425",
			wantURL:    "http://192.0.2.12:3425",
			wantSource: "systemd-user",
			wantCalls:  []string{sysKey, userKey},
		},
		{
			name:       "failing system query falls through to the user unit",
			systemErr:  true,
			user:       "Environment=MAGPIE_ADDR=192.0.2.13:3425",
			wantURL:    "http://192.0.2.13:3425",
			wantSource: "systemd-user",
			wantCalls:  []string{sysKey, userKey},
		},
		{
			name:       "nothing found uses the default",
			system:     "Environment=",
			wantURL:    defaultGatewayURL,
			wantSource: "default",
			wantCalls:  []string{sysKey, userKey},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := unitRunner(tt.system, tt.user, tt.systemErr, tt.userErr)
			gotURL, gotSource := Discover(t.Context(), DiscoverOptions{
				ConfigURL: tt.config,
				Getenv:    envFunc(tt.env),
				Runner:    r,
			})
			if gotURL != tt.wantURL || gotSource != tt.wantSource {
				t.Errorf("Discover() = (%q, %q), want (%q, %q)", gotURL, gotSource, tt.wantURL, tt.wantSource)
			}
			if !slices.Equal(r.calls, tt.wantCalls) {
				t.Errorf("runner calls = %q, want %q", r.calls, tt.wantCalls)
			}
		})
	}
}

// TestDiscoverReturnsNoOtherVariables checks that other assignments in the
// unit's Environment never reach the result, even when they look like secrets.
func TestDiscoverReturnsNoOtherVariables(t *testing.T) {
	const secret = "s3cr3t-value-for-test"
	env := "Environment=API_KEY=" + secret + ` OTHER="` + secret + `" MAGPIE_ADDR=192.0.2.20:3425`
	r := unitRunner(env, "", false, false)
	gotURL, gotSource := Discover(t.Context(), DiscoverOptions{Getenv: envFunc(nil), Runner: r})
	if gotURL != "http://192.0.2.20:3425" || gotSource != "systemd" {
		t.Fatalf("Discover() = (%q, %q), want the MAGPIE_ADDR gateway from systemd", gotURL, gotSource)
	}
	if strings.Contains(gotURL+gotSource, secret) {
		t.Errorf("Discover() result contains a value from another variable")
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"192.0.2.1:3425", "http://192.0.2.1:3425"},
		{"  192.0.2.1:3425/  ", "http://192.0.2.1:3425"},
		{"http://192.0.2.1:3425/", "http://192.0.2.1:3425"},
		{"https://gw.example.test//", "https://gw.example.test"},
		{"localhost:3425", "http://localhost:3425"},
		{"ftp://192.0.2.1", ""},
		{"http://:3425", ""},
		{":3425", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := normalizeURL(tt.in); got != tt.want {
				t.Errorf("normalizeURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestShellWords(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"a b", []string{"a", "b"}},
		{"  lead\ttrail  ", []string{"lead", "trail"}},
		{`"a b" c`, []string{"a b", "c"}},
		{`a\ b`, []string{"a b"}},
		{`"a\"b"`, []string{`a"b`}},
		{`'x\y' z`, []string{`x\y`, "z"}},
		{`KEY="v w"x`, []string{"KEY=v wx"}},
		{`"" z`, []string{"z"}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := slices.Collect(shellWords(tt.in))
			if !slices.Equal(got, tt.want) {
				t.Errorf("shellWords(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestProbe(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantUp     bool
		wantStatus int
	}{
		{
			name:       "200 is up",
			status:     http.StatusOK,
			body:       `{"object":"list","data":[]}`,
			wantUp:     true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "204 is up",
			status:     http.StatusNoContent,
			wantUp:     true,
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "401 whose body names magpie is up",
			status:     http.StatusUnauthorized,
			body:       `{"error":{"message":"no API key was sent: send a magpie gateway key"}}`,
			wantUp:     true,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "401 magpie match ignores case",
			status:     http.StatusUnauthorized,
			body:       `{"error":"MAGPIE says no"}`,
			wantUp:     true,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "401 from another service is down",
			status:     http.StatusUnauthorized,
			body:       `{"error":"invalid credentials"}`,
			wantUp:     false,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "magpie past the first 4 KB does not count",
			status:     http.StatusUnauthorized,
			body:       strings.Repeat("x", 5000) + "magpie",
			wantUp:     false,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "500 is down even with magpie in the body",
			status:     http.StatusInternalServerError,
			body:       "magpie internal error",
			wantUp:     false,
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("X-Api-Key") != "" {
					t.Error("probe sent a credential header")
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			h := Probe(t.Context(), srv.Client(), srv.URL)
			if h.Up != tt.wantUp {
				t.Errorf("Up = %v, want %v (Err %q)", h.Up, tt.wantUp, h.Err)
			}
			if h.Status != tt.wantStatus {
				t.Errorf("Status = %d, want %d", h.Status, tt.wantStatus)
			}
			if (h.Err != "") == h.Up {
				t.Errorf("Err = %q with Up = %v; Err must be set exactly when down", h.Err, h.Up)
			}
			if h.Latency <= 0 {
				t.Errorf("Latency = %v, want > 0", h.Latency)
			}
		})
	}
}

func TestProbeTrailingSlash(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	if h := Probe(t.Context(), srv.Client(), srv.URL+"/"); !h.Up {
		t.Errorf("Probe with trailing slash: Up = false, Err %q", h.Err)
	}
}

func TestProbeConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close() // nothing listens on this port any more

	h := Probe(t.Context(), nil, base)
	if h.Up || h.Status != 0 || h.Err == "" {
		t.Fatalf("Probe() = %+v, want down with no status and an error", h)
	}
	if strings.Contains(h.Err, "/v1/models") {
		t.Errorf("Err %q includes the request path", h.Err)
	}
}

func TestProbeTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	client := &http.Client{Timeout: 50 * time.Millisecond}
	h := Probe(t.Context(), client, srv.URL)
	if h.Up || h.Err == "" {
		t.Fatalf("Probe() = %+v, want down with an error on timeout", h)
	}
}

func TestProbeEmptyURL(t *testing.T) {
	if h := Probe(t.Context(), nil, ""); h.Up || h.Err == "" {
		t.Errorf("Probe with empty URL = %+v, want down with an error", h)
	}
}
