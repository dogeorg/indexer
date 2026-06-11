package web

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/dogeorg/indexer/spec"
)

const syncHeightsRefreshInterval = 30 * time.Second

type syncHeightSnapshot struct {
	CoreBlocksHeight  *int64
	CoreHeadersHeight *int64
	CoreSyncUpdatedAt *time.Time
}

type syncHeightCache struct {
	client          spec.CoreRequestClient
	refreshInterval time.Duration
	now             func() time.Time

	mu                sync.RWMutex
	coreBlocksHeight  int64
	coreHeadersHeight int64
	updatedAt         time.Time
	hasData           bool
}

func newSyncHeightCache(client spec.CoreRequestClient) *syncHeightCache {
	if client == nil {
		return nil
	}
	return &syncHeightCache{
		client:          client,
		refreshInterval: syncHeightsRefreshInterval,
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

	coreBlocksHeight := c.coreBlocksHeight
	coreHeadersHeight := c.coreHeadersHeight
	updatedAt := c.updatedAt.UTC()

	return syncHeightSnapshot{
		CoreBlocksHeight:  &coreBlocksHeight,
		CoreHeadersHeight: &coreHeadersHeight,
		CoreSyncUpdatedAt: &updatedAt,
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
	c.coreBlocksHeight = result.Blocks
	c.coreHeadersHeight = result.Headers
	c.updatedAt = c.now()
	c.hasData = true
	c.mu.Unlock()
}
