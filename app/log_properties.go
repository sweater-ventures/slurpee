package app

import (
	"context"
	"encoding/json"

	"github.com/sweater-ventures/slurpee/db"
)

// ExtractLogProperties looks up the log_config for the given subject (using the
// app-level cache) and extracts the configured property values from the event's
// JSONB data. Returns nil if no log_config exists or the data cannot be parsed.
func ExtractLogProperties(ctx context.Context, slurpee *Application, subject string, data []byte) map[string]string {
	logConfig, found := getLogConfig(ctx, slurpee, subject)
	if !found {
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
// using the app-level log config cache.
func BatchExtractLogProperties(ctx context.Context, slurpee *Application, events []db.Event) []map[string]string {
	result := make([]map[string]string, len(events))

	for i, event := range events {
		lc, found := getLogConfig(ctx, slurpee, event.Subject)
		if !found || len(lc.LogProperties) == 0 {
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

// getLogConfig returns the LogConfig for a subject, using the app-level cache.
func getLogConfig(ctx context.Context, slurpee *Application, subject string) (db.LogConfig, bool) {
	lc, found, inCache := slurpee.LogConfigCache.Get(subject)
	if inCache {
		return lc, found
	}
	lc, err := slurpee.DB.GetLogConfigBySubject(ctx, subject)
	if err != nil {
		slurpee.LogConfigCache.Set(subject, db.LogConfig{}, false)
		return db.LogConfig{}, false
	}
	slurpee.LogConfigCache.Set(subject, lc, true)
	return lc, true
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
