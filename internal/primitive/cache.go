package primitive

import (
	"container/list"
	"sync"
)

type cacheItem struct {
	key   ConfigHash
	value Value
}

// MemoCache is a deterministic bounded LRU. Evaluation happens under its lock:
// this deliberately coalesces concurrent identical cache misses into one pass.
type MemoCache struct {
	mu           sync.Mutex
	capacity     int
	items        map[ConfigHash]*list.Element
	lru          list.List
	hits, misses int
}

func NewMemoCache(capacity int) *MemoCache {
	if capacity < 1 {
		capacity = 1
	}
	return &MemoCache{capacity: capacity, items: make(map[ConfigHash]*list.Element, capacity)}
}

func (c *MemoCache) GetOrEval(key ConfigHash, f func() (Value, error)) (Value, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el := c.items[key]; el != nil {
		c.hits++
		c.lru.MoveToFront(el)
		return el.Value.(cacheItem).value, nil
	}
	c.misses++
	v, err := f()
	if err != nil {
		return Value{}, err
	}
	if c.lru.Len() == c.capacity {
		last := c.lru.Back()
		evicted := last.Value.(cacheItem).value
		delete(c.items, last.Value.(cacheItem).key)
		c.lru.Remove(last)
		if evicted.Kind == KindZone {
			releaseZones(evicted.Zone)
		}
	}
	c.items[key] = c.lru.PushFront(cacheItem{key: key, value: v})
	return v, nil
}

func (c *MemoCache) Stats() (hits, misses int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses
}
func (c *MemoCache) Len() int { c.mu.Lock(); defer c.mu.Unlock(); return c.lru.Len() }
