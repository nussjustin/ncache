package ncache

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLookupCache_GetSet(t *testing.T) {
	for _, test := range []struct {
		Name string
		Opts *LookupOpts

		InitialValue       interface{}
		InitialTTL         time.Duration
		InitialStalePeriod time.Duration
		InitialWait        time.Duration

		LookupSleep      time.Duration
		LookupValue      interface{}
		LookupError      string
		LookupPanicValue interface{}

		ExpectedValue interface{}
		ExpectStale   bool
		ExpectedError string

		ExpectLookup bool
	}{
		// TODO: test error when setting value in lookup (currently not exposed to users)
		{
			Name: "Fresh",

			InitialValue: "hello",
			InitialTTL:   time.Second,

			ExpectedValue: "hello",
		},
		{
			Name: "Stale",
			Opts: &LookupOpts{
				RefreshMode: RefreshDefault,
			},

			InitialValue:       "hello",
			InitialTTL:         1 * time.Microsecond,
			InitialStalePeriod: 5 * time.Second,
			InitialWait:        1 * time.Millisecond,

			ExpectedValue: "hello",
			ExpectStale:   true,
		},
		{
			Name: "Stale + refresh expired (same as default)",
			Opts: &LookupOpts{
				RefreshMode: RefreshExpired,
			},

			InitialValue:       "hello",
			InitialTTL:         1 * time.Microsecond,
			InitialStalePeriod: 5 * time.Second,
			InitialWait:        5 * time.Millisecond,

			ExpectedValue: "hello",
			ExpectStale:   true,
		},
		{
			Name: "Stale + refresh",
			Opts: &LookupOpts{
				RefreshMode: RefreshStaleSync,
			},

			InitialValue:       "hello",
			InitialTTL:         1 * time.Microsecond,
			InitialStalePeriod: 5 * time.Second,
			InitialWait:        1 * time.Millisecond,

			LookupValue: "world",

			ExpectedValue: "world",
			ExpectStale:   false,

			ExpectLookup: true,
		},
		{
			Name: "Stale + refresh in background",
			Opts: &LookupOpts{
				RefreshMode: RefreshStale,
			},

			InitialValue:       "hello",
			InitialTTL:         1 * time.Microsecond,
			InitialStalePeriod: 5 * time.Second,
			InitialWait:        1 * time.Millisecond,

			LookupValue: "world",

			ExpectedValue: "hello",
			ExpectStale:   true,

			ExpectLookup: true,
		},
		{
			Name: "Expired",

			InitialValue: "hello",
			InitialTTL:   1 * time.Microsecond,
			InitialWait:  1 * time.Millisecond,

			LookupValue: "world",

			ExpectedValue: "world",

			ExpectLookup: true,
		},
		{
			Name: "Lookup error",

			InitialValue: "hello",
			InitialTTL:   1 * time.Microsecond,
			InitialWait:  1 * time.Millisecond,

			LookupValue: "world",
			LookupError: "damn",

			ExpectedError: "damn",

			ExpectLookup: true,
		},
		{
			Name: "Lookup timeout",
			Opts: &LookupOpts{
				Timeout: 5 * time.Millisecond,
			},

			InitialValue: "hello",
			InitialTTL:   1 * time.Microsecond,
			InitialWait:  1 * time.Millisecond,

			LookupValue: "world",
			LookupError: "damn",
			LookupSleep: 250 * time.Millisecond,

			ExpectedError: context.DeadlineExceeded.Error(),

			ExpectLookup: true,
		},
		{
			Name: "Caller context canceled",

			InitialValue: "hello",
			InitialTTL:   1 * time.Microsecond,
			InitialWait:  1 * time.Millisecond,

			LookupValue: "world",
			LookupError: "damn",
			LookupSleep: 250 * time.Millisecond,

			ExpectedError: context.DeadlineExceeded.Error(),

			ExpectLookup: true,
		},
		{
			Name: "Lookup panic",

			InitialValue: "hello",
			InitialTTL:   1 * time.Microsecond,
			InitialWait:  1 * time.Millisecond,

			LookupPanicValue: "some panic",

			ExpectedError: "panic while looking up key \"Lookup panic\": some panic",

			ExpectLookup: true,
		},
		{
			Name: "Lookup panic with error instance",

			InitialValue: "hello",
			InitialTTL:   1 * time.Microsecond,
			InitialWait:  1 * time.Millisecond,

			LookupPanicValue: errors.New("some panic with real error"),

			ExpectedError: "some panic with real error",

			ExpectLookup: true,
		},
	} {
		test := test
		t.Run(test.Name, func(t *testing.T) {
			lookupCalled := make(chan struct{}, 1)
			lookup := func(ctx context.Context, key string) (val interface{}, err error) {
				lookupCalled <- struct{}{}

				if key != test.Name {
					// this runs on another goroutine so we are not allowed to use t here.
					// instead just panic
					panic(fmt.Sprintf("expected key=%q, got key=%q", test.Name, key))
				}

				if test.LookupSleep > 0 {
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(test.LookupSleep):
					}
				}

				if test.LookupPanicValue != nil {
					panic(test.LookupPanicValue)
				}
				if test.LookupError != "" {
					return nil, errors.New(test.LookupError)
				}
				return test.LookupValue, nil
			}

			c := NewLRU(4)
			if err := c.Set(context.Background(), test.Name, test.InitialValue, test.InitialTTL, test.InitialStalePeriod); err != nil {
				t.Fatalf("failed to set initial value for key %q: %s", test.Name, err)
			}

			if test.InitialWait > 0 {
				time.Sleep(test.InitialWait)
			}

			lc := NewLookupCache(c, nil)

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			gotValue, gotStale, gotErr := lc.GetSet(ctx, test.Name, lookup, test.Opts)

			if test.ExpectedError == "" {
				assertNoError(t, gotErr)
			} else {
				assertError(t, test.ExpectedError, gotErr)
			}

			assertStaleState(t, test.ExpectStale, gotStale)
			assertValue(t, test.ExpectedValue, gotValue)

			// check underlying cache against reported results
			lruValue, lruStale, _ := c.Get(context.Background(), test.Name)
			assertStaleState(t, lruStale, gotStale)
			assertValue(t, lruValue, gotValue)

			var receiveTimeout time.Duration
			if test.Opts != nil && test.Opts.RefreshMode == RefreshStale {
				// we need to wait when refreshing in background. we also add a few milliseconds to avoid timing issues.
				receiveTimeout = test.LookupSleep + time.Second
			}

			if test.ExpectLookup {
				assertReceive(t, lookupCalled, receiveTimeout)
			} else {
				assertNoReceive(t, lookupCalled, receiveTimeout)
			}
		})
	}
}

