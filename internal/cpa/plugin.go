package cpa

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/pluginabi"
	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/pluginapi"
	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/xiaoyi"
)

const (
	// PluginID is the stable plugin and provider identifier.
	PluginID = "xiaoyiwork"
	// PluginName is the display name used by management clients.
	PluginName = "Xiaoyi OAuth"
	// Version is the plugin release version.
	Version = "0.4.2"

	authFileName = "xiaoyiwork-local.json"
)

// HostCallFunc invokes a host callback and returns its raw JSON envelope.
type HostCallFunc func(method string, payload []byte) ([]byte, error)

type rpcRegistration struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  rpcCapabilities    `json:"capabilities"`
}

type rpcCapabilities struct {
	ModelProvider          bool                         `json:"model_provider"`
	AuthProvider           bool                         `json:"auth_provider"`
	Scheduler              bool                         `json:"scheduler"`
	ModelRouter            bool                         `json:"model_router"`
	Executor               bool                         `json:"executor"`
	ExecutorModelScope     pluginapi.ExecutorModelScope `json:"executor_model_scope,omitempty"`
	ExecutorInputFormats   []string                     `json:"executor_input_formats,omitempty"`
	ExecutorOutputFormats  []string                     `json:"executor_output_formats,omitempty"`
	RequestLifecyclePlugin bool                         `json:"request_lifecycle_plugin"`
	CommandLinePlugin      bool                         `json:"command_line_plugin"`
	ManagementAPI          bool                         `json:"management_api"`
	QuotaProvider          bool                         `json:"quota_provider"`
}

// Plugin implements the CPA plugin capability set for the XiaoyiWork proxy.
type Plugin struct {
	mu             sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	config         PluginConfig
	hostCall       HostCallFunc
	keys           xiaoyi.Resolver
	client         *xiaoyi.Client
	cloudMu        sync.Mutex
	cloudCache     map[string]xiaoyi.BusinessCredential
	loginMu        sync.Mutex
	loginSessions  map[string]*oauthSession
	loginServer    *http.Server
	lastLoginState string
}

// NewPlugin creates an uninitialised plugin instance.
func NewPlugin(hostCall HostCallFunc) *Plugin {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := DefaultPluginConfig()
	return &Plugin{
		ctx:           ctx,
		cancel:        cancel,
		config:        cfg,
		hostCall:      hostCall,
		client:        xiaoyi.NewClient(cfg.BaseURL),
		cloudCache:    make(map[string]xiaoyi.BusinessCredential),
		loginSessions: make(map[string]*oauthSession),
	}
}

// SetHostCall replaces the host callback after the C ABI handshake.
func (p *Plugin) SetHostCall(fn HostCallFunc) {
	p.mu.Lock()
	p.hostCall = fn
	p.mu.Unlock()
}

// Config returns a copy of the current configuration.
func (p *Plugin) Config() PluginConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config
}

// HandleMethod dispatches one JSON-RPC style plugin method.
func (p *Plugin) HandleMethod(method string, raw []byte) (any, error) {
	switch method {
	case pluginabi.MethodPluginRegister:
		return p.handleRegister(raw)
	case pluginabi.MethodPluginReconfigure, "plugin.reload":
		return p.handleRegister(raw)
	case pluginabi.MethodPluginQuiesce:
		return map[string]any{"quiesced": true}, nil
	case pluginabi.MethodPluginShutdown:
		p.loginMu.Lock()
		if p.loginServer != nil {
			_ = p.loginServer.Close()
			p.loginServer = nil
		}
		p.loginMu.Unlock()
		p.cancel()
		return map[string]any{"shutdown": true}, nil

	case pluginabi.MethodAuthIdentifier:
		return p.handleAuthIdentifier()
	case pluginabi.MethodAuthParse:
		return p.handleAuthParse(raw)
	case pluginabi.MethodAuthLoginStart:
		return p.handleAuthLoginStart(raw)
	case pluginabi.MethodAuthLoginPoll:
		return p.handleAuthLoginPoll(raw)
	case pluginabi.MethodAuthRefresh:
		return p.handleAuthRefresh(raw)

	case pluginabi.MethodModelStatic:
		return p.handleModelStatic(raw)
	case pluginabi.MethodModelForAuth:
		return p.handleModelForAuth(raw)
	case pluginabi.MethodModelRoute:
		return p.handleModelRoute(raw)

	case pluginabi.MethodExecutorIdentifier:
		return p.handleExecutorIdentifier()
	case pluginabi.MethodExecutorExecute:
		return p.handleExecutorExecute(raw)
	case pluginabi.MethodExecutorExecuteStream:
		return p.handleExecutorExecuteStream(raw)
	case pluginabi.MethodExecutorCountTokens:
		return map[string]any{"tokens": 0}, nil

	case pluginabi.MethodQuotaIdentifier:
		return p.handleQuotaIdentifier()
	case pluginabi.MethodQuotaDescribe:
		return p.handleQuotaDescribe()
	case pluginabi.MethodQuotaFetch:
		return p.handleQuotaFetch(raw)

	case pluginabi.MethodManagementRegister:
		return p.handleManagementRegister(raw)
	case pluginabi.MethodManagementHandle:
		return p.handleManagementHandle(raw)

	default:
		return nil, fmt.Errorf("unknown method: %s", method)
	}
}

