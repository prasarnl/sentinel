package llmbench

import (
	"encoding/json"
	"testing"
)

// Frames captured from the live qwen35B failure: vLLM with a reasoning parser
// streams thinking in delta.reasoning and leaves delta.content empty until the
// thinking ends. Counting only content made a working model look like a dead
// stream, because max_tokens ran out before it stopped reasoning.
func TestChunkOutputCountsReasoning(t *testing.T) {
	tests := []struct {
		name          string
		json          string
		wantText      string
		wantReasoning bool
	}{
		{
			name:     "ordinary content",
			json:     `{"choices":[{"delta":{"content":"hello"}}]}`,
			wantText: "hello",
		},
		{
			name:          "vllm reasoning parser",
			json:          `{"choices":[{"delta":{"reasoning":"Here"},"finish_reason":null}]}`,
			wantText:      "Here",
			wantReasoning: true,
		},
		{
			name:          "reasoning_content spelling",
			json:          `{"choices":[{"delta":{"reasoning_content":"thinking"}}]}`,
			wantText:      "thinking",
			wantReasoning: true,
		},
		{
			// The opening frame of every stream: a role announcement with no
			// text. It must not count as a first token, or TTFT would measure
			// connection setup rather than generation.
			name: "role-only opener yields nothing",
			json: `{"choices":[{"delta":{"role":"assistant","content":""}}]}`,
		},
		{
			name: "no choices",
			json: `{"choices":[]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var chunk chatChunk
			if err := json.Unmarshal([]byte(tc.json), &chunk); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			text, reasoning := chunk.output()
			if text != tc.wantText {
				t.Errorf("text = %q, want %q", text, tc.wantText)
			}
			if reasoning != tc.wantReasoning {
				t.Errorf("reasoning = %v, want %v", reasoning, tc.wantReasoning)
			}
		})
	}
}
