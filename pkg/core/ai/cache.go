package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// QueryCache provides caching for AI query results
type QueryCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	ttl     time.Duration
}

type cacheEntry struct {
	value     string
	createdAt time.Time
}

// NewQueryCache creates a new query cache
func NewQueryCache(ttl time.Duration) *QueryCache {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	return &QueryCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
	}
}

// Get retrieves a cached result
func (c *QueryCache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[hashKey(key)]
	if !ok {
		return "", false
	}

	// Check if expired
	if time.Since(entry.createdAt) > c.ttl {
		return "", false
	}

	return entry.value, true
}

// Set stores a result in the cache
func (c *QueryCache) Set(key string, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[hashKey(key)] = &cacheEntry{
		value:     value,
		createdAt: time.Now(),
	}
}

// Delete removes a cached result
func (c *QueryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, hashKey(key))
}

// Clear clears all cached results
func (c *QueryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*cacheEntry)
}

// Size returns the number of cached entries
func (c *QueryCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries)
}

// CleanExpired removes expired entries
func (c *QueryCache) CleanExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	for key, entry := range c.entries {
		if time.Since(entry.createdAt) > c.ttl {
			delete(c.entries, key)
			count++
		}
	}

	return count
}

// hashKey creates a hash of the key for consistent storage
func hashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}
