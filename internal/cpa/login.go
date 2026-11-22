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
	server   *http.Server
}

func (p *Plugin) handleAuthLoginStart(raw []byte) (any, error) {
	state := uuid.NewString()
	verifier, challenge, err := xiaoyi.PKCEPair()
	if err != nil {
		return nil, pluginabi.NewError("oauth_pkce_failed", err.Error(), 500)
	}
	session := &oauthSession{
		state:    state,
		verifier: verifier,
		expires:  time.Now().Add(10 * time.Minute),
		done:     make(chan struct{}),
	}
	p.loginMu.Lock()
	if p.login != nil && p.login.server != nil {
		_ = p.login.server.Close()
	}
	p.login = session
	p.loginMu.Unlock()

	url := xiaoyi.AuthorizeURL(state, challenge)
	panel := strings.TrimRight(p.Config().PanelBaseURL, "/")
	// The Huawei redirect is fixed at 127.0.0.1:17355; run our own callback
	// server so no desktop client is needed.
	ln, listenErr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", xiaoyi.CallbackPort))
	if listenErr == nil {
		mux := http.NewServeMux()
		mux.HandleFunc(xiaoyi.CallbackPath, func(w http.ResponseWriter, r *http.Request) {
			p.finishOAuth(session, w, r)
		})
		server := &http.Server{Handler: mux}
		session.server = server
		go func() { _ = server.Serve(ln) }()
		go func() {
			<-session.done
			time.Sleep(2 * time.Second)
			_ = server.Close()
		}()
	} else if payload, importErr := importCloudCredential(); importErr == nil && payload.BusinessCredential != "" {
		session.payload = payload
		close(session.done)
	} else {
		return nil, pluginabi.NewError(
			"oauth_callback_port_busy",
			fmt.Sprintf("回调端口 %d 被占用，请先退出小艺Work 桌面端", xiaoyi.CallbackPort),
			http.StatusConflict,
		)
	}
	return pluginapi.AuthLoginStartResponse{
		Provider:  PluginID,
		URL:       url,
		State:     state,
		ExpiresAt: session.expires,
		Metadata: map[string]any{
			"message": "用华为账号登录，无需安装小艺Work",
			"panel":   panel,
		},
	}, nil
}

func (p *Plugin) finishOAuth(session *oauthSession, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("state") != session.state {
		http.Error(w, "state 不匹配，请重新发起登录", http.StatusBadRequest)
		return
	}
	if errText := q.Get("error"); errText != "" {
		session.fail(fmt.Errorf("华为账号登录失败: %s %s", errText, q.Get("error_description")))
		http.Error(w, "登录失败，请回到 ECPA 重试", http.StatusBadRequest)
		return
	}
	code := strings.TrimSpace(q.Get("code"))
	if code == "" {
		session.fail(fmt.Errorf("回调缺少 code"))
		http.Error(w, "回调缺少 code", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	tokens, err := xiaoyi.ExchangeCode(ctx, code, session.verifier)
	if err != nil {
		session.fail(err)
		http.Error(w, "换取 token 失败", http.StatusBadGateway)
		return
	}
	claims := xiaoyi.ParseIDToken(tokens.IDToken)
	uid := claims.UID
	if uid == "" {
		session.fail(fmt.Errorf("id_token 缺少 uid"))
		http.Error(w, "id_token 缺少 uid", http.StatusBadGateway)
		return
	}
	deviceID, _ := xiaoyi.DeviceID(uid)
	credential, validUntil, err := xiaoyi.CreateBusinessCredential(ctx, tokens.AccessToken, uid, deviceID, claims.ClientID)
	if err != nil {
		session.fail(err)
		http.Error(w, "创建云端凭据失败", http.StatusBadGateway)
		return
	}
	payload := authFilePayload{
		Type:               PluginID,
		Provider:           PluginID,
		OAuthProvider:      PluginID,
		AccountID:          uid,
		UID:                uid,
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
	close(session.done)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>登录成功</title><body style="font:16px system-ui;background:#0f1115;color:#e8eaf0;display:grid;place-items:center;height:100vh"><div style="text-align:center"><h2>登录成功</h2><p>可以关闭这个窗口，回到 ECPA。</p></div></body>`))
}

func (s *oauthSession) fail(err error) {
	s.err = err
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

func (p *Plugin) handleAuthLoginPoll(raw []byte) (any, error) {
	p.loginMu.Lock()
	session := p.login
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
			return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: "登录超时，请重试"}, nil
		}
		return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusPending, Message: "等待华为账号登录完成…"}, nil
	}
}
