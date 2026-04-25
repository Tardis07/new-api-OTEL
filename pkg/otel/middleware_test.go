package otel

import (
	"testing"
)

func TestParseClaudeChunk(t *testing.T) {
	enabled = true

	samples := []struct {
		name string
		data string
	}{
		{"message_start", `{"type":"message_start","message":{"id":"msg_abc","type":"message","role":"assistant","model":"claude-opus-4-6","usage":{"input_tokens":25,"output_tokens":1}}}`},
		{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello world"}}`},
		{"tool_use_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_123","name":"get_weather"}}`},
		{"input_json_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"loc"}}`},
		{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":15}}`},
	}

	for _, s := range samples {
		t.Run(s.name, func(t *testing.T) {
			result := parseClaudeChunk(s.data)
			if result == nil {
				t.Errorf("parseClaudeChunk returned nil for %s", s.name)
			} else {
				t.Logf("parseClaudeChunk(%s) = %d attrs", s.name, len(result))
				for _, attr := range result {
					t.Logf("  %s = %v", attr.Key, attr.Value.AsInterface())
				}
			}
		})
	}
}
