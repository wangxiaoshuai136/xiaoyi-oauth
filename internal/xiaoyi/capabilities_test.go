package xiaoyi

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	openRouterFetch = func() (map[string]Capability, bool) { return nil, false }
	os.Exit(m.Run())
}

func TestStaticCapabilitiesCoverCatalog(t *testing.T) {
	for _, m := range FallbackCatalog() {
		if m.ContextWindow <= 0 || m.MaxOutputTokens <= 0 {
			t.Fatalf("%s is missing capabilities", m.ID)
		}
	}
}

func TestOpenRouterMappingCoversKnownModels(t *testing.T) {
	for modelID, remoteID := range openRouterIDs {
		if remoteID == "" {
			t.Fatalf("%s has no OpenRouter id", modelID)
		}
		if staticCapabilities[modelID].Context <= 0 {
			t.Fatalf("%s has no static fallback", modelID)
		}
	}
}

func TestCapabilitiesPrefersOpenRouter(t *testing.T) {
	oldFetch := openRouterFetch
	openRouterFetch = func() (map[string]Capability, bool) {
		return map[string]Capability{"z-ai/glm-5.3-flash": {Context: 1234567, Output: 234567}}, true
	}
	t.Cleanup(func() {
		openRouterFetch = oldFetch
		capabilityMu.Lock()
		capabilityCache = nil
		capabilityExpiry = time.Time{}
		capabilityMu.Unlock()
	})
	capabilityMu.Lock()
	capabilityCache = nil
	capabilityExpiry = time.Time{}
	capabilityMu.Unlock()

	caps := Capabilities("GLM_5.3_FLASH_VE")
	if caps.Context != 1234567 || caps.Output != 234567 {
		t.Fatalf("dynamic capabilities not applied: %+v", caps)
	}
	if got := strings.ToUpper("glm_5.3_flash_ve"); got != "GLM_5.3_FLASH_VE" {
		t.Fatalf("unexpected normalization: %s", got)
	}
}
