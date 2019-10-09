package ncache

import (
	"context"
	"fmt"
	"sync"
)

type sfGroup struct {
	c *LookupCache

	mu sync.Mutex
	m  map[string]*sfWaiter
}

func newSfGroup(c *LookupCache) *sfGroup {
	return &sfGroup{c: c, m: make(map[string]*sfWaiter)}
}

func (sfg *sfGroup) do(ctx context.Context, key string, lookup LookupFunc, opts LookupOpts) (val interface{}, err error) {
	// TODO(nussjustin): Check lock contention. Consider sharding if necessary. (Note: Go 1.14 adds bytes/hash)
	sfg.mu.Lock()
	w, ok := sfg.m[key]
	if !ok {
		w = newSfWaiter(sfg.c)
		w.do(key, lookup, opts)
		sfg.m[key] = w
	}
	sfg.mu.Unlock()
	return w.wait(ctx)
}

type sfWaiter struct {
	c *LookupCache

	ctx    context.Context
	cancel func()

	key string

	mu  sync.Mutex
	val interface{}
	err error
}

func newSfWaiter(c *LookupCache) *sfWaiter {
	return &sfWaiter{c: c}
}

func (sfw *sfWaiter) do(key string, lookup LookupFunc, opts LookupOpts) {
	sfw.ctx, sfw.cancel = context.WithTimeout(context.Background(), opts.Timeout)
	sfw.key = key

	go func() {
		sfw.mu.Lock()
		defer sfw.mu.Unlock()

		defer sfw.cancel()
		defer func() {
			if v := recover(); v != nil {
				err, ok := v.(error)
				if !ok {
					err = fmt.Errorf("panic while looking up key %q: %s", sfw.key, v)
				}
				sfw.val, sfw.err = nil, err
			}

			if sfw.err == nil {
				// TODO(nussjustin): Report error
				_ = sfw.c.store.Set(sfw.ctx, sfw.key, sfw.val, opts.TTL, opts.StaleFor)
			}
		}()

		sfw.val, sfw.err = lookup(sfw.ctx, sfw.key)
		if sfw.err != nil {
			// we don't want to return both a value and an error **ever**
			sfw.val = nil
		}
	}()
}

func (sfw *sfWaiter) wait(ctx context.Context) (val interface{}, err error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-sfw.ctx.Done():
		if err := ctx.Err(); err != nil && err != context.Canceled {
			return nil, ctx.Err()
		}
		sfw.mu.Lock()
		val, err = sfw.val, sfw.err
		sfw.mu.Unlock()
		return
	}
}
