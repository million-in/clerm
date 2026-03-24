package capability

import (
	"sync"
	"time"

	"github.com/million-in/clerm/internal/platform"
)

type ReplayStore interface {
	Reserve(tokenID string, ttl time.Duration) error
}

const replaySweepInterval = 256

type MemoryReplayStore struct {
	mu           sync.Mutex
	now          func() time.Time
	used         map[string]time.Time
	reservations uint64
}

func NewMemoryReplayStore() *MemoryReplayStore {
	return NewMemoryReplayStoreWithClock(time.Now)
}

func NewMemoryReplayStoreWithClock(now func() time.Time) *MemoryReplayStore {
	if now == nil {
		now = time.Now
	}
	return &MemoryReplayStore{
		now:  now,
		used: map[string]time.Time{},
	}
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

	s.mu.Lock()
	defer s.mu.Unlock()
	if expiry, exists := s.used[tokenID]; exists && expiry.After(now) {
		return platform.New(platform.CodeValidation, "capability token has already been used")
	}
	delete(s.used, tokenID)
	s.reservations++
	if s.reservations == 1 || s.reservations%replaySweepInterval == 0 {
		s.cleanupExpiredLocked(now)
	}
	s.used[tokenID] = expiresAt
	return nil
}

func (s *MemoryReplayStore) nowTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *MemoryReplayStore) cleanupExpiredLocked(now time.Time) {
	for key, expiry := range s.used {
		if !expiry.After(now) {
			delete(s.used, key)
		}
	}
}
