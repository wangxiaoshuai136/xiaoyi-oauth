package xiaoyi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Model describes one model exposed by the XiaoyiWork local proxy.
type Model struct {
	// ID is the provider-native model id sent upstream.
	ID string
	// Display is the friendly name shown in CPA.
	Display string
	// Thinking reports whether the model emits reasoning_content.
	Thinking bool
	// ContextWindow is the model's advertised input window in tokens.
	ContextWindow int
	// MaxOutputTokens is the model's advertised output limit in tokens.
	MaxOutputTokens int
}

func withCapabilities(m Model) Model {
	caps := Capabilities(m.ID)
	m.ContextWindow = caps.Context
	m.MaxOutputTokens = caps.Output
	return m
}

// FallbackCatalog mirrors the catalog observed for XiaoyiWork 0.7.7.1.
func FallbackCatalog() []Model {
	return []Model{
		withCapabilities(Model{ID: "Auto-Model", Display: "Auto-Model"}),
		withCapabilities(Model{ID: "LLM_DeepSeekV4_Think", Display: "DeepSeek-V4-Flash", Thinking: true}),
		withCapabilities(Model{ID: "DeepSeek_V4.1_Flash_VE", Display: "DeepSeek-V4.1-Flash"}),
		withCapabilities(Model{ID: "OpenPanguV2-Pro", Display: "openPangu-2.1-Pro"}),
		withCapabilities(Model{ID: "LLM_GLM5.3", Display: "GLM-5.3"}),
		withCapabilities(Model{ID: "GLM_5.3_FLASH_VE", Display: "GLM-5.3-Flash"}),
		withCapabilities(Model{ID: "MiniMax_M3.1_Flash", Display: "MiniMax M3.1 Flash"}),
		withCapabilities(Model{ID: "Kimi_K3.1_Pro_VE", Display: "Kimi K3.1 Pro"}),
	}
}

var (
	catalogMu     sync.Mutex
	cachedCatalog []Model
	catalogExpiry time.Time
	catalogTTL    = 60 * time.Second
)

// Catalog returns the models the desktop app currently offers. It prefers the
// newest %APPDATA%\xiaoyiwork\users\*\models.json and falls back to the built-in
// list when the file is missing or unreadable.
func Catalog() []Model {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	now := time.Now()
	if cachedCatalog != nil && now.Before(catalogExpiry) {
		return cachedCatalog
	}
	models := loadModelsJSON()
	if len(models) == 0 {
		models = FallbackCatalog()
	}
	cachedCatalog = models
	catalogExpiry = now.Add(catalogTTL)
	return models
}

// FindModel resolves a model by upstream id or display name (case-insensitive).
func FindModel(id string) (Model, bool) {
	target := strings.ToLower(strings.TrimSpace(id))
	if target == "" {
		return Model{}, false
	}
	for _, m := range Catalog() {
		if strings.ToLower(m.ID) == target || strings.ToLower(m.Display) == target {
			return m, true
		}
	}
	return Model{}, false
}

func loadModelsJSON() []Model {
	root := dataDir()
	if root == "" {
		return nil
	}
	files, err := filepath.Glob(filepath.Join(root, "users", "*", "models.json"))
	if err != nil || len(files) == 0 {
		return nil
	}
	sort.Slice(files, func(i, j int) bool {
		si, _ := os.Stat(files[i])
		sj, _ := os.Stat(files[j])
		if si == nil || sj == nil {
			return files[i] < files[j]
		}
		return si.ModTime().After(sj.ModTime())
	})

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var doc struct {
			Models []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
				ID    string `json:"id"`
			} `json:"models"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			continue
		}
		var out []Model
		for _, item := range doc.Models {
			id := strings.TrimSpace(item.Value)
			if id == "" {
				id = strings.TrimSpace(item.ID)
			}
			if id == "" {
				continue
			}
			display := strings.TrimSpace(item.Name)
			if display == "" {
				display = id
			}
			out = append(out, withCapabilities(Model{ID: id, Display: display, Thinking: looksThinking(id)}))
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func looksThinking(id string) bool {
	lower := strings.ToLower(id)
	return strings.Contains(lower, "think") || strings.Contains(lower, "reason")
}

func dataDir() string {
	if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
		return filepath.Join(appData, "xiaoyiwork")
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		return filepath.Join(localAppData, "xiaoyiwork")
	}
	return ""
}
