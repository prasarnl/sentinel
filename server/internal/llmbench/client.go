package llmbench

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ErrModelNotRunning is returned by UnloadModel when the target reports the
// model isn't currently loaded (a 404) — this means there's nothing to do,
// not that the unload failed, so callers should treat it as a no-op.
var ErrModelNotRunning = errors.New("model is not currently running")

type chatRequest struct {
	Model      string         `json:"model"`
	Messages   []chatMessage  `json:"messages"`
	MaxTokens  int            `json:"max_tokens"`
	Stream     bool           `json:"stream"`
	StreamOpts *streamOptions `json:"stream_options,omitempty"`
	// NCtx/NBatch are non-standard, best-effort extras some llama.cpp-family
	// servers accept in the chat completion body; omitted entirely when nil
	// rather than sent as 0, since 0 isn't a meaningful override.
	NCtx   *int `json:"n_ctx,omitempty"`
	NBatch *int `json:"n_batch,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// RequestResult is the timing/token outcome of a single streamed chat
// completion request.
type RequestResult struct {
	TTFT             time.Duration
	TotalTime        time.Duration
	CompletionTokens int
}

type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// ListModels queries an OpenAI-compatible endpoint's model listing
// (GET {baseURL}/v1/models) and returns the available model IDs, sorted.
func ListModels(ctx context.Context, httpClient *http.Client, baseURL, apiKey string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/v1/models"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("target returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var parsed modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("failed to parse model list: %w", err)
	}

	models := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	sort.Strings(models)
	return models, nil
}

// UnloadAllModels asks a model-swap-capable proxy (e.g. llama-swap) to stop
// every currently running backend, freeing all VRAM it holds.
func UnloadAllModels(ctx context.Context, httpClient *http.Client, baseURL, apiKey string) error {
	url := strings.TrimRight(baseURL, "/") + "/api/models/unload"
	return doUnloadRequest(ctx, httpClient, url, apiKey)
}

// UnloadModel asks a model-swap-capable proxy to stop one specific backend.
// Returns ErrModelNotRunning (not a real failure) if the target reports the
// model isn't currently loaded.
func UnloadModel(ctx context.Context, httpClient *http.Client, baseURL, apiKey, model string) error {
	url := strings.TrimRight(baseURL, "/") + "/api/models/unload/" + model
	return doUnloadRequest(ctx, httpClient, url, apiKey)
}

func doUnloadRequest(ctx context.Context, httpClient *http.Client, url, apiKey string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrModelNotRunning
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("unload returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// streamChatCompletion sends a single streaming chat completion request to
// an OpenAI-compatible endpoint (POST {baseURL}/v1/chat/completions) and
// measures time-to-first-token and total generation time by reading the
// SSE stream as it arrives.
func streamChatCompletion(ctx context.Context, httpClient *http.Client, baseURL, apiKey, model, prompt string, maxTokens int, contextWindow, batchSize *int) (RequestResult, error) {
	reqBody := chatRequest{
		Model:      model,
		Messages:   []chatMessage{{Role: "user", Content: prompt}},
		MaxTokens:  maxTokens,
		Stream:     true,
		StreamOpts: &streamOptions{IncludeUsage: true},
		NCtx:       contextWindow,
		NBatch:     batchSize,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return RequestResult{}, err
	}

	url := strings.TrimRight(baseURL, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return RequestResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	start := time.Now()
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return RequestResult{}, fmt.Errorf(
			"connecting to target failed after %s (client timeout %s): %w",
			fmtElapsed(time.Since(start)), httpClient.Timeout, err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return RequestResult{}, fmt.Errorf("target returned %d after %s: %s", resp.StatusCode, fmtElapsed(time.Since(start)), strings.TrimSpace(string(msg)))
	}

	var (
		ttft        time.Duration
		gotFirst    bool
		content     strings.Builder
		usageTokens int
		haveUsage   bool
		rawSample   strings.Builder // verbatim lines, for diagnosing unrecognized response shapes
	)

	// 4MB per line: some backends (e.g. llama.cpp/llama-swap) can emit a
	// large final chunk carrying full timing/logprobs data, and the
	// default 64KB scanner limit would otherwise fail the whole request
	// over an oversized-but-harmless trailing line.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && rawSample.Len() < rawSampleCap {
			rawSample.WriteString(line)
			rawSample.WriteByte('\n')
		}
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk chatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip malformed/keep-alive frames
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			if !gotFirst {
				ttft = time.Since(start)
				gotFirst = true
			}
			content.WriteString(chunk.Choices[0].Delta.Content)
		}
		if chunk.Usage != nil {
			usageTokens = chunk.Usage.CompletionTokens
			haveUsage = true
		}
	}
	// A stream that ends here without having reached "[DONE]" — via a
	// scanner error (including a client-side timeout mid-stream) — is the
	// case that most needs diagnosing: the model may well have already
	// produced output before the connection died, so report exactly how
	// much progress had been made rather than just the bare transport
	// error, which otherwise looks identical to "never connected at all".
	if err := scanner.Err(); err != nil {
		return RequestResult{}, fmt.Errorf(
			"stream interrupted after %s (received_first_token=%v, content_chars=%d, client timeout %s, raw_sample=%q): %w",
			fmtElapsed(time.Since(start)), gotFirst, content.Len(), httpClient.Timeout, truncate(rawSample.String(), rawSampleCap), err,
		)
	}
	if !gotFirst {
		return RequestResult{}, fmt.Errorf(
			"target closed the stream after %s without sending any content (client timeout %s) — the response ended before \"[DONE]\" and before any token was received; raw response sample: %q",
			fmtElapsed(time.Since(start)), httpClient.Timeout, truncate(rawSample.String(), rawSampleCap),
		)
	}

	total := time.Since(start)
	tokens := usageTokens
	if !haveUsage {
		tokens = EstimateTokens(content.String())
	}

	return RequestResult{TTFT: ttft, TotalTime: total, CompletionTokens: tokens}, nil
}

func fmtElapsed(d time.Duration) string {
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// rawSampleCap bounds how much raw stream text gets embedded in a failure
// error, so it's enough to diagnose an unrecognized response shape (e.g. a
// backend that doesn't emit the OpenAI delta.content format we expect)
// without unbounded growth of stored error text.
const rawSampleCap = 2000

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}
