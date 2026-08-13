package ceph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type mgmtError struct {
	msg string
}

func (e mgmtError) Error() string { return e.msg }

type client struct {
	http *http.Client
}

func newClient() *client {
	return &client{http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *client) get(ctx context.Context, base, path string) (any, error) {
	return c.do(ctx, http.MethodGet, base, path, nil, 10*time.Second)
}

func (c *client) post(ctx context.Context, base, path string, body any, timeout time.Duration) (any, error) {
	return c.do(ctx, http.MethodPost, base, path, body, timeout)
}

func (c *client) do(ctx context.Context, method, base, path string, body any, timeout time.Duration) (any, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil, mgmtError{msg: "Ceph management API is not configured (set in ops system settings)"}
	}
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, r)
	if err != nil {
		return nil, mgmtError{msg: "Ceph management API is unreachable"}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	httpClient := c.http
	if timeout != httpClient.Timeout {
		clone := *httpClient
		clone.Timeout = timeout
		httpClient = &clone
	}
	res, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil || strings.Contains(err.Error(), "Timeout") || strings.Contains(err.Error(), "deadline") {
			return nil, mgmtError{msg: "Ceph management API request timed out"}
		}
		return nil, mgmtError{msg: "Ceph management API is unreachable"}
	}
	defer res.Body.Close()
	slurp, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 400 {
		return nil, mgmtError{msg: fmt.Sprintf("Ceph management API returned HTTP %d", res.StatusCode)}
	}
	if len(bytes.TrimSpace(slurp)) == 0 {
		return map[string]any{}, nil
	}
	var out any
	if err := json.Unmarshal(slurp, &out); err != nil {
		return nil, mgmtError{msg: "Ceph management API returned invalid JSON"}
	}
	return out, nil
}
