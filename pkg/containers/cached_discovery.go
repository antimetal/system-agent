// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package containers

import (
	"sync"
	"time"
)

// CachedDiscovery wraps a Discovery instance with a caching layer to reduce
// filesystem operations when discovering containers repeatedly.
//
// The cache stores discovered containers per subsystem/version and invalidates
// after a configurable TTL. This is particularly beneficial when multiple collectors
// (CPU, memory) discover the same containers on every collection cycle.
type CachedDiscovery struct {
	discovery *Discovery
	cache     *pathCache
}

// pathCache stores discovered containers with TTL-based expiration
type pathCache struct {
	mu      sync.RWMutex
	entries map[cacheKey]cacheEntry
	ttl     time.Duration

	// Metrics for observability
	hits   uint64
	misses uint64
}

type cacheKey struct {
	subsystem string
	version   int
}

type cacheEntry struct {
	containers []Container
	cachedAt   time.Time
}

// NewCachedDiscovery creates a new cached container discovery instance.
// The ttl parameter controls how long discovered containers are cached before
// a fresh discovery is performed. A typical value is 30 seconds.
func NewCachedDiscovery(cgroupPath string, ttl time.Duration) *CachedDiscovery {
	return &CachedDiscovery{
		discovery: NewDiscovery(cgroupPath),
		cache: &pathCache{
			entries: make(map[cacheKey]cacheEntry),
			ttl:     ttl,
		},
	}
}

// DetectCgroupVersion delegates to the underlying discovery
func (cd *CachedDiscovery) DetectCgroupVersion() (int, error) {
	return cd.discovery.DetectCgroupVersion()
}

// DiscoverContainers performs cached container discovery.
// If valid cached results exist, they are returned immediately.
// Otherwise, a fresh discovery is performed and the cache is updated.
func (cd *CachedDiscovery) DiscoverContainers(subsystem string, version int) ([]Container, error) {
	key := cacheKey{subsystem: subsystem, version: version}

	// Try cache first
	if containers, ok := cd.cache.get(key); ok {
		return containers, nil
	}

	// Cache miss - perform fresh discovery
	containers, err := cd.discovery.DiscoverContainers(subsystem, version)
	if err != nil {
		return nil, err
	}

	// Update cache
	cd.cache.set(key, containers)

	return containers, nil
}

// DiscoverAllContainers performs cached discovery across all cgroup versions.
// This method uses a special cache key to store the combined results.
func (cd *CachedDiscovery) DiscoverAllContainers() ([]Container, error) {
	// Use a special key for "all" discovery
	key := cacheKey{subsystem: "_all", version: 0}

	// Try cache first
	if containers, ok := cd.cache.get(key); ok {
		return containers, nil
	}

	// Cache miss - perform fresh discovery
	containers, err := cd.discovery.DiscoverAllContainers()
	if err != nil {
		return nil, err
	}

	// Update cache
	cd.cache.set(key, containers)

	return containers, nil
}

// Invalidate clears the entire cache, forcing the next discovery to scan the filesystem.
// This is useful when you know containers have been created or destroyed.
func (cd *CachedDiscovery) Invalidate() {
	cd.cache.invalidate()
}

// Stats returns cache performance statistics
func (cd *CachedDiscovery) Stats() CacheStats {
	return cd.cache.stats()
}

// get retrieves containers from cache if they exist and haven't expired
func (pc *pathCache) get(key cacheKey) ([]Container, bool) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	entry, exists := pc.entries[key]
	if !exists {
		pc.misses++
		return nil, false
	}

	// Check if entry has expired
	if time.Since(entry.cachedAt) > pc.ttl {
		pc.misses++
		return nil, false
	}

	pc.hits++
	return entry.containers, true
}

// set stores containers in the cache with the current timestamp
func (pc *pathCache) set(key cacheKey, containers []Container) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	pc.entries[key] = cacheEntry{
		containers: containers,
		cachedAt:   time.Now(),
	}
}

// invalidate clears all cache entries
func (pc *pathCache) invalidate() {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	pc.entries = make(map[cacheKey]cacheEntry)
}

// stats returns cache performance metrics
func (pc *pathCache) stats() CacheStats {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	return CacheStats{
		Hits:    pc.hits,
		Misses:  pc.misses,
		Entries: len(pc.entries),
		HitRate: calculateHitRate(pc.hits, pc.misses),
	}
}

// CacheStats provides visibility into cache performance
type CacheStats struct {
	Hits    uint64  // Number of successful cache hits
	Misses  uint64  // Number of cache misses (expired or not present)
	Entries int     // Current number of cached entries
	HitRate float64 // Cache hit rate (0.0 to 1.0)
}

func calculateHitRate(hits, misses uint64) float64 {
	total := hits + misses
	if total == 0 {
		return 0.0
	}
	return float64(hits) / float64(total)
}