func (p *Plugin) handleRegister(raw []byte) (any, error) {
	if len(raw) > 0 {
		var in struct {
			ConfigYAML []byte `json:"config_yaml"`
		}
		if err := json.Unmarshal(raw, &in); err == nil && len(in.ConfigYAML) > 0 {
			cfg := ParsePluginConfig(in.ConfigYAML)
			p.mu.Lock()
			previous := p.config.BaseURL
			p.config = cfg
			if p.client == nil || previous != cfg.BaseURL {
				p.client = xiaoyi.NewClient(cfg.BaseURL)
			}
			p.mu.Unlock()
			p.keys.Invalidate()
		}
	}

	return rpcRegistration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             PluginName,
			Version:          Version,
			Author:           "WenPaul",
			GitHubRepository: "https://github.com/wangxiaoshuai136/xiaoyi-oauth",
			Logo:             "https://raw.githubusercontent.com/wangxiaoshuai136/xiaoyi-oauth/main/icon.png",
			ConfigFields: []pluginapi.ConfigField{
				{
					Name:        "base_url",
					Type:        pluginapi.ConfigFieldTypeString,
					Default:     xiaoyi.CloudBaseURL,
					Description: "小艺Work 上游地址。默认云端（不需要桌面端运行）；填 127.0.0.1 可改走本地代理",
				},
				{
					Name:        "model_prefix",
					Type:        pluginapi.ConfigFieldTypeString,
					Default:     "xiaoyiwork/",
					Description: "暴露给客户端的模型前缀，例如 xiaoyiwork/GLM_5.3_VE",
				},
				{
					Name:        "api_key",
					Type:        pluginapi.ConfigFieldTypeString,
					Default:     "",
					Description: "可选。留空时自动从注册表 + DPAPI 解密小艺Work proxyKey，不落盘",
				},
				{
					Name:        "route_unprefixed_models",
					Type:        pluginapi.ConfigFieldTypeBoolean,
					Default:     true,
					Description: "是否同时路由不带前缀的原生模型 id",
				},
				{
					Name:        "request_timeout_seconds",
					Type:        pluginapi.ConfigFieldTypeInteger,
					Default:     600,
					Description: "单个上游请求的超时秒数",
				},
				{
					Name:        "panel_base_url",
					Type:        pluginapi.ConfigFieldTypeString,
					Default:     "http://127.0.0.1:8317",
					Description: "登录窗口打开的面板地址",
				},
				{
					Name:        "debug",
					Type:        pluginapi.ConfigFieldTypeBoolean,
					Default:     false,
					Description: "在宿主日志中输出详细排障信息",
				},
			},
		},
		Capabilities: rpcCapabilities{
			ModelProvider:         true,
			AuthProvider:          true,
			ModelRouter:           true,
			Executor:              true,
			ExecutorModelScope:    pluginapi.ExecutorModelScopeOAuth,
			ExecutorInputFormats:  []string{"chat-completions"},
			ExecutorOutputFormats: []string{"chat-completions"},
			ManagementAPI:         true,
			QuotaProvider:         true,
		},
	}, nil
}

func (p *Plugin) callHost(method string, req any) ([]byte, error) {
	p.mu.RLock()
	fn := p.hostCall
	p.mu.RUnlock()
	if fn == nil {
		return nil, fmt.Errorf("host callback is unavailable")
	}
	var payload []byte
	if req != nil {
		raw, err := json.Marshal(req)
		if err != nil {
			return nil, err
		}
		payload = raw
	}
	return fn(method, payload)
}

func (p *Plugin) log(level, message string, fields map[string]string) {
	req := map[string]any{
		"level":   level,
		"message": message,
		"fields":  fields,
	}
	_, _ = p.callHost(pluginabi.MethodHostLog, req)
}

func (p *Plugin) emitStreamChunk(streamID string, payload []byte) error {
	req := map[string]any{
		"stream_id": streamID,
		"payload":   payload,
	}
	_, err := p.callHost(pluginabi.MethodHostStreamEmit, req)
	return err
}

func (p *Plugin) closeStream(streamID string, streamErr *pluginabi.Error) error {
	req := map[string]any{"stream_id": streamID}
	if streamErr != nil {
		req["error"] = streamErr.Message
		req["error_detail"] = streamErr
	}
	_, err := p.callHost(pluginabi.MethodHostStreamClose, req)
	return err
}

func (p *Plugin) resolveKey(force bool) (string, string, error) {
	cfg := p.Config()
	key, source, err := p.keys.Resolve(cfg.APIKey, force)
	if err != nil {
		return "", "", err
	}
	if cfg.Debug {
		p.log("debug", "resolved XiaoyiWork proxy key", map[string]string{"source": source})
	}
	return key, source, nil
}

func (p *Plugin) requestContext() (context.Context, context.CancelFunc) {
	cfg := p.Config()
	timeout := time.Duration(cfg.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return context.WithTimeout(p.ctx, timeout)
}

func cleanModel(model, prefix string) string {
	model = strings.TrimSpace(model)
	if prefix != "" {
		model = strings.TrimPrefix(model, prefix)
	}
	model = strings.TrimPrefix(model, PluginID+"/")
	return strings.TrimSpace(model)
}
