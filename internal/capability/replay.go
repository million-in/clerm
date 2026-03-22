package capability

import (
	"sync"
	"time"

	"github.com/million-in/clerm/internal/platform"
)

type ReplayStore interface {
	Reserve(tokenID string, ttl time.Duration) error
}

type MemoryReplayStore struct {
	mu   sync.Mutex
	now  func() time.Time
	used map[string]time.Time
}

func NewMemoryReplayStore() *MemoryReplayStore {
	return &MemoryReplayStore{
		now:  time.Now,
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
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	expiresAt := now.Add(ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	for key, expiry := range s.used {
		if !expiry.After(now) {
			delete(s.used, key)
		}
	}
	if expiry, exists := s.used[tokenID]; exists && expiry.After(now) {
		return platform.New(platform.CodeValidation, "capability token has already been used")
	}
	s.used[tokenID] = expiresAt
	return nil
}
