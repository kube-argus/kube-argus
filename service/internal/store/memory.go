package store

import (
	"context"
	"sync"
	"time"
)

// memory is a single-replica in-process store. Entries expire lazily on read
// plus a background sweep. Good for dev / single replica; use redis for HA.
type memory struct {
	mu      sync.Mutex
	auth    map[string]authEntry
	codes   map[string]codeEntry
	refresh map[string]refreshEntry
	stopCh  chan struct{}
}

type authEntry struct {
	val AuthRequest
	exp time.Time
}

type codeEntry struct {
	val CodeGrant
	exp time.Time
}

type refreshEntry struct {
	val RefreshGrant
	exp time.Time
}

func newMemory() *memory {
	m := &memory{
		auth:    make(map[string]authEntry),
		codes:   make(map[string]codeEntry),
		refresh: make(map[string]refreshEntry),
		stopCh:  make(chan struct{}),
	}
	go m.sweep()
	return m
}

func (m *memory) SaveAuthRequest(_ context.Context, key string, ar AuthRequest, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.auth[key] = authEntry{val: ar, exp: time.Now().Add(ttl)}
	return nil
}

func (m *memory) TakeAuthRequest(_ context.Context, key string) (AuthRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.auth[key]
	delete(m.auth, key)
	if !ok || time.Now().After(e.exp) {
		return AuthRequest{}, ErrNotFound
	}
	return e.val, nil
}

func (m *memory) SaveCode(_ context.Context, code string, g CodeGrant, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codes[code] = codeEntry{val: g, exp: time.Now().Add(ttl)}
	return nil
}

func (m *memory) TakeCode(_ context.Context, code string) (CodeGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.codes[code]
	delete(m.codes, code) // one-shot
	if !ok || time.Now().After(e.exp) {
		return CodeGrant{}, ErrNotFound
	}
	return e.val, nil
}

func (m *memory) SaveRefresh(_ context.Context, token string, g RefreshGrant, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refresh[token] = refreshEntry{val: g, exp: time.Now().Add(ttl)}
	return nil
}

func (m *memory) TakeRefresh(_ context.Context, token string) (RefreshGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.refresh[token]
	delete(m.refresh, token) // one-shot: rotated on every use
	if !ok || time.Now().After(e.exp) {
		return RefreshGrant{}, ErrNotFound
	}
	return e.val, nil
}

func (m *memory) Close() error {
	close(m.stopCh)
	return nil
}

func (m *memory) sweep() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case now := <-t.C:
			m.mu.Lock()
			for k, e := range m.auth {
				if now.After(e.exp) {
					delete(m.auth, k)
				}
			}
			for k, e := range m.codes {
				if now.After(e.exp) {
					delete(m.codes, k)
				}
			}
			for k, e := range m.refresh {
				if now.After(e.exp) {
					delete(m.refresh, k)
				}
			}
			m.mu.Unlock()
		}
	}
}
