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
		if refreshErr := p.refreshCloudAuth(ctx, &auth, true); refreshErr == nil {
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
	freePoints, _ := toFloat(aiPoints["freePoints"])
	giftPoints, _ := toFloat(aiPoints["giftPoints"])
	purchasedPoints, _ := toFloat(aiPoints["purchasedPoints"])
	memberPoints, _ := toFloat(aiPoints["memberPoints"])
	compstPoints, _ := toFloat(aiPoints["compstPoints"])
	if points == 0 {
		points = freePoints + giftPoints + purchasedPoints + memberPoints + compstPoints
	}
	plan := firstString(membership["skuGroupName"], membership["skuName"], "小艺Work")
	membershipID := firstString(membership["skuGroupId"])
	tier := firstString(subscription["skuName"], subscription["skuGroupName"], "华为账号")
	expire := firstString(subscription["expireTime"])
	// The trial plan grants 200 free points per month; the API only returns the
	// remaining balance, so the monthly allowance is the known denominator.
	freeTotal := freePoints
	if membershipID == "xy_free_benefit" {
		freeTotal = 200
	}
	counts := []struct {
		name  string
		value float64
		total float64
	}{
		{"免费额度", freePoints, freeTotal},
		{"赠送积分", giftPoints, giftPoints},
		{"购买积分", purchasedPoints, purchasedPoints},
		{"会员积分", memberPoints, memberPoints},
		{"补偿积分", compstPoints, compstPoints},
	}
	resp := pluginapi.QuotaFetchResponse{
		Subscription: &pluginapi.QuotaSubscription{Plan: plan, TierName: tier, TierID: tier},
		Summary: []pluginapi.QuotaMetric{
			{Key: "ai_points", Label: "总积分", Value: points, Unit: "积分", Format: "number"},
		},
	}
	var buckets []pluginapi.QuotaBucket
	now := time.Now()
	nextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
	for _, item := range counts {
		if points > 0 && item.value <= 0 {
			continue
		}
		fraction := 0.0
		if item.total > 0 {
			fraction = item.value / item.total
			if fraction > 1 {
				fraction = 1
			}
		}
		desc := fmt.Sprintf("剩余 %g", item.value)
		if item.total > item.value {
			desc += fmt.Sprintf(" / %g", item.total)
		}
		desc += " 积分"
		if item.total > 0 {
			desc += fmt.Sprintf(" · %.0f%%", fraction*100)
		}
		if expire != "" && item.name == "会员积分" {
			desc += " · 有效期至 " + expire
		}
		resetTime := ""
		if item.name == "免费额度" {
			desc += " · 每月 1 日刷新"
			resetTime = nextMonth.Format(time.RFC3339)
		}
		buckets = append(buckets, pluginapi.QuotaBucket{
			Window:            item.name,
			RemainingFraction: fraction,
			ResetTime:         resetTime,
			Description:       desc,
		})
	}
	if len(buckets) > 0 {
		resp.Groups = []pluginapi.QuotaGroup{{DisplayName: "积分构成", Buckets: buckets}}
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
