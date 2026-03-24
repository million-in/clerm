package capability

import (
	"sync"
	"time"

	"github.com/million-in/clerm/internal/platform"
)

type ReplayStore interface {
	Reserve(tokenID string, ttl time.Duration) error
}

const replayShardCount = 256

const defaultReplaySweepInterval = 30 * time.Second

// MemoryReplayStore is an in-process replay cache.
// It is not sufficient for multi-process or multi-host deployments; use a
// distributed ReplayStore implementation in production for shared replay protection.
type MemoryReplayStore struct {
	now      func() time.Time
	shards   [replayShardCount]replayShard
	sweepDur time.Duration
}

type replayShard struct {
	mu   sync.Mutex
	used map[string]time.Time
}

func NewMemoryReplayStore() *MemoryReplayStore {
	return NewMemoryReplayStoreWithClockAndSweep(time.Now, defaultReplaySweepInterval)
}

func NewMemoryReplayStoreWithClock(now func() time.Time) *MemoryReplayStore {
	return NewMemoryReplayStoreWithClockAndSweep(now, defaultReplaySweepInterval)
}

func NewMemoryReplayStoreWithClockAndSweep(now func() time.Time, sweepInterval time.Duration) *MemoryReplayStore {
	if now == nil {
		now = time.Now
	}
	store := &MemoryReplayStore{
		now:      now,
		sweepDur: sweepInterval,
	}
	for i := range store.shards {
		store.shards[i].used = make(map[string]time.Time)
	}
	if sweepInterval > 0 {
		go store.cleanupLoop()
	}
	return store
}

func (s *MemoryReplayStore) Reserve(tokenID string, ttl time.Duration) error {
	if s == nil {
		return platform.New(platform.CodeInvalidArgument, "replay store is required")
	}
	if tokenID == "" {
		return platform.New(platform.CodeInvalidArgument, "capability token id is required")
	}
	if ttl <= 0 {
		return platform.New(platform.CodeValidation, "capability token has no remaining lifetime")
	}
	now := s.nowTime()
	expiresAt := now.Add(ttl)

	shard := s.shardFor(tokenID)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if expiry, exists := shard.used[tokenID]; exists && expiry.After(now) {
		return platform.New(platform.CodeValidation, "capability token has already been used")
	}
	delete(shard.used, tokenID)
	shard.used[tokenID] = expiresAt
	return nil
}

func (s *MemoryReplayStore) nowTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *MemoryReplayStore) cleanupLoop() {
	ticker := time.NewTicker(s.sweepDur)
	defer ticker.Stop()
	for range ticker.C {
		now := s.nowTime()
		for i := range s.shards {
			shard := &s.shards[i]
			shard.mu.Lock()
			for key, expiry := range shard.used {
				if !expiry.After(now) {
					delete(shard.used, key)
				}
			}
			shard.mu.Unlock()
		}
	}
}

func (s *MemoryReplayStore) shardFor(tokenID string) *replayShard {
	return &s.shards[replayShardIndex(tokenID)]
}

func replayShardIndex(tokenID string) int {
	var hash uint32 = 2166136261
	for i := 0; i < len(tokenID); i++ {
		hash ^= uint32(tokenID[i])
		hash *= 16777619
	}
	return int(hash % replayShardCount)
}
