package auth

import "sync"

type keyedMutex struct {
	mu    sync.Mutex
	locks map[int64]*sync.Mutex
}

func newKeyedMutex() *keyedMutex {
	return &keyedMutex{locks: make(map[int64]*sync.Mutex)}
}

func (k *keyedMutex) Lock(id int64) func() {
	k.mu.Lock()
	m, ok := k.locks[id]
	if !ok {
		m = &sync.Mutex{}
		k.locks[id] = m
	}
	k.mu.Unlock()
	m.Lock()
	return m.Unlock
}
