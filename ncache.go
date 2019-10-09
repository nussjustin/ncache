package ncache

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// Cache defines methods for storing and retrieving values.
//
// Implementations of Cache must be safe for concurrent use.
type Cache interface {
	// Get returns the value with the given key, whether the value was in the cache and whether it's TTL expired.
	Get(ctx context.Context, key string) (val interface{}, stale bool, ok bool)

	// Set sets the value for for a key with an optional TTL and optional stale period.
	//
	// A value of 0 for the TTL or stale period means no TTL/stale period.
	Set(ctx context.Context, key string, val interface{}, ttl time.Duration, stalePeriod time.Duration) error
}

// LRU implements a capped Cache using a LRU algorithm for expiring old entries.
type LRU struct {
	size int

	nowFunc func() time.Time // for tests

	mu           sync.Mutex
	entries      *list.List
	entriesByKey map[string]*list.Element
}

type lruEntry struct {
	key string
	val interface{}

	// use int64 instead of time.Time to reduce GC pressure (time.Time contains a pointer)
	absExpireTime int64 // in nanoseconds
	stalePeriod   int64 // in nanoseconds
}

func (le lruEntry) expired(now int64) bool {
	return le.absExpireTime > 0 && le.absExpireTime+le.stalePeriod <= now
}

func (le lruEntry) stale(now int64) bool {
	return le.absExpireTime > 0 && le.absExpireTime <= now && le.absExpireTime+le.stalePeriod > now
}

// NewLRU returns a new LRU Cache with the given size.
func NewLRU(size int) *LRU {
	return &LRU{
		size: size,

		nowFunc: time.Now,

		entries:      list.New(),
		entriesByKey: make(map[string]*list.Element),
	}
}

// Get implements the Cache interface.
func (l *LRU) Get(ctx context.Context, key string) (val interface{}, stale bool, ok bool) {
	now := l.nowFunc().UnixNano()

	l.mu.Lock()
	e := l.entriesByKey[key]
	if e == nil {
		l.mu.Unlock()
		return nil, false, false
	}

	le := e.Value.(lruEntry)
	if le.expired(now) {
		l.entries.Remove(e)
		delete(l.entriesByKey, key)

		l.mu.Unlock()
		return nil, false, false
	}

	l.entries.MoveToFront(e)
	l.mu.Unlock()

	return le.val, le.stale(now), true
}

// Size returns the number of entries in the cache.
func (l *LRU) Size() int {
	l.mu.Lock()
	s := l.entries.Len()
	l.mu.Unlock()
	return s
}

// Set implements the Cache interface.
func (l *LRU) Set(ctx context.Context, key string, val interface{}, ttl time.Duration, stalePeriod time.Duration) error {
	var absExpireTime int64
	if ttl > 0 {
		absExpireTime = l.nowFunc().Add(ttl).UnixNano()
	}

	if stalePeriod < 0 {
		stalePeriod = 0
	}

	le := lruEntry{
		key:           key,
		val:           val,
		absExpireTime: absExpireTime,
		stalePeriod:   stalePeriod.Nanoseconds(),
	}

	l.mu.Lock()
	e := l.entriesByKey[key]
	if e != nil {
		l.entries.MoveToFront(e)
		e.Value = le
	} else {
		e = l.entries.PushFront(le)
		l.entriesByKey[key] = e

		if l.entries.Len() > l.size {
			l.removeOldestLocked()
		}
	}
	l.mu.Unlock()
	return nil
}

func (l *LRU) removeOldestLocked() {
	last := l.entries.Back()
	if last != nil {
		key := l.entries.Remove(last).(lruEntry).key
		delete(l.entriesByKey, key)
	}
}
