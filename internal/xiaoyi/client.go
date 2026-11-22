package xiaoyi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// UpstreamError reports a non-2xx answer from the local XiaoyiWork proxy.
type UpstreamError struct {
	Status int
	Body   string
}

func (e *UpstreamError) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > 800 {
		body = body[:800] + "…"
	}
	if body == "" {
		body = http.StatusText(e.Status)
	}
	return fmt.Sprintf("小艺Work 上游返回 HTTP %d: %s", e.Status, body)
}

// IsUnauthorized reports whether the upstream rejected the proxy key.
func IsUnauthorized(err error) bool {
	if up, ok := err.(*UpstreamError); ok {
		return up.Status == http.StatusUnauthorized || up.Status == http.StatusForbidden
	}
	return false
}

// Client talks to the local OpenAI-compatible model proxy.
type Client struct {
	baseURL string
	http    *http.Client
}

// Auth is what one upstream request needs: a local proxy key or the cloud
// business credential bundle.
type Auth struct {
	LocalKey           string
	BusinessCredential string
	UID                string
	DeviceID           string
}

// NewClient builds a client that never uses the system proxy: the upstream is
// loopback and honouring HTTP_PROXY would break it.
func NewClient(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DisableCompression:    true,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ResponseHeaderTimeout: 90 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   8,
	}
	return &Client{baseURL: baseURL, http: &http.Client{Transport: transport}}
}

// BaseURL returns the configured proxy root (without /chat/completions).
func (c *Client) BaseURL() string { return c.baseURL }

// ChatStream opens a streaming chat completion. The caller must close the body.
func (c *Client) ChatStream(ctx context.Context, auth Auth, payload []byte) (io.ReadCloser, http.Header, error) {
	req, err := c.newRequest(ctx, auth, payload)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("连接小艺Work 本地代理失败（请确认小艺Work 正在运行）: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		resp.Body.Close()
		return nil, nil, &UpstreamError{Status: resp.StatusCode, Body: string(body)}
	}
	return resp.Body, resp.Header, nil
}

// Chat performs a non-streaming chat completion.
func (c *Client) Chat(ctx context.Context, auth Auth, payload []byte) ([]byte, http.Header, error) {
	req, err := c.newRequest(ctx, auth, payload)
	if err != nil {
		return nil, nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("连接小艺Work 本地代理失败（请确认小艺Work 正在运行）: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
	if err != nil {
		return nil, nil, fmt.Errorf("读取小艺Work 上游响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, &UpstreamError{Status: resp.StatusCode, Body: string(body)}
	}
	return body, resp.Header, nil
}

// Probe checks that the local proxy is listening. The proxy exposes no /models
// route, so a TCP dial is the only cheap liveness check.
func (c *Client) Probe(ctx context.Context, key string) error {
	_ = key
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return err
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":80"
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return fmt.Errorf("小艺Work 本地代理未监听 %s: %w", host, err)
	}
	return conn.Close()
}

func (c *Client) newRequest(ctx context.Context, auth Auth, payload []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "cpa-plugin-xiaoyiwork/0.1.0")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "identity")
	if isLoopback(c.baseURL) {
		if key := strings.TrimSpace(auth.LocalKey); key != "" {
			// XiaoyiWork accepts X-Api-Key and only the exact "Bearer" spelling.
			req.Header.Set("X-Api-Key", key)
			req.Header.Set("Authorization", "Bearer "+key)
		}
		return req, nil
	}
	req.Header.Set("businessCredential", strings.TrimSpace(auth.BusinessCredential))
	req.Header.Set("x-uid", strings.TrimSpace(auth.UID))
	req.Header.Set("x-device-id", strings.TrimSpace(auth.DeviceID))
	req.Header.Set("x-businessId", BusinessID)
	req.Header.Set("x-request-from", "xiaoyiWork")
	return req, nil
}

func isLoopback(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// IsLoopbackURL reports whether raw points at a local proxy.
func IsLoopbackURL(raw string) bool { return isLoopback(raw) }
