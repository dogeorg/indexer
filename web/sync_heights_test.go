package web

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dogeorg/doge"
	"github.com/dogeorg/doge/koinu"
	dogewalkerspec "github.com/dogeorg/dogewalker/spec"
)

type fakeBlockchain struct {
	blocks  int64
	headers int64
	err     error
}

func (f fakeBlockchain) WaitForSync(_ context.Context) bool { return false }
func (f fakeBlockchain) RetryMode(_ int, _ time.Duration) dogewalkerspec.Blockchain { return f }
func (f fakeBlockchain) GetBlockHeader(_ string, _ context.Context) (dogewalkerspec.BlockHeader, error) {
	return dogewalkerspec.BlockHeader{}, nil
}
func (f fakeBlockchain) GetBlock(_ string, _ context.Context) (doge.Block, int, error) {
	return doge.Block{}, 0, nil
}
func (f fakeBlockchain) GetBlockHash(_ int64, _ context.Context) (string, error) { return "", nil }
func (f fakeBlockchain) GetBestBlockHash(_ context.Context) (string, error)       { return "", nil }
func (f fakeBlockchain) GetBlockCount(_ context.Context) (int64, error)           { return 0, nil }
func (f fakeBlockchain) GetBlockchainInfo(_ context.Context) (dogewalkerspec.BlockchainInfo, error) {
	if f.err != nil {
		return dogewalkerspec.BlockchainInfo{}, f.err
	}
	return dogewalkerspec.BlockchainInfo{Blocks: f.blocks, Headers: f.headers}, nil
}
func (f fakeBlockchain) EstimateFee(_ context.Context, _ int) (koinu.Koinu, error) {
	return 0, nil
}
func (f fakeBlockchain) GetRawMempool(_ context.Context) (dogewalkerspec.RawMempool, error) {
	return dogewalkerspec.RawMempool{}, nil
}
func (f fakeBlockchain) GetRawMempoolTxList(_ context.Context) ([]string, error) { return nil, nil }
func (f fakeBlockchain) GetRawTransaction(_ context.Context, _ string) (doge.BlockTx, error) {
	return doge.BlockTx{}, nil
}
func (f fakeBlockchain) SendRawTransaction(_ context.Context, _ string) (string, error) {
	return "", nil
}

func TestSyncHeightCacheSnapshotUnavailableWithoutData(t *testing.T) {
	cache := newSyncHeightCache(fakeBlockchain{})

	snapshot := cache.snapshot()
	if snapshot.CoreBlocksHeight != nil || snapshot.CoreHeadersHeight != nil || snapshot.CoreSyncUpdatedAt != nil {
		t.Fatalf("expected empty snapshot, got %+v", snapshot)
	}
}

func TestSyncHeightCacheRefreshStoresFreshData(t *testing.T) {
	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	cache := newSyncHeightCache(fakeBlockchain{blocks: 10, headers: 12})
	cache.now = func() time.Time { return now }

	cache.refresh(context.Background())

	snapshot := cache.snapshot()
	if snapshot.CoreBlocksHeight == nil || *snapshot.CoreBlocksHeight != 10 {
		t.Fatalf("unexpected blocks height: %+v", snapshot)
	}
	if snapshot.CoreHeadersHeight == nil || *snapshot.CoreHeadersHeight != 12 {
		t.Fatalf("unexpected headers height: %+v", snapshot)
	}
	if snapshot.CoreSyncUpdatedAt == nil || !snapshot.CoreSyncUpdatedAt.Equal(now) {
		t.Fatalf("unexpected updated at: %+v", snapshot)
	}
}

func TestSyncHeightCacheRefreshKeepsLastSuccessfulDataOnFailure(t *testing.T) {
	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	cache := newSyncHeightCache(fakeBlockchain{err: errors.New("boom")})
	cache.now = func() time.Time { return now }
	cache.coreBlocksHeight = 10
	cache.coreHeadersHeight = 12
	cache.updatedAt = now.Add(-time.Minute)
	cache.hasData = true

	cache.refresh(context.Background())

	snapshot := cache.snapshot()
	if snapshot.CoreBlocksHeight == nil || *snapshot.CoreBlocksHeight != 10 {
		t.Fatalf("expected last successful blocks height to remain, got %+v", snapshot)
	}
	if snapshot.CoreHeadersHeight == nil || *snapshot.CoreHeadersHeight != 12 {
		t.Fatalf("expected last successful headers height to remain, got %+v", snapshot)
	}
}
