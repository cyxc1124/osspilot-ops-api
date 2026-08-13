package alerts

import (
	"encoding/json"
	"fmt"
	"strings"
)

var ruleTypes = map[string]struct{}{
	"bucket_capacity_80": {}, "bucket_capacity_90": {}, "tenant_quota_exceeded": {},
	"rgw_5xx_rate": {}, "upload_failure_rate": {}, "download_failure_rate": {},
	"frequent_delete": {}, "frequent_download": {}, "edit_save_failure": {}, "audit_write_failure": {},
}

var channelTypes = map[string]string{
	"email": "recipients", "webhook": "url", "wecom": "webhook_url", "feishu": "webhook_url", "alertmanager": "url",
}

func validRuleType(s string) error {
	if _, ok := ruleTypes[s]; !ok {
		return fmt.Errorf("unknown rule_type")
	}
	return nil
}

func validSeverity(s string) error {
	if s != "warning" && s != "critical" {
		return fmt.Errorf("severity must be warning or critical")
	}
	return nil
}

func validChannel(channelType string, config map[string]any) error {
	key, ok := channelTypes[channelType]
	if !ok {
		return fmt.Errorf("unknown channel_type")
	}
	if config == nil {
		return fmt.Errorf("%s channel requires config.%s", channelType, key)
	}
	v, exists := config[key]
	if !exists || v == nil {
		return fmt.Errorf("%s channel requires config.%s", channelType, key)
	}
	if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s channel requires config.%s", channelType, key)
	}
	return nil
}

func asObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return map[string]any{}
	}
	return m
}
