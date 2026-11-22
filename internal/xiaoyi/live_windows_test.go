//go:build windows

package xiaoyi

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveProxyKeyAndChat is opt-in because it consumes real XiaoyiWork quota:
// set XIAOYIWORK_LIVE_TEST=1 and keep the desktop app running.
func TestLiveProxyKeyAndChat(t *testing.T) {
	if os.Getenv("XIAOYIWORK_LIVE_TEST") != "1" {
		t.Skip("set XIAOYIWORK_LIVE_TEST=1 to run against the live XiaoyiWork proxy")
	}
	key, err := DecryptProxyKey()
	if err != nil {
		t.Fatalf("DecryptProxyKey: %v", err)
	}
	if !strings.HasPrefix(key, "mpk_") {
		t.Fatalf("unexpected proxy key prefix")
	}
	t.Logf("proxy key decrypted (prefix=%s, length=%d)", key[:4], len(key))

	client := NewClient(DefaultBaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	body, headers, err := client.ChatStream(ctx, key, []byte(`{"model":"GLM_5.3_FLASH_VE","messages":[{"role":"user","content":"Say OK"}],"stream":true}`))
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	defer body.Close()
	t.Logf("content-type=%s", headers.Get("Content-Type"))
	raw, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "data:") {
		t.Fatalf("expected SSE data frames, got %q", truncate(text))
	}
	if strings.Contains(text, "\"error\"") {
		t.Fatalf("upstream returned an error frame: %s", truncate(text))
	}
	t.Logf("first frames: %s", truncate(text))
}

func truncate(text string) string {
	if len(text) > 400 {
		return text[:400] + "…"
	}
	return text
}