func assertError(tb testing.TB, expected string, err error) {
	tb.Helper()
	if err == nil {
		tb.Errorf("expected error %q, got no error", expected)
	} else if err.Error() != expected {
		tb.Errorf("expected error %q, got error %q", expected, err)
	}
}

func assertNoError(tb testing.TB, err error) {
	tb.Helper()
	if err != nil {
		tb.Errorf("expected no error, got error %q", err)
	}
}

func assertNoReceive(tb testing.TB, c <-chan struct{}, timeout time.Duration) {
	tb.Helper()

	if timeout > 0 {
		select {
		case <-c:
			tb.Error("unexpected value received on channel")
		case <-time.After(timeout):
		}
	} else {
		select {
		case <-c:
			tb.Error("unexpected value received on channel")
		default:
		}
	}
}

func assertReceive(tb testing.TB, c <-chan struct{}, timeout time.Duration) {
	tb.Helper()

	if timeout > 0 {
		select {
		case <-c:
		case <-time.After(timeout):
			tb.Errorf("expected to receive value on channel but got nothing after %s", timeout)
		}
	} else {
		select {
		case <-c:
		default:
			tb.Error("expected to receive value on channel but got nothing")
		}
	}
}

func assertStaleState(tb testing.TB, expected, got bool) {
	tb.Helper()
	if expected && !got {
		tb.Error("expected value to be stale, got non-stale value")
	} else if !expected && got {
		tb.Error("expected value to be non-stale, got stale value")
	}
}

