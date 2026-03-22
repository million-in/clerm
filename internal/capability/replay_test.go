package capability

import (
	"testing"
	"time"
)

func TestMemoryReplayStoreAllowsReuseAfterExpiry(t *testing.T) {
	now := time.Unix(1711000000, 0).UTC()
	store := NewMemoryReplayStore()
	store.now = func() time.Time { return now }

	if err := store.Reserve("tok-1", time.Second); err != nil {
		t.Fatalf("Reserve(first) error = %v", err)
	}
	now = now.Add(2 * time.Second)
	if err := store.Reserve("tok-1", time.Second); err != nil {
		t.Fatalf("Reserve(after expiry) error = %v", err)
	}
}

func TestMemoryReplayStorePeriodicCleanupRemovesExpiredEntries(t *testing.T) {
	now := time.Unix(1711000000, 0).UTC()
	store := NewMemoryReplayStore()
	store.now = func() time.Time { return now }
	store.used["expired"] = now.Add(-time.Second)
	store.used["live"] = now.Add(time.Minute)
	store.reservations = replaySweepInterval - 1

	if err := store.Reserve("tok-2", time.Minute); err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if _, ok := store.used["expired"]; ok {
		t.Fatal("expected expired token to be removed during sweep")
	}
	if _, ok := store.used["live"]; !ok {
		t.Fatal("expected live token to remain after sweep")
	}
}
