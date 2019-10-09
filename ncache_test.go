package ncache

import (
	"context"
	"math/rand"
	"reflect"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func TestLRU(t *testing.T) {
	for _, test := range []struct {
		Name string

		Key, Value    string
		SkipSet       bool
		TTL, StaleFor time.Duration

		WaitTime time.Duration

		ExpectedValue         string
		ExpectStale, ExpectOk bool
	}{
		{
			Name:    "Missing",
			SkipSet: true,
		},
		{
			Name: "NoTTL",

			Key:   "hello",
			Value: "world",

			ExpectedValue: "world",
			ExpectOk:      true,
		},
		{
			Name: "WithTTL",

			Key:   "hello",
			Value: "world",
			TTL:   3 * time.Second,

			ExpectedValue: "world",
			ExpectOk:      true,
		},
		{
			Name: "WithExpiredTTL",

			Key:   "hello",
			Value: "world",
			TTL:   3 * time.Second,

			WaitTime: 5 * time.Second,

			ExpectOk: false,
		},
		{
			Name: "WithExpiredTTLAndStalePeriod",

			Key:      "hello",
			Value:    "world",
			TTL:      3 * time.Second,
			StaleFor: 3 * time.Second,

			WaitTime: 5 * time.Second,

			ExpectedValue: "world",
			ExpectStale:   true,
			ExpectOk:      true,
		},
		{
			Name: "WithExpiredTTLAndExpiredStalePeriod",

			Key:      "hello",
			Value:    "world",
			TTL:      3 * time.Second,
			StaleFor: 2 * time.Second,

			WaitTime: 5 * time.Second,

			ExpectOk: false,
		},
	} {
		test := test
		t.Run(test.Name, func(t *testing.T) {
			now := time.Now()

			c := NewLRU(4)
			c.nowFunc = func() time.Time { return now }

			if !test.SkipSet {
				if err := c.Set(context.Background(), test.Key, test.Value, test.TTL, test.StaleFor); err != nil {
					t.Fatalf("failed to set value for key %q: %s", test.Key, err)
				}
			}

			now = now.Add(test.WaitTime)

			gotValue, gotStale, gotOk := c.Get(context.Background(), test.Key)
			if gotOk != test.ExpectOk {
				t.Fatalf("expected ok=%v, got ok=%v", test.ExpectOk, gotOk)
			}
			if gotOk && !reflect.DeepEqual(gotValue, test.ExpectedValue) {
				t.Errorf("expected val=%q, got val=%q", test.ExpectedValue, gotValue)
			}
			if gotStale != test.ExpectStale {
				t.Errorf("expected stale=%v, got stale=%v", test.ExpectStale, gotStale)
			}

			if test.ExpectOk && c.Size() != 1 {
				t.Errorf("expected .Size()=%d, got .Size()=%d", 1, c.Size())
			} else if !test.ExpectOk && c.Size() != 0 {
				t.Errorf("expected .Size()=%d, got .Size()=%d", 0, c.Size())
			}
		})
	}
}

func TestLRURemoval(t *testing.T) {
	c := NewLRU(4)

	mustSet := func(key string, val string) {
		t.Helper()
		if err := c.Set(context.Background(), key, val, 0, 0); err != nil {
			t.Fatalf("failed to set key %q: %s", key, err)
		}
	}

	mustGet := func(key string, expected string) {
		t.Helper()
		value, _, ok := c.Get(context.Background(), key)
		if !ok {
			t.Errorf("expected ok=%v, got ok=%v", true, ok)
		}
		if !reflect.DeepEqual(value, expected) {
			t.Errorf("expected val=%q, got val=%q", expected, value)
		}
	}

	mustNotGet := func(key string) {
		t.Helper()
		value, _, ok := c.Get(context.Background(), key)
		if ok {
			t.Errorf("expected ok=%v, got ok=%v", false, ok)
		}
		if value != nil {
			t.Errorf("expected val=nil, got val=%q", value)
		}
	}

	mustSet("#1", "v1")
	mustSet("#2", "v2")
	mustSet("#3", "v3")
	mustSet("#4", "v4")

	mustGet("#1", "v1")
	mustGet("#2", "v2")
	mustGet("#3", "v3")
	mustGet("#4", "v4")

	mustSet("#5", "v5")

	mustGet("#5", "v5")
	mustGet("#4", "v4")
	mustGet("#3", "v3")
	mustGet("#2", "v2")
	mustNotGet("#1")

	mustSet("#6", "v6")

	mustGet("#6", "v6")
	mustNotGet("#5")
	mustGet("#4", "v4")
	mustGet("#3", "v3")
	mustGet("#2", "v2")

	mustSet("#6", "v6.1")
	mustSet("#7", "v7")

	mustGet("#7", "v7")
	mustGet("#6", "v6.1")
	mustNotGet("#4")
	mustGet("#3", "v3")
	mustGet("#2", "v2")
}

func BenchmarkLRU_Get(b *testing.B) {
	size := runtime.GOMAXPROCS(0)
	if size > 1 {
		size = size / 2
	}
	c := NewLRU(size)

	b.RunParallel(func(pb *testing.PB) {
		key := "key#" + strconv.Itoa(rand.Int())
		if err := c.Set(context.Background(), key, key, 0, 0); err != nil {
			b.Fatalf("failed to set key %q: %s", key, err)
		}
		for pb.Next() {
			_, _, _ = c.Get(context.Background(), key)
		}
	})
}

func BenchmarkLRU_Set(b *testing.B) {
	size := runtime.GOMAXPROCS(0)
	if size > 1 {
		size = size / 2
	}
	c := NewLRU(size)

	b.RunParallel(func(pb *testing.PB) {
		key := "key#" + strconv.Itoa(rand.Int())
		if err := c.Set(context.Background(), key, key, 0, 0); err != nil {
			b.Fatalf("failed to set key %q: %s", key, err)
		}
		for pb.Next() {
			_ = c.Set(context.Background(), key, key, 0, 0)
		}
	})
}
