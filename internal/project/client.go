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

type HTTPError struct {
	Status int
	Detail string
}

func (e *HTTPError) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	return fmt.Sprintf("tenant api status %d", e.Status)
}

type AccessItem struct {
	ID          int64   `json:"id"`
	AccountID   int64   `json:"account_id"`
	AccountName string  `json:"account_name"`
	Status      string  `json:"status"`
	RequestedBy *int64  `json:"requested_by"`
	RequestedAt any     `json:"requested_at"`
	ReviewedBy  *int64  `json:"reviewed_by"`
	ReviewedAt  any     `json:"reviewed_at"`
	ReviewNote  *string `json:"review_note"`
	RGWUID      *string `json:"rgw_uid"`
	AppCount    int64   `json:"application_count"`
	KeyCount    int64   `json:"access_key_count"`
}

func (c *Client) ListAccess(ctx context.Context, status string) ([]AccessItem, error) {
	path := "/internal/api-access"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	var out struct {
		Items []AccessItem `json:"items"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		return []AccessItem{}, nil
	}
	return out.Items, nil
}

func (c *Client) GetAccess(ctx context.Context, username string) (*AccessItem, error) {
	var out AccessItem
	if err := c.doJSON(ctx, http.MethodGet, "/internal/api-access/"+url.PathEscape(username), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) PutSettings(ctx context.Context, settings map[string]string) error {
	if c == nil {
		return nil
	}
	return c.do(ctx, http.MethodPut, "/internal/settings", map[string]any{"settings": settings})
}

type UsageBucket struct {
	BucketName  string `json:"bucket_name"`
	UsedBytes   int64  `json:"used_bytes"`
	ObjectCount int64  `json:"object_count"`
	TrashBytes  int64  `json:"trash_bytes"`
	TrashCount  int64  `json:"trash_object_count"`
}

type Usage struct {
	UsedBytes    int64         `json:"used_bytes"`
	ObjectCount  int64         `json:"object_count"`
	TrashBytes   int64         `json:"trash_bytes"`
	TrashCount   int64         `json:"trash_object_count"`
	VersionBytes int64         `json:"version_bytes"`
	VersionCount int64         `json:"version_object_count"`
	Buckets      []UsageBucket `json:"buckets"`
}

func (c *Client) GetUsage(ctx context.Context) (*Usage, error) {
	if c == nil {
		return nil, &HTTPError{Status: http.StatusServiceUnavailable, Detail: "tenant projection is not configured"}
	}
	var out Usage
	if err := c.doJSON(ctx, http.MethodGet, "/internal/stats/usage", nil, &out); err != nil {
		return nil, err
	}
	if out.Buckets == nil {
		out.Buckets = []UsageBucket{}
	}
	return &out, nil
}

func (c *Client) EnqueueInventory(ctx context.Context, bucketName string) (string, error) {
	var out struct {
		JobID string `json:"job_id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/internal/buckets/"+url.PathEscape(bucketName)+"/inventory", map[string]any{}, &out); err != nil {
		return "", err
	}
	return out.JobID, nil
}

func (c *Client) ReviewAccess(ctx context.Context, username, action string, note *string) (*AccessItem, error) {
	body := map[string]any{}
	if note != nil {
		body["note"] = *note
	}
	var out AccessItem
	path := "/internal/api-access/" + url.PathEscape(username) + "/" + action
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type RawResponse struct {
	Status      int
	ContentType string
	Body        []byte
}

// DoRaw calls a tenant internal path and returns status + body (capped at 8MiB).
func (c *Client) DoRaw(ctx context.Context, method, path string, body io.Reader, contentType string) (*RawResponse, error) {
	if c == nil {
		return nil, &HTTPError{Status: http.StatusServiceUnavailable, Detail: "tenant projection is not configured"}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return &RawResponse{Status: res.StatusCode, ContentType: res.Header.Get("Content-Type"), Body: raw}, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any) error {
	return c.doJSON(ctx, method, path, body, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, dest any) error {
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
		if dest == nil {
			return nil
		}
		return json.NewDecoder(res.Body).Decode(dest)
	}
	slurp, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	return &HTTPError{Status: res.StatusCode, Detail: detailFromBody(slurp, res.StatusCode)}
}

func detailFromBody(b []byte, status int) string {
	var e struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(b, &e) == nil && e.Detail != "" {
		return e.Detail
	}
	if s := strings.TrimSpace(string(b)); s != "" {
		return s
	}
	return http.StatusText(status)
}
