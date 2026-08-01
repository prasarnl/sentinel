package llmscrape

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Scrape failures are typed so a caller can tell the cases apart and report
// them honestly. All three otherwise present identically — as an endpoint
// with no data — which makes a real outage indistinguishable from a runtime
// that simply doesn't publish metrics.
var (
	// ErrUnreachable: nothing answered (connection refused, DNS, timeout).
	ErrUnreachable = errors.New("endpoint unreachable")
	// ErrNoMetrics: the host answered but served no /metrics. Expected for
	// runtimes like LM Studio that expose only an OpenAI-compatible API.
	ErrNoMetrics = errors.New("no prometheus metrics endpoint")
	// ErrUnknownRuntime: /metrics exists but is not a runtime we understand,
	// e.g. a node_exporter or an unrelated service on that port.
	ErrUnknownRuntime = errors.New("not a recognized llm runtime")
)

const defaultTimeout = 3 * time.Second

// maxMetricsBytes caps a response body. vLLM's /metrics grows with histogram
// buckets and label cardinality, but a well-behaved endpoint stays far under
// this; the cap exists so a misbehaving one can't exhaust memory.
const maxMetricsBytes = 8 << 20

// Scraper fetches and normalizes metrics from inference endpoints, retaining
// the previous scrape's counters per endpoint so rates can be derived.
//
// Not safe for concurrent use on the same endpoint; callers scraping in
// parallel should use one Scraper per goroutine or serialize access.
type Scraper struct {
	client *http.Client
	prev   map[string]counterState
	models map[string]cachedModel
}

type cachedModel struct {
	name string
	at   time.Time
}

// modelCacheTTL bounds how long a model name is reused. It has to expire:
// llama-swap changes the model behind a stable endpoint, so caching forever
// would keep reporting a model that is no longer loaded.
const modelCacheTTL = 60 * time.Second

func NewScraper(timeout time.Duration) *Scraper {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Scraper{
		client: &http.Client{Timeout: timeout},
		prev:   make(map[string]counterState),
		models: make(map[string]cachedModel),
	}
}

// Scrape fetches one endpoint and returns a normalized sample. Rate fields
// are nil on the first successful scrape of an endpoint, since there is no
// previous reading to diff against.
func (s *Scraper) Scrape(ctx context.Context, ep Endpoint, now time.Time) (Sample, error) {
	url := strings.TrimSuffix(ep.URL, "/")

	body, err := s.get(ctx, url+"/metrics", ep.APIKey)
	if err != nil {
		return Sample{}, err
	}

	metrics := parseProm(body)
	if len(metrics) == 0 {
		// Answered, but not in the exposition format at all. LM Studio does
		// exactly this: GET /metrics returns 200 with a JSON error body
		// rather than a 404, so status alone can't be trusted to tell a
		// metrics endpoint from a runtime that has none.
		return Sample{}, ErrNoMetrics
	}
	runtime := detectRuntime(metrics)
	if runtime == "" {
		return Sample{}, ErrUnknownRuntime
	}
	// An explicitly configured runtime wins, so a caller can force the
	// mapping for a backend whose metric prefix we don't recognize.
	if ep.Runtime != "" && ep.Runtime != RuntimeAuto {
		runtime = ep.Runtime
	}

	sample := Sample{Time: now, Endpoint: url, Runtime: runtime}

	var ctr counterState
	ctr.at = now
	switch runtime {
	case RuntimeVLLM:
		mapVLLM(metrics, &sample, &ctr)
	case RuntimeLlamaCpp:
		mapLlamaCpp(metrics, &sample, &ctr)
	default:
		return Sample{}, fmt.Errorf("%w: %s", ErrUnknownRuntime, runtime)
	}

	sample.Model = s.modelName(ctx, url, metrics, runtime, ep.APIKey, now)

	if prev, ok := s.prev[url]; ok {
		applyRates(&sample, prev, ctr)
	}
	s.prev[url] = ctr

	return sample, nil
}

// Probe reports whether an endpoint serves metrics for a runtime we
// understand, without retaining any counter state. Used by discovery to test
// candidate ports.
func (s *Scraper) Probe(ctx context.Context, url string, apiKey string) (runtime string, err error) {
	body, err := s.get(ctx, strings.TrimSuffix(url, "/")+"/metrics", apiKey)
	if err != nil {
		return "", err
	}
	metrics := parseProm(body)
	if len(metrics) == 0 {
		return "", ErrNoMetrics
	}
	if rt := detectRuntime(metrics); rt != "" {
		return rt, nil
	}
	return "", ErrUnknownRuntime
}

// Forget drops retained state for an endpoint, so a later scrape starts fresh
// rather than diffing against a stale reading across a long gap.
func (s *Scraper) Forget(url string) {
	url = strings.TrimSuffix(url, "/")
	delete(s.prev, url)
	delete(s.models, url)
}

func detectRuntime(m promMetrics) string {
	switch {
	case m.hasPrefix("vllm:"):
		return RuntimeVLLM
	case m.hasPrefix("llamacpp:"):
		return RuntimeLlamaCpp
	default:
		return ""
	}
}

// modelName resolves which model an endpoint serves. vLLM labels its metrics
// with it, so no extra request is needed — which also avoids /v1/models,
// the one path vLLM's auth middleware guards. llama.cpp carries no such
// label, so its name comes from /v1/models, re-fetched periodically because
// llama-swap swaps models behind a stable endpoint.
func (s *Scraper) modelName(ctx context.Context, url string, m promMetrics, runtime, apiKey string, now time.Time) string {
	if runtime == RuntimeVLLM {
		names := append(append([]string{}, vllmRunningNames...), vllmKVCacheNames...)
		if model := m.firstLabel("model_name", names...); model != "" {
			return model
		}
	}

	if cached, ok := s.models[url]; ok && now.Sub(cached.at) < modelCacheTTL {
		return cached.name
	}
	name := s.fetchModelName(ctx, url, apiKey)
	s.models[url] = cachedModel{name: name, at: now}
	return name
}

func (s *Scraper) fetchModelName(ctx context.Context, url, apiKey string) string {
	body, err := s.get(ctx, url+"/v1/models", apiKey)
	if err != nil {
		return ""
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || len(out.Data) == 0 {
		return ""
	}
	return out.Data[0].ID
}

func (s *Scraper) get(ctx context.Context, url, apiKey string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()

	// A 404 means something is listening and answering HTTP but publishes no
	// metrics — a different situation from nothing being there at all.
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrNoMetrics
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: http %d", ErrNoMetrics, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetricsBytes))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return string(body), nil
}
