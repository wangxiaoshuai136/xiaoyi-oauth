package cpa

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// upstreamChunk is the shape XiaoyiWork emits inside each SSE data frame.
type upstreamChunk struct {
	Error *struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Usage   map[string]any `json:"usage"`
	Choices []struct {
		Delta        map[string]any `json:"delta"`
		FinishReason string         `json:"finish_reason"`
	} `json:"choices"`
}

type toolCallAccumulator struct {
	ID        string
	Name      string
	Arguments string
}

// StreamTranslator turns XiaoyiWork SSE frames into standard OpenAI chunks and
// also accumulates a complete answer for non-streaming callers.
type StreamTranslator struct {
	id         string
	created    int64
	model      string
	roleSent   bool
	usage      map[string]any
	finish     string
	finishSent bool
	ended      bool

	content   strings.Builder
	reasoning strings.Builder
	tools     map[int]*toolCallAccumulator
	toolOrder []int
}

// NewStreamTranslator starts one translation session for a client-facing model.
func NewStreamTranslator(model string) *StreamTranslator {
	return &StreamTranslator{
		id:      "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24],
		created: time.Now().Unix(),
		model:   model,
		tools:   make(map[int]*toolCallAccumulator),
	}
}

// FrameError is an error frame delivered inside the upstream SSE stream.
type FrameError struct {
	Code    string
	Message string
}

// Empty reports whether the upstream sent an error frame without any detail.
func (e *FrameError) Empty() bool {
	return strings.TrimSpace(e.Code) == "" && strings.TrimSpace(e.Message) == ""
}

// QuotaExhausted reports the Xiaoyi entitlement/points rejection. The upstream
// phrases it as a generic "用户权益校验失败", so map it explicitly.
func (e *FrameError) QuotaExhausted() bool {
	return strings.TrimSpace(e.Code) == "111110"
}

func (e *FrameError) Error() string {
	if e.Empty() {
		return "小艺Work 上游返回空错误帧（无 code/message，模型侧中断）"
	}
	if e.QuotaExhausted() {
		return "小艺账号积分/权益不足（111110）：积分可能已耗尽，请充值或等待额度刷新后再试"
	}
	return fmt.Sprintf("小艺Work 上游错误 %s: %s", e.Code, e.Message)
}

// Translate converts one data payload into zero or more OpenAI SSE frames.
func (t *StreamTranslator) Translate(payload []byte) ([][]byte, error) {
	payload = trimSSEPayload(payload)
	if len(payload) == 0 || string(payload) == "[DONE]" {
		return nil, nil
	}
	var frame upstreamChunk
	if err := json.Unmarshal(payload, &frame); err != nil {
		return nil, nil
	}
	if frame.Error != nil {
		failure := &FrameError{Message: strings.TrimSpace(frame.Error.Message)}
		if frame.Error.Code != nil {
			failure.Code = fmt.Sprintf("%v", frame.Error.Code)
		}
		return nil, failure
	}
	if usage := normalizeUsage(frame.Usage); usage != nil {
		t.usage = usage
	}
	if len(frame.Choices) == 0 {
		return nil, nil
	}
	choice := frame.Choices[0]
	if choice.FinishReason != "" {
		t.finish = choice.FinishReason
	}

	t.absorb(choice.Delta)
	delta := t.sanitizeDelta(choice.Delta)
	if len(delta) == 0 && choice.FinishReason == "" {
		return nil, nil
	}
	chunk := t.buildChunk(delta, choice.FinishReason)
	if choice.FinishReason != "" {
		t.finishSent = true
	}
	raw, err := json.Marshal(chunk)
	if err != nil {
		return nil, err
	}
	return [][]byte{sseFrame(raw)}, nil
}

// Finish emits the closing frames: a final choice (if the upstream never sent
// one) plus a usage frame. XiaoyiWork never sends [DONE]; the host needs it.
func (t *StreamTranslator) Finish() [][]byte {
	var out [][]byte
	if t.ended {
		return out
	}
	t.ended = true
	if t.finish == "" {
		t.finish = "stop"
		if len(t.tools) > 0 {
			t.finish = "tool_calls"
		}
	}
	if !t.finishSent {
		final := t.buildChunk(map[string]any{}, t.finish)
		if raw, err := json.Marshal(final); err == nil {
			out = append(out, sseFrame(raw))
		}
	}
	if t.usage != nil {
		usageChunk := map[string]any{
			"id":      t.id,
			"object":  "chat.completion.chunk",
			"created": t.created,
			"model":   t.model,
			"choices": []any{},
			"usage":   t.usage,
		}
		if raw, err := json.Marshal(usageChunk); err == nil {
			out = append(out, sseFrame(raw))
		}
	}
	out = append(out, []byte("data: [DONE]\n\n"))
	return out
}

// Completion builds a non-streaming chat.completion body.
func (t *StreamTranslator) Completion() []byte {
	finish := t.finish
	if finish == "" {
		finish = "stop"
		if len(t.tools) > 0 {
			finish = "tool_calls"
		}
	}
	message := map[string]any{
		"role":    "assistant",
		"content": t.content.String(),
	}
	if t.reasoning.Len() > 0 {
		message["reasoning_content"] = t.reasoning.String()
	}
	if calls := t.toolCalls(); len(calls) > 0 {
		message["tool_calls"] = calls
		message["content"] = ""
	}
	resp := map[string]any{
		"id":      t.id,
		"object":  "chat.completion",
		"created": t.created,
		"model":   t.model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": finish,
		}},
	}
	if t.usage != nil {
		resp["usage"] = t.usage
	}
	raw, _ := json.Marshal(resp)
	return raw
}

