package cpa

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/pluginabi"
	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/pluginapi"
	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/xiaoyi"
)

type oauthSession struct {
	state    string
	verifier string
	expires  time.Time
	done     chan struct{}
	payload  authFilePayload
	err      error
	browser  *xiaoyi.LoginBrowser
}

func (p *Plugin) handleAuthLoginStart(raw []byte) (any, error) {
	state := uuid.NewString()
	verifier, challenge, err := xiaoyi.PKCEPair()
	if err != nil {
		return nil, pluginabi.NewError("oauth_pkce_failed", err.Error(), 500)
	}
	if err := p.ensureLoginServer(); err != nil {
		return nil, pluginabi.NewError("oauth_callback_unavailable", err.Error(), http.StatusConflict)
	}
	session := &oauthSession{
		state:    state,
		verifier: verifier,
		expires:  time.Now().Add(15 * time.Minute),
		done:     make(chan struct{}),
	}
	p.loginMu.Lock()
	if p.loginSessions == nil {
		p.loginSessions = make(map[string]*oauthSession)
	}
	now := time.Now()
	for key, old := range p.loginSessions {
		if now.After(old.expires) {
			delete(p.loginSessions, key)
		}
	}
	p.loginSessions[state] = session
	p.lastLoginState = state
	p.loginMu.Unlock()

	authURL := xiaoyi.AuthorizeURL(state, challenge)
	browser, err := xiaoyi.LaunchLoginBrowser(authURL)
	if err != nil {
		return nil, pluginabi.NewError("oauth_browser_unavailable", err.Error(), http.StatusInternalServerError)
	}
	session.browser = browser
	panel := strings.TrimRight(p.Config().PanelBaseURL, "/")
	statusURL := fmt.Sprintf("%s/v0/resource/plugins/%s/status?state=%s", panel, PluginID, state)
	return pluginapi.AuthLoginStartResponse{
		Provider:  PluginID,
		URL:       statusURL,
		State:     state,
		ExpiresAt: session.expires,
		Metadata:  map[string]any{"message": "已由插件打开独立登录窗口；二维码/短信/密码均可使用"},
	}, nil
}

func (p *Plugin) ensureLoginServer() error {
	p.loginMu.Lock()
	defer p.loginMu.Unlock()
	if p.loginServer != nil {
		return nil
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", xiaoyi.CallbackPort))
	if err != nil {
		return fmt.Errorf("回调端口 %d 被占用（旧插件实例或小艺Work 正在占用）；请重启一次 ECPA 内核后重试", xiaoyi.CallbackPort)
	}
	mux := http.NewServeMux()
	mux.HandleFunc(xiaoyi.CallbackPath, p.handleOAuthCallback)
	server := &http.Server{Handler: mux}
	p.loginServer = server
	go func() { _ = server.Serve(ln) }()
	return nil
}

func (p *Plugin) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := strings.TrimSpace(q.Get("state"))
	code := strings.TrimSpace(q.Get("code"))
	if errText := q.Get("error"); errText != "" {
		p.failSession(state, fmt.Errorf("华为账号登录失败: %s %s", errText, q.Get("error_description")))
		http.Error(w, "登录失败，请回到 ECPA 重试", http.StatusBadRequest)
		return
	}
	if code == "" {
		http.Error(w, "回调缺少 code", http.StatusBadRequest)
		return
	}

	p.loginMu.Lock()
	session := p.loginSessions[state]
	var candidates []*oauthSession
	if session == nil {
		for _, candidate := range p.loginSessions {
			if time.Now().Before(candidate.expires) {
				candidates = append(candidates, candidate)
			}
		}
	}
	p.loginMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if session != nil {
		if err := p.completeSession(ctx, session, code); err != nil {
			p.failSession(state, err)
			http.Error(w, "登录失败："+err.Error(), http.StatusBadGateway)
			return
		}
		writeLoginSuccess(w)
		return
	}
	// The ECPA dialog may have restarted the flow; try every live verifier.
	for _, candidate := range candidates {
		if err := p.completeSession(ctx, candidate, code); err == nil {
			writeLoginSuccess(w)
			return
		}
	}
	http.Error(w, "登录会话已过期（插件可能刚重启），请回到 ECPA 重新点“开始登录”", http.StatusBadRequest)
}

