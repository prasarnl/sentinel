package llmbench

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"sentinel/server/internal/models"
)

// maxErrorSamples caps how many distinct failed-request error messages a
// run's summary carries, so one badly-behaved target can't bloat storage.
const maxErrorSamples = 5

// Target is the minimal connection info the runner needs to reach an LLM
// endpoint. Which model to test is chosen per Run call, not fixed here, so
// one target can benchmark many models.
type Target struct {
	BaseURL string
	APIKey  string
}

// ProgressEvent is emitted after each completed (non-warmup) request so
// callers can stream live status while a run is in flight.
type ProgressEvent struct {
	Completed        int
	Total            int
	Failed           int
	LastTTFTMs       float64
	LastTokensPerSec float64
	LastError        string // set when this particular request failed
}

// maxWarmupAttempts bounds retries of a single warmup request. A
// model-swap proxy (e.g. llama-swap) can give up waiting on a cold backend
// start and close the stream with no content while the backend keeps
// loading in the background — the very next attempt then succeeds in
// seconds because the model is now resident. One clean close is therefore
// not evidence the target is unreachable, only that the first attempt lost
// a race with the proxy's own patience.
const maxWarmupAttempts = 3

// warmupRetryDelay is a brief pause between warmup retries, giving the
// backend a moment to finish the load it started on the previous attempt
// rather than immediately re-triggering another cold start.
const warmupRetryDelay = 2 * time.Second

// Run executes warmup + measured requests against target for the given
// model, distributing measured requests across cfg.Concurrency workers,
// and returns the aggregate summary. progress (may be nil) is called after
// each measured request completes.
//
// Warmup requests use cfg.ModelLoadTimeoutSecs rather than
// cfg.RequestTimeoutSecs: against a swap-capable proxy, the very first
// request to a model can trigger a cold process start + weight load that
// takes far longer than steady-state generation, and that one-time cost
// must not leak into the measured latency stats.
func Run(ctx context.Context, target Target, model string, cfg models.LLMBenchmarkConfig, progress func(ProgressEvent)) (models.BenchmarkSummary, error) {
	prompt := BuildPrompt(cfg.PromptTokens)
	loadClient := &http.Client{Timeout: time.Duration(cfg.ModelLoadTimeoutSecs) * time.Second}
	steadyClient := &http.Client{Timeout: time.Duration(cfg.RequestTimeoutSecs) * time.Second}

	runWith := func(c *http.Client) (RequestResult, error) {
		return streamChatCompletion(ctx, c, target.BaseURL, target.APIKey, model, prompt, cfg.MaxTokens, cfg.ContextWindow, cfg.BatchSize)
	}

	// Warmup requests are discarded (cold start / model load skew) but
	// repeated failure here means the target is genuinely unreachable —
	// fail fast rather than burning through the whole measured run. The
	// phase/index is included so a warmup timeout isn't indistinguishable
	// from a measured-request timeout in the resulting error message.
	for i := 0; i < cfg.WarmupRequests; i++ {
		var err error
		for attempt := 1; attempt <= maxWarmupAttempts; attempt++ {
			_, err = runWith(loadClient)
			if err == nil {
				break
			}
			if attempt < maxWarmupAttempts {
				select {
				case <-ctx.Done():
					return models.BenchmarkSummary{}, ctx.Err()
				case <-time.After(warmupRetryDelay):
				}
			}
		}
		if err != nil {
			return models.BenchmarkSummary{}, fmt.Errorf("warmup request %d/%d failed after %d attempts: %w", i+1, cfg.WarmupRequests, maxWarmupAttempts, err)
		}
	}

	runOne := func() (RequestResult, error) { return runWith(steadyClient) }

	total := cfg.NumRequests
	results := make([]RequestResult, 0, total)
	var (
		mu           sync.Mutex
		completed    int
		failed       int
		firstErr     error
		errorSamples []string
		seenErrors   = map[string]bool{}
	)

	sem := make(chan struct{}, cfg.Concurrency)
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < total; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			res, err := runOne()

			mu.Lock()
			defer mu.Unlock()
			completed++
			evt := ProgressEvent{}
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
				msg := err.Error()
				evt.LastError = msg
				if !seenErrors[msg] && len(errorSamples) < maxErrorSamples {
					seenErrors[msg] = true
					errorSamples = append(errorSamples, msg)
				}
			} else {
				results = append(results, res)
				evt.LastTTFTMs = msFloat(res.TTFT)
				if genSecs := (res.TotalTime - res.TTFT).Seconds(); genSecs > 0 {
					evt.LastTokensPerSec = float64(res.CompletionTokens) / genSecs
				}
			}
			evt.Completed = completed
			evt.Total = total
			evt.Failed = failed
			if progress != nil {
				progress(evt)
			}
		}()
	}
	wg.Wait()
	wallTime := time.Since(start)

	if len(results) == 0 {
		if firstErr == nil {
			firstErr = errors.New("benchmark produced no successful requests")
		}
		return models.BenchmarkSummary{}, firstErr
	}

	return aggregate(results, failed, errorSamples, wallTime), nil
}

func aggregate(results []RequestResult, failed int, errorSamples []string, wallTime time.Duration) models.BenchmarkSummary {
	ttfts := make([]float64, len(results))
	tps := make([]float64, len(results))
	totalTokens := 0
	for i, r := range results {
		ttfts[i] = msFloat(r.TTFT)
		if genSecs := (r.TotalTime - r.TTFT).Seconds(); genSecs > 0 {
			tps[i] = float64(r.CompletionTokens) / genSecs
		}
		totalTokens += r.CompletionTokens
	}

	return models.BenchmarkSummary{
		Requests:               len(results),
		Failed:                 failed,
		Errors:                 errorSamples,
		WallTimeMs:             msFloat(wallTime),
		ThroughputTokensPerSec: float64(totalTokens) / wallTime.Seconds(),
		TTFTMs:                 stats(ttfts),
		TokensPerSec:           stats(tps),
	}
}

// msFloat converts a duration to fractional milliseconds. time.Duration's
// own Milliseconds() truncates to an integer, which rounds fast/local
// requests (sub-millisecond TTFT) down to 0 and loses signal.
func msFloat(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func stats(values []float64) models.LatencyStats {
	if len(values) == 0 {
		return models.LatencyStats{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}

	return models.LatencyStats{
		P50:  percentile(sorted, 0.50),
		P95:  percentile(sorted, 0.95),
		Mean: sum / float64(len(sorted)),
		Min:  sorted[0],
		Max:  sorted[len(sorted)-1],
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := p * float64(len(sorted)-1)
	lo := int(idx)
	hi := lo + 1
	if hi >= len(sorted) {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo] + (sorted[hi]-sorted[lo])*frac
}
