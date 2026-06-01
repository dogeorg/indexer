package web

import (
	"context"
	"log"
	"sync"
	"time"
)

const syncHeightsRefreshInterval = 30 * time.Second
const syncHeightsStaleAfter = 2 * time.Minute

// coreRequestClient is the generic JSON-RPC shape exposed by dogewalker's Core client.
type coreRequestClient interface {
	Request(ctx context.Context, method string, params []any, result any) (int, error)
}

type syncHeightSnapshot struct {
	BlocksHeight  *int64
	HeadersHeight *int64
	UpdatedAt     *time.Time
}

type syncHeightCache struct {
	client          coreRequestClient
	refreshInterval time.Duration
	staleAfter      time.Duration
	now             func() time.Time

	mu            sync.RWMutex
	blocksHeight  int64
	headersHeight int64
	updatedAt     time.Time
	hasData       bool
}

func newSyncHeightCache(client coreRequestClient) *syncHeightCache {
	if client == nil {
		return nil
	}
	return &syncHeightCache{
		client:          client,
		refreshInterval: syncHeightsRefreshInterval,
		staleAfter:      syncHeightsStaleAfter,
		now:             time.Now,
	}
}

func (c *syncHeightCache) run(ctx context.Context) {
	if c == nil {
		return
	}

	c.refresh(ctx)

	ticker := time.NewTicker(c.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.refresh(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (c *syncHeightCache) snapshot() syncHeightSnapshot {
	if c == nil {
		return syncHeightSnapshot{}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.hasData {
		return syncHeightSnapshot{}
	}
	if c.staleAfter > 0 && c.now().Sub(c.updatedAt) > c.staleAfter {
		return syncHeightSnapshot{}
	}

	blocksHeight := c.blocksHeight
	headersHeight := c.headersHeight
	updatedAt := c.updatedAt.UTC()

	return syncHeightSnapshot{
		BlocksHeight:  &blocksHeight,
		HeadersHeight: &headersHeight,
		UpdatedAt:     &updatedAt,
	}
}

func (c *syncHeightCache) refresh(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var result struct {
		Blocks  int64 `json:"blocks"`
		Headers int64 `json:"headers"`
	}
	if _, err := c.client.Request(ctx, "getblockchaininfo", []any{}, &result); err != nil {
		log.Printf("[Indexer] sync heights refresh failed: getblockchaininfo: %v", err)
		return
	}

	c.mu.Lock()
	c.blocksHeight = result.Blocks
	c.headersHeight = result.Headers
	c.updatedAt = c.now()
	c.hasData = true
	c.mu.Unlock()
}
