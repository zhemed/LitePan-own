package playback

import (
	"context"
	"sync"
)

const maximumRangeConcurrency = 8

// accountRangeLimiter 让同一账号的本地代理与 FUSE Range 请求共享驱动声明的并发上限。
type accountRangeLimiter struct {
	mu       sync.Mutex
	accounts map[int64]chan struct{}
}

func (l *accountRangeLimiter) acquire(ctx context.Context, accountID int64, limit int) (func(), error) {
	if accountID <= 0 || limit <= 0 {
		return func() {}, nil
	}
	if limit > maximumRangeConcurrency {
		limit = maximumRangeConcurrency
	}

	l.mu.Lock()
	if l.accounts == nil {
		l.accounts = make(map[int64]chan struct{})
	}
	sem := l.accounts[accountID]
	if sem == nil {
		sem = make(chan struct{}, limit)
		l.accounts[accountID] = sem
	}
	l.mu.Unlock()

	select {
	case sem <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() { <-sem })
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
