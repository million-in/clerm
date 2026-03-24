package capability_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/million-in/clerm/capability"
)

func TestMemoryReplayStoreAllowsReuseAfterExpiry(t *testing.T) {
	now := time.Unix(1711000000, 0).UTC()
	store := capability.NewMemoryReplayStoreWithClock(func() time.Time { return now })

	if err := store.Reserve("tok-1", time.Second); err != nil {
		t.Fatalf("Reserve(first) error = %v", err)
	}
	now = now.Add(2 * time.Second)
	if err := store.Reserve("tok-1", time.Second); err != nil {
		t.Fatalf("Reserve(after expiry) error = %v", err)
	}
}

func TestMemoryReplayStoreRejectsDuplicateBeforeExpiry(t *testing.T) {
	store := capability.NewMemoryReplayStoreWithClock(func() time.Time {
		return time.Unix(1711000000, 0).UTC()
	})
	if err := store.Reserve("tok-1", time.Minute); err != nil {
		t.Fatalf("Reserve(first) error = %v", err)
	}
	if err := store.Reserve("tok-1", time.Minute); err == nil {
		t.Fatal("expected replay rejection")
	}
}

func TestMemoryReplayStoreConcurrentReserveAllowsSingleWinner(t *testing.T) {
	store := capability.NewMemoryReplayStore()
	var successCount int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.Reserve("shared-token", time.Minute); err == nil {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}
	wg.Wait()
	if successCount != 1 {
		t.Fatalf("expected exactly one successful reserve, got %d", successCount)
	}
}
