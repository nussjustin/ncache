package ncache

import (
	"context"
	"time"
)

// LookupCache wraps an existing Cache, adding functionality for looking up and setting missing and/or expired values
// in a way that no key will looked up multiple times at once.
type LookupCache struct {
	opts  LookupOpts
	store Cache

	sf *sfGroup
}

// NewCache returns a new LookupCache based on the given store.
//
// An optional set of LookupOpts can be given, that will be used to override the global defaults by merging the values
// from the given LookupOpts with the global instance.
//
// Currently the global defaults are defined as:
//
//     LookupOpts{
//         RefreshMode: RefreshExpired,
//         Timeout:     10 * time.Second,
//     }
func NewLookupCache(store Cache, defaultOpts *LookupOpts) *LookupCache {
	c := &LookupCache{
		opts: LookupOpts{
			RefreshMode: RefreshExpired,
			Timeout:     10 * time.Second,
		},
		store: store,
	}
	c.sf = newSfGroup(c)
	if defaultOpts != nil {
		c.opts = c.opts.Merge(*defaultOpts)
	}
	return c
}

// GetSet returns the value for the given key or looks up  new value if the key was not found in the underlying Cache.
//
// The lookup behaviour can be configured using the opts parameter and is based on the LookupOpts given to NewCache.
//
// Callers should always check the returned error for nil instead of the value, since the cache value could be nil.
func (c *LookupCache) GetSet(ctx context.Context, key string, lookup LookupFunc, opts *LookupOpts) (val interface{}, stale bool, err error) {
	mopts := c.opts
	if opts != nil {
		mopts = mopts.Merge(*opts)
	}

	val, stale, ok := c.store.Get(ctx, key)
	if !ok {
		val, err = c.sf.do(ctx, key, lookup, mopts)
		return val, false, err
	}

	if stale {
		switch mopts.RefreshMode {
		case RefreshDefault, RefreshExpired:
		case RefreshStale:
			// use the default timeout since this lookup should not be bound to our call
			mopts.Timeout = c.opts.Timeout
			go func() { _, _ = c.sf.do(ctx, key, lookup, mopts) }()
		case RefreshStaleSync:
			val, err = c.sf.do(ctx, key, lookup, mopts)
			return val, false, err
		}
	}

	return val, stale, nil
}

// LookupFunc is the type for functions used to lookup values that will be stored in the cache.
//
// If a lookup functions returns a non-nil error, the returned value will be ignored.
type LookupFunc func(ctx context.Context, key string) (val interface{}, err error)

// LookupOpts contains options for looking up values in GetSet operations.
type LookupOpts struct {
	// RefreshMode controls how stale values are refreshed.
	RefreshMode RefreshMode

	// StaleFor sets an optional duration for which a value will be retained after it's TTL expired, in which the value
	// can still be served.
	StaleFor time.Duration

	// Timeout is the timeout used for looking up the new value.
	Timeout time.Duration

	// TTL is an optional duration after which a value should be deleted (or become stale if StaleFor is > 0).
	TTL time.Duration
}

// Merge returns a new LookupOpts based on o and overriding all fields with the values from other when set there.
func (o *LookupOpts) Merge(other LookupOpts) LookupOpts {
	if o == nil {
		return other
	}

	n := *o
	if other.RefreshMode != RefreshDefault {
		n.RefreshMode = other.RefreshMode
	}
	if other.StaleFor > 0 {
		n.StaleFor = other.StaleFor
	}
	if other.Timeout > 0 {
		n.Timeout = other.Timeout
	}
	if other.TTL > 0 {
		n.TTL = other.TTL
	}
	return n
}

// RefreshMode controls how stale values are refreshed when using GetSet.
type RefreshMode uint

const (
	// RefreshDefault is the default refresh action.
	//
	// If set on the LookupCache itself, this is the same as RefreshExpired.
	// If set for a single GetSet operation, the LookupCache wide default will be used.
	RefreshDefault RefreshMode = iota

	// RefreshExpired will refresh only entries that are completely expired and not available anymore.
	RefreshExpired

	// RefreshMode will cause any stale value to be refreshed in the background, without waiting for
	// the lookup to complete, returning the old value from GetSet instead.
	RefreshStale

	// RefreshStaleSync will cause any stale value to be refreshed in the foreground, waiting for the lookup to
	// complete and returning the new value from GetSet.
	RefreshStaleSync
)
