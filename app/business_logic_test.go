package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCheckSendScope(t *testing.T) {
	tests := []struct {
		name           string
		subjectPattern string
		subject        string
		expected       bool
	}{
		{"exact match", "events.user.created", "events.user.created", true},
		{"no match", "events.user.created", "events.order.created", false},
		{"glob wildcard matches suffix", "events.*", "events.user.created", true},
		{"glob wildcard matches prefix", "*created", "events.user.created", true},
		{"glob wildcard matches all", "*", "anything.goes.here", true},
		{"underscore single char", "events._", "events.x", true},
		{"underscore no match empty", "events._", "events.", false},
		{"underscore no match multiple", "events._", "events.xy", false},
		{"empty pattern empty subject", "", "", true},
		{"empty pattern non-empty subject", "", "something", false},
		{"non-empty pattern empty subject", "events.*", "", false},
		{"glob matches empty string", "events.*", "events.", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckSendScope(tt.subjectPattern, tt.subject)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchLikePattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		value    string
		expected bool
	}{
		// Glob wildcard
		{"glob at end", "abc*", "abcdef", true},
		{"glob at start", "*def", "abcdef", true},
		{"glob in middle", "abc*def", "abcXYZdef", true},
		{"glob matches empty", "abc*", "abc", true},
		{"multiple glob", "*abc*def*", "XXabcYYdefZZ", true},
		{"consecutive glob", "a**b", "aXb", true},
		{"only glob", "*", "anything", true},
		{"glob matches empty string", "*", "", true},

		// Underscore wildcard
		{"underscore matches one char", "a_c", "abc", true},
		{"underscore no match too short", "a_c", "ac", false},
		{"underscore no match too long", "a_c", "abbc", false},
		{"multiple underscores", "a__c", "abbc", true},

		// Combined wildcards
		{"glob and underscore", "a_*", "abcdef", true},
		{"underscore and glob", "_bc*", "abcdef", true},

		// No wildcards (literal match)
		{"exact match", "abc", "abc", true},
		{"literal no match", "abc", "abd", false},
		{"literal different length", "abc", "ab", false},

		// Edge cases
		{"both empty", "", "", true},
		{"empty pattern non-empty value", "", "abc", false},
		{"non-empty pattern empty value", "abc", "", false},
		{"single underscore empty value", "_", "", false},
		{"single underscore single char", "_", "x", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchLikePattern(tt.pattern, tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchesFilter(t *testing.T) {
	tests := []struct {
		name      string
		filter    []byte
		eventData []byte
		expected  bool
	}{
		// Nil / empty filter matches all
		{"nil filter", nil, []byte(`{"key":"value"}`), true},
		{"empty filter bytes", []byte{}, []byte(`{"key":"value"}`), true},
		{"empty filter object", []byte(`{}`), []byte(`{"key":"value"}`), true},

		// Matching filter
		{"single key match", []byte(`{"type":"user"}`), []byte(`{"type":"user","name":"alice"}`), true},
		{"multiple key match", []byte(`{"type":"user","action":"create"}`), []byte(`{"type":"user","action":"create","id":1}`), true},

		// Non-matching filter
		{"key mismatch", []byte(`{"type":"order"}`), []byte(`{"type":"user"}`), false},
		{"missing key in data", []byte(`{"missing":"value"}`), []byte(`{"type":"user"}`), false},
		{"partial key match only one matches", []byte(`{"type":"user","action":"delete"}`), []byte(`{"type":"user","action":"create"}`), false},

		// Nested JSON values
		{"nested object match", []byte(`{"meta":{"env":"prod"}}`), []byte(`{"meta":{"env":"prod"},"key":"val"}`), true},
		{"nested object mismatch", []byte(`{"meta":{"env":"staging"}}`), []byte(`{"meta":{"env":"prod"}}`), false},

		// Numeric values
		{"numeric match", []byte(`{"count":42}`), []byte(`{"count":42,"label":"test"}`), true},
		{"numeric mismatch", []byte(`{"count":99}`), []byte(`{"count":42}`), false},

		// Boolean values
		{"boolean match", []byte(`{"active":true}`), []byte(`{"active":true}`), true},
		{"boolean mismatch", []byte(`{"active":true}`), []byte(`{"active":false}`), false},

		// Invalid JSON
		{"invalid filter JSON", []byte(`not json`), []byte(`{"key":"value"}`), false},
		{"invalid event data JSON", []byte(`{"key":"value"}`), []byte(`not json`), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesFilter(tt.filter, tt.eventData)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateBackoff(t *testing.T) {
	tests := []struct {
		name              string
		attemptNum        int
		maxBackoffSeconds int
		expected          time.Duration
	}{
		{"first attempt (0)", 0, 300, 1 * time.Second},
		{"second attempt (1)", 1, 300, 2 * time.Second},
		{"third attempt (2)", 2, 300, 4 * time.Second},
		{"fourth attempt (3)", 3, 300, 8 * time.Second},
		{"fifth attempt (4)", 4, 300, 16 * time.Second},
		{"respects max cap", 10, 300, 300 * time.Second},
		{"exactly at cap", 8, 256, 256 * time.Second},
		{"exceeds cap", 9, 256, 256 * time.Second},
		{"small cap", 3, 5, 5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateBackoff(tt.attemptNum, tt.maxBackoffSeconds)
			assert.Equal(t, tt.expected, result)
		})
	}
}
