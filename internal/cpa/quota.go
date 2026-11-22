package cpa

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/pluginapi"
	"github.com/wangxiaoshuai136/xiaoyi-oauth/internal/xiaoyi"
)

func (p *Plugin) handleQuotaIdentifier() (any, error) {
	return rpcIdentifierResponse{
		Identifier:          PluginID,
		Identifiers:         []string{PluginID},
		CredentialProviders: []string{PluginID},
	}, nil
}

func (p *Plugin) handleQuotaDescribe() (any, error) {
	return pluginapi.QuotaDescribeResponse{
		SupportedProviders: []string{PluginID},
		DisplayName:        "Xiaoyi OAuth 积分",
		SupportsReset:      false,
	}, nil
}

func (p *Plugin) handleQuotaFetch(raw []byte) (any, error) {
	var req pluginapi.QuotaFetchRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	auth, err := p.authForRequest(rpcExecutorRequest{
		ExecutorRequest: pluginapi.ExecutorRequest{StorageJSON: req.StorageJSON, AuthMetadata: req.Metadata},
	})
	if err != nil {
		return pluginapi.QuotaFetchResponse{}, err
	}
	ctx, cancel := context.WithTimeout(p.ctx, 20*time.Second)
	defer cancel()
	body, err := xiaoyi.QueryPrivilege(ctx, auth.Auth.BusinessCredential, auth.UID, auth.DeviceID)
	if err != nil && xiaoyi.IsUnauthorized(err) {
		if refreshErr := p.refreshCloudAuth(ctx, &auth); refreshErr == nil {
			body, err = xiaoyi.QueryPrivilege(ctx, auth.Auth.BusinessCredential, auth.UID, auth.DeviceID)
		}
	}
	if err != nil {
		return pluginapi.QuotaFetchResponse{}, err
	}
	return buildQuotaResponse(body), nil
}

func buildQuotaResponse(body []byte) pluginapi.QuotaFetchResponse {
	var root map[string]any
	_ = json.Unmarshal(body, &root)
	for _, key := range []string{"data", "result"} {
		if nested, ok := root[key].(map[string]any); ok && len(nested) > 0 {
			merged := map[string]any{}
			for k, v := range root {
				merged[k] = v
			}
			for k, v := range nested {
				merged[k] = v
			}
			root = merged
			break
		}
	}
	celia := asMap(root["celiaPremiumPrivilege"])
	aiPoints := asMap(celia["aiPoints"])
	membership := asMap(celia["membership"])
	subscription := asMap(celia["subscription"])

	points, _ := toFloat(aiPoints["totalPoints"])
	privilegeList, _ := root["privilegeList"].([]any)
	if points == 0 && len(privilegeList) > 0 {
		for _, item := range privilegeList {
			points += privilegePoints(asMap(item))
		}
	}
	plan := firstString(membership["skuGroupName"], membership["skuName"], "小艺Work")
	tier := firstString(subscription["skuName"], subscription["skuGroupName"], "华为账号")
	expire := firstString(subscription["expireTime"])
	hasResource, hasFlag := root["hasAvailableResource"].(bool)
	remainingFraction := 1.0
	if hasFlag && !hasResource {
		remainingFraction = 0
	} else if !hasFlag && points <= 0 {
		remainingFraction = 0
	}
	desc := fmt.Sprintf("剩余 %g 积分", points)
	if expire != "" {
		desc += " · 有效期至 " + expire
	}

	resp := pluginapi.QuotaFetchResponse{
		Subscription: &pluginapi.QuotaSubscription{Plan: plan, TierName: tier, TierID: tier},
		Summary: []pluginapi.QuotaMetric{
			{Key: "ai_points", Label: "AI 积分", Value: points, Unit: "积分", Format: "number"},
		},
		Groups: []pluginapi.QuotaGroup{{
			DisplayName: "小艺Work 积分",
			Buckets: []pluginapi.QuotaBucket{{
				Window:            "AI 积分",
				RemainingFraction: remainingFraction,
				ResetTime:         expire,
				Description:       desc,
			}},
		}},
	}
	return resp
}

func privilegePoints(item map[string]any) float64 {
	total := 0.0
	if details, ok := item["privilegeDetail"].([]any); ok {
		for _, raw := range details {
			switch value := raw.(type) {
			case float64:
				total += value
			case string:
				for _, field := range strings.FieldsFunc(value, func(r rune) bool { return r < '0' || r > '9' }) {
					if n, ok := toFloat(field); ok {
						total += n
					}
				}
			}
		}
		return total
	}
	for _, key := range []string{"remainingPoints", "remainPoint", "remainPoints", "point"} {
		if n, ok := toFloat(item[key]); ok {
			total += n
		}
	}
	return total
}

func asMap(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func toFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case string:
		var n float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%f", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}

func firstString(values ...any) string {
	for _, value := range values {
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
