// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

func TestMemoryCollector(t *testing.T) {
	// Create a test meminfo file
	tempDir := t.TempDir()
	meminfoPath := filepath.Join(tempDir, "meminfo")

	meminfoContent := `MemTotal:        8192000 kB
MemFree:         1024000 kB
MemAvailable:    4096000 kB
Buffers:          256000 kB
Cached:          2048000 kB
SwapCached:       128000 kB
Active:          3072000 kB
Inactive:        2048000 kB
SwapTotal:       4096000 kB
SwapFree:        3072000 kB
Dirty:             16384 kB
Writeback:             0 kB
AnonPages:       1536000 kB
Mapped:           512000 kB
Shmem:            128000 kB
Slab:             256000 kB
SReclaimable:     128000 kB
SUnreclaim:       128000 kB
KernelStack:       16384 kB
PageTables:        32768 kB
CommitLimit:     8192000 kB
Committed_AS:    5120000 kB
VmallocTotal:   34359738367 kB
VmallocUsed:      524288 kB
HugePages_Total:       0
HugePages_Free:        0
Hugepagesize:       2048 kB
`

	err := os.WriteFile(meminfoPath, []byte(meminfoContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test meminfo file: %v", err)
	}

	// Create collector with test config
	config := performance.CollectionConfig{
		HostProcPath: tempDir,
		HostSysPath:  tempDir,
	}

	logger := logr.Discard()
	collector := NewMemoryCollector(logger, config)

	// Test collection
	ctx := context.Background()
	result, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Failed to collect memory stats: %v", err)
	}

	stats, ok := result.(*performance.MemoryStats)
	if !ok {
		t.Fatalf("Result is not *performance.MemoryStats, got %T", result)
	}

	// Verify values (converted from kB to bytes)
	tests := []struct {
		name     string
		got      uint64
		expected uint64
	}{
		{"MemTotal", stats.MemTotal, 8192000 * 1024},
		{"MemFree", stats.MemFree, 1024000 * 1024},
		{"MemAvailable", stats.MemAvailable, 4096000 * 1024},
		{"Buffers", stats.Buffers, 256000 * 1024},
		{"Cached", stats.Cached, 2048000 * 1024},
		{"SwapCached", stats.SwapCached, 128000 * 1024},
		{"Active", stats.Active, 3072000 * 1024},
		{"Inactive", stats.Inactive, 2048000 * 1024},
		{"SwapTotal", stats.SwapTotal, 4096000 * 1024},
		{"SwapFree", stats.SwapFree, 3072000 * 1024},
		{"Dirty", stats.Dirty, 16384 * 1024},
		{"Writeback", stats.Writeback, 0},
		{"AnonPages", stats.AnonPages, 1536000 * 1024},
		{"Mapped", stats.Mapped, 512000 * 1024},
		{"Shmem", stats.Shmem, 128000 * 1024},
		{"Slab", stats.Slab, 256000 * 1024},
		{"SReclaimable", stats.SReclaimable, 128000 * 1024},
		{"SUnreclaim", stats.SUnreclaim, 128000 * 1024},
		{"KernelStack", stats.KernelStack, 16384 * 1024},
		{"PageTables", stats.PageTables, 32768 * 1024},
		{"CommitLimit", stats.CommitLimit, 8192000 * 1024},
		{"CommittedAS", stats.CommittedAS, 5120000 * 1024},
		{"VmallocTotal", stats.VmallocTotal, 34359738367 * 1024},
		{"VmallocUsed", stats.VmallocUsed, 524288 * 1024},
		{"HugePages_Total", stats.HugePages_Total, 0},
		{"HugePages_Free", stats.HugePages_Free, 0},
		{"HugePagesize", stats.HugePagesize, 2048 * 1024},
	}

	for _, tc := range tests {
		if tc.got != tc.expected {
			t.Errorf("%s: got %d, expected %d", tc.name, tc.got, tc.expected)
		}
	}
}

func TestMemoryCollector_MissingFile(t *testing.T) {
	// Create collector with non-existent path
	config := performance.CollectionConfig{
		HostProcPath: "/non/existent/path",
		HostSysPath:  "/non/existent/path",
	}

	logger := logr.Discard()
	collector := NewMemoryCollector(logger, config)

	// Test collection - should return error
	ctx := context.Background()
	_, err := collector.Collect(ctx)
	if err == nil {
		t.Fatal("Expected error for missing file, got nil")
	}

	if !strings.Contains(err.Error(), "failed to open") {
		t.Errorf("Expected error to contain 'failed to open', got: %v", err)
	}
}

func TestMemoryCollector_PartialData(t *testing.T) {
	// Create a test meminfo file with only some fields
	tempDir := t.TempDir()
	meminfoPath := filepath.Join(tempDir, "meminfo")

	// Only include some fields, missing others
	meminfoContent := `MemTotal:        8192000 kB
MemFree:         1024000 kB
Buffers:          256000 kB
Cached:          2048000 kB
SwapTotal:       4096000 kB
SwapFree:        3072000 kB
`

	err := os.WriteFile(meminfoPath, []byte(meminfoContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test meminfo file: %v", err)
	}

	// Create collector with test config
	config := performance.CollectionConfig{
		HostProcPath: tempDir,
		HostSysPath:  tempDir,
	}

	logger := logr.Discard()
	collector := NewMemoryCollector(logger, config)

	// Test collection - should succeed with partial data
	ctx := context.Background()
	result, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Failed to collect memory stats: %v", err)
	}

	stats, ok := result.(*performance.MemoryStats)
	if !ok {
		t.Fatalf("Result is not *performance.MemoryStats, got %T", result)
	}

	// Verify present values
	if stats.MemTotal != 8192000*1024 {
		t.Errorf("MemTotal: got %d, expected %d", stats.MemTotal, 8192000*1024)
	}
	if stats.MemFree != 1024000*1024 {
		t.Errorf("MemFree: got %d, expected %d", stats.MemFree, 1024000*1024)
	}
	if stats.Buffers != 256000*1024 {
		t.Errorf("Buffers: got %d, expected %d", stats.Buffers, 256000*1024)
	}
	if stats.Cached != 2048000*1024 {
		t.Errorf("Cached: got %d, expected %d", stats.Cached, 2048000*1024)
	}
	if stats.SwapTotal != 4096000*1024 {
		t.Errorf("SwapTotal: got %d, expected %d", stats.SwapTotal, 4096000*1024)
	}
	if stats.SwapFree != 3072000*1024 {
		t.Errorf("SwapFree: got %d, expected %d", stats.SwapFree, 3072000*1024)
	}

	// Verify missing values are zero
	if stats.MemAvailable != 0 {
		t.Errorf("MemAvailable should be 0 when missing, got %d", stats.MemAvailable)
	}
	if stats.Active != 0 {
		t.Errorf("Active should be 0 when missing, got %d", stats.Active)
	}
	if stats.Inactive != 0 {
		t.Errorf("Inactive should be 0 when missing, got %d", stats.Inactive)
	}
	if stats.Dirty != 0 {
		t.Errorf("Dirty should be 0 when missing, got %d", stats.Dirty)
	}
}
