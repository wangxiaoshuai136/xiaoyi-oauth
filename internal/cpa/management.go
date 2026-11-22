package cpa

import (
	"encoding/json"
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
		return pluginapi.ManagementResponse{
			StatusCode: http.StatusOK,
			Headers: http.Header{
				"Content-Type":  []string{"text/html; charset=utf-8"},
				"Cache-Control": []string{"no-store"},
			},
			Body: []byte(statusHTML),
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
<p>这个窗口只是状态提示。真正的小艺Work 凭据由桌面端保存，插件在后台读取。</p>
<ol>
<li>确认 <b>小艺Work 桌面端</b> 已启动并登录。</li>
<li>回到 ECPA 的登录窗口，插件会在几秒内自动完成导入。</li>
<li>导入后，账号会以 <code>Xiaoyi OAuth (uid)</code> 出现在凭据列表，可正常启用/禁用。</li>
</ol>
<div class="hint">导入的凭据包含 proxyKey，便于网关在没有重新解密的情况下路由；401 时插件会自动重读桌面端凭据。</div>
</main></body></html>`
