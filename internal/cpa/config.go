package cpa

import (
	"strconv"
	"strings"

	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/xiaoyi"
)

// PluginConfig holds the plugin-owned settings under plugins.configs.xiaoyiwork.
type PluginConfig struct {
	// BaseURL is the local OpenAI-compatible proxy root.
	BaseURL string
	// ModelPrefix is prepended to exposed model ids.
	ModelPrefix string
	// APIKey overrides automatic registry/DPAPI discovery.
	APIKey string
	// RouteUnprefixedModels also routes bare upstream model ids to this plugin.
	RouteUnprefixedModels bool
	// RequestTimeoutSeconds bounds one upstream request.
	RequestTimeoutSeconds int
	// Debug emits verbose host logs.
	Debug bool
	// AutoFallback retries an empty upstream error frame with a stronger model.
	AutoFallback bool
	// LoginBrowser selects who opens the sign-in page: "isolated" launches the
	// default browser with a throwaway profile, "host" lets ECPA open it.
	LoginBrowser string
}

// DefaultPluginConfig returns the documented defaults.
func DefaultPluginConfig() PluginConfig {
	return PluginConfig{
		BaseURL:               xiaoyi.CloudBaseURL,
		ModelPrefix:           "xiaoyiwork/",
		RouteUnprefixedModels: true,
		RequestTimeoutSeconds: 600,
		AutoFallback:          false,
		LoginBrowser:          "isolated",
	}
}

// ParsePluginConfig reads the flat key: value YAML the host passes at register.
func ParsePluginConfig(raw []byte) PluginConfig {
	cfg := DefaultPluginConfig()
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = unquoteYAML(strings.TrimSpace(value))
		switch key {
		case "base_url":
			if value != "" {
				cfg.BaseURL = strings.TrimRight(value, "/")
			}
		case "model_prefix":
			cfg.ModelPrefix = value
		case "api_key":
			cfg.APIKey = value
		case "route_unprefixed_models":
			cfg.RouteUnprefixedModels = parseBool(value, cfg.RouteUnprefixedModels)
		case "request_timeout_seconds":
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				cfg.RequestTimeoutSeconds = n
			}
		case "debug":
			cfg.Debug = parseBool(value, cfg.Debug)
		case "auto_fallback":
			cfg.AutoFallback = parseBool(value, cfg.AutoFallback)
		case "login_browser":
			switch strings.ToLower(value) {
			case "host", "isolated":
				cfg.LoginBrowser = strings.ToLower(value)
			}
		}
	}
	if cfg.ModelPrefix != "" && !strings.HasSuffix(cfg.ModelPrefix, "/") {
		cfg.ModelPrefix += "/"
	}
	return cfg
}

func unquoteYAML(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return value
}

func parseBool(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "on", "1":
		return true
	case "false", "no", "off", "0":
		return false
	}
	return fallback
}