func assertValue(tb testing.TB, expected, got interface{}) {
	tb.Helper()
	switch {
	case expected == nil && got != nil:
		tb.Errorf("expected no value, got value %v", got)
	case expected != nil && got == nil:
		tb.Errorf("expected value %v, got no value", expected)
	case !reflect.DeepEqual(expected, got):
		tb.Errorf("expected value %v, got value %v", expected, got)
	}
}

func TestLookupCache_GetSet_Dedupe(t *testing.T) {
	lc := NewLookupCache(NewLRU(4), &LookupOpts{TTL: time.Second})

	var lookupCalled uint64

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = lc.GetSet(context.Background(), "hello", func(context.Context, string) (interface{}, error) {
				atomic.AddUint64(&lookupCalled, 1)
				time.Sleep(25 * time.Microsecond)
				return "world", nil
			}, nil)
		}()
	}
	wg.Wait()

	if calls := atomic.LoadUint64(&lookupCalled); calls != 1 {
		t.Errorf("expected exactly 1 lookup, got %d", calls)
	}
}

func BenchmarkLookupCache_GetSet(b *testing.B) {
	b.Run("Dedup", func(b *testing.B) {
		size := runtime.GOMAXPROCS(0)
		if size > 1 {
			size = size / 2
		}
		c := NewLRU(size)
		lc := NewLookupCache(c, &LookupOpts{
			TTL: 1 * time.Microsecond,
		})

		key := "key"

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, _, _ = lc.GetSet(context.Background(), key, func(context.Context, string) (interface{}, error) {
					return key, nil
				}, nil)
			}
		})
	})

	b.Run("Existing", func(b *testing.B) {
		size := runtime.GOMAXPROCS(0)
		if size > 1 {
			size = size / 2
		}
		c := NewLRU(size)
		lc := NewLookupCache(c, nil)

		b.RunParallel(func(pb *testing.PB) {
			key := "key#" + strconv.Itoa(rand.Int())
			if err := c.Set(context.Background(), key, key, 0, 0); err != nil {
				b.Fatalf("failed to set key %q: %s", key, err)
			}

			for pb.Next() {
				_, _, _ = lc.GetSet(context.Background(), key, func(context.Context, string) (interface{}, error) {
					panic("should never be called")
				}, nil)
			}
		})
	})
}

func TestLookupOpts_Merge(t *testing.T) {
	for _, test := range []struct {
		Name string
		A, B LookupOpts
		E    LookupOpts
	}{
		{
			Name: "Both Empty",
			A:    LookupOpts{},
			B:    LookupOpts{},
			E:    LookupOpts{},
		},
		{
			Name: "A empty",
			A:    LookupOpts{},
			B: LookupOpts{
				RefreshMode: RefreshStale,
				StaleFor:    1,
				Timeout:     2,
				TTL:         3,
			},
			E: LookupOpts{
				RefreshMode: RefreshStale,
				StaleFor:    1,
				Timeout:     2,
				TTL:         3,
			},
		},
		{
			Name: "B empty",
			A: LookupOpts{
				RefreshMode: RefreshStaleSync,
				StaleFor:    1,
				Timeout:     2,
				TTL:         3,
			},
			B: LookupOpts{},
			E: LookupOpts{
				RefreshMode: RefreshStaleSync,
				StaleFor:    1,
				Timeout:     2,
				TTL:         3,
			},
		},
		{
			Name: "Partial override",
			A: LookupOpts{
				StaleFor: 10,
				TTL:      3,
			},
			B: LookupOpts{
				RefreshMode: RefreshExpired,
				StaleFor:    1,
				Timeout:     2,
			},
			E: LookupOpts{
				RefreshMode: RefreshExpired,
				StaleFor:    1,
				Timeout:     2,
				TTL:         3,
			},
		},
		{
			Name: "Ignore default refresh in B",
			A: LookupOpts{
				RefreshMode: RefreshExpired,
			},
			B: LookupOpts{
				RefreshMode: RefreshDefault,
			},
			E: LookupOpts{
				RefreshMode: RefreshExpired,
			},
		},
	} {
		test := test
		t.Run(test.Name, func(t *testing.T) {
			g := test.A.merge(test.B)
			if !reflect.DeepEqual(g, test.E) {
				t.Errorf("result mismatch\n\texpected: %#v\n\t     got: %#v", test.E, g)
			}
		})
	}
}
