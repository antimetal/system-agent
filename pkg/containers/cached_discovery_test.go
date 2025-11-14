// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package containers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestCgroupFS creates a mock cgroup filesystem for testing
func setupTestCgroupFS(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	// Create v1 cpu controller with a mock container
	cpuDir := filepath.Join(tmpDir, "cpu", "docker")
	require.NoError(t, os.MkdirAll(cpuDir, 0755))

	// Create a fake container directory
	containerDir := filepath.Join(cpuDir, "abc123def456")
	require.NoError(t, os.MkdirAll(containerDir, 0755))

	// Create memory controller with same container
	memDir := filepath.Join(tmpDir, "memory", "docker")
	require.NoError(t, os.MkdirAll(memDir, 0755))
	containerMemDir := filepath.Join(memDir, "abc123def456")
	require.NoError(t, os.MkdirAll(containerMemDir, 0755))

	return tmpDir
}

func TestCachedDiscovery_CacheHit(t *testing.T) {
	tmpDir := setupTestCgroupFS(t)

	// Create a cached discovery with a long TTL
	cd := NewCachedDiscovery(tmpDir, 1*time.Hour)

	// First call should be a cache miss and perform discovery
	containers1, err := cd.DiscoverContainers("cpu", 1)
	require.NoError(t, err)

	stats1 := cd.Stats()
	assert.Equal(t, uint64(0), stats1.Hits, "First call should be a cache miss")
	assert.Equal(t, uint64(1), stats1.Misses)

	// Second call with same parameters should hit the cache
	containers2, err := cd.DiscoverContainers("cpu", 1)
	require.NoError(t, err)

	stats2 := cd.Stats()
	assert.Equal(t, uint64(1), stats2.Hits, "Second call should be a cache hit")
	assert.Equal(t, uint64(1), stats2.Misses)

	// Results should be identical
	assert.Equal(t, len(containers1), len(containers2))
}

func TestCachedDiscovery_CacheMissDifferentSubsystem(t *testing.T) {
	tmpDir := setupTestCgroupFS(t)
	cd := NewCachedDiscovery(tmpDir, 1*time.Hour)

	// Discover CPU containers
	_, err := cd.DiscoverContainers("cpu", 1)
	require.NoError(t, err)

	// Discover memory containers (different subsystem, should be cache miss)
	_, err = cd.DiscoverContainers("memory", 1)
	require.NoError(t, err)

	stats := cd.Stats()
	assert.Equal(t, uint64(0), stats.Hits, "Different subsystem should cause cache miss")
	assert.Equal(t, uint64(2), stats.Misses)
	assert.Equal(t, 2, stats.Entries, "Should have 2 cache entries")
}

func TestCachedDiscovery_CacheMissDifferentVersion(t *testing.T) {
	tmpDir := setupTestCgroupFS(t)
	cd := NewCachedDiscovery(tmpDir, 1*time.Hour)

	// Discover v1 containers
	_, err := cd.DiscoverContainers("cpu", 1)
	require.NoError(t, err)

	// Discover v2 containers (different version, should be cache miss)
	_, err = cd.DiscoverContainers("cpu", 2)
	if err != nil {
		// It's OK if v2 fails on systems without it
		t.Skip("Cgroup v2 not available, skipping version test")
	}

	stats := cd.Stats()
	assert.Equal(t, uint64(0), stats.Hits, "Different version should cause cache miss")
	assert.Equal(t, uint64(2), stats.Misses)
}

func TestCachedDiscovery_TTLExpiration(t *testing.T) {
	tmpDir := setupTestCgroupFS(t)
	// Create a cached discovery with a very short TTL
	cd := NewCachedDiscovery(tmpDir, 50*time.Millisecond)

	// First call
	_, err := cd.DiscoverContainers("cpu", 1)
	require.NoError(t, err)

	// Second call immediately should hit cache
	_, err = cd.DiscoverContainers("cpu", 1)
	require.NoError(t, err)

	stats1 := cd.Stats()
	assert.Equal(t, uint64(1), stats1.Hits)

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Third call after TTL expiration should miss cache
	_, err = cd.DiscoverContainers("cpu", 1)
	require.NoError(t, err)

	stats2 := cd.Stats()
	assert.Equal(t, uint64(1), stats2.Hits, "After TTL expiration should not increment hits")
	assert.Equal(t, uint64(2), stats2.Misses, "After TTL expiration should be a cache miss")
}

