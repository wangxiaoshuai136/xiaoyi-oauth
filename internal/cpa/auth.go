package cpa

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// mergeAuthPayload updates only the fields the plugin owns. Everything else in
// the auth file (host-managed note, priority, headers, ...) must survive a
// credential rewrite; unmarshalling into authFilePayload and marshalling it
// back would silently drop those keys.
func mergeAuthPayload(raw []byte, payload authFilePayload) []byte {
	doc := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &doc)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	patch := map[string]any{}
	if err := json.Unmarshal(encoded, &patch); err != nil {
		return nil
	}
	for key, value := range patch {
		doc[key] = value
	}
	merged, err := json.Marshal(doc)
	if err != nil {
		return nil
	}
	return merged
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
			{Identifier: PluginID, Name: AuthEntryName},
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
	auth := authData(id, payload.DisplayName, payload.KeySource, req.RawJSON)
	auth.NextRefreshAfter = nextRefreshAfter(payload)
	if note := noteFromJSON(req.RawJSON); note != "" {
		if auth.Metadata == nil {
			auth.Metadata = map[string]any{}
		}
		auth.Metadata["note"] = note
		if auth.Attributes == nil {
			auth.Attributes = map[string]string{}
		}
		auth.Attributes["note"] = note
	}
	return pluginapi.AuthParseResponse{
		Handled: true,
		Auth:    auth,
	}, nil
}

