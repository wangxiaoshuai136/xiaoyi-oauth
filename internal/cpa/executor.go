package cpa

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/wangxiaoshuai136/cpa-plugin-xiaoyiwork/internal/pluginabi"
	"github.com/wangxiaoshuai136/cpa-plugin-xiaoyiwork/internal/pluginapi"
	"github.com/wangxiaoshuai136/cpa-plugin-xiaoyiwork/internal/xiaoyi"
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
	"reasoning_effort",
	"thinking",
}

func (p *Plugin) handleExecutorIdentifier() (any, error) {
	return map[string]any{"identifier": PluginID}, nil
}

func (p *Plugin) handleExecutorExecute(raw []byte) (any, error) {
	var req rpcExecutorRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode executor.execute request: %w", err)
	}
	key, _, err := p.resolveKey(false)
	if err != nil {
		return nil, keyError(err)
	}
	_, clientModel, payload, err := p.prepareRequest(req)
	if err != nil {
		return nil, err
	}

	ctx, cancel := p.requestContext()
	defer cancel()
	body, _, err := p.client.Chat(ctx, key, payload)
	if err != nil && xiaoyi.IsUnauthorized(err) {
		// The desktop app may have rotated its key; re-read once and retry.
		p.keys.Invalidate()
		if fresh, _, freshErr := p.resolveKey(true); freshErr == nil {
			key = fresh
			body, _, err = p.client.Chat(ctx, key, payload)
		}
	}
	if err != nil {
		if xiaoyi.IsUnauthorized(err) {
			p.keys.Invalidate()
		}
		return nil, upstreamError(err)
	}

	trans := NewStreamTranslator(clientModel)
	if looksLikeSSE(body) {
		if err := consumeSSE(body, trans); err != nil {
			return nil, err
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
	key, _, err := p.resolveKey(false)
	if err != nil {
		return nil, keyError(err)
	}
	_, clientModel, payload, err := p.prepareRequest(req)
	if err != nil {
		return nil, err
	}

	ctx, cancel := p.requestContext()
	body, _, err := p.client.ChatStream(ctx, key, payload)
	if err != nil && xiaoyi.IsUnauthorized(err) {
		p.keys.Invalidate()
		if fresh, _, freshErr := p.resolveKey(true); freshErr == nil {
			key = fresh
			body, _, err = p.client.ChatStream(ctx, key, payload)
		}
	}
	if err != nil {
		cancel()
		if xiaoyi.IsUnauthorized(err) {
			p.keys.Invalidate()
		}
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
			return upstreamError(err)
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
		"无法读取小艺Work proxyKey，请确认小艺Work 桌面端正在运行且已登录："+err.Error(),
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
	return pluginabi.NewError("xiaoyiwork_upstream_error", err.Error(), http.StatusBadGateway)
}