func (p *Plugin) completeSession(ctx context.Context, session *oauthSession, code string) error {
	tokens, err := xiaoyi.ExchangeCode(ctx, code, session.verifier)
	if err != nil {
		return err
	}
	claims := xiaoyi.ParseIDToken(tokens.IDToken)
	if claims.UID == "" {
		return fmt.Errorf("id_token 缺少 uid")
	}
	deviceID, _ := xiaoyi.DeviceID(claims.UID)
	credential, validUntil, err := xiaoyi.CreateBusinessCredential(ctx, tokens.AccessToken, claims.UID, deviceID, claims.ClientID)
	if err != nil {
		return err
	}
	payload := authFilePayload{
		Type:               PluginID,
		Provider:           PluginID,
		OAuthProvider:      PluginID,
		AccountID:          claims.UID,
		UID:                claims.UID,
		DisplayName:        claims.Name,
		BusinessCredential: credential,
		BusinessValidUntil: validUntil,
		AccessToken:        tokens.AccessToken,
		AccessTokenExp:     tokens.ExpiresAt,
		RefreshToken:       tokens.RefreshToken,
		ClientID:           claims.ClientID,
		DeviceID:           deviceID,
		KeySource:          "oauth",
		UpdatedAt:          time.Now().Format(time.RFC3339),
	}
	payload.Name = labelFor(payload)
	session.payload = payload
	// Persist immediately: the account must exist even if the dialog stops
	// polling after the browser shows "登录成功".
	p.persistLoginPayload(payload)
	p.cloudMu.Lock()
	p.cloudCache[payload.UID] = xiaoyi.BusinessCredential{Credential: credential, UID: payload.UID, ValidUntil: validUntil}
	p.cloudMu.Unlock()
	select {
	case <-session.done:
	default:
		close(session.done)
	}
	go func() {
		time.Sleep(2 * time.Second)
		session.browser.Close()
	}()
	return nil
}

func (p *Plugin) failSession(state string, err error) {
	p.loginMu.Lock()
	session := p.loginSessions[state]
	p.loginMu.Unlock()
	if session == nil {
		return
	}
	session.err = err
	go func() {
		time.Sleep(2 * time.Second)
		session.browser.Close()
	}()
	select {
	case <-session.done:
	default:
		close(session.done)
	}
}

func writeLoginSuccess(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>登录成功</title><body style="font:16px system-ui;background:#0f1115;color:#e8eaf0;display:grid;place-items:center;height:100vh"><div style="text-align:center"><h2>登录成功</h2><p>可以关闭这个窗口，回到 ECPA。</p></div></body>`))
}

func (p *Plugin) handleAuthLoginPoll(raw []byte) (any, error) {
	var req pluginapi.AuthLoginPollRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	p.loginMu.Lock()
	session := p.loginSessions[strings.TrimSpace(req.State)]
	if session == nil || !sessionDone(session) {
		for _, candidate := range p.loginSessions {
			if candidate.err == nil && candidate.payload.UID != "" && sessionDone(candidate) {
				session = candidate
				break
			}
		}
	}
	if session == nil {
		session = p.loginSessions[p.lastLoginState]
	}
	p.loginMu.Unlock()
	if session == nil {
		return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusPending, Message: "请点击开始登录"}, nil
	}
	select {
	case <-session.done:
		if session.err != nil {
			return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: session.err.Error()}, nil
		}
		storage, _ := json.Marshal(session.payload)
		auth := authData(session.payload.UID, session.payload.DisplayName, "oauth", storage)
		auth.NextRefreshAfter = time.Now().Add(6 * time.Hour)
		return pluginapi.AuthLoginPollResponse{
			Status:  pluginapi.AuthLoginStatusSuccess,
			Message: "已登录：" + labelFor(session.payload),
			Auth:    auth,
		}, nil
	default:
		if time.Now().After(session.expires) {
			session.browser.Close()
			return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: "登录超时，请重试"}, nil
		}
		return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusPending, Message: "等待华为账号登录完成…"}, nil
	}
}

func sessionDone(session *oauthSession) bool {
	if session == nil {
		return false
	}
	select {
	case <-session.done:
		return true
	default:
		return false
	}
}
