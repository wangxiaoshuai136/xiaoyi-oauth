package cpa

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/pluginabi"
	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/pluginapi"
	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/xiaoyi"
)

type rpcExecutorRequest struct {
	pluginapi.ExecutorRequest
	StreamID       string `json:"stream_id,omitempty"`
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type rpcExecutorStreamResponse struct {
	Headers http.Header                     `json:"headers,omitempty"`
	Chunks  []pluginapi.ExecutorStreamChunk `json:"chunks,omitempty"`
}

// Parameters XiaoyiWork ignores. Passing them through would only make callers
// believe limits took effect.
var unsupportedParams = []string{
	"max_tokens",
	"max_completion_tokens",
	"temperature",
	"top_p",
	"top_k",
	"stop",
	"n",
	"presence_penalty",
	"frequency_penalty",
	"logprobs",
	"top_logprobs",
	"seed",
	"user",
	"stream_options",
	"thinking",
}

// fallbackModels maps a model whose upstream generation can abort with an
// empty SSE error frame to a stronger model that completes the same request.
// Substituting models costs more quota, so it is opt-in via auto_fallback.
var fallbackModels = map[string]string{
	"GLM_5.3_FLASH_VE": "LLM_GLM5.3",
}

func (p *Plugin) fallbackTarget(model string, err error) (string, bool) {
	if !p.Config().AutoFallback {
		return "", false
	}
	var frameErr *FrameError
	if !errors.As(err, &frameErr) || !frameErr.Empty() {
		return "", false
	}
	target, ok := fallbackModels[model]
	if !ok || target == "" || target == model {
		return "", false
	}
	return target, true
}

func withModel(payload []byte, model string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil, err
	}
	doc["model"] = model
	return json.Marshal(doc)
}

func (p *Plugin) handleExecutorIdentifier() (any, error) {
	return map[string]any{"identifier": PluginID}, nil
}

// requestAuth is everything one upstream request needs, plus what a refresh
// would need if the cloud credential expires.
type requestAuth struct {
	Auth               xiaoyi.Auth
	Local              bool
	UID                string
	DeviceID           string
	ClientID           string
	AccessToken        string
	AccessTokenExp     int64
	RefreshToken       string
	BusinessValidUntil int64
}

func (p *Plugin) authForRequest(req rpcExecutorRequest) (requestAuth, error) {
	cfg := p.Config()
	var payload authFilePayload
	if len(req.StorageJSON) > 0 {
		_ = json.Unmarshal(req.StorageJSON, &payload)
	}
	if xiaoyi.IsLoopbackURL(cfg.BaseURL) {
		key := storedProxyKey(req.StorageJSON, req.AuthMetadata)
		if key == "" {
			key = strings.TrimSpace(cfg.APIKey)
		}
		if key == "" {
			key = strings.TrimSpace(os.Getenv(xiaoyi.KeyEnvPrimary))
		}
		if key == "" {
			key = strings.TrimSpace(os.Getenv(xiaoyi.KeyEnvLegacy))
		}
		if key == "" {
			return requestAuth{}, fmt.Errorf("本地代理模式需要 api_key，请先登录或配置")
		}
		return requestAuth{Auth: xiaoyi.Auth{LocalKey: key}, Local: true}, nil
	}

	uid := firstNonEmpty(payload.UID, payload.AccountID)
	deviceID := payload.DeviceID
	credential := payload.BusinessCredential
	refreshToken := payload.RefreshToken
	clientID := payload.ClientID
	if credential == "" || uid == "" {
		return requestAuth{}, fmt.Errorf("凭据缺少 businessCredential/uid，请重新登录")
	}
	if deviceID == "" {
		deviceID, _ = xiaoyi.DeviceID(uid)
	}
	if clientID == "" {
		clientID = xiaoyi.HuaweiClientID
	}
	p.cloudMu.Lock()
	if cached, ok := p.cloudCache[uid]; ok && cached.Credential != "" {
		credential = cached.Credential
	}
	p.cloudMu.Unlock()
	if credential == "" {
		return requestAuth{}, fmt.Errorf("没有可用的 businessCredential")
	}
	return requestAuth{
		Auth: xiaoyi.Auth{
			BusinessCredential: credential,
			UID:                uid,
			DeviceID:           deviceID,
		},
		UID:            uid,
		DeviceID:       deviceID,
		ClientID:       clientID,
		AccessToken:    payload.AccessToken,
		AccessTokenExp: payload.AccessTokenExp,
		RefreshToken:   refreshToken,
	}, nil
}

