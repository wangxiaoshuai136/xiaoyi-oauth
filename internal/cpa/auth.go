package cpa

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/pluginabi"
	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/pluginapi"
	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/xiaoyi"
)

// authFilePayload is the credential written by the login flow. Unlike the
// first release, it carries the proxy key so the account is a real credential
// the gateway can route with, exactly like an OAuth account.
type authFilePayload struct {
	Type           string   `json:"type"`
	Provider       string   `json:"provider"`
	OAuthProvider  string   `json:"oauth_provider,omitempty"`
	AccountID      string   `json:"account_id,omitempty"`
	Name           string   `json:"name,omitempty"`
	UID            string   `json:"uid,omitempty"`
	ProxyKey       string   `json:"proxy_key,omitempty"`
	KeySource      string   `json:"key_source,omitempty"`
	UpdatedAt      string   `json:"updated_at,omitempty"`
	Disabled       bool     `json:"disabled,omitempty"`
	ExcludedModels []string `json:"excluded_models,omitempty"`
}

type rpcIdentifierResponse struct {
	Identifier          string                        `json:"identifier"`
	Identifiers         []string                      `json:"identifiers,omitempty"`
	CredentialProviders []string                      `json:"credential_providers,omitempty"`
	Entries             []pluginapi.AuthProviderEntry `json:"entries,omitempty"`
}

func (p *Plugin) handleAuthIdentifier() (any, error) {
	return rpcIdentifierResponse{
		Identifier:          PluginID,
		Identifiers:         []string{PluginID},
		CredentialProviders: []string{PluginID},
		Entries: []pluginapi.AuthProviderEntry{
			{Identifier: PluginID, Name: PluginName},
		},
	}, nil
}

func (p *Plugin) handleAuthParse(raw []byte) (any, error) {
	var req pluginapi.AuthParseRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	if !looksLikeXiaoyiAuth(req) {
		return pluginapi.AuthParseResponse{Handled: false}, nil
	}
	var payload authFilePayload
	if len(req.RawJSON) > 0 {
		_ = json.Unmarshal(req.RawJSON, &payload)
	}
	id := payload.UID
	if id == "" {
		id = payload.AccountID
	}
	if id == "" {
		id = "local"
	}
	return pluginapi.AuthParseResponse{
		Handled: true,
		Auth:    authData(id, payload.UID, payload.KeySource, req.RawJSON),
	}, nil
}

func looksLikeXiaoyiAuth(req pluginapi.AuthParseRequest) bool {
	for _, value := range []string{req.Provider, req.FileName, string(req.RawJSON)} {
		lower := strings.ToLower(value)
		if strings.Contains(lower, PluginID) || strings.Contains(value, "小艺") || strings.Contains(lower, "xiaoyi") {
			return true
		}
	}
	return false
}

func (p *Plugin) handleAuthLoginStart(raw []byte) (any, error) {
	state := uuid.NewString()
	key, source, _ := p.resolveKey(true)
	uid := identityUID()
	panel := strings.TrimRight(p.Config().PanelBaseURL, "/")
	url := fmt.Sprintf("%s/v0/resource/plugins/%s/status?state=%s", panel, PluginID, state)
	message := "正在导入小艺Work 桌面端登录态"
	if key == "" {
		message = "请先启动并登录小艺Work 桌面端，再点击重试"
	}
	return pluginapi.AuthLoginStartResponse{
		Provider:  PluginID,
		URL:       url,
		State:     state,
		ExpiresAt: time.Now().Add(15 * time.Minute),
		Metadata: map[string]any{
			"message":    message,
			"uid":        uid,
			"key_source": source,
		},
	}, nil
}

func (p *Plugin) handleAuthLoginPoll(raw []byte) (any, error) {
	key, source, err := p.resolveKey(true)
	uid := identityUID()
	if err != nil || key == "" {
		message := "请先启动并登录小艺Work 桌面端"
		if err != nil {
			message += "：" + err.Error()
		}
		return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusPending, Message: message}, nil
	}
	storage := credentialJSON(uid, key, source)
	auth := authData(accountID(uid), uid, source, storage)
	auth.NextRefreshAfter = time.Now().Add(time.Hour)
	return pluginapi.AuthLoginPollResponse{
		Status:  pluginapi.AuthLoginStatusSuccess,
		Message: "已导入小艺Work 登录凭证（" + labelFor(uid) + "）",
		Auth:    auth,
	}, nil
}

