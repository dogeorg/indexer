package web

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type fakeCoreRequestClient struct {
	blocks  int64
	headers int64
	err     error
}

func (f fakeCoreRequestClient) Request(_ context.Context, _ string, _ []any, result any) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	payload, err := json.Marshal(struct {
		Blocks  int64 `json:"blocks"`
		Headers int64 `json:"headers"`
	}{
		Blocks:  f.blocks,
		Headers: f.headers,
	})
	if err != nil {
		return 0, err
	}
	return 0, json.Unmarshal(payload, result)
}

func TestSyncHeightCacheSnapshotUnavailableWithoutData(t *testing.T) {
	cache := newSyncHeightCache(fakeCoreRequestClient{})

	snapshot := cache.snapshot()
	if snapshot.CoreBlocksHeight != nil || snapshot.CoreHeadersHeight != nil || snapshot.CoreSyncUpdatedAt != nil {
		t.Fatalf("expected empty snapshot, got %+v", snapshot)
	}
}

func TestSyncHeightCacheRefreshStoresFreshData(t *testing.T) {
	now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	cache := newSyncHeightCache(fakeCoreRequestClient{blocks: 10, headers: 12})
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
	cache := newSyncHeightCache(fakeCoreRequestClient{err: errors.New("boom")})
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