// refreshCloudAuth renews the UAT (when needed) and issues a new business
// credential, so the plugin keeps working without the desktop app running.
func (p *Plugin) refreshCloudAuth(ctx context.Context, auth *requestAuth) error {
	access := auth.AccessToken
	if access == "" || (auth.AccessTokenExp > 0 && time.Now().UnixMilli() >= auth.AccessTokenExp) {
		if auth.RefreshToken == "" {
			return fmt.Errorf("refresh token 为空，需要重新登录小艺Work")
		}
		fresh, gatewayErr := xiaoyi.RefreshAccessToken(ctx, auth.Auth.BusinessCredential, auth.UID, auth.DeviceID, auth.RefreshToken)
		var tokens xiaoyi.OAuthTokens
		if gatewayErr == nil {
			tokens = xiaoyi.OAuthTokens{AccessToken: fresh, ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}
		} else {
			tokens, gatewayErr = xiaoyi.RefreshOAuth(ctx, auth.RefreshToken)
			if gatewayErr != nil {
				return gatewayErr
			}
		}
		access = tokens.AccessToken
		auth.AccessToken = tokens.AccessToken
		auth.AccessTokenExp = tokens.ExpiresAt
		if tokens.RefreshToken != "" {
			auth.RefreshToken = tokens.RefreshToken
		}
	}
	credential, validUntil, err := xiaoyi.CreateBusinessCredential(ctx, access, auth.UID, auth.DeviceID, auth.ClientID)
	if err != nil {
		return err
	}
	auth.Auth.BusinessCredential = credential
	auth.BusinessValidUntil = validUntil
	p.cloudMu.Lock()
	p.cloudCache[auth.UID] = xiaoyi.BusinessCredential{Credential: credential, UID: auth.UID, ValidUntil: validUntil}
	p.cloudMu.Unlock()
	p.persistCloudCredential(auth)
	return nil
}

func (p *Plugin) doChat(ctx context.Context, auth *requestAuth, payload []byte) ([]byte, error) {
	body, _, err := p.client.Chat(ctx, auth.Auth, payload)
	if err == nil || !xiaoyi.IsUnauthorized(err) {
		return body, err
	}
	if auth.Local {
		p.keys.Invalidate()
		if fresh, _, freshErr := p.resolveKey(true); freshErr == nil {
			auth.Auth.LocalKey = fresh
			body, _, err = p.client.Chat(ctx, auth.Auth, payload)
			return body, err
		}
		return nil, err
	}
	if refreshErr := p.refreshCloudAuth(ctx, auth); refreshErr != nil {
		return nil, err
	}
	body, _, err = p.client.Chat(ctx, auth.Auth, payload)
	return body, err
}

func (p *Plugin) doChatStream(ctx context.Context, auth *requestAuth, payload []byte) (io.ReadCloser, error) {
	body, _, err := p.client.ChatStream(ctx, auth.Auth, payload)
	if err == nil || !xiaoyi.IsUnauthorized(err) {
		return body, err
	}
	if auth.Local {
		p.keys.Invalidate()
		if fresh, _, freshErr := p.resolveKey(true); freshErr == nil {
			auth.Auth.LocalKey = fresh
			body, _, err = p.client.ChatStream(ctx, auth.Auth, payload)
			return body, err
		}
		return nil, err
	}
	if refreshErr := p.refreshCloudAuth(ctx, auth); refreshErr != nil {
		return nil, err
	}
	body, _, err = p.client.ChatStream(ctx, auth.Auth, payload)
	return body, err
}

