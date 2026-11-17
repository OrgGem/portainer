package resourcemetrics

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type Sample struct {
	Data []byte
	At   time.Time
}
type entry struct {
	Sample
	expires time.Time
}
type Cache struct {
	mu      sync.Mutex
	entries map[string]entry
	flight  singleflight.Group
	slots   chan struct{}
}

func NewCache() *Cache { return &Cache{entries: map[string]entry{}, slots: make(chan struct{}, 6)} }

func (c *Cache) fresh(ctx context.Context, fetch func() ([]byte, error)) (Sample, error) {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return Sample{}, ctx.Err()
	}
	data, err := fetch()
	return Sample{Data: data, At: time.Now().UTC()}, err
}

// Only measurements are cached. Callers must authorize the environment and
// resource on EVERY request, before entering this cache.
func (c *Cache) Get(ctx context.Context, key string, ttl time.Duration, fetch func() ([]byte, error)) (Sample, error) {
	if ttl <= 0 {
		return c.fresh(ctx, fetch)
	}
	get := func() (Sample, bool) {
		c.mu.Lock()
		defer c.mu.Unlock()
		e, ok := c.entries[key]
		return e.Sample, ok && time.Now().Before(e.expires)
	}
	if s, ok := get(); ok {
		return s, nil
	}
	result := c.flight.DoChan(key, func() (any, error) {
		if s, ok := get(); ok {
			return s, nil
		}
		s, err := c.fresh(ctx, fetch)
		if err != nil {
			return Sample{}, err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		size := len(s.Data)
		now := time.Now()
		for k, e := range c.entries {
			if now.After(e.expires) {
				delete(c.entries, k)
			} else {
				size += len(e.Data)
			}
		}
		if len(c.entries) >= 256 || size > 32<<20 {
			clear(c.entries)
		}
		if len(s.Data) <= 8<<20 {
			c.entries[key] = entry{s, now.Add(ttl)}
		}
		return s, nil
	})
	select {
	case <-ctx.Done():
		return Sample{}, ctx.Err()
	case r := <-result:
		if r.Err != nil {
			return Sample{}, r.Err
		}
		return r.Val.(Sample), nil
	}
}
