package lock

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type DistributedLocker interface {
	Acquire(ctx context.Context, key string, ttl time.Duration) (token string, ok bool, err error)
	Release(ctx context.Context, key string, token string) error
}

type lockEntry struct {
	token     string
	expiresAt int64 // UnixNano
}

type RedisStyleLocker struct {
	mu    sync.Mutex
	locks map[string]lockEntry
	seq   atomic.Uint64
}

func NewRedisStyleLocker() *RedisStyleLocker {
	return &RedisStyleLocker{
		locks: make(map[string]lockEntry, 256),
	}
}

func (l *RedisStyleLocker) Acquire(ctx context.Context, key string, ttl time.Duration) (string, bool, error) {
	select {
	case <-ctx.Done():
		return "", false, ctx.Err()
	default:
	}

	now := time.Now().UnixNano()
	l.mu.Lock()
	defer l.mu.Unlock()

	if existing, exists := l.locks[key]; exists && now < existing.expiresAt {
		return "", false, nil
	}

	token := fmt.Sprintf("lock-tok-%d", l.seq.Add(1))
	l.locks[key] = lockEntry{
		token:     token,
		expiresAt: now + ttl.Nanoseconds(),
	}
	return token, true, nil
}

func (l *RedisStyleLocker) Release(ctx context.Context, key string, token string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if existing, exists := l.locks[key]; exists && existing.token == token {
		delete(l.locks, key)
	}
	return nil
}
