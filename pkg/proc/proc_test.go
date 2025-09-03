// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package proc_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/antimetal/agent/pkg/proc"
)

func TestBootTime(t *testing.T) {
	const numGoroutines = 10
	results := make(chan time.Time, numGoroutines)
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bt, err := proc.BootTime()
			assert.NoError(t, err)
			results <- bt
		}()
	}

	wg.Wait()
	close(results)

	var bootTimes []time.Time
	for bt := range results {
		bootTimes = append(bootTimes, bt)
	}

	// All results should be identical
	require.Greater(t, len(bootTimes), 0, "Should have at least one result")
	expectedBootTime := bootTimes[0]
	assert.False(t, expectedBootTime.IsZero(), "Boot time should not be zero")

	for i, bt := range bootTimes {
		assert.Equal(t, expectedBootTime, bt, "Concurrent call %d should return same boot time", i)
	}

	t.Logf("System boot time: %v", expectedBootTime)
}

func TestUserHZ(t *testing.T) {
	const numGoroutines = 10
	results := make(chan int64, numGoroutines)
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hz, err := proc.UserHZ()
			assert.NoError(t, err)
			results <- hz
		}()
	}

	wg.Wait()
	close(results)

	var userHZValues []int64
	for hz := range results {
		userHZValues = append(userHZValues, hz)
	}

	// All results should be identical
	require.Greater(t, len(userHZValues), 0, "Should have at least one result")
	expectedUserHZ := userHZValues[0]
	assert.Greater(t, expectedUserHZ, int64(0), "USER_HZ should be positive")

	for i, hz := range userHZValues {
		assert.Equal(t, expectedUserHZ, hz, "Concurrent call %d should return same USER_HZ", i)
	}

	t.Logf("USER_HZ: %d", expectedUserHZ)
}

func TestPageSize(t *testing.T) {
	const numGoroutines = 10
	results := make(chan int64, numGoroutines)
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ps, err := proc.PageSize()
			assert.NoError(t, err)
			results <- ps
		}()
	}

	wg.Wait()
	close(results)

	var pageSizes []int64
	for ps := range results {
		pageSizes = append(pageSizes, ps)
	}

	// All results should be identical
	require.Greater(t, len(pageSizes), 0, "Should have at least one result")
	expectedPageSize := pageSizes[0]
	assert.Greater(t, expectedPageSize, int64(0), "Page size should be positive")
	assert.True(t, isPowerOfTwo(expectedPageSize), "Page size (%d) should be a power of 2", expectedPageSize)

	for i, ps := range pageSizes {
		assert.Equal(t, expectedPageSize, ps, "Concurrent call %d should return same page size", i)
	}

	t.Logf("Page size: %d bytes (%d KB)", expectedPageSize, expectedPageSize/1024)
}

func TestInvalidProcPath(t *testing.T) {
	invalidPath := "/nonexistent/proc"

	// BootTime should fail with invalid path
	_, err := proc.BootTime(invalidPath)
	assert.Error(t, err, "BootTime should fail with invalid proc path")

	// UserHZ should fallback to default value with invalid path
	userHZ, err := proc.UserHZ(invalidPath)
	assert.NoError(t, err, "UserHZ should not fail with invalid path (has fallback)")
	assert.Equal(t, int64(100), userHZ, "UserHZ should fallback to 100")

	// PageSize should fallback to default value with invalid path
	pageSize, err := proc.PageSize(invalidPath)
	assert.NoError(t, err, "PageSize should not fail with invalid path (has fallback)")
	assert.Equal(t, int64(4096), pageSize, "PageSize should fallback to 4096")
}

func TestMultiplePathsError(t *testing.T) {
	// Test that multiple paths return an error
	_, err := proc.BootTime("/proc", "/another/proc")
	assert.Error(t, err, "BootTime should fail with multiple paths")

	_, err = proc.UserHZ("/proc", "/another/proc")
	assert.Error(t, err, "UserHZ should fail with multiple paths")

	_, err = proc.PageSize("/proc", "/another/proc")
	assert.Error(t, err, "PageSize should fail with multiple paths")
}

// Helper function to check if a number is a power of 2
func isPowerOfTwo(n int64) bool {
	return n > 0 && (n&(n-1)) == 0
}
