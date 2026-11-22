package xiaoyi

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the local model proxy shipped with the XiaoyiWork
	// desktop app. OpenAI clients append /chat/completions to it.
	DefaultBaseURL = "http://127.0.0.1:19693/celia/v1/sse-api"

	// KeyEnvPrimary is the preferred override for users who already know the key.
	KeyEnvPrimary = "XIAOYIWORK_PROXY_KEY"
	// KeyEnvLegacy mirrors the environment variable used by the DSH plugin.
	KeyEnvLegacy = "DSH_XIAOYIWORK_PROXY_KEY"
)

// Resolver reads and caches the XiaoyiWork proxy key. Successful lookups are
// cached for a few minutes; failures are cached briefly so a missing app does
// not start a PowerShell-free but still costly decrypt per request.
type Resolver struct {
	mu      sync.Mutex
	key     string
	source  string
	expiry  time.Time
	lastErr error
	errAt   time.Time
}

const (
	successTTL = 5 * time.Minute
	failureTTL = 10 * time.Second
)

// Invalidate drops any cached value, e.g. after the upstream returns 401.
func (r *Resolver) Invalidate() {
	r.mu.Lock()
	r.key = ""
	r.source = ""
	r.expiry = time.Time{}
	r.lastErr = nil
	r.errAt = time.Time{}
	r.mu.Unlock()
}

// Resolve returns the proxy key and a human-readable source label.
// Order: explicit config override, environment, local registry/DPAPI.
func (r *Resolver) Resolve(override string, force bool) (string, string, error) {
	if key := strings.TrimSpace(override); key != "" {
		return key, "config", nil
	}
	if key := strings.TrimSpace(os.Getenv(KeyEnvPrimary)); key != "" {
		return key, "env:" + KeyEnvPrimary, nil
	}
	if key := strings.TrimSpace(os.Getenv(KeyEnvLegacy)); key != "" {
		return key, "env:" + KeyEnvLegacy, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if !force {
		if r.key != "" && now.Before(r.expiry) {
			return r.key, r.source, nil
		}
		if r.lastErr != nil && now.Sub(r.errAt) < failureTTL {
			return "", "", r.lastErr
		}
	}

	key, err := DecryptProxyKey()
	if err != nil {
		r.key = ""
		r.source = ""
		r.lastErr = err
		r.errAt = now
		r.expiry = time.Time{}
		return "", "", err
	}
	if key == "" {
		err = fmt.Errorf("解密成功但凭据中没有 proxyKey 字段")
		r.lastErr = err
		r.errAt = now
		return "", "", err
	}

	r.key = key
	r.source = "registry:dpapi"
	r.lastErr = nil
	r.expiry = now.Add(successTTL)
	return key, r.source, nil
}
