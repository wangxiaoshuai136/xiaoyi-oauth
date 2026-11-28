package cpa

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/pluginapi"
)

func TestLoginPollDoesNotReuseCompletedSession(t *testing.T) {
	completed := &oauthSession{state: "old", done: make(chan struct{})}
	completed.payload = authFilePayload{UID: "1000", DisplayName: "旧账号"}
	close(completed.done)

	pending := &oauthSession{
		state:   "new",
		done:    make(chan struct{}),
		expires: time.Now().Add(10 * time.Minute),
	}
	p := &Plugin{loginSessions: map[string]*oauthSession{"old": completed, "new": pending}}
	p.lastLoginState = "new"

	raw, _ := json.Marshal(pluginapi.AuthLoginPollRequest{State: "new"})
	res, err := p.handleAuthLoginPoll(raw)
	if err != nil {
		t.Fatalf("poll failed: %v", err)
	}
	poll, ok := res.(pluginapi.AuthLoginPollResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", res)
	}
	if poll.Status != pluginapi.AuthLoginStatusPending {
		t.Fatalf("new login must stay pending, got %s (%s)", poll.Status, poll.Message)
	}
}
