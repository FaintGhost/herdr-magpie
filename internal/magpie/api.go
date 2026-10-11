package magpie

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	helloPath = "/api/hello"
	usagePath = "/v1/magpie/usage"
	// usagePeriod is the window the panel reads. The gateway's own default is the
	// last 30 days, so the period must be asked for.
	usagePeriod = "today"
	// apiTimeout caps one gateway request.
	apiTimeout = 5 * time.Second
	// usageBodyLimit bounds one response. The usage answer lists every session, so it can grow large.
	usageBodyLimit = 64 << 20
)

// APIClient reads the gateway. It calls only GET /api/hello and GET
// /v1/magpie/usage. The gateway key is read from KeyFile on each call, sent only
// as an Authorization header, and never logged or put in an error.
type APIClient struct {
	BaseURL string       // the gateway's base URL, as Discover returns it
	KeyFile string       // the gateway key file: one line, mode 0600
	HTTP    *http.Client // nil means http.DefaultClient
}

// Hello calls GET /api/hello, which takes no key, and returns the name and version
// magpie reports. An answer that does not name magpie is an error. From an
// address other than this machine, the gateway answers 401 without a key, and
// Hello returns that error.
func (c APIClient) Hello(ctx context.Context) (name, version string, err error) {
	return c.hello(ctx, "")
}

// HelloWithKey calls GET /api/hello with the gateway key from KeyFile, which
// shows whether the gateway accepts that key. A missing or empty key file is an
// error here, and a key the gateway refuses gives an HTTP 401 error.
func (c APIClient) HelloWithKey(ctx context.Context) (name, version string, err error) {
	key, err := readGatewayKey(c.KeyFile)
	if err != nil {
		return "", "", err
	}
	if key == "" {
		return "", "", errors.New("gateway key: the key file is empty or missing")
	}
	return c.hello(ctx, key)
}

// hello calls GET /api/hello, sending key as a bearer token when it is not empty.
func (c APIClient) hello(ctx context.Context, key string) (name, version string, err error) {
	var body struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err = c.get(ctx, helloPath, key, &body); err != nil {
		return "", "", err
	}
	if body.Name != "magpie" {
		return "", "", fmt.Errorf("gateway %s: not a magpie gateway", helloPath)
	}
	return body.Name, body.Version, nil
}

// Usage calls GET /v1/magpie/usage?period=today with the gateway key, so the
// summary covers the gateway's current day. It returns nil, nil when there is no
// key file or the file is empty, so the panel shows no API numbers.
func (c APIClient) Usage(ctx context.Context) (*UsageSummary, error) {
	key, err := readGatewayKey(c.KeyFile)
	if err != nil || key == "" {
		return nil, err
	}
	var body usageBody
	if err := c.get(ctx, usagePath+"?period="+usagePeriod, key, &body); err != nil {
		return nil, err
	}
	return body.summary(), nil
}

// get sends GET base+path, with key as a bearer token when it is not empty, and
// decodes the JSON answer into v.
func (c APIClient) get(ctx context.Context, path, key string, v any) error {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		return errors.New("no gateway URL")
	}
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return fmt.Errorf("gateway %s: invalid URL", path)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("gateway %s: %s", path, transportErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gateway %s: HTTP %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, usageBodyLimit)).Decode(v); err != nil {
		return fmt.Errorf("gateway %s: decode: %w", path, err)
	}
	return nil
}

// readGatewayKey returns the key in path, or "" when the file does not exist.
func readGatewayKey(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("gateway key: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// usageBody is the part of GET /v1/magpie/usage the panel reads. The answer also
// holds sessions, accounts, provider keys and a time series, which are left out.
type usageBody struct {
	Calls  int          `json:"calls"`
	Cost   float64      `json:"cost"`
	Agents []usageGroup `json:"agents"`
	Models []usageGroup `json:"models"`
}

type usageGroup struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Calls  int     `json:"calls"`
	Errors int     `json:"errors"`
	Cost   float64 `json:"cost"`
}

func (b usageBody) summary() *UsageSummary {
	s := &UsageSummary{Calls: b.Calls, Cost: b.Cost}
	for _, g := range b.Agents {
		s.Agents = append(s.Agents, g.row())
	}
	for _, g := range b.Models {
		s.Models = append(s.Models, g.row())
	}
	return s
}

// row names a group by its display name, or by its id when the name is empty.
func (g usageGroup) row() UsageRow {
	name := g.Name
	if name == "" {
		name = g.ID
	}
	return UsageRow{Name: name, Calls: g.Calls, Errors: g.Errors, Cost: g.Cost}
}
