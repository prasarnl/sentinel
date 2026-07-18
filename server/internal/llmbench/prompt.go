package llmbench

import "strings"

// wordsPerToken approximates English tokenization (~0.75 tokens per word)
// so the benchmark can size prompts and estimate completion length without
// needing the target's actual tokenizer.
const wordsPerToken = 0.75

var fillerWords = strings.Fields(
	`the quick brown fox jumps over the lazy dog while the sun sets slowly ` +
		`behind the distant mountains casting long shadows across the quiet ` +
		`valley where a small river winds its way through fields of wildflowers ` +
		`and scattered trees that sway gently in the evening breeze as birds ` +
		`return to their nests and the first stars begin to appear in the ` +
		`darkening sky above the peaceful countryside while travelers stop to ` +
		`rest beside the water and share stories about the long roads they ` +
		`have traveled and the strange sights they have seen along the way`,
)

// BuildPrompt returns deterministic filler text sized to approximate
// promptTokens tokens, for use as the benchmark's input prompt.
func BuildPrompt(promptTokens int) string {
	if promptTokens <= 0 {
		promptTokens = 1
	}
	wantWords := int(float64(promptTokens) * wordsPerToken)
	if wantWords < 1 {
		wantWords = 1
	}
	words := make([]string, 0, wantWords)
	for len(words) < wantWords {
		words = append(words, fillerWords...)
	}
	return strings.Join(words[:wantWords], " ") + "."
}

// EstimateTokens approximates a completion token count from generated text
// when the target doesn't report usage stats in its stream.
func EstimateTokens(text string) int {
	words := len(strings.Fields(text))
	if words == 0 {
		return 0
	}
	tokens := int(float64(words) / wordsPerToken)
	if tokens < 1 {
		tokens = 1
	}
	return tokens
}
