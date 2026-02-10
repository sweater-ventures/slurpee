package app

import "sync"

type cacheEntry[V any] struct {
	value V
	found bool // distinguishes "cached miss" from "not in cache"
}

// Cache is a concurrent-safe in-memory cache with support for caching negative lookups.
type Cache[K comparable, V any] struct {
	mu    sync.RWMutex
	items map[K]cacheEntry[V]
}

func NewCache[K comparable, V any]() *Cache[K, V] {
	return &Cache[K, V]{items: make(map[K]cacheEntry[V])}
}

// Get returns (value, found, inCache). If inCache is false, the key has never been cached.
// If inCache is true and found is false, the key was cached as a miss.
func (c *Cache[K, V]) Get(key K) (V, bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, inCache := c.items[key]
	if !inCache {
		var zero V
		return zero, false, false
	}
	return entry.value, entry.found, true
}

// Set stores a value in the cache. Use found=false to cache a negative lookup.
func (c *Cache[K, V]) Set(key K, value V, found bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = cacheEntry[V]{value: value, found: found}
}

// Flush clears all entries from the cache.
func (c *Cache[K, V]) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[K]cacheEntry[V])
}
