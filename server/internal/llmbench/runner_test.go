package llmbench

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"sentinel/server/internal/models"
)

// sseChunk writes one OpenAI-style streamed chat-completion chunk followed
// by [DONE], mimicking a target that produced real output.
func writeSuccessfulStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
	w.Write([]byte("data: {\"usage\":{\"completion_tokens\":1}}\n\n"))
	w.Write([]byte("data: [DONE]\n\n"))
}

// writeEmptyClosedStream sends a 200 OK with no SSE data at all, matching
// what a model-swap proxy does when it gives up waiting on a cold backend
// start: headers arrive, then the connection is closed with zero content.
func writeEmptyClosedStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
}

func testConfig() models.LLMBenchmarkConfig {
	return models.LLMBenchmarkConfig{
		Concurrency:          1,
		NumRequests:          1,
		WarmupRequests:       1,
		PromptTokens:         4,
		MaxTokens:            4,
		RequestTimeoutSecs:   5,
		ModelLoadTimeoutSecs: 5,
	}
}

// TestRun_WarmupRetriesAfterColdStartClose reproduces the "target closed
// the stream ... without sending any content" failure: a proxy that closes
// the very first warmup attempt empty (cold backend start losing the race
// against the proxy's own patience) before the backend becomes ready.
// Run must retry rather than aborting the whole benchmark on that single
// clean close.
func TestRun_WarmupRetriesAfterColdStartClose(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			writeEmptyClosedStream(w)
			return
		}
		writeSuccessfulStream(w)
	}))
	defer srv.Close()

	target := Target{BaseURL: srv.URL}
	_, err := Run(context.Background(), target, "test-model", testConfig(), nil)
	if err != nil {
		t.Fatalf("Run failed despite the second attempt succeeding: %v", err)
	}
	if got := calls.Load(); got < 2 {
		t.Fatalf("expected at least 2 requests (failed warmup + retry), got %d", got)
	}
}

// TestRun_WarmupFailsAfterExhaustingRetries ensures a target that never
// produces content still fails the run — the retry exists to ride out a
// transient cold-start hiccup, not to mask a genuinely unreachable target.
func TestRun_WarmupFailsAfterExhaustingRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeEmptyClosedStream(w)
	}))
	defer srv.Close()

	target := Target{BaseURL: srv.URL}
	_, err := Run(context.Background(), target, "test-model", testConfig(), nil)
	if err == nil {
		t.Fatal("expected Run to fail when every warmup attempt closes empty")
	}
	if !strings.Contains(err.Error(), "warmup request 1/1 failed after") {
		t.Fatalf("expected error to report exhausted attempts, got: %v", err)
	}
	if got := calls.Load(); int(got) != maxWarmupAttempts {
		t.Fatalf("expected exactly %d attempts, got %d", maxWarmupAttempts, got)
	}
}