func (p *Plugin) handleExecutorExecute(raw []byte) (any, error) {
	var req rpcExecutorRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode executor.execute request: %w", err)
	}
	auth, err := p.authForRequest(req)
	if err != nil {
		return nil, keyError(err)
	}
	upstreamModel, clientModel, payload, err := p.prepareRequest(req)
	if err != nil {
		return nil, err
	}

	ctx, cancel := p.requestContext()
	defer cancel()
	body, err := p.doChat(ctx, &auth, payload)
	if err != nil {
		return nil, upstreamError(err)
	}

	trans := NewStreamTranslator(clientModel)
	if looksLikeSSE(body) {
		frameErr := consumeSSE(body, trans)
		if frameErr != nil {
			if target, ok := p.fallbackTarget(upstreamModel, frameErr); ok {
				p.log("warn", fmt.Sprintf("%s 返回空错误帧，自动回退到 %s 重试", upstreamModel, target), nil)
				if fbPayload, encodeErr := withModel(payload, target); encodeErr == nil {
					if fbBody, chatErr := p.doChat(ctx, &auth, fbPayload); chatErr == nil {
						fbTrans := NewStreamTranslator(clientModel)
						if fbErr := consumeSSE(fbBody, fbTrans); fbErr == nil {
							trans, frameErr = fbTrans, nil
						} else {
							frameErr = fbErr
						}
					} else {
						frameErr = chatErr
					}
				}
			}
		}
		if frameErr != nil {
			return nil, upstreamError(frameErr)
		}
	} else {
		var probe map[string]any
		if err := json.Unmarshal(body, &probe); err == nil {
			probe["model"] = clientModel
			if raw, err := json.Marshal(probe); err == nil {
				body = raw
			}
		}
	}

	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	return pluginapi.ExecutorResponse{Payload: trans.Completion(), Headers: headers}, nil
}

func (p *Plugin) handleExecutorExecuteStream(raw []byte) (any, error) {
	var req rpcExecutorRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode executor.execute_stream request: %w", err)
	}
	auth, err := p.authForRequest(req)
	if err != nil {
		return nil, keyError(err)
	}
	_, clientModel, payload, err := p.prepareRequest(req)
	if err != nil {
		return nil, err
	}

	ctx, cancel := p.requestContext()
	body, err := p.doChatStream(ctx, &auth, payload)
	if err != nil {
		cancel()
		return nil, upstreamError(err)
	}

	streamID := strings.TrimSpace(req.StreamID)
	if streamID == "" {
		streamID = uuid.NewString()
	}

	go func() {
		defer cancel()
		defer body.Close()
		trans := NewStreamTranslator(clientModel)
		if err := streamSSE(body, trans, func(frame []byte) error {
			return p.emitStreamChunk(streamID, frame)
		}); err != nil {
			_ = p.closeStream(streamID, upstreamError(err))
			return
		}
		for _, frame := range trans.Finish() {
			if err := p.emitStreamChunk(streamID, frame); err != nil {
				_ = p.closeStream(streamID, pluginabi.NewError("stream_emit_failed", err.Error(), http.StatusBadGateway))
				return
			}
		}
		_ = p.closeStream(streamID, nil)
	}()

	headers := make(http.Header)
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")
	return rpcExecutorStreamResponse{Headers: headers}, nil
}

