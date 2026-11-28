package xiaoyi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Capability is the advertised context window and output limit of a model.
type Capability struct {
	Context int
	Output  int
	// Levels lists the supported named reasoning efforts, ascending.
	Levels []string
	// ZeroAllowed reports whether reasoning can be disabled ("none").
	ZeroAllowed bool
}

// staticCapabilities are the fallbacks used when OpenRouter cannot be reached.
// Values follow the public model families (models.dev / OpenRouter).
var staticCapabilities = map[string]Capability{
	"AUTO-MODEL":             {Context: 1048576, Output: 131072, Levels: []string{"low", "medium", "high", "max"}, ZeroAllowed: true},
	"LLM_DEEPSEEKV4_THINK":   {Context: 1048576, Output: 393216, Levels: []string{"high", "xhigh"}, ZeroAllowed: true},
	"DEEPSEEK_V4.1_FLASH_VE": {Context: 1048576, Output: 393216, Levels: []string{"low", "high", "max"}, ZeroAllowed: true},
	"OPENPANGUV2-PRO":        {Context: 524288, Output: 131072, Levels: []string{"low", "medium", "high"}, ZeroAllowed: true},
	"LLM_GLM5.3":             {Context: 1048576, Output: 943718, Levels: []string{"low", "high", "max"}},
	"GLM_5.3_FLASH_VE":       {Context: 1048576, Output: 943718, Levels: []string{"low", "high", "max"}},
	"MINIMAX_M3.1_FLASH":     {Context: 1048576, Output: 524288, Levels: []string{"low", "medium", "high", "xhigh", "max"}},
	"KIMI_K3.1_PRO_VE":       {Context: 1048576, Output: 943718, Levels: []string{"low", "high", "max"}, ZeroAllowed: true},
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
		if len(remote.Levels) > 0 {
			caps.Levels = remote.Levels
			caps.ZeroAllowed = remote.ZeroAllowed
		}
	}
	return caps
}

var effortRanks = map[string]int{
	"none": 0, "minimal": 1, "low": 2, "medium": 3, "high": 4, "xhigh": 5, "max": 6,
}

func sortEfforts(levels []string) []string {
	sort.SliceStable(levels, func(i, j int) bool {
		return effortRanks[levels[i]] < effortRanks[levels[j]]
	})
	return levels
}

// NormalizeEffort maps a client-provided reasoning effort to the closest level
// the model actually supports. The second result reports whether it changed.
func NormalizeEffort(modelID, effort string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(effort))
	if value == "" {
		return effort, false
	}
	caps := Capabilities(modelID)
	if len(caps.Levels) == 0 {
		return effort, false
	}
	if value == "none" {
		if caps.ZeroAllowed {
			return "none", false
		}
		return caps.Levels[0], true
	}
	for _, level := range caps.Levels {
		if level == value {
			return effort, false
		}
	}
	rank, ok := effortRanks[value]
	if !ok {
		return effort, false
	}
	best := caps.Levels[0]
	bestDiff := 1 << 30
	for _, level := range caps.Levels {
		levelRank, ok := effortRanks[level]
		if !ok {
			continue
		}
		diff := levelRank - rank
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff || (diff == bestDiff && levelRank > effortRanks[best]) {
			best, bestDiff = level, diff
		}
	}
	return best, best != value
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
			Reasoning struct {
				Mandatory        bool     `json:"mandatory"`
				SupportedEfforts []string `json:"supported_efforts"`
			} `json:"reasoning"`
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
		caps := Capability{Context: item.ContextLen, Output: item.TopProvider.MaxCompletionTokens}
		for _, effort := range item.Reasoning.SupportedEfforts {
			effort = strings.ToLower(strings.TrimSpace(effort))
			switch effort {
			case "":
				continue
			case "none":
				caps.ZeroAllowed = !item.Reasoning.Mandatory
			default:
				caps.Levels = append(caps.Levels, effort)
			}
		}
		caps.Levels = sortEfforts(caps.Levels)
		out[item.ID] = caps
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}
