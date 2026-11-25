package cpa

import (
	"encoding/json"
	"strings"

	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/pluginapi"
	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/xiaoyi"
)

func (p *Plugin) handleModelStatic(raw []byte) (any, error) {
	var req pluginapi.StaticModelRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	return pluginapi.ModelResponse{
		Provider: PluginID,
		Models:   p.catalogModels(exclusionRules(req.Host.ExcludedModels)),
	}, nil
}

func (p *Plugin) handleModelForAuth(raw []byte) (any, error) {
	var req pluginapi.AuthModelRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	return pluginapi.ModelResponse{
		Provider: PluginID,
		Models:   p.catalogModels(exclusionRules(req.Host.ExcludedModels)),
	}, nil
}

func (p *Plugin) handleModelRoute(raw []byte) (any, error) {
	var req pluginapi.ModelRouteRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	cfg := p.Config()
	requested := strings.TrimSpace(req.RequestedModel)
	lower := strings.ToLower(requested)
	prefix := strings.ToLower(cfg.ModelPrefix)

	if prefix != "" && strings.HasPrefix(lower, prefix) {
		base := cleanModel(requested, cfg.ModelPrefix)
		if _, ok := xiaoyi.FindModel(base); ok && !excluded(base, nil) {
			return pluginapi.ModelRouteResponse{
				Handled:     true,
				TargetKind:  pluginapi.ModelRouteTargetProvider,
				Target:      PluginID,
				TargetModel: cfg.ModelPrefix + base,
			}, nil
		}
		return pluginapi.ModelRouteResponse{Handled: false}, nil
	}

	if cfg.RouteUnprefixedModels {
		if model, ok := xiaoyi.FindModel(lower); ok {
			return pluginapi.ModelRouteResponse{
				Handled:     true,
				TargetKind:  pluginapi.ModelRouteTargetProvider,
				Target:      PluginID,
				TargetModel: cfg.ModelPrefix + model.ID,
			}, nil
		}
	}
	return pluginapi.ModelRouteResponse{Handled: false}, nil
}

func (p *Plugin) catalogModels(rules []string) []pluginapi.ModelInfo {
	cfg := p.Config()
	models := xiaoyi.Catalog()
	out := make([]pluginapi.ModelInfo, 0, len(models))
	for _, m := range models {
		if excluded(m.ID, rules) || excluded(m.Display, rules) {
			continue
		}
		display := m.Display
		if display == "" {
			display = m.ID
		}
		contextWindow := int64(m.ContextWindow)
		if contextWindow <= 0 {
			contextWindow = 131072
		}
		maxOutput := int64(m.MaxOutputTokens)
		if maxOutput <= 0 {
			maxOutput = 8192
		}
		out = append(out, pluginapi.ModelInfo{
			ID:                         cfg.ModelPrefix + m.ID,
			Name:                       m.ID,
			DisplayName:                display + " · 小艺Work",
			Object:                     "model",
			OwnedBy:                    PluginID,
			Type:                       "chat",
			ContextLength:              contextWindow,
			InputTokenLimit:            contextWindow,
			OutputTokenLimit:           maxOutput,
			MaxCompletionTokens:        maxOutput,
			SupportedGenerationMethods: []string{"chat.completions"},
			SupportedInputModalities:   []string{"text"},
			SupportedOutputModalities:  []string{"text"},
			Thinking: &pluginapi.ThinkingSupport{
				ZeroAllowed:    true,
				DynamicAllowed: true,
				Levels:         []string{"low", "medium", "high"},
			},
		})
	}
	return out
}

func exclusionRules(host map[string][]string) []string {
	var rules []string
	for key, list := range host {
		if strings.EqualFold(key, PluginID) || strings.EqualFold(key, "xiaoyi") {
			rules = append(rules, list...)
		}
	}
	return rules
}

func excluded(model string, rules []string) bool {
	target := strings.ToLower(strings.TrimSpace(model))
	for _, rule := range rules {
		pattern := strings.ToLower(strings.TrimSpace(rule))
		if pattern == "" {
			continue
		}
		if pattern == "*" || pattern == target {
			return true
		}
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(target, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}
