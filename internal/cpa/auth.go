package cpa

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wangxiaoshuai136/cpa-plugin-xiaoyiwork/internal/pluginabi"
	"github.com/wangxiaoshuai136/cpa-plugin-xiaoyiwork/internal/pluginapi"
)

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
	return pluginapi.AuthParseResponse{
		Handled: true,
		Auth:    p.authData("local", "registry"),
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
	key, source, err := p.resolveKey(true)
	message := "已从小艺Work 本地凭据读取 proxyKey（" + source + "）"
	if err != nil || key == "" {
		message = "等待小艺Work 登录：" + errorText(err)
	}
	return pluginapi.AuthLoginStartResponse{
		Provider:  PluginID,
		State:     uuid.NewString(),
		ExpiresAt: time.Now().Add(15 * time.Minute),
		Metadata:  map[string]any{"message": message},
	}, nil
}

func (p *Plugin) handleAuthLoginPoll(raw []byte) (any, error) {
	key, source, err := p.resolveKey(true)
	if err != nil || key == "" {
		message := "请先启动并登录小艺Work 桌面端"
		if err != nil {
			message += "：" + err.Error()
		}
		return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusPending, Message: message}, nil
	}
	return pluginapi.AuthLoginPollResponse{
		Status:  pluginapi.AuthLoginStatusSuccess,
		Message: "已接入小艺Work 本地模型代理（凭据来源：" + source + "）",
		Auth:    p.authData("local", source),
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
	id := strings.TrimSpace(req.AuthID)
	if id == "" {
		id = "local"
	}
	return pluginapi.AuthRefreshResponse{
		Auth:             p.authData(id, source),
		NextRefreshAfter: time.Now().Add(time.Hour),
	}, nil
}

func (p *Plugin) authData(id, source string) pluginapi.AuthData {
	return pluginapi.AuthData{
		Provider: PluginID,
		ID:       id,
		Label:    PluginName,
		Metadata: map[string]any{
			"account_id":     id,
			"provider":       PluginID,
			"oauth_provider": PluginID,
			"auth_kind":      "local-proxy",
			"key_source":     source,
		},
		Attributes: map[string]string{"oauth_provider": PluginID},
	}
}

// ensureAuthFile creates one keyless credential. The proxy key never touches
// disk: it is resolved from the registry on demand.
func (p *Plugin) ensureAuthFile() {
	if p.hasOwnAuth() {
		return
	}
	payload := map[string]any{
		"type":       PluginID,
		"provider":   PluginID,
		"name":       PluginName,
		"created_by": "cpa-plugin-" + PluginID,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if _, err := p.callHost(pluginabi.MethodHostAuthSave, pluginapi.HostAuthSaveRequest{
		Name: authFileName,
		JSON: raw,
	}); err != nil {
		if p.Config().Debug {
			p.log("warn", "写入小艺Work 凭据失败："+err.Error(), nil)
		}
		return
	}
	p.log("info", "已创建小艺Work 本地代理凭据（不含密钥）", nil)
}

func (p *Plugin) hasOwnAuth() bool {
	raw, err := p.callHost(pluginabi.MethodHostAuthList, map[string]any{})
	if err != nil {
		return false
	}
	var env struct {
		Result struct {
			Files []pluginapi.HostAuthFileEntry `json:"files"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return false
	}
	for _, f := range env.Result.Files {
		if strings.EqualFold(f.Provider, PluginID) || strings.EqualFold(f.Type, PluginID) {
			return true
		}
	}
	return false
}

func errorText(err error) string {
	if err == nil {
		return "未知原因"
	}
	return fmt.Sprintf("%v", err)
}
