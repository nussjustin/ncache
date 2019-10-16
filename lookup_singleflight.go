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
	// TODO(nussjustin): Check lock contention. Consider sharding if necessary. : Go 1.14 adds bytes/hash)
	sfg.mu.Lock()
	w, ok := sfg.m[key]
	if !ok {
		w = newSfWaiter(sfg, key, lookup, opts)
		w.do()
		sfg.m[key] = w
	}
	sfg.mu.Unlock()
	return w.wait(ctx)
}

func (sfg *sfGroup) forget(key string) {
	sfg.mu.Lock()
	delete(sfg.m, key)
	sfg.mu.Unlock()
}

type sfWaiter struct {
	sfg *sfGroup

	key    string
	lookup LookupFunc
	opts   LookupOpts

	ctx    context.Context
	cancel func()

	mu  sync.Mutex
	val interface{}
	err error
}

func newSfWaiter(sfg *sfGroup, key string, lookup LookupFunc, opts LookupOpts) *sfWaiter {
	return &sfWaiter{sfg: sfg, key: key, lookup: lookup, opts: opts}
}

func errorFromPanic(v interface{}) error {
	err, ok := v.(error)
	if ok {
		return err
	}
	return fmt.Errorf("%v", v)
}

func (sfw *sfWaiter) do() {
	sfw.ctx, sfw.cancel = context.WithTimeout(context.Background(), sfw.opts.Timeout)
	go sfw.doRun()
}

func (sfw *sfWaiter) doRun() {
	defer func() {
		if sfw.err != nil {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), sfw.opts.SetTimeout)
		defer cancel()

		_ = sfw.sfg.c.store.Set(ctx, sfw.key, sfw.val, sfw.opts.TTL, sfw.opts.StaleFor)
	}()

	var val interface{}
	var err error
	defer func() {
		defer sfw.cancel()

		if v := recover(); v != nil {
			val, err = nil, errorFromPanic(v)
		} else if err != nil {
			val = nil
		}

		sfw.mu.Lock()
		sfw.val, sfw.err = val, err
		sfw.mu.Unlock()

		sfw.sfg.forget(sfw.key)
	}()

	val, err = sfw.lookup(sfw.ctx, sfw.key)
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