func noteFromJSON(raw []byte) string {
	var doc map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	note, _ := doc["note"].(string)
	return strings.TrimSpace(note)
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

func (p *Plugin) handleAuthRefresh(raw []byte) (any, error) {
	var req pluginapi.AuthRefreshRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	var payload authFilePayload
	if len(req.StorageJSON) > 0 {
		_ = json.Unmarshal(req.StorageJSON, &payload)
	}
	payload = preferNewestPayload(payload)
	if payload.BusinessCredential == "" {
		return nil, pluginabi.NewError("xiaoyiwork_credential_unavailable", "凭据缺少 businessCredential，请重新登录", 503)
	}
	id := firstNonEmpty(req.AuthID, payload.UID, "local")
	ra := requestAuth{
		Auth: xiaoyi.Auth{
			BusinessCredential: payload.BusinessCredential,
			UID:                payload.UID,
			DeviceID:           payload.DeviceID,
		},
		UID:                payload.UID,
		DeviceID:           payload.DeviceID,
		ClientID:           payload.ClientID,
		AccessToken:        payload.AccessToken,
		AccessTokenExp:     payload.AccessTokenExp,
		RefreshToken:       payload.RefreshToken,
		BusinessValidUntil: payload.BusinessValidUntil,
	}
	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()
	if err := p.refreshCloudAuth(ctx, &ra, false); err != nil {
		return nil, pluginabi.NewError("xiaoyiwork_refresh_failed", "凭据续期失败："+err.Error(), 502)
	}
	payload.BusinessCredential = ra.Auth.BusinessCredential
	payload.BusinessValidUntil = ra.BusinessValidUntil
	payload.AccessToken = ra.AccessToken
	payload.AccessTokenExp = ra.AccessTokenExp
	payload.RefreshToken = ra.RefreshToken
	payload.UpdatedAt = time.Now().Format(time.RFC3339)
	p.persistLoginPayload(payload)
	storage, _ := json.Marshal(payload)
	auth := authData(id, payload.DisplayName, payload.KeySource, storage)
	// The host replaces its metadata with ours whenever we return any keys, so
	// carry over host-managed fields such as the account note ("note").
	preserveHostAuthFields(&auth, req.Metadata, req.Attributes)
	auth.NextRefreshAfter = nextRefreshAfter(payload)
	return pluginapi.AuthRefreshResponse{Auth: auth, NextRefreshAfter: auth.NextRefreshAfter}, nil
}

// preserveHostAuthFields keeps host-managed metadata/attributes that the
// plugin does not own. Dropping them would erase the account note on refresh.
func preserveHostAuthFields(auth *pluginapi.AuthData, metadata map[string]any, attributes map[string]string) {
	if auth == nil {
		return
	}
	if auth.Metadata == nil {
		auth.Metadata = map[string]any{}
	}
	for key, value := range metadata {
		if _, exists := auth.Metadata[key]; !exists {
			auth.Metadata[key] = value
		}
	}
	if len(attributes) == 0 {
		return
	}
	if auth.Attributes == nil {
		auth.Attributes = map[string]string{}
	}
	for key, value := range attributes {
		if _, exists := auth.Attributes[key]; !exists {
			auth.Attributes[key] = value
		}
	}
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
		payload.Name = labelFor(payload)
		if merged := mergeAuthPayload(raw, payload); merged != nil {
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

// persistCloudCredential keeps the newest business credential on disk: the
// cloud allows only one live credential per account/device, so losing it would
// force another browser login.
func (p *Plugin) persistCloudCredential(auth *requestAuth) {
	if auth == nil || auth.UID == "" || auth.Auth.BusinessCredential == "" {
		return
	}
	path := filepath.Join(resolveOAuthDir(), "xiaoyiwork-"+auth.UID+".json")
	var raw []byte
	if data, err := os.ReadFile(path); err == nil {
		raw = data
	}
	payload := authFilePayload{
		Type:               PluginID,
		Provider:           PluginID,
		OAuthProvider:      PluginID,
		UID:                auth.UID,
		AccountID:          auth.UID,
		BusinessCredential: auth.Auth.BusinessCredential,
		BusinessValidUntil: auth.BusinessValidUntil,
		AccessToken:        auth.AccessToken,
		AccessTokenExp:     auth.AccessTokenExp,
		RefreshToken:       auth.RefreshToken,
		ClientID:           auth.ClientID,
		DeviceID:           auth.DeviceID,
		KeySource:          "refreshed",
		UpdatedAt:          time.Now().Format(time.RFC3339),
	}
	payload.Name = labelFor(payload)
	var existing struct {
		Name string `json:"name"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &existing)
	}
	if existing.Name != "" {
		payload.Name = existing.Name
	}
	if merged := mergeAuthPayload(raw, payload); merged != nil {
		_ = os.WriteFile(path, merged, 0o600)
	}
	writeHostAuthCopy(auth.UID, payload)
}

// persistLoginPayload writes a fresh OAuth login to disk immediately, so the
// account exists even when the GUI dialog closes before its next poll.
func (p *Plugin) persistLoginPayload(payload authFilePayload) {
	if payload.UID == "" {
		return
	}
	dir := resolveOAuthDir()
	path := filepath.Join(dir, "xiaoyiwork-"+payload.UID+".json")
	var raw []byte
	if data, err := os.ReadFile(path); err == nil {
		raw = data
	}
	payload.AccountID = payload.UID
	payload.Name = labelFor(payload)
	if merged := mergeAuthPayload(raw, payload); merged != nil {
		_ = os.WriteFile(path, merged, 0o600)
	}
	writeHostAuthCopy(payload.UID, payload)
	_ = os.Remove(filepath.Join(dir, authFileName))
}

// writeHostAuthCopy mirrors the credential into the host-owned auth file so a
// core restart does not reload stale tokens from its own copy.
func writeHostAuthCopy(uid string, payload authFilePayload) {
	if uid == "" {
		return
	}
	path := filepath.Join(resolveOAuthDir(), uid)
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if merged := mergeAuthPayload(raw, payload); merged != nil {
		_ = os.WriteFile(path, merged, 0o600)
	}
}

// preferNewestPayload keeps whichever copy of the credential carries the
// freshest access token. The host may hand over a storage snapshot captured
// before a re-login or a cloud refresh.
func preferNewestPayload(payload authFilePayload) authFilePayload {
	if payload.UID == "" {
		return payload
	}
	dir := resolveOAuthDir()
	newest := payload
	for _, path := range []string{
		filepath.Join(dir, "xiaoyiwork-"+payload.UID+".json"),
		filepath.Join(dir, payload.UID),
	} {
		raw, err := os.ReadFile(path)
		if err != nil || len(raw) == 0 {
			continue
		}
		var stored authFilePayload
		if json.Unmarshal(raw, &stored) != nil {
			continue
		}
		newest = mergeMissingPayloadFields(newest, stored)
		if stored.AccessTokenExp > newest.AccessTokenExp || newest.AccessToken == "" {
			newest.AccessToken = stored.AccessToken
			newest.AccessTokenExp = stored.AccessTokenExp
		}
	}
	newest.Disabled = payload.Disabled
	newest.ExcludedModels = payload.ExcludedModels
	if payload.Name != "" {
		newest.Name = payload.Name
	}
	if newest.UID == "" {
		newest.UID = payload.UID
	}
	return newest
}

// mergeMissingPayloadFields fills plugin fields the host snapshot dropped
// (notably business_valid_until) from the on-disk credential.
func mergeMissingPayloadFields(newest, stored authFilePayload) authFilePayload {
	if newest.ClientID == "" {
		newest.ClientID = stored.ClientID
	}
	if newest.DeviceID == "" {
		newest.DeviceID = stored.DeviceID
	}
	if newest.RefreshToken == "" {
		newest.RefreshToken = stored.RefreshToken
	}
	if newest.BusinessCredential == "" {
		newest.BusinessCredential = stored.BusinessCredential
	}
	if newest.BusinessValidUntil == 0 {
		newest.BusinessValidUntil = stored.BusinessValidUntil
	}
	if newest.UID == "" {
		newest.UID = stored.UID
	}
	if newest.AccountID == "" {
		newest.AccountID = stored.AccountID
	}
	return newest
}

// nextRefreshAfter spreads refreshes before the access token expires instead
// of refreshing right after login.
func nextRefreshAfter(payload authFilePayload) time.Time {
	now := time.Now()
	if payload.BusinessValidUntil > 0 {
		at := time.Unix(payload.BusinessValidUntil, 0).Add(-2 * time.Hour)
		if at.After(now) {
			return at
		}
		return now
	}
	if payload.AccessTokenExp > 0 {
		at := time.UnixMilli(payload.AccessTokenExp).Add(-10 * time.Minute)
		if at.Before(now) {
			return now
		}
		return at
	}
	return now.Add(30 * time.Minute)
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
