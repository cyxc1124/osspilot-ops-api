package project

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	base   string
	secret string
	http   *http.Client
}

func New(base, secret string) *Client {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" || secret == "" {
		return nil
	}
	return &Client{base: base, secret: secret, http: &http.Client{Timeout: 10 * time.Second}}
}

type Account struct {
	Username           string  `json:"username"`
	PasswordHash       string  `json:"password_hash"`
	DisplayName        *string `json:"display_name"`
	Email              *string `json:"email"`
	Phone              *string `json:"phone"`
	Status             string  `json:"status"`
	MustChangePassword *bool   `json:"must_change_password,omitempty"`
}

type BucketItem struct {
	BucketName  string  `json:"bucket_name"`
	DisplayName *string `json:"display_name"`
}

func (c *Client) UpsertAccount(ctx context.Context, a Account) error {
	if c == nil {
		return nil
	}
	return c.do(ctx, http.MethodPut, "/internal/accounts/"+url.PathEscape(a.Username), a)
}

func (c *Client) DeleteAccount(ctx context.Context, username string) error {
	if c == nil {
		return nil
	}
	return c.do(ctx, http.MethodDelete, "/internal/accounts/"+url.PathEscape(username), nil)
}

func (c *Client) ReplaceBuckets(ctx context.Context, username string, items []BucketItem) error {
	if c == nil {
		return nil
	}
	if items == nil {
		items = []BucketItem{}
	}
	return c.do(ctx, http.MethodPut, "/internal/accounts/"+url.PathEscape(username)+"/buckets", map[string]any{"items": items})
}

func (c *Client) do(ctx context.Context, method, path string, body any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}
	slurp, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	return fmt.Errorf("tenant %s %s: %d %s", method, path, res.StatusCode, bytes.TrimSpace(slurp))
}
