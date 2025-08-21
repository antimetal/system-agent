// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build hardware

package collectors

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// TestPMU_EnumerateHardwareEvents tests hardware PMU event enumeration
func TestPMU_EnumerateHardwareEvents(t *testing.T) {
	requireBareMetal(t)

	t.Run("CPU Events", func(t *testing.T) {
		events := enumerateCPUEvents()
		require.NotEmpty(t, events, "Should find CPU PMU events")

		t.Logf("Found %d CPU PMU events:", len(events))
		for name, config := range events {
			t.Logf("  - %s: config=0x%x", name, config)
		}

		// Check for common events
		expectedEvents := []string{
			"cpu-cycles",
			"instructions",
			"cache-references",
			"cache-misses",
		}

		for _, expected := range expectedEvents {
			_, found := events[expected]
			if !found {
				t.Logf("Warning: Expected event '%s' not found", expected)
			}
		}
	})

	t.Run("PMU Capabilities", func(t *testing.T) {
		caps := getPMUCapabilities()
		
		t.Logf("PMU Capabilities:")
		t.Logf("  - Format: %+v", caps.Format)
		t.Logf("  - Type: %d", caps.Type)
		t.Logf("  - Caps: %+v", caps.Caps)

		// Verify basic capabilities
		assert.NotEmpty(t, caps.Format, "Should have PMU format descriptors")
	})

	t.Run("Cache Events", func(t *testing.T) {
		cacheEvents := enumerateCacheEvents()
		
		if len(cacheEvents) > 0 {
			t.Logf("Found %d cache-specific events:", len(cacheEvents))
			for name, config := range cacheEvents {
				t.Logf("  - %s: config=0x%x", name, config)
			}
		} else {
			t.Log("No cache-specific PMU events found (may not be supported)")
		}
	})

	t.Run("Uncore Events", func(t *testing.T) {
		uncoreEvents := enumerateUncoreEvents()
		
		if len(uncoreEvents) > 0 {
			t.Logf("Found %d uncore PMU devices:", len(uncoreEvents))
			for device, events := range uncoreEvents {
				t.Logf("  Device: %s (%d events)", device, len(events))
				// Show first few events
				count := 0
				for name := range events {
					if count < 5 {
						t.Logf("    - %s", name)
						count++
					}
				}
				if len(events) > 5 {
					t.Logf("    ... and %d more", len(events)-5)
				}
			}
		} else {
			t.Log("No uncore PMU events found (may not be supported on this CPU)")
		}
	})
}

// TestPMU_OpenHardwareEvents tests opening various hardware events
func TestPMU_OpenHardwareEvents(t *testing.T) {
	requireBareMetal(t)

	testCases := []struct {
		name   string
		typ    uint32
		config uint64
	}{
		{
			name:   "CPU Cycles",
			typ:    unix.PERF_TYPE_HARDWARE,
			config: unix.PERF_COUNT_HW_CPU_CYCLES,
		},
		{
			name:   "Instructions",
			typ:    unix.PERF_TYPE_HARDWARE,
			config: unix.PERF_COUNT_HW_INSTRUCTIONS,
		},
		{
			name:   "Cache References",
			typ:    unix.PERF_TYPE_HARDWARE,
			config: unix.PERF_COUNT_HW_CACHE_REFERENCES,
		},
		{
			name:   "Branch Instructions",
			typ:    unix.PERF_TYPE_HARDWARE,
			config: unix.PERF_COUNT_HW_BRANCH_INSTRUCTIONS,
		},
		{
			name:   "Branch Misses",
			typ:    unix.PERF_TYPE_HARDWARE,
			config: unix.PERF_COUNT_HW_BRANCH_MISSES,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			attr := unix.PerfEventAttr{
				Type:   tc.typ,
				Config: tc.config,
				Size:   uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
				Sample: 1000000, // Sample period
			}

			// Try to open on CPU 0
			fd, err := unix.PerfEventOpen(&attr, -1, 0, -1, unix.PERF_FLAG_FD_CLOEXEC)
			if err != nil {
				t.Logf("Failed to open %s: %v (may not be supported)", tc.name, err)
				return
			}
			defer unix.Close(fd)

			t.Logf("Successfully opened %s (fd=%d)", tc.name, fd)

			// Try to read counter value
			var count uint64
			_, err = unix.Read(fd, (*[8]byte)(unsafe.Pointer(&count))[:])
			if err == nil {
				t.Logf("  Initial count: %d", count)
			}
		})
	}
}

