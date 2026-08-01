package api

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"sentinel/llmscrape"
	"sentinel/server/internal/models"
	"sentinel/server/internal/ws"
)

const (
	// pollInterval matches the agent's default collection cadence, so remote
	// endpoints and agent-scraped ones produce comparable series.
	pollInterval = 10 * time.Second

	// Remote endpoints are reached over the network rather than loopback, so
	// they get a more forgiving timeout than the agent uses.
	remoteScrapeTimeout = 5 * time.Second

	// maxConcurrentPolls keeps one slow endpoint from delaying the others
	// while bounding outbound connections.
	maxConcurrentPolls = 4
)

// PollRemoteEndpoints scrapes registered endpoints that no agent can reach —
// those with a NULL host_id — and stores the results like any other sample.
//
// Runs for the lifetime of the process, mirroring markStaleHostsOffline.
func PollRemoteEndpoints(ctx context.Context, pool *pgxpool.Pool, hub *ws.Hub) {
	// One scraper for the whole loop, since rate derivation depends on
	// retaining the previous scrape's counters per endpoint.
	scraper := llmscrape.NewScraper(remoteScrapeTimeout)
	var mu sync.Mutex // guards scraper, which is not safe for concurrent use

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			endpoints, err := remoteEndpoints(ctx, pool)
			if err != nil {
				log.Printf("poll: list remote endpoints: %v", err)
				continue
			}
			pollAll(ctx, pool, hub, scraper, &mu, endpoints)
		}
	}
}

type remoteEndpoint struct {
	id      string
	url     string
	runtime string
	apiKey  string
}

func remoteEndpoints(ctx context.Context, pool *pgxpool.Pool) ([]remoteEndpoint, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, url, runtime, COALESCE(api_key, '')
		FROM llm_endpoints
		WHERE host_id IS NULL AND enabled = true`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []remoteEndpoint
	for rows.Next() {
		var e remoteEndpoint
		if err := rows.Scan(&e.id, &e.url, &e.runtime, &e.apiKey); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func pollAll(ctx context.Context, pool *pgxpool.Pool, hub *ws.Hub, scraper *llmscrape.Scraper, mu *sync.Mutex, endpoints []remoteEndpoint) {
	sem := make(chan struct{}, maxConcurrentPolls)
	var wg sync.WaitGroup

	for _, ep := range endpoints {
		wg.Add(1)
		go func(ep remoteEndpoint) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			mu.Lock()
			sample, err := scraper.Scrape(ctx, llmscrape.Endpoint{
				URL: ep.url, Runtime: ep.runtime, APIKey: ep.apiKey,
			}, time.Now().UTC())
			mu.Unlock()

			if err != nil {
				recordScrapeError(ctx, pool, ep.id, err)
				return
			}
			storeRemoteSample(ctx, pool, hub, ep.id, sample)
		}(ep)
	}
	wg.Wait()
}

// recordScrapeError stores why an endpoint produced nothing. The message is
// kept verbatim so the UI can tell unreachable apart from "answers but
// publishes no metrics" — LM Studio's case, which is a permanent property of
// that runtime rather than an outage, and shouldn't read like one.
func recordScrapeError(ctx context.Context, pool *pgxpool.Pool, endpointID string, scrapeErr error) {
	_, err := pool.Exec(ctx,
		`UPDATE llm_endpoints SET last_scrape_error = $2, last_scrape_at = now() WHERE id = $1`,
		endpointID, scrapeErr.Error())
	if err != nil {
		log.Printf("poll: record scrape error for %s: %v", endpointID, err)
	}
}

// storeRemoteSample writes a sample with a NULL host_id, since a remote
// endpoint belongs to no monitored host.
func storeRemoteSample(ctx context.Context, pool *pgxpool.Pool, hub *ws.Hub, endpointID string, s models.LLMSample) {
	_, err := pool.Exec(ctx, `
		INSERT INTO metrics_llm (
			time, host_id, endpoint_id, endpoint, runtime, model,
			kv_cache_usage_ratio, kv_cache_tokens,
			prompt_tokens_total, generated_tokens_total,
			prompt_tokens_per_sec, generated_tokens_per_sec,
			prefix_cache_queries_total, prefix_cache_hits_total, prefix_cache_hit_ratio,
			requests_running, requests_waiting,
			ttft_ms_avg, tpot_ms_avg, preemptions_per_sec,
			spec_decode_acceptance_rate, spec_decode_accepted_per_draft
		) VALUES ($1,NULL,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
		s.Time, endpointID, s.Endpoint, s.Runtime, nullIfEmpty(s.Model),
		s.KVCacheUsageRatio, s.KVCacheTokens,
		s.PromptTokensTotal, s.GeneratedTokensTotal,
		s.PromptTokensPerSec, s.GeneratedTokensPerSec,
		s.PrefixCacheQueriesTotal, s.PrefixCacheHitsTotal, s.PrefixCacheHitRatio,
		s.RequestsRunning, s.RequestsWaiting,
		s.TTFTMsAvg, s.TPOTMsAvg, s.PreemptionsPerSec,
		s.SpecDecodeAcceptanceRate, s.SpecDecodeAcceptedPerDraft)
	if err != nil {
		log.Printf("poll: store sample for %s: %v", endpointID, err)
		return
	}

	if _, err := pool.Exec(ctx,
		`UPDATE llm_endpoints SET last_scrape_at = $2, last_scrape_error = NULL WHERE id = $1`,
		endpointID, s.Time); err != nil {
		log.Printf("poll: clear scrape error for %s: %v", endpointID, err)
	}

	// Published under the endpoint id, since there is no host to key on.
	if msg, err := json.Marshal(map[string]any{"endpoint_id": endpointID, "sample": s}); err == nil {
		hub.Publish(endpointID, msg)
	}
}
