package collector

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	gopsnet "github.com/shirou/gopsutil/v3/net"

	"sentinel/agent/internal/models"
	"sentinel/llmscrape"
)

const (
	// discoveryInterval is how often listening ports are re-examined for new
	// inference servers.
	discoveryInterval = 60 * time.Second

	// negativeTTL is how long a port that answered but wasn't an inference
	// runtime is left alone. Without this, every unrelated service on the box
	// would be re-probed each cycle — a real cost on a host with dozens of
	// listeners, and pointless traffic aimed at services that never asked
	// for it.
	negativeTTL = 10 * time.Minute

	// Probes target loopback and should answer immediately, so they get a
	// much tighter budget than a real scrape.
	probeTimeout  = 1 * time.Second
	scrapeTimeout = 3 * time.Second

	// maxConcurrentProbes bounds a discovery sweep. A host can have many
	// listening ports and probing them serially would take longer than the
	// collection interval.
	maxConcurrentProbes = 8
)

// llmState holds everything discovery and scraping share. Discovery runs off
// the collection goroutine so a slow sweep never delays the tick that reports
// CPU/memory/disk/GPU, so access is guarded.
type llmState struct {
	mu sync.Mutex

	// endpoints currently believed to serve inference metrics, keyed by URL.
	endpoints map[string]llmscrape.Endpoint
	// negative records URLs probed and rejected, with when, for the TTL.
	negative map[string]time.Time

	lastDiscovery time.Time
	discovering   bool
}

// collectLLM scrapes every known inference endpoint and returns normalized
// samples. Like GPU collection it is entirely best-effort: an endpoint that
// is down, unreachable, or serving something unrecognized contributes no
// sample rather than failing the tick.
func (c *Collector) collectLLM(now time.Time) []models.LLMSample {
	c.seedConfiguredEndpoints()
	c.maybeDiscover(now)

	c.llm.mu.Lock()
	endpoints := make([]llmscrape.Endpoint, 0, len(c.llm.endpoints))
	for _, ep := range c.llm.endpoints {
		endpoints = append(endpoints, ep)
	}
	c.llm.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout*time.Duration(len(endpoints)+1))
	defer cancel()

	var samples []models.LLMSample
	for _, ep := range endpoints {
		sample, err := c.llmScraper.Scrape(ctx, ep, now)
		if err != nil {
			// A discovered endpoint that stopped answering may have moved or
			// shut down, so forget it and let discovery re-evaluate.
			// Explicitly configured ones are kept: the operator asked for
			// them, and a restart shouldn't silently drop them.
			if !c.isConfigured(ep.URL) {
				c.forgetEndpoint(ep.URL)
			}
			continue
		}
		samples = append(samples, sample)
	}
	return samples
}

// seedConfiguredEndpoints makes sure every endpoint from config.yaml is
// active, regardless of what discovery finds.
func (c *Collector) seedConfiguredEndpoints() {
	c.llm.mu.Lock()
	defer c.llm.mu.Unlock()

	for _, ep := range c.llmConfig.Endpoints {
		url := strings.TrimSuffix(strings.TrimSpace(ep.URL), "/")
		if url == "" {
			continue
		}
		if _, exists := c.llm.endpoints[url]; !exists {
			c.llm.endpoints[url] = llmscrape.Endpoint{
				URL:     url,
				Runtime: ep.Runtime,
				APIKey:  ep.APIKey,
			}
		}
	}
}

func (c *Collector) isConfigured(url string) bool {
	for _, ep := range c.llmConfig.Endpoints {
		if strings.TrimSuffix(strings.TrimSpace(ep.URL), "/") == url {
			return true
		}
	}
	return false
}

func (c *Collector) forgetEndpoint(url string) {
	c.llm.mu.Lock()
	delete(c.llm.endpoints, url)
	c.llm.mu.Unlock()
	c.llmScraper.Forget(url)
}

// maybeDiscover kicks off a discovery sweep in the background when one is
// due. It returns immediately; newly found endpoints are picked up by a later
// tick rather than this one.
func (c *Collector) maybeDiscover(now time.Time) {
	if !c.llmConfig.AutodetectEnabled() {
		return
	}

	c.llm.mu.Lock()
	due := c.llm.lastDiscovery.IsZero() || now.Sub(c.llm.lastDiscovery) >= discoveryInterval
	if !due || c.llm.discovering {
		c.llm.mu.Unlock()
		return
	}
	c.llm.discovering = true
	c.llm.lastDiscovery = now
	c.llm.mu.Unlock()

	go func() {
		defer func() {
			c.llm.mu.Lock()
			c.llm.discovering = false
			c.llm.mu.Unlock()
		}()
		c.discover(now)
	}()
}

// discover probes local listening ports for an inference runtime. Enumerating
// what is actually listening, rather than guessing a handful of well-known
// ports, is what lets a newly started server on an arbitrary port be picked
// up without any configuration.
func (c *Collector) discover(now time.Time) {
	candidates := c.probeCandidates(now)
	if len(candidates) == 0 {
		return
	}

	sem := make(chan struct{}, maxConcurrentProbes)
	var wg sync.WaitGroup

	for _, url := range candidates {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
			defer cancel()

			runtime, err := c.llmProbe.Probe(ctx, url, "")
			c.llm.mu.Lock()
			defer c.llm.mu.Unlock()
			if err != nil {
				c.llm.negative[url] = now
				return
			}
			delete(c.llm.negative, url)
			c.llm.endpoints[url] = llmscrape.Endpoint{URL: url, Runtime: runtime}
		}(url)
	}
	wg.Wait()
}

// probeCandidates returns loopback-reachable URLs worth probing: every
// distinct listening TCP port, minus those already known and those rejected
// recently enough to still be within the negative TTL.
func (c *Collector) probeCandidates(now time.Time) []string {
	conns, err := gopsnet.Connections("tcp")
	if err != nil {
		return nil
	}

	c.llm.mu.Lock()
	defer c.llm.mu.Unlock()

	seen := map[uint32]bool{}
	var candidates []string
	for _, conn := range conns {
		if conn.Status != "LISTEN" || conn.Laddr.Port == 0 {
			continue
		}
		if !listensLocally(conn.Laddr.IP) {
			continue
		}
		if seen[conn.Laddr.Port] {
			continue
		}
		seen[conn.Laddr.Port] = true

		url := fmt.Sprintf("http://127.0.0.1:%d", conn.Laddr.Port)
		if _, active := c.llm.endpoints[url]; active {
			continue
		}
		if rejectedAt, ok := c.llm.negative[url]; ok && now.Sub(rejectedAt) < negativeTTL {
			continue
		}
		candidates = append(candidates, url)
	}
	return candidates
}

// listensLocally reports whether a listening socket is reachable over
// loopback: either bound to loopback directly or to all interfaces.
func listensLocally(ip string) bool {
	switch ip {
	case "127.0.0.1", "::1", "0.0.0.0", "::", "":
		return true
	default:
		return false
	}
}

// llmEndpointFor builds a discovered-endpoint entry with runtime detection
// left to the scraper.
func llmEndpointFor(url string) llmscrape.Endpoint {
	return llmscrape.Endpoint{URL: url, Runtime: llmscrape.RuntimeAuto}
}