// TestPMU_RawEvents tests raw PMU event configuration
func TestPMU_RawEvents(t *testing.T) {
	requireBareMetal(t)

	// Get CPU PMU type
	pmuType := getCPUPMUType()
	if pmuType == 0 {
		t.Skip("Could not determine CPU PMU type")
	}

	t.Logf("CPU PMU type: %d", pmuType)

	// Try some raw events (these are x86_64 specific)
	if runtime.GOARCH == "amd64" {
		t.Run("Raw x86 Events", func(t *testing.T) {
			// Example: L1D cache load misses (event 0x51, umask 0x01)
			rawConfig := uint64(0x0151)
			
			attr := unix.PerfEventAttr{
				Type:   pmuType,
				Config: rawConfig,
				Size:   uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
				Sample: 1000000,
			}

			fd, err := unix.PerfEventOpen(&attr, -1, 0, -1, unix.PERF_FLAG_FD_CLOEXEC)
			if err != nil {
				t.Logf("Failed to open raw event 0x%x: %v", rawConfig, err)
				return
			}
			defer unix.Close(fd)

			t.Logf("Successfully opened raw event 0x%x", rawConfig)
		})
	}
}

// TestPMU_GroupedEvents tests event group functionality
func TestPMU_GroupedEvents(t *testing.T) {
	requireBareMetal(t)

	// Create a group leader (cycles)
	leaderAttr := unix.PerfEventAttr{
		Type:   unix.PERF_TYPE_HARDWARE,
		Config: unix.PERF_COUNT_HW_CPU_CYCLES,
		Size:   uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
	}

	leaderFd, err := unix.PerfEventOpen(&leaderAttr, -1, 0, -1, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		t.Skipf("Failed to create group leader: %v", err)
	}
	defer unix.Close(leaderFd)

	// Add member to group (instructions)
	memberAttr := unix.PerfEventAttr{
		Type:   unix.PERF_TYPE_HARDWARE,
		Config: unix.PERF_COUNT_HW_INSTRUCTIONS,
		Size:   uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
	}

	memberFd, err := unix.PerfEventOpen(&memberAttr, -1, 0, leaderFd, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		t.Logf("Failed to add group member: %v", err)
		return
	}
	defer unix.Close(memberFd)

	t.Log("Successfully created PMU event group (cycles + instructions)")
	
	// Enable the group
	err = unix.IoctlSetInt(leaderFd, unix.PERF_EVENT_IOC_ENABLE, 0)
	assert.NoError(t, err, "Failed to enable event group")
	
	// Do some work
	sum := 0
	for i := 0; i < 1000000; i++ {
		sum += i
	}
	_ = sum
	
	// Disable the group
	err = unix.IoctlSetInt(leaderFd, unix.PERF_EVENT_IOC_DISABLE, 0)
	assert.NoError(t, err, "Failed to disable event group")
}

// Helper functions for PMU enumeration

// Note: detectPMUDevice is defined in profiler_hardware_test.go

type PMUCapabilities struct {
	Type   uint32
	Format map[string]string
	Events map[string]uint64
	Caps   map[string]string
}

func getPMUCapabilities() PMUCapabilities {
	caps := PMUCapabilities{
		Format: make(map[string]string),
		Events: make(map[string]uint64),
		Caps:   make(map[string]string),
	}

	// Detect PMU device
	pmuDevice := detectPMUDevice()
	if pmuDevice == "" {
		return caps
	}

	// Read PMU type
	typePath := fmt.Sprintf("/sys/bus/event_source/devices/%s/type", pmuDevice)
	if data, err := os.ReadFile(typePath); err == nil {
		if typ, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32); err == nil {
			caps.Type = uint32(typ)
		}
	}

	// Read format descriptors
	formatPath := fmt.Sprintf("/sys/bus/event_source/devices/%s/format", pmuDevice)
	if entries, err := os.ReadDir(formatPath); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				path := filepath.Join(formatPath, entry.Name())
				if data, err := os.ReadFile(path); err == nil {
					caps.Format[entry.Name()] = strings.TrimSpace(string(data))
				}
			}
		}
	}

	// Read capabilities
	capsPath := fmt.Sprintf("/sys/bus/event_source/devices/%s/caps", pmuDevice)
	if entries, err := os.ReadDir(capsPath); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				path := filepath.Join(capsPath, entry.Name())
				if data, err := os.ReadFile(path); err == nil {
					caps.Caps[entry.Name()] = strings.TrimSpace(string(data))
				}
			}
		}
	}

	return caps
}

