package xiaoyi

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Capability is the advertised context window and output limit of a model.
type Capability struct {
	Context int
	Output  int
}

// staticCapabilities are the fallbacks used when OpenRouter cannot be reached.
// Values follow the public model families (models.dev / OpenRouter).
var staticCapabilities = map[string]Capability{
	"AUTO-MODEL":             {Context: 1048576, Output: 131072},
	"LLM_DEEPSEEKV4_THINK":   {Context: 1048576, Output: 393216},
	"DEEPSEEK_V4.1_FLASH_VE": {Context: 1048576, Output: 393216},
	"OPENPANGUV2-PRO":        {Context: 524288, Output: 131072},
	"LLM_GLM5.3":             {Context: 1048576, Output: 943718},
	"GLM_5.3_FLASH_VE":       {Context: 1048576, Output: 943718},
	"MINIMAX_M3.1_FLASH":     {Context: 1048576, Output: 524288},
	"KIMI_K3.1_PRO_VE":       {Context: 1048576, Output: 943718},
}

// openRouterIDs maps XiaoyiWork upstream ids to OpenRouter model ids. The
// desktop app resolves context windows the same way
// (enable_openrouter_model_context_window_tokens), because the Xiaoyi catalog
// API only returns names, descriptions and point multipliers.
var openRouterIDs = map[string]string{
	"LLM_DEEPSEEKV4_THINK":   "deepseek/deepseek-v4-flash",
	"DEEPSEEK_V4.1_FLASH_VE": "deepseek/deepseek-v4.1-flash",
	"LLM_GLM5.3":             "z-ai/glm-5.3",
	"GLM_5.3_FLASH_VE":       "z-ai/glm-5.3-flash",
	"MINIMAX_M3.1_FLASH":     "minimax/minimax-m3.1-flash",
	"KIMI_K3.1_PRO_VE":       "moonshotai/kimi-k3.1-pro",
}

const (
	openRouterModelsURL = "https://openrouter.ai/api/v1/models"
	capabilityTTL       = 6 * time.Hour
	capabilityRetry     = 10 * time.Minute
)

var (
	capabilityMu     sync.Mutex
	capabilityCache  map[string]Capability
	capabilityExpiry time.Time
)

// openRouterFetch is replaceable so tests never depend on the network.
var openRouterFetch = fetchOpenRouterCapabilities

// Capabilities returns the context/output limits for one XiaoyiWork model.
// OpenRouter's public catalog wins when it is reachable; otherwise the static
// table is used.
func Capabilities(modelID string) Capability {
	key := strings.ToUpper(strings.TrimSpace(modelID))
	caps := staticCapabilities[key]
	remoteID := openRouterIDs[key]
	if remoteID == "" {
		return caps
	}
	if remote, ok := lookupOpenRouter(remoteID); ok {
		if remote.Context > 0 {
			caps.Context = remote.Context
		}
		if remote.Output > 0 {
			caps.Output = remote.Output
		}
	}
	return caps
}

func lookupOpenRouter(modelID string) (Capability, bool) {
	capabilityMu.Lock()
	defer capabilityMu.Unlock()
	now := time.Now()
	if capabilityCache != nil && now.Before(capabilityExpiry) {
		caps, ok := capabilityCache[modelID]
		return caps, ok
	}
	cache, ok := openRouterFetch()
	if !ok {
		capabilityExpiry = now.Add(capabilityRetry)
		cache = capabilityCache
	} else {
		capabilityCache = cache
		capabilityExpiry = now.Add(capabilityTTL)
	}
	if cache == nil {
		return Capability{}, false
	}
	caps, found := cache[modelID]
	return caps, found
}

func fetchOpenRouterCapabilities() (map[string]Capability, bool) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(openRouterModelsURL)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var doc struct {
		Data []struct {
			ID          string `json:"id"`
			ContextLen  int    `json:"context_length"`
			TopProvider struct {
				MaxCompletionTokens int `json:"max_completion_tokens"`
			} `json:"top_provider"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, false
	}
	out := make(map[string]Capability, len(doc.Data))
	for _, item := range doc.Data {
		if item.ID == "" || item.ContextLen <= 0 {
			continue
		}
		out[item.ID] = Capability{Context: item.ContextLen, Output: item.TopProvider.MaxCompletionTokens}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}
