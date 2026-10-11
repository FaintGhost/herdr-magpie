package magpie

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultGatewayURL = "http://127.0.0.1:3425"
	systemctlTimeout  = 3 * time.Second
	probeTimeout      = 2 * time.Second
	probeBodyLimit    = 4 << 10 // bytes of a probe response body inspected
)

// DiscoverOptions configures Discover. The zero value reads the process
// environment and runs the real systemctl.
type DiscoverOptions struct {
	ConfigURL string              // config gateway_url; wins when set
	Getenv    func(string) string // nil means os.Getenv
	Runner    Runner              // nil means ExecRunner{}
}

// found is a candidate gateway URL and where it came from. The zero value means no candidate.
type found struct {
	url    string
	source string
}

// fromURL labels the normalized raw value with source. It returns the zero found when raw is not a usable URL.
func fromURL(source, raw string) found {
	if u := normalizeURL(raw); u != "" {
		return found{url: u, source: source}
	}
	return found{}
}

// Discover returns the gateway base URL and where it came from. The order is:
// config, MAGPIE_GATEWAY_URL, MAGPIE_PUBLIC_URL, MAGPIE_ADDR, the
// magpie.service Environment of the system manager and then of the user
// manager, and finally http://127.0.0.1:3425. Sources are "config",
// "env:<NAME>", "systemd", "systemd-user" and "default".
func Discover(ctx context.Context, opts DiscoverOptions) (gatewayURL, source string) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if hit := cmp.Or(
		fromURL("config", opts.ConfigURL),
		fromURL("env:MAGPIE_GATEWAY_URL", getenv("MAGPIE_GATEWAY_URL")),
		fromURL("env:MAGPIE_PUBLIC_URL", getenv("MAGPIE_PUBLIC_URL")),
		fromURL("env:MAGPIE_ADDR", getenv("MAGPIE_ADDR")),
	); hit.url != "" {
		return hit.url, hit.source
	}
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	if u := unitGateway(ctx, runner, "systemctl", "show", "magpie.service", "-p", "Environment"); u != "" {
		return u, "systemd"
	}
	if u := unitGateway(ctx, runner, "systemctl", "--user", "show", "magpie.service", "-p", "Environment"); u != "" {
		return u, "systemd-user"
	}
	return defaultGatewayURL, "default"
}

// unitGateway asks systemctl for a unit's Environment and extracts the gateway
// URL from it. Any failure returns "" so discovery moves to the next step.
func unitGateway(ctx context.Context, r Runner, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, systemctlTimeout)
	defer cancel()
	out, err := r.Run(ctx, name, args...)
	if err != nil {
		return ""
	}
	return gatewayFromEnvironment(out)
}

// gatewayFromEnvironment takes the output of `systemctl show -p Environment`
// and returns MAGPIE_PUBLIC_URL, or MAGPIE_ADDR when that is missing or not
// usable. Other assignments are skipped without being stored, so their values
// cannot reach a result or an error.
func gatewayFromEnvironment(out []byte) string {
	text := strings.TrimPrefix(strings.TrimSpace(string(out)), "Environment=")
	var public, addr string
	for word := range shellWords(text) {
		key, value, ok := strings.Cut(word, "=")
		if !ok {
			continue
		}
		switch key {
		case "MAGPIE_PUBLIC_URL":
			public = value
		case "MAGPIE_ADDR":
			addr = value
		}
	}
	return cmp.Or(normalizeURL(public), normalizeURL(addr))
}

// shellWords yields the words of a systemd Environment= list. Words are split
// on unquoted whitespace. Double or single quotes group text, and a backslash
// escapes the next byte outside single quotes. Empty words are skipped.
func shellWords(s string) iter.Seq[string] {
	return func(yield func(string) bool) {
		var word []byte
		var quote byte // 0, '"' or '\''
		emit := func() bool {
			if len(word) == 0 {
				return true
			}
			ok := yield(string(word))
			word = word[:0]
			return ok
		}
		for i := 0; i < len(s); i++ {
			c := s[i]
			switch {
			case quote == '\'':
				if c == '\'' {
					quote = 0
				} else {
					word = append(word, c)
				}
			case c == '\\' && i+1 < len(s):
				i++
				word = append(word, s[i])
			case quote == '"' && c == '"':
				quote = 0
			case quote == 0 && (c == '"' || c == '\''):
				quote = c
			case quote == 0 && isSpace(c):
				if !emit() {
					return
				}
			default:
				word = append(word, c)
			}
		}
		emit()
	}
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// normalizeURL trims s, adds http:// when there is no scheme, and removes
// trailing slashes. It returns "" unless the result is an http(s) URL with a host.
func normalizeURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	s = strings.TrimRight(s, "/")
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return s
}

// Health is the outcome of one gateway probe.
type Health struct {
	Up      bool          // the endpoint answered like a magpie gateway
	Status  int           // HTTP status code; 0 when no response arrived
	Latency time.Duration // how long the probe took
	Err     string        // why the gateway is down; empty when Up
}

// Probe checks liveness with an unauthenticated GET <gatewayURL>/v1/models,
// capped at 2 s. It is up on any 2xx. It is also up on a 401 whose first 4 KB
// contain "magpie" (case-insensitive): a LAN gateway rejects anonymous calls
// but identifies itself. Everything else is down. A nil client uses a default one.
func Probe(ctx context.Context, client *http.Client, gatewayURL string) Health {
	if gatewayURL == "" {
		return Health{Err: "no gateway URL"}
	}
	if client == nil {
		client = &http.Client{}
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	start := time.Now()
	h := probeEndpoint(ctx, client, strings.TrimRight(gatewayURL, "/")+"/v1/models")
	h.Latency = time.Since(start)
	return h
}

// probeEndpoint sends the request and classifies the response.
func probeEndpoint(ctx context.Context, client *http.Client, endpoint string) Health {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Health{Err: "invalid gateway URL"}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Health{Err: transportErr(err)}
	}
	defer resp.Body.Close()
	// A read error only shortens the body inspected, so the status still decides.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, probeBodyLimit))

	h := Health{Status: resp.StatusCode}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		h.Up = true
	case resp.StatusCode == http.StatusUnauthorized && bytes.Contains(bytes.ToLower(body), []byte("magpie")):
		h.Up = true
	case resp.StatusCode == http.StatusUnauthorized:
		h.Err = "401 from a service that does not identify as magpie"
	default:
		h.Err = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return h
}

// transportErr describes a failed request without the URL, which could carry userinfo.
func transportErr(err error) string {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		err = ue.Err
	}
	return err.Error()
}
