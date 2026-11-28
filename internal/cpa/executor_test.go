package cpa

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQuotaExhaustedFrame(t *testing.T) {
	frame := &FrameError{Code: "111110", Message: "用户权益校验失败"}
	if !frame.QuotaExhausted() {
		t.Fatal("111110 must be treated as quota exhaustion")
	}
	if !strings.Contains(frame.Error(), "积分") {
		t.Fatalf("message should mention points: %s", frame.Error())
	}
	mapped := upstreamError(frame)
	if mapped == nil || mapped.Code != "xiaoyiwork_quota_exhausted" {
		t.Fatalf("unexpected mapping: %#v", mapped)
	}
	if mapped.HTTPStatus != 402 || mapped.Retryable {
		t.Fatalf("quota errors must be 402 and non-retryable: %#v", mapped)
	}
}

func TestFrameErrorEmpty(t *testing.T) {
	empty := &FrameError{}
	if !empty.Empty() {
		t.Fatal("an empty error frame must be treated as empty")
	}
	if empty.Error() == "" {
		t.Fatal("empty frame error needs a readable message")
	}
	detailed := &FrameError{Code: "11115", Message: "server inner"}
	if detailed.Empty() {
		t.Fatal("a detailed error frame must not be treated as empty")
	}
}

func TestWithModel(t *testing.T) {
	in := []byte(`{"model":"GLM_5.3_FLASH_VE","messages":[{"role":"user","content":"x"}],"stream":true}`)
	out, err := withModel(in, "LLM_GLM5.3")
	if err != nil {
		t.Fatalf("withModel: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc["model"] != "LLM_GLM5.3" {
		t.Fatalf("model = %v", doc["model"])
	}
	if _, ok := doc["messages"]; !ok {
		t.Fatal("messages must be preserved")
	}
	if doc["stream"] != true {
		t.Fatal("stream must be preserved")
	}
}
