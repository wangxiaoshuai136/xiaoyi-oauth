package cpa

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMergeAuthPayloadKeepsHostFields(t *testing.T) {
	raw := []byte(`{"type":"xiaoyiwork","note":"小艺","priority":49,"business_credential":"old","disabled":true}`)
	payload := authFilePayload{
		Type:               PluginID,
		Provider:           PluginID,
		OAuthProvider:      PluginID,
		BusinessCredential: "new",
		Name:               "Xiaoyi OAuth",
	}
	merged := mergeAuthPayload(raw, payload)
	if merged == nil {
		t.Fatal("mergeAuthPayload returned nil")
	}
	var doc map[string]any
	if err := json.Unmarshal(merged, &doc); err != nil {
		t.Fatalf("decode merged payload: %v", err)
	}
	if doc["note"] != "小艺" {
		t.Fatalf("host note was lost: %v", doc["note"])
	}
	if doc["priority"] != float64(49) {
		t.Fatalf("host priority was lost: %v", doc["priority"])
	}
	if doc["disabled"] != true {
		t.Fatalf("disabled flag must survive a refresh write: %v", doc["disabled"])
	}
	if doc["business_credential"] != "new" {
		t.Fatalf("plugin field not updated: %v", doc["business_credential"])
	}
	if doc["provider"] != PluginID || doc["oauth_provider"] != PluginID {
		t.Fatalf("provider fields not updated: %v / %v", doc["provider"], doc["oauth_provider"])
	}
}

func TestNextRefreshAfterUsesTokenExpiry(t *testing.T) {
	exp := time.Now().Add(50 * time.Minute).UnixMilli()
	got := nextRefreshAfter(authFilePayload{AccessTokenExp: exp})
	want := time.UnixMilli(exp).Add(-10 * time.Minute)
	if got.Sub(want).Abs() > time.Second {
		t.Fatalf("expected refresh at %s, got %s", want, got)
	}
}

func TestNextRefreshAfterExpiredTokenRefreshesImmediately(t *testing.T) {
	got := nextRefreshAfter(authFilePayload{
		AccessTokenExp: time.Now().Add(-time.Hour).UnixMilli(),
		RefreshToken:   "refresh",
	})
	if time.Since(got) > time.Minute {
		t.Fatalf("expired token should refresh immediately, got %s", got)
	}
}

func TestNoteFromJSON(t *testing.T) {
	if got := noteFromJSON([]byte(`{"note":" 小艺 "}`)); got != "小艺" {
		t.Fatalf("note = %q", got)
	}
	if got := noteFromJSON([]byte(`{"uid":"1"}`)); got != "" {
		t.Fatalf("unexpected note %q", got)
	}
	if got := noteFromJSON(nil); got != "" {
		t.Fatalf("unexpected note %q", got)
	}
}

func TestPreserveHostAuthFields(t *testing.T) {
	auth := authData("3500", "温保禄", "oauth", []byte(`{}`))
	metadata := map[string]any{"note": "小艺", "host_only": "keep"}
	attributes := map[string]string{"note": "小艺", "oauth_provider": "should-not-override"}
	preserveHostAuthFields(&auth, metadata, attributes)
	if auth.Metadata["note"] != "小艺" || auth.Metadata["host_only"] != "keep" {
		t.Fatalf("host metadata lost: %#v", auth.Metadata)
	}
	if auth.Attributes["note"] != "小艺" {
		t.Fatalf("host attribute note lost: %#v", auth.Attributes)
	}
	if auth.Attributes["oauth_provider"] != PluginID {
		t.Fatalf("plugin attribute must win: %#v", auth.Attributes)
	}
}