func (t *StreamTranslator) buildChunk(delta map[string]any, finish string) map[string]any {
	choice := map[string]any{
		"index": 0,
		"delta": delta,
	}
	if finish != "" {
		choice["finish_reason"] = finish
	} else {
		choice["finish_reason"] = nil
	}
	return map[string]any{
		"id":      t.id,
		"object":  "chat.completion.chunk",
		"created": t.created,
		"model":   t.model,
		"choices": []any{choice},
	}
}

func (t *StreamTranslator) sanitizeDelta(delta map[string]any) map[string]any {
	out := make(map[string]any, len(delta))
	if role, ok := delta["role"].(string); ok && role != "" && !t.roleSent {
		out["role"] = role
		t.roleSent = true
	}
	if content, ok := delta["content"].(string); ok && content != "" {
		out["content"] = content
	}
	if reasoning, ok := delta["reasoning_content"].(string); ok && reasoning != "" {
		out["reasoning_content"] = reasoning
	}
	if calls, ok := delta["tool_calls"].([]any); ok {
		cleaned := make([]any, 0, len(calls))
		for _, item := range calls {
			call, ok := item.(map[string]any)
			if !ok {
				continue
			}
			sanitized := map[string]any{}
			if index, ok := call["index"]; ok {
				sanitized["index"] = index
			}
			if id, ok := call["id"].(string); ok && id != "" {
				sanitized["id"] = id
			}
			if typ, ok := call["type"].(string); ok && typ != "" {
				sanitized["type"] = typ
			}
			if function, ok := call["function"].(map[string]any); ok {
				fn := map[string]any{}
				if name, ok := function["name"].(string); ok && name != "" {
					fn["name"] = name
				}
				if args, ok := function["arguments"].(string); ok && args != "" {
					fn["arguments"] = args
				}
				if len(fn) > 0 {
					sanitized["function"] = fn
				}
			}
			if len(sanitized) > 1 {
				cleaned = append(cleaned, sanitized)
			}
		}
		if len(cleaned) > 0 {
			out["tool_calls"] = cleaned
		}
	}
	return out
}

func (t *StreamTranslator) absorb(delta map[string]any) {
	if content, ok := delta["content"].(string); ok && content != "" {
		t.content.WriteString(content)
	}
	if reasoning, ok := delta["reasoning_content"].(string); ok && reasoning != "" {
		t.reasoning.WriteString(reasoning)
	}
	if calls, ok := delta["tool_calls"].([]any); ok {
		for position, item := range calls {
			call, ok := item.(map[string]any)
			if !ok {
				continue
			}
			index := position
			if idx, ok := toInt(call["index"]); ok {
				index = idx
			}
			acc := t.tools[index]
			if acc == nil {
				acc = &toolCallAccumulator{}
				t.tools[index] = acc
				t.toolOrder = append(t.toolOrder, index)
			}
			if id, ok := call["id"].(string); ok && id != "" {
				acc.ID = id
			}
			if function, ok := call["function"].(map[string]any); ok {
				if name, ok := function["name"].(string); ok && name != "" {
					acc.Name = name
				}
				if args, ok := function["arguments"].(string); ok && args != "" {
					acc.Arguments = mergeArguments(acc.Arguments, args)
				}
			}
		}
	}
}

func (t *StreamTranslator) toolCalls() []any {
	if len(t.tools) == 0 {
		return nil
	}
	out := make([]any, 0, len(t.tools))
	for _, index := range t.toolOrder {
		acc := t.tools[index]
		if acc == nil || acc.Name == "" {
			continue
		}
		id := acc.ID
		if id == "" {
			id = "call_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
		}
		out = append(out, map[string]any{
			"id":   id,
			"type": "function",
			"function": map[string]any{
				"name":      acc.Name,
				"arguments": acc.Arguments,
			},
		})
	}
	return out
}

func mergeArguments(existing, next string) string {
	if existing == "" {
		return next
	}
	if next == existing {
		return existing
	}
	if strings.HasPrefix(next, existing) {
		return next
	}
	return existing + next
}

func normalizeUsage(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	prompt, okPrompt := firstInt(raw, "prompt_tokens", "input_tokens")
	completion, okCompletion := firstInt(raw, "completion_tokens", "output_tokens")
	total, okTotal := firstInt(raw, "total_tokens")
	if !okPrompt && !okCompletion && !okTotal {
		return nil
	}
	if !okTotal {
		total = prompt + completion
	}
	usage := map[string]any{
		"prompt_tokens":     prompt,
		"completion_tokens": completion,
		"total_tokens":      total,
	}
	if details, ok := raw["prompt_tokens_details"].(map[string]any); ok {
		if cached, ok := toInt(details["cached_tokens"]); ok {
			usage["prompt_tokens_details"] = map[string]any{"cached_tokens": cached}
		}
	}
	if details, ok := raw["completion_tokens_details"].(map[string]any); ok {
		if reasoning, ok := toInt(details["reasoning_tokens"]); ok {
			usage["completion_tokens_details"] = map[string]any{"reasoning_tokens": reasoning}
		}
	}
	return usage
}

func firstInt(m map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		if v, ok := toInt(m[key]); ok {
			return v, true
		}
	}
	return 0, false
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}

func trimSSEPayload(payload []byte) []byte {
	text := strings.TrimSpace(string(payload))
	text = strings.TrimPrefix(text, "data:")
	text = strings.TrimSpace(text)
	return []byte(text)
}

func sseFrame(raw []byte) []byte {
	return []byte("data: " + string(raw) + "\n\n")
}