func (p *Plugin) prepareRequest(req rpcExecutorRequest) (upstreamModel, clientModel string, payload []byte, err error) {
	cfg := p.Config()
	clientModel = strings.TrimSpace(req.Model)
	lookup := cleanModel(req.Model, cfg.ModelPrefix)
	if lookup == "" {
		return "", "", nil, pluginabi.NewError("invalid_model", "请求缺少模型名", http.StatusBadRequest)
	}
	model, ok := xiaoyi.FindModel(lookup)
	if !ok {
		return "", "", nil, pluginabi.NewError(
			"unknown_model",
			fmt.Sprintf("小艺Work 不提供模型 %q（可用模型见插件模型列表）", lookup),
			http.StatusBadRequest,
		)
	}

	body := req.Payload
	if len(body) == 0 {
		body = req.OriginalRequest
	}
	if len(body) == 0 {
		return "", "", nil, pluginabi.NewError("invalid_request", "请求体为空", http.StatusBadRequest)
	}

	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", "", nil, pluginabi.NewError("invalid_request", "请求体不是合法 JSON: "+err.Error(), http.StatusBadRequest)
	}
	doc["model"] = model.ID
	doc["stream"] = true
	if effort, ok := doc["reasoning_effort"].(string); ok {
		if strings.TrimSpace(effort) == "" {
			delete(doc, "reasoning_effort")
		} else if mapped, changed := xiaoyi.NormalizeEffort(model.ID, effort); changed {
			doc["reasoning_effort"] = mapped
			if cfg.Debug {
				p.log("debug", "reasoning_effort "+effort+" -> "+mapped, nil)
			}
		}
	}
	for _, key := range unsupportedParams {
		delete(doc, key)
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return "", "", nil, pluginabi.NewError("invalid_request", "重新编码请求失败: "+err.Error(), http.StatusBadRequest)
	}
	return model.ID, clientModel, encoded, nil
}

func streamSSE(body io.Reader, trans *StreamTranslator, emit func([]byte) error) error {
	reader := bufio.NewReaderSize(body, 1<<20)
	for {
		line, readErr := reader.ReadString('\n')
		if payload, ok := sseDataLine(line); ok {
			frames, err := trans.Translate(payload)
			if err != nil {
				return err
			}
			for _, frame := range frames {
				if err := emit(frame); err != nil {
					return err
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

func consumeSSE(body []byte, trans *StreamTranslator) error {
	for _, line := range strings.Split(string(body), "\n") {
		payload, ok := sseDataLine(line)
		if !ok {
			continue
		}
		if _, err := trans.Translate(payload); err != nil {
			return err
		}
	}
	return nil
}

func sseDataLine(line string) ([]byte, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return nil, false
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" || payload == "[DONE]" {
		return nil, false
	}
	return []byte(payload), true
}

func looksLikeSSE(body []byte) bool {
	head := string(body)
	if len(head) > 2048 {
		head = head[:2048]
	}
	return strings.Contains(head, "data:") && strings.Contains(head, "\n")
}

func keyError(err error) *pluginabi.Error {
	return pluginabi.NewError(
		"xiaoyiwork_key_unavailable",
		"无法读取小艺Work 登录凭据，请先在插件里重新导入账号："+err.Error(),
		http.StatusServiceUnavailable,
	)
}

func upstreamError(err error) *pluginabi.Error {
	if err == nil {
		return nil
	}
	var pe *pluginabi.Error
	if errors.As(err, &pe) {
		return pe
	}
	var up *xiaoyi.UpstreamError
	if errors.As(err, &up) {
		switch up.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return pluginabi.NewError("xiaoyiwork_unauthorized", up.Error(), http.StatusUnauthorized)
		case http.StatusTooManyRequests:
			e := pluginabi.NewError("xiaoyiwork_rate_limited", up.Error(), http.StatusTooManyRequests)
			e.Retryable = true
			return e
		default:
			return pluginabi.NewError("xiaoyiwork_upstream_error", up.Error(), http.StatusBadGateway)
		}
	}
	var frameErr *FrameError
	if errors.As(err, &frameErr) && frameErr.QuotaExhausted() {
		e := pluginabi.NewError("xiaoyiwork_quota_exhausted", frameErr.Error(), http.StatusPaymentRequired)
		e.Retryable = false
		return e
	}
	return pluginabi.NewError("xiaoyiwork_upstream_error", err.Error(), http.StatusBadGateway)
}
