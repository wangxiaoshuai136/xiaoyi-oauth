package xiaoyi

import "testing"

func TestFallbackCatalogCapabilities(t *testing.T) {
	models := FallbackCatalog()
	if len(models) == 0 {
		t.Fatal("empty fallback catalog")
	}
	for _, m := range models {
		if m.ContextWindow <= 0 {
			t.Fatalf("%s has no context window", m.ID)
		}
		if m.MaxOutputTokens <= 0 {
			t.Fatalf("%s has no output limit", m.ID)
		}
	}
	glm, ok := withCapabilities(Model{ID: "GLM_5.3_FLASH_VE"}), true
	if !ok || glm.ContextWindow != 1048576 {
		t.Fatalf("GLM-5.3-Flash context = %d", glm.ContextWindow)
	}
	ds := withCapabilities(Model{ID: "DeepSeek_V4.1_Flash_VE"})
	if ds.ContextWindow != 1048576 || ds.MaxOutputTokens != 64000 {
		t.Fatalf("DeepSeek V4.1 Flash = %d/%d", ds.ContextWindow, ds.MaxOutputTokens)
	}
}
