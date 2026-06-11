package web

import (
	"context"
	"log"
	"sync"
	"time"

	walkerspec "github.com/dogeorg/dogewalker/spec"
)

const syncHeightsRefreshInterval = 30 * time.Second

type syncHeightSnapshot struct {
	CoreBlocksHeight  *int64
	CoreHeadersHeight *int64
	CoreSyncUpdatedAt *time.Time
}

type syncHeightCache struct {
	blockchain      walkerspec.Blockchain
	refreshInterval time.Duration
	now             func() time.Time

	mu                sync.RWMutex
	coreBlocksHeight  int64
	coreHeadersHeight int64
	updatedAt         time.Time
	hasData           bool
}

func newSyncHeightCache(blockchain walkerspec.Blockchain) *syncHeightCache {
	if blockchain == nil {
		return nil
	}
	return &syncHeightCache{
		blockchain:      blockchain,
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

	info, err := c.blockchain.GetBlockchainInfo(ctx)
	if err != nil {
		log.Printf("[Indexer] sync heights refresh failed: getblockchaininfo: %v", err)
		return
	}

	c.mu.Lock()
	c.coreBlocksHeight = info.Blocks
	c.coreHeadersHeight = info.Headers
	c.updatedAt = c.now()
	c.hasData = true
	c.mu.Unlock()
}
