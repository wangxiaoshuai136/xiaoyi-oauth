package cpa

import (
	"encoding/json"
	"html"
	"net/http"
	"strings"

	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/pluginapi"
)

func (p *Plugin) handleManagementRegister(raw []byte) (any, error) {
	return pluginapi.ManagementRegistrationResponse{
		Routes: []pluginapi.ManagementRoute{
			{Method: http.MethodGet, Path: "/v0/management/xiaoyiwork/api/state", Description: "Xiaoyi OAuth credential state"},
		},
		Resources: []pluginapi.ResourceRoute{
			{Path: "/status", Menu: PluginName, Description: "Xiaoyi OAuth 登录状态与凭据来源"},
		},
	}, nil
}

func (p *Plugin) handleManagementHandle(raw []byte) (any, error) {
	var req pluginapi.ManagementRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	state := ""
	if req.Query != nil {
		state = req.Query.Get("state")
	}
	path := strings.ToLower(strings.TrimSpace(req.Path))
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		path = path[:idx]
	}
	if strings.Contains(path, "/api/state") {
		payload, err := importCloudCredential()
		state := map[string]any{
			"credential_ready": payload.BusinessCredential != "",
			"mode":             "cloud",
			"uid":              payload.UID,
			"label":            labelFor(payload),
			"expires_at":       payload.BusinessValidUntil,
			"error":            errorText(err),
		}
		body, _ := json.Marshal(state)
		return pluginapi.ManagementResponse{
			StatusCode: http.StatusOK,
			Headers:    http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
			Body:       body,
		}, nil
	}
	if path == "/status" || strings.HasPrefix(path, "/v0/resource/plugins/"+PluginID) {
		page := statusHTML
		p.loginMu.Lock()
		session := p.loginSessions[state]
		p.loginMu.Unlock()
		if session != nil {
			if sessionDone(session) {
				page = `<!doctype html><meta charset="utf-8"><body style="font:16px system-ui;background:#0f1115;color:#e8eaf0;display:grid;place-items:center;height:100vh"><div><h2>登录已完成</h2><p>可以关闭这个窗口，回到 ECPA。</p></div></body>`
			} else if session.authURL != "" {
				escaped := html.EscapeString(session.authURL)
				page = `<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=` + escaped + `"><body style="font:16px system-ui;background:#0f1115;color:#e8eaf0;display:grid;place-items:center;height:100vh"><div><h2>正在打开华为账号登录…</h2><p>如果没有自动跳转，<a style="color:#7cc4ff" href="` + escaped + `">点这里</a>。</p></div></body>`
			}
		}
		return pluginapi.ManagementResponse{
			StatusCode: http.StatusOK,
			Headers: http.Header{
				"Content-Type":  []string{"text/html; charset=utf-8"},
				"Cache-Control": []string{"no-store"},
			},
			Body: []byte(page),
		}, nil
	}
	return pluginapi.ManagementResponse{StatusCode: http.StatusNotFound, Body: []byte("not found")}, nil
}

const statusHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Xiaoyi OAuth</title>
<style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0f1115;color:#e8eaf0;font:15px/1.7 system-ui,-apple-system,"Segoe UI",sans-serif}
main{max-width:560px;padding:36px 32px;border:1px solid #262b36;border-radius:16px;background:#151922}
h1{margin:0 0 6px;font-size:20px}
p{color:#a9b1c3;margin:10px 0}
ol{color:#c8ceda;padding-left:20px}
li{margin:6px 0}
code{background:#0b0e14;border:1px solid #262b36;border-radius:6px;padding:1px 5px;color:#9ecbff}
.hint{margin-top:18px;padding:12px 14px;border-radius:10px;background:#101a14;border:1px solid #1f3a2a;color:#9fe0b5}
</style></head><body><main>
<h1>Xiaoyi OAuth 登录</h1>
<p>用华为账号直接登录，<b>不需要安装或启动小艺Work 桌面端</b>。</p>
<ol>
<li>在 ECPA 里点击「开始登录」，插件会拉起一个独立登录窗口（不占用日常浏览器）。</li>
<li>完成登录与授权，浏览器会跳回本机回调，插件自动换取凭据。</li>
<li>回到 ECPA，账号会以 <code>Xiaoyi OAuth (姓名)</code> 出现，可正常启用/禁用。</li>
</ol>
<div class="hint">凭据包含云端 businessCredential 与 refresh token；过期时插件会自动续期，无需桌面端。</div>
</main></body></html>`
