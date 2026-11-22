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

// authFilePayload is a self-contained credential: the cloud business
// credential, its Huawei session tokens and the device identity.
type authFilePayload struct {
	Type               string   `json:"type"`
	Provider           string   `json:"provider"`
	OAuthProvider      string   `json:"oauth_provider,omitempty"`
	AccountID          string   `json:"account_id,omitempty"`
	Name               string   `json:"name,omitempty"`
	UID                string   `json:"uid,omitempty"`
	DisplayName        string   `json:"display_name,omitempty"`
	ProxyKey           string   `json:"proxy_key,omitempty"`
	KeySource          string   `json:"key_source,omitempty"`
	BusinessCredential string   `json:"business_credential,omitempty"`
	BusinessValidUntil int64    `json:"business_valid_until,omitempty"`
	AccessToken        string   `json:"access_token,omitempty"`
	AccessTokenExp     int64    `json:"access_token_exp,omitempty"`
	RefreshToken       string   `json:"refresh_token,omitempty"`
	ClientID           string   `json:"client_id,omitempty"`
	DeviceID           string   `json:"device_id,omitempty"`
	UpdatedAt          string   `json:"updated_at,omitempty"`
	Disabled           bool     `json:"disabled,omitempty"`
	ExcludedModels     []string `json:"excluded_models,omitempty"`
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
	id := firstNonEmpty(payload.UID, payload.AccountID, "local")
	return pluginapi.AuthParseResponse{
		Handled: true,
		Auth:    authData(id, payload.DisplayName, payload.KeySource, req.RawJSON),
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
	payload, err := importCloudCredential()
	panel := strings.TrimRight(p.Config().PanelBaseURL, "/")
	url := fmt.Sprintf("%s/v0/resource/plugins/%s/status?state=%s", panel, PluginID, state)
	message := "正在导入小艺Work 云端登录态"
	if err != nil {
		message = "未找到小艺Work 登录态，请先在小艺Work 中登录一次：" + err.Error()
	}
	return pluginapi.AuthLoginStartResponse{
		Provider:  PluginID,
		URL:       url,
		State:     state,
		ExpiresAt: time.Now().Add(15 * time.Minute),
		Metadata: map[string]any{
			"message": message,
			"uid":     payload.UID,
		},
	}, nil
}

func (p *Plugin) handleAuthLoginPoll(raw []byte) (any, error) {
	payload, err := importCloudCredential()
	if err != nil || payload.BusinessCredential == "" {
		message := "请先在小艺Work 中完成一次登录"
		if err != nil {
			message += "：" + err.Error()
		}
		return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusPending, Message: message}, nil
	}
	storage, _ := json.Marshal(payload)
	auth := authData(payload.UID, payload.DisplayName, "keychain", storage)
	auth.NextRefreshAfter = time.Now().Add(6 * time.Hour)
	return pluginapi.AuthLoginPollResponse{
		Status:  pluginapi.AuthLoginStatusSuccess,
		Message: "已导入小艺Work 云端凭证（" + labelFor(payload) + "）",
		Auth:    auth,
	}, nil
}

func (p *Plugin) handleAuthRefresh(raw []byte) (any, error) {
	var req pluginapi.AuthRefreshRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	payload, err := importCloudCredential()
	if err != nil || payload.BusinessCredential == "" {
		return nil, pluginabi.NewError("xiaoyiwork_credential_unavailable", "无法读取小艺Work 云端凭证: "+errorText(err), 503)
	}
	id := firstNonEmpty(req.AuthID, payload.UID, "local")
	storage, _ := json.Marshal(payload)
	auth := authData(id, payload.DisplayName, "keychain", storage)
	auth.NextRefreshAfter = time.Now().Add(6 * time.Hour)
	return pluginapi.AuthRefreshResponse{Auth: auth, NextRefreshAfter: auth.NextRefreshAfter}, nil
}

