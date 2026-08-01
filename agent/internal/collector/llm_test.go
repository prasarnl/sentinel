package collector

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"sentinel/agent/internal/config"
)

const vllmMetricsFixture = `
vllm:num_requests_running{model_name="qwen35B"} 2.0
vllm:kv_cache_usage_perc{model_name="qwen35B"} 0.63
vllm:generation_tokens_total{model_name="qwen35B"} 900.0
`

func boolPtr(b bool) *bool { return &b }

// newMetricsServer serves a vLLM-shaped /metrics on loopback, standing in for
// a real inference server.
func newMetricsServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			fmt.Fprint(w, vllmMetricsFixture)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestConfiguredEndpointIsScraped(t *testing.T) {
	srv := newMetricsServer(t)
	c := New(config.LLM{
		Autodetect: boolPtr(false),
		Endpoints:  []config.LLMEndpoint{{URL: srv.URL}},
	})

	samples := c.collectLLM(time.Now())
	if len(samples) != 1 {
		t.Fatalf("got %d samples, want 1", len(samples))
	}
	if samples[0].Model != "qwen35B" {
		t.Errorf("model = %q, want qwen35B", samples[0].Model)
	}
}

// A configured endpoint is the operator's explicit instruction, so a failure
// must not silently drop it — otherwise a restart of the inference server
// would permanently stop monitoring until the agent restarted.
func TestConfiguredEndpointSurvivesFailure(t *testing.T) {
	c := New(config.LLM{
		Autodetect: boolPtr(false),
		Endpoints:  []config.LLMEndpoint{{URL: "http://127.0.0.1:1"}},
	})

	if samples := c.collectLLM(time.Now()); len(samples) != 0 {
		t.Fatalf("got %d samples from a dead endpoint, want 0", len(samples))
	}

	c.llm.mu.Lock()
	_, stillTracked := c.llm.endpoints["http://127.0.0.1:1"]
	c.llm.mu.Unlock()
	if !stillTracked {
		t.Error("configured endpoint was dropped after a failed scrape; it should be retried")
	}
}

// A discovered endpoint that stops answering may have moved or shut down, so
// it is forgotten and left to rediscovery rather than retried forever.
func TestDiscoveredEndpointIsForgottenOnFailure(t *testing.T) {
	c := New(config.LLM{Autodetect: boolPtr(false)})

	dead := "http://127.0.0.1:1"
	c.llm.endpoints[dead] = llmEndpointFor(dead)

	c.collectLLM(time.Now())

	c.llm.mu.Lock()
	_, stillTracked := c.llm.endpoints[dead]
	c.llm.mu.Unlock()
	if stillTracked {
		t.Error("discovered endpoint kept after failing; it should be forgotten for rediscovery")
	}
}

// Discovery must not re-probe a port that already answered as something
// other than an inference runtime. Without the negative cache, every
// unrelated service on the host is hit on every cycle.
func TestNegativeCacheSuppressesReprobing(t *testing.T) {
	c := New(config.LLM{})
	now := time.Now()

	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "go_goroutines 12\n")
	}))
	defer other.Close()
	port := mustPort(t, other.URL)
	target := fmt.Sprintf("http://127.0.0.1:%s", port)

	// Simulate discovery having already rejected it.
	c.llm.negative[target] = now

	for _, cand := range c.probeCandidates(now.Add(time.Minute)) {
		if cand == target {
			t.Fatal("a recently rejected port was offered for probing again")
		}
	}

	// Once the TTL lapses it becomes a candidate again, so a port that later
	// starts serving an inference runtime is still picked up.
	found := false
	for _, cand := range c.probeCandidates(now.Add(negativeTTL + time.Minute)) {
		if cand == target {
			found = true
		}
	}
	if !found {
		t.Error("port never re-probed after the negative TTL lapsed")
	}
}

func TestProbeCandidatesSkipsKnownEndpoints(t *testing.T) {
	srv := newMetricsServer(t)
	c := New(config.LLM{})

	target := fmt.Sprintf("http://127.0.0.1:%s", mustPort(t, srv.URL))
	c.llm.endpoints[target] = llmEndpointFor(target)

	for _, cand := range c.probeCandidates(time.Now()) {
		if cand == target {
			t.Error("an already-tracked endpoint was offered for probing")
		}
	}
}

// The point of socket enumeration: a server on an arbitrary port is found
// with no configuration naming it.
func TestDiscoveryFindsServerOnArbitraryPort(t *testing.T) {
	srv := newMetricsServer(t)
	target := fmt.Sprintf("http://127.0.0.1:%s", mustPort(t, srv.URL))

	c := New(config.LLM{})
	now := time.Now()

	// Socket enumeration needs OS support and, on some platforms,
	// privileges. If the listener isn't visible there is nothing to assert.
	var visible bool
	for _, cand := range c.probeCandidates(now) {
		if cand == target {
			visible = true
		}
	}
	if !visible {
		t.Skip("listening sockets not enumerable in this environment")
	}

	c.discover(now)

	c.llm.mu.Lock()
	ep, found := c.llm.endpoints[target]
	c.llm.mu.Unlock()
	if !found {
		t.Fatalf("discovery did not pick up %s", target)
	}
	if ep.Runtime != "vllm" {
		t.Errorf("runtime = %q, want vllm", ep.Runtime)
	}
}

func TestAutodetectDisabledSkipsDiscovery(t *testing.T) {
	c := New(config.LLM{Autodetect: boolPtr(false)})
	c.maybeDiscover(time.Now())

	c.llm.mu.Lock()
	defer c.llm.mu.Unlock()
	if !c.llm.lastDiscovery.IsZero() {
		t.Error("discovery ran despite autodetect being disabled")
	}
}

func TestListensLocally(t *testing.T) {
	local := []string{"127.0.0.1", "::1", "0.0.0.0", "::", ""}
	for _, ip := range local {
		if !listensLocally(ip) {
			t.Errorf("listensLocally(%q) = false, want true", ip)
		}
	}
	// A socket bound only to a specific external interface isn't reachable
	// over loopback, so probing 127.0.0.1 for it would be pointless.
	if listensLocally("192.168.1.10") {
		t.Error("listensLocally(192.168.1.10) = true, want false")
	}
}

func mustPort(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	return u.Port()
}