func (p *Plugin) handleAuthRefresh(raw []byte) (any, error) {
	var req pluginapi.AuthRefreshRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	key, source, err := p.resolveKey(true)
	if err != nil || key == "" {
		return nil, pluginabi.NewError("xiaoyiwork_key_unavailable", "无法读取小艺Work proxyKey: "+errorText(err), 503)
	}
	uid := identityUID()
	id := strings.TrimSpace(req.AuthID)
	if id == "" {
		id = accountID(uid)
	}
	storage := credentialJSON(uid, key, source)
	auth := authData(id, uid, source, storage)
	auth.NextRefreshAfter = time.Now().Add(time.Hour)
	return pluginapi.AuthRefreshResponse{Auth: auth, NextRefreshAfter: auth.NextRefreshAfter}, nil
}

func authData(id, uid, source string, storage []byte) pluginapi.AuthData {
	if id == "" {
		id = accountID(uid)
	}
	if source == "" {
		source = "registry"
	}
	return pluginapi.AuthData{
		Provider:    PluginID,
		ID:          id,
		Label:       labelFor(uid),
		StorageJSON: storage,
		Metadata: map[string]any{
			"account_id":     id,
			"uid":            uid,
			"provider":       PluginID,
			"oauth_provider": PluginID,
			"auth_kind":      "desktop",
			"key_source":     source,
		},
		Attributes: map[string]string{"oauth_provider": PluginID},
	}
}

func credentialJSON(uid, key, source string) []byte {
	payload := authFilePayload{
		Type:          PluginID,
		Provider:      PluginID,
		OAuthProvider: PluginID,
		AccountID:     accountID(uid),
		Name:          labelFor(uid),
		UID:           uid,
		ProxyKey:      key,
		KeySource:     source,
		UpdatedAt:     time.Now().Format(time.RFC3339),
	}
	raw, _ := json.Marshal(payload)
	return raw
}

func accountID(uid string) string {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return "local"
	}
	return uid
}

func labelFor(uid string) string {
	if strings.TrimSpace(uid) == "" {
		return PluginName + " (local)"
	}
	return PluginName + " (" + xiaoyi.ShortUID(uid) + ")"
}

func identityUID() string {
	identity, err := xiaoyi.ReadIdentity()
	if err != nil {
		return ""
	}
	return identity.UID
}

// ensureAuthFile keeps one credential per desktop account. It prefers writing
// the oauth file directly so a keyless record from 0.1.x is upgraded in place
// without losing the user's disabled/excluded-model settings.
func (p *Plugin) ensureAuthFile() {
	key, source, _ := p.resolveKey(false)
	uid := identityUID()
	dir := resolveOAuthDir()
	pattern := filepath.Join(dir, authFileName)
	if existing := strings.TrimSpace(uid); existing != "" {
		pattern = filepath.Join(dir, "xiaoyiwork-"+existing+".json")
	}
	if raw, err := os.ReadFile(pattern); err == nil {
		var payload authFilePayload
		_ = json.Unmarshal(raw, &payload)
		if key != "" {
			payload.ProxyKey = key
			payload.KeySource = source
		}
		if payload.UID == "" {
			payload.UID = uid
		}
		payload.Type = PluginID
		payload.Provider = PluginID
		payload.OAuthProvider = PluginID
		payload.UpdatedAt = time.Now().Format(time.RFC3339)
		if merged, err := json.Marshal(payload); err == nil {
			_ = os.WriteFile(pattern, merged, 0o600)
		}
		return
	}

	name := authFileName
	if uid != "" {
		name = "xiaoyiwork-" + uid + ".json"
	}
	if _, err := p.callHost(pluginabi.MethodHostAuthSave, pluginapi.HostAuthSaveRequest{
		Name: name,
		JSON: credentialJSON(uid, key, source),
	}); err != nil {
		if p.Config().Debug {
			p.log("warn", "写入小艺Work 凭据失败："+err.Error(), nil)
		}
		return
	}
	p.log("info", "已写入小艺Work 登录凭证（"+labelFor(uid)+"）", nil)
}

func resolveOAuthDir() string {
	for _, candidate := range []string{`../oauth`, `../../oauth`, `oauth`} {
		if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
			return candidate
		}
	}
	return `../oauth`
}

// storedProxyKey returns a key persisted in the credential, if any.
func storedProxyKey(storage []byte, metadata map[string]any) string {
	if len(storage) > 0 {
		var payload authFilePayload
		if err := json.Unmarshal(storage, &payload); err == nil {
			if key := strings.TrimSpace(payload.ProxyKey); key != "" {
				return key
			}
		}
	}
	if metadata != nil {
		if key, ok := metadata["proxy_key"].(string); ok && strings.TrimSpace(key) != "" {
			return strings.TrimSpace(key)
		}
	}
	return ""
}

func errorText(err error) string {
	if err == nil {
		return "未知原因"
	}
	return fmt.Sprintf("%v", err)
}