// importCloudCredential reads everything needed for app-free cloud requests.
func importCloudCredential() (authFilePayload, error) {
	credential, err := xiaoyi.ReadBusinessCredential()
	if err != nil {
		return authFilePayload{}, err
	}
	session, _ := xiaoyi.ReadAuthSession()
	identity, _ := xiaoyi.ReadIdentity()
	uid := firstNonEmpty(credential.UID, identity.UID)
	deviceID, _ := xiaoyi.DeviceID(uid)
	display := xiaoyi.DisplayName(session.IDToken)
	payload := authFilePayload{
		Type:               PluginID,
		Provider:           PluginID,
		OAuthProvider:      PluginID,
		AccountID:          uid,
		UID:                uid,
		DisplayName:        display,
		BusinessCredential: credential.Credential,
		BusinessValidUntil: credential.ValidUntil,
		AccessToken:        session.AccessToken,
		AccessTokenExp:     session.ExpiresAt,
		RefreshToken:       session.RefreshToken,
		ClientID:           xiaoyi.ClientID(session.IDToken),
		DeviceID:           deviceID,
		KeySource:          "keychain",
		UpdatedAt:          time.Now().Format(time.RFC3339),
	}
	payload.Name = labelFor(payload)
	return payload, nil
}

func authData(id, display, source string, storage []byte) pluginapi.AuthData {
	if id == "" {
		id = "local"
	}
	if source == "" {
		source = "keychain"
	}
	name := display
	if name == "" {
		name = xiaoyi.ShortUID(id)
	}
	return pluginapi.AuthData{
		Provider:    PluginID,
		ID:          id,
		Label:       PluginName + " (" + name + ")",
		StorageJSON: storage,
		Metadata: map[string]any{
			"account_id":     id,
			"uid":            id,
			"display_name":   display,
			"provider":       PluginID,
			"oauth_provider": PluginID,
			"auth_kind":      "cloud",
			"key_source":     source,
		},
		Attributes: map[string]string{"oauth_provider": PluginID},
	}
}

func labelFor(payload authFilePayload) string {
	if payload.DisplayName != "" {
		return PluginName + " (" + payload.DisplayName + ")"
	}
	if payload.UID != "" {
		return PluginName + " (" + xiaoyi.ShortUID(payload.UID) + ")"
	}
	return PluginName
}

// ensureAuthFile keeps one credential per desktop account, upgrading an
// existing file in place so disabled/excluded-model settings survive.
func (p *Plugin) ensureAuthFile() {
	payload, err := importCloudCredential()
	if err != nil || payload.BusinessCredential == "" {
		return
	}
	dir := resolveOAuthDir()
	name := authFileName
	if payload.UID != "" {
		name = "xiaoyiwork-" + payload.UID + ".json"
	}
	path := filepath.Join(dir, name)
	if raw, err := os.ReadFile(path); err == nil {
		var existing authFilePayload
		_ = json.Unmarshal(raw, &existing)
		existing.Type = PluginID
		existing.Provider = PluginID
		existing.OAuthProvider = PluginID
		existing.UID = payload.UID
		existing.AccountID = payload.UID
		existing.DisplayName = payload.DisplayName
		existing.BusinessCredential = payload.BusinessCredential
		existing.BusinessValidUntil = payload.BusinessValidUntil
		existing.AccessToken = payload.AccessToken
		existing.AccessTokenExp = payload.AccessTokenExp
		existing.RefreshToken = payload.RefreshToken
		existing.ClientID = payload.ClientID
		existing.DeviceID = payload.DeviceID
		existing.KeySource = payload.KeySource
		existing.UpdatedAt = payload.UpdatedAt
		existing.Name = labelFor(existing)
		if merged, err := json.Marshal(existing); err == nil {
			_ = os.WriteFile(path, merged, 0o600)
		}
		return
	}
	storage, _ := json.Marshal(payload)
	if _, err := p.callHost(pluginabi.MethodHostAuthSave, pluginapi.HostAuthSaveRequest{
		Name: name,
		JSON: storage,
	}); err != nil {
		if p.Config().Debug {
			p.log("warn", "写入小艺Work 凭据失败："+err.Error(), nil)
		}
		return
	}
	p.log("info", "已写入小艺Work 云端凭证（"+labelFor(payload)+"）", nil)
}

func resolveOAuthDir() string {
	for _, candidate := range []string{`../oauth`, `../../oauth`, `oauth`} {
		if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
			return candidate
		}
	}
	return `../oauth`
}

// storedProxyKey returns a local-proxy key persisted in the credential.
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
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
