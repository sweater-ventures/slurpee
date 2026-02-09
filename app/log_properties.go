package app

import (
	"context"
	"encoding/json"

	"github.com/sweater-ventures/slurpee/db"
)

// ExtractLogProperties looks up the log_config for the given subject and extracts
// the configured property values from the event's JSONB data. Returns nil if no
// log_config exists or the data cannot be parsed.
func ExtractLogProperties(ctx context.Context, querier db.Querier, subject string, data []byte) map[string]string {
	logConfig, err := querier.GetLogConfigBySubject(ctx, subject)
	if err != nil {
		return nil
	}

	var dataObj map[string]any
	if err := json.Unmarshal(data, &dataObj); err != nil {
		return nil
	}

	if len(logConfig.LogProperties) == 0 {
		return nil
	}

	props := make(map[string]string)
	for _, prop := range logConfig.LogProperties {
		if val, ok := dataObj[prop]; ok {
			props[prop] = formatPropertyValue(val)
		}
	}

	if len(props) == 0 {
		return nil
	}
	return props
}

// BatchExtractLogProperties extracts log properties for a batch of events efficiently
// by caching log_config lookups per subject.
func BatchExtractLogProperties(ctx context.Context, querier db.Querier, events []db.Event) []map[string]string {
	cache := make(map[string]*db.LogConfig)
	result := make([]map[string]string, len(events))

	for i, event := range events {
		lc, cached := cache[event.Subject]
		if !cached {
			config, err := querier.GetLogConfigBySubject(ctx, event.Subject)
			if err != nil {
				cache[event.Subject] = nil
			} else {
				cache[event.Subject] = &config
				lc = &config
			}
		}
		if lc == nil || len(lc.LogProperties) == 0 {
			continue
		}

		var dataObj map[string]any
		if err := json.Unmarshal(event.Data, &dataObj); err != nil {
			continue
		}

		props := make(map[string]string)
		for _, prop := range lc.LogProperties {
			if val, ok := dataObj[prop]; ok {
				props[prop] = formatPropertyValue(val)
			}
		}
		if len(props) > 0 {
			result[i] = props
		}
	}
	return result
}

// formatPropertyValue converts an arbitrary JSON value to a display string.
func formatPropertyValue(val any) string {
	switch v := val.(type) {
	case string:
		return v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
