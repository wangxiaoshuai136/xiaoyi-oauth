package cpa

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTranslateContentAndUsage(t *testing.T) {
	tr := NewStreamTranslator("xiaoyiwork/GLM_5.3")
	frames, err := tr.Translate([]byte(`{"usage":{"input_tokens":9,"output_tokens":2,"total_tokens":11},"choices":[{"delta":{"role":"assistant"}}]}`))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected role frame, got %d", len(frames))
	}
	frames, err = tr.Translate([]byte(`{"choices":[{"delta":{"content":"OK"}}]}`))
	if err != nil || len(frames) != 1 {
		t.Fatalf("content frame: %v %d", err, len(frames))
	}
	var chunk map[string]any
	body := strings.TrimSpace(strings.TrimPrefix(string(frames[0]), "data:"))
	if err := json.Unmarshal([]byte(body), &chunk); err != nil {
		t.Fatalf("decode chunk: %v", err)
	}
	if chunk["object"] != "chat.completion.chunk" {
		t.Fatalf("object = %v", chunk["object"])
	}
	choice := chunk["choices"].([]any)[0].(map[string]any)
	if choice["index"].(float64) != 0 {
		t.Fatalf("index = %v", choice["index"])
	}
	delta := choice["delta"].(map[string]any)
	if delta["content"] != "OK" {
		t.Fatalf("content = %v", delta["content"])
	}

	frames, err = tr.Translate([]byte(`{"choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`))
	if err != nil || len(frames) != 1 {
		t.Fatalf("finish frame: %v %d", err, len(frames))
	}
	ended := tr.Finish()
	if len(ended) != 2 {
		t.Fatalf("expected usage frame + [DONE], got %d", len(ended))
	}
	if string(ended[1]) != "data: [DONE]\n\n" {
		t.Fatalf("missing DONE marker: %q", ended[1])
	}
	usageBody := strings.TrimSpace(strings.TrimPrefix(string(ended[0]), "data:"))
	var usageChunk map[string]any
	if err := json.Unmarshal([]byte(usageBody), &usageChunk); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	usage := usageChunk["usage"].(map[string]any)
	if usage["prompt_tokens"].(float64) != 9 || usage["completion_tokens"].(float64) != 2 {
		t.Fatalf("usage = %v", usage)
	}
}

func TestCompletionMergesToolCallFragments(t *testing.T) {
	tr := NewStreamTranslator("flash")
	frames := []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"ci"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ty\":\"Beijing\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	for _, frame := range frames {
		if _, err := tr.Translate([]byte(frame)); err != nil {
			t.Fatalf("translate: %v", err)
		}
	}
	var completion map[string]any
	if err := json.Unmarshal(tr.Completion(), &completion); err != nil {
		t.Fatalf("decode completion: %v", err)
	}
	message := completion["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	calls := message["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("tool calls = %d", len(calls))
	}
	call := calls[0].(map[string]any)
	fn := call["function"].(map[string]any)
	if fn["name"] != "get_weather" || fn["arguments"] != `{"city":"Beijing"}` {
		t.Fatalf("call = %v", call)
	}
}

func TestTranslateSurfacesErrorFrame(t *testing.T) {
	tr := NewStreamTranslator("m")
	_, err := tr.Translate([]byte(`{"error":{"code":11115,"message":"server inner"}}`))
	if err == nil || !strings.Contains(err.Error(), "server inner") {
		t.Fatalf("expected upstream error, got %v", err)
	}
}

func TestCleanModelStripsPrefix(t *testing.T) {
	if got := cleanModel("xiaoyiwork/GLM_5.3", "xiaoyiwork/"); got != "GLM_5.3" {
		t.Fatalf("cleanModel = %q", got)
	}
}