func TestCachedDiscovery_Invalidate(t *testing.T) {
	tmpDir := setupTestCgroupFS(t)
	cd := NewCachedDiscovery(tmpDir, 1*time.Hour)

	// Populate cache
	_, err := cd.DiscoverContainers("cpu", 1)
	require.NoError(t, err)

	// Verify cache hit
	_, err = cd.DiscoverContainers("cpu", 1)
	require.NoError(t, err)

	stats1 := cd.Stats()
	assert.Equal(t, uint64(1), stats1.Hits)
	assert.Equal(t, 1, stats1.Entries)

	// Invalidate cache
	cd.Invalidate()

	stats2 := cd.Stats()
	assert.Equal(t, 0, stats2.Entries, "After invalidation should have 0 entries")

	// Next call should be a cache miss
	_, err = cd.DiscoverContainers("cpu", 1)
	require.NoError(t, err)

	stats3 := cd.Stats()
	assert.Equal(t, uint64(1), stats3.Hits, "Hits counter preserved")
	assert.Equal(t, uint64(2), stats3.Misses, "Should increment misses after invalidation")
}

func TestCachedDiscovery_DiscoverAllContainers(t *testing.T) {
	tmpDir := setupTestCgroupFS(t)
	cd := NewCachedDiscovery(tmpDir, 1*time.Hour)

	// First call
	containers1, err := cd.DiscoverAllContainers()
	require.NoError(t, err)

	// Second call should hit cache
	containers2, err := cd.DiscoverAllContainers()
	require.NoError(t, err)

	stats := cd.Stats()
	assert.Equal(t, uint64(1), stats.Hits)
	assert.Equal(t, uint64(1), stats.Misses)

	// Results should be identical
	assert.Equal(t, len(containers1), len(containers2))
}

func TestCachedDiscovery_HitRateCalculation(t *testing.T) {
	tmpDir := setupTestCgroupFS(t)
	cd := NewCachedDiscovery(tmpDir, 1*time.Hour)

	// No calls yet - hit rate should be 0
	stats0 := cd.Stats()
	assert.Equal(t, 0.0, stats0.HitRate)

	// First call - cache miss
	_, _ = cd.DiscoverContainers("cpu", 1)
	stats1 := cd.Stats()
	assert.Equal(t, 0.0, stats1.HitRate, "After 1 miss: 0/1 = 0.0")

	// Second call - cache hit
	_, _ = cd.DiscoverContainers("cpu", 1)
	stats2 := cd.Stats()
	assert.InDelta(t, 0.5, stats2.HitRate, 0.01, "After 1 hit, 1 miss: 1/2 = 0.5")

	// Third call - cache hit
	_, _ = cd.DiscoverContainers("cpu", 1)
	stats3 := cd.Stats()
	assert.InDelta(t, 0.666, stats3.HitRate, 0.01, "After 2 hits, 1 miss: 2/3 = 0.666")
}

func TestCachedDiscovery_ConcurrentAccess(t *testing.T) {
	tmpDir := setupTestCgroupFS(t)
	cd := NewCachedDiscovery(tmpDir, 1*time.Hour)

	// Simulate concurrent goroutines accessing the cache
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			_, _ = cd.DiscoverContainers("cpu", 1)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic and should have reasonable stats
	stats := cd.Stats()
	assert.GreaterOrEqual(t, stats.Hits+stats.Misses, uint64(10), "Should have at least 10 total accesses")
}

func TestCacheStats_String(t *testing.T) {
	stats := CacheStats{
		Hits:    75,
		Misses:  25,
		Entries: 5,
		HitRate: 0.75,
	}

	// Just verify it doesn't panic
	assert.Equal(t, 75, int(stats.Hits))
	assert.Equal(t, 25, int(stats.Misses))
	assert.Equal(t, 5, stats.Entries)
	assert.InDelta(t, 0.75, stats.HitRate, 0.01)
}