func enumerateCPUEvents() map[string]uint64 {
	events := make(map[string]uint64)
	
	// Detect PMU device
	pmuDevice := detectPMUDevice()
	if pmuDevice == "" {
		return events
	}
	
	eventsPath := fmt.Sprintf("/sys/bus/event_source/devices/%s/events", pmuDevice)

	entries, err := os.ReadDir(eventsPath)
	if err != nil {
		return events
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			path := filepath.Join(eventsPath, entry.Name())
			if data, err := os.ReadFile(path); err == nil {
				// Parse event configuration
				configStr := strings.TrimSpace(string(data))
				if config := parseEventConfig(configStr); config != 0 {
					events[entry.Name()] = config
				}
			}
		}
	}

	return events
}

func enumerateCacheEvents() map[string]uint64 {
	events := make(map[string]uint64)
	
	// Look for cache-specific PMU
	devicesPath := "/sys/bus/event_source/devices"
	entries, err := os.ReadDir(devicesPath)
	if err != nil {
		return events
	}

	for _, entry := range entries {
		if strings.Contains(entry.Name(), "cache") {
			eventsPath := filepath.Join(devicesPath, entry.Name(), "events")
			if subEntries, err := os.ReadDir(eventsPath); err == nil {
				for _, subEntry := range subEntries {
					if !subEntry.IsDir() {
						path := filepath.Join(eventsPath, subEntry.Name())
						if data, err := os.ReadFile(path); err == nil {
							configStr := strings.TrimSpace(string(data))
							if config := parseEventConfig(configStr); config != 0 {
								events[fmt.Sprintf("%s/%s", entry.Name(), subEntry.Name())] = config
							}
						}
					}
				}
			}
		}
	}

	return events
}

func enumerateUncoreEvents() map[string]map[string]uint64 {
	uncoreEvents := make(map[string]map[string]uint64)
	
	devicesPath := "/sys/bus/event_source/devices"
	entries, err := os.ReadDir(devicesPath)
	if err != nil {
		return uncoreEvents
	}

	for _, entry := range entries {
		if strings.Contains(entry.Name(), "uncore") {
			deviceEvents := make(map[string]uint64)
			eventsPath := filepath.Join(devicesPath, entry.Name(), "events")
			
			if subEntries, err := os.ReadDir(eventsPath); err == nil {
				for _, subEntry := range subEntries {
					if !subEntry.IsDir() {
						path := filepath.Join(eventsPath, subEntry.Name())
						if data, err := os.ReadFile(path); err == nil {
							configStr := strings.TrimSpace(string(data))
							if config := parseEventConfig(configStr); config != 0 {
								deviceEvents[subEntry.Name()] = config
							}
						}
					}
				}
			}
			
			if len(deviceEvents) > 0 {
				uncoreEvents[entry.Name()] = deviceEvents
			}
		}
	}

	return uncoreEvents
}

func getCPUPMUType() uint32 {
	// Detect PMU device
	pmuDevice := detectPMUDevice()
	if pmuDevice == "" {
		return 0
	}

	typePath := fmt.Sprintf("/sys/bus/event_source/devices/%s/type", pmuDevice)
	data, err := os.ReadFile(typePath)
	if err != nil {
		return 0
	}

	typ, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
	if err != nil {
		return 0
	}

	return uint32(typ)
}

func parseEventConfig(configStr string) uint64 {
	// Parse event configuration string
	// Format: "event=0xNN,umask=0xNN" or "config=0xNNNN" or just "0xNNNN"
	
	configStr = strings.TrimSpace(configStr)
	
	// Direct hex value
	if strings.HasPrefix(configStr, "0x") {
		if val, err := strconv.ParseUint(configStr[2:], 16, 64); err == nil {
			return val
		}
	}

	// config=value format
	if strings.HasPrefix(configStr, "config=") {
		valStr := strings.TrimPrefix(configStr, "config=")
		if strings.HasPrefix(valStr, "0x") {
			if val, err := strconv.ParseUint(valStr[2:], 16, 64); err == nil {
				return val
			}
		} else {
			if val, err := strconv.ParseUint(valStr, 10, 64); err == nil {
				return val
			}
		}
	}

	// event=0xNN,umask=0xNN format
	var event, umask uint64
	parts := strings.Split(configStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "event=") {
			valStr := strings.TrimPrefix(part, "event=")
			if strings.HasPrefix(valStr, "0x") {
				event, _ = strconv.ParseUint(valStr[2:], 16, 64)
			} else {
				event, _ = strconv.ParseUint(valStr, 10, 64)
			}
		} else if strings.HasPrefix(part, "umask=") {
			valStr := strings.TrimPrefix(part, "umask=")
			if strings.HasPrefix(valStr, "0x") {
				umask, _ = strconv.ParseUint(valStr[2:], 16, 64)
			} else {
				umask, _ = strconv.ParseUint(valStr, 10, 64)
			}
		}
	}

	if event != 0 || umask != 0 {
		return (umask << 8) | event
	}

	return 0
}