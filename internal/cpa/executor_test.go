package cpa

import (
	"encoding/json"
	"testing"
)

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
