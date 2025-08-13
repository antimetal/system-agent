// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux
// +build linux

package collectors

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// PerfEventInfo represents information about an available perf event
type PerfEventInfo struct {
	Name        string // Human readable name (e.g., "cpu-cycles", "cache-misses")
	Type        uint32 // PERF_TYPE_* constant
	Config      uint64 // Event-specific config value
	Description string // Optional description
	Available   bool   // Whether this event is actually available
	Source      string // Source of event discovery (hardware, software, pmu, tracepoint)
}

// Hardware events as defined in linux/perf_event.h
var hardwareEvents = []PerfEventInfo{
	{Name: "cpu-cycles", Type: unix.PERF_TYPE_HARDWARE, Config: unix.PERF_COUNT_HW_CPU_CYCLES, Description: "CPU cycles", Source: "hardware"},
	{Name: "instructions", Type: unix.PERF_TYPE_HARDWARE, Config: unix.PERF_COUNT_HW_INSTRUCTIONS, Description: "Retired instructions", Source: "hardware"},
	{Name: "cache-references", Type: unix.PERF_TYPE_HARDWARE, Config: unix.PERF_COUNT_HW_CACHE_REFERENCES, Description: "Cache accesses", Source: "hardware"},
	{Name: "cache-misses", Type: unix.PERF_TYPE_HARDWARE, Config: unix.PERF_COUNT_HW_CACHE_MISSES, Description: "Cache misses", Source: "hardware"},
	{Name: "branch-instructions", Type: unix.PERF_TYPE_HARDWARE, Config: unix.PERF_COUNT_HW_BRANCH_INSTRUCTIONS, Description: "Branch instructions", Source: "hardware"},
	{Name: "branch-misses", Type: unix.PERF_TYPE_HARDWARE, Config: unix.PERF_COUNT_HW_BRANCH_MISSES, Description: "Branch mispredictions", Source: "hardware"},
	{Name: "bus-cycles", Type: unix.PERF_TYPE_HARDWARE, Config: unix.PERF_COUNT_HW_BUS_CYCLES, Description: "Bus cycles", Source: "hardware"},
}

// Software events as defined in linux/perf_event.h
var softwareEvents = []PerfEventInfo{
	{Name: "cpu-clock", Type: unix.PERF_TYPE_SOFTWARE, Config: unix.PERF_COUNT_SW_CPU_CLOCK, Description: "CPU clock timer", Source: "software"},
	{Name: "task-clock", Type: unix.PERF_TYPE_SOFTWARE, Config: unix.PERF_COUNT_SW_TASK_CLOCK, Description: "Task clock timer", Source: "software"},
	{Name: "page-faults", Type: unix.PERF_TYPE_SOFTWARE, Config: unix.PERF_COUNT_SW_PAGE_FAULTS, Description: "Page faults", Source: "software"},
	{Name: "context-switches", Type: unix.PERF_TYPE_SOFTWARE, Config: unix.PERF_COUNT_SW_CONTEXT_SWITCHES, Description: "Context switches", Source: "software"},
	{Name: "cpu-migrations", Type: unix.PERF_TYPE_SOFTWARE, Config: unix.PERF_COUNT_SW_CPU_MIGRATIONS, Description: "CPU migrations", Source: "software"},
	{Name: "minor-faults", Type: unix.PERF_TYPE_SOFTWARE, Config: unix.PERF_COUNT_SW_PAGE_FAULTS_MIN, Description: "Minor page faults", Source: "software"},
	{Name: "major-faults", Type: unix.PERF_TYPE_SOFTWARE, Config: unix.PERF_COUNT_SW_PAGE_FAULTS_MAJ, Description: "Major page faults", Source: "software"},
	{Name: "alignment-faults", Type: unix.PERF_TYPE_SOFTWARE, Config: unix.PERF_COUNT_SW_ALIGNMENT_FAULTS, Description: "Alignment faults", Source: "software"},
	{Name: "emulation-faults", Type: unix.PERF_TYPE_SOFTWARE, Config: unix.PERF_COUNT_SW_EMULATION_FAULTS, Description: "Emulation faults", Source: "software"},
	{Name: "dummy", Type: unix.PERF_TYPE_SOFTWARE, Config: unix.PERF_COUNT_SW_DUMMY, Description: "Dummy event", Source: "software"},
}

// EnumerateAvailablePerfEvents discovers which perf events are available on this system
func EnumerateAvailablePerfEvents() ([]PerfEventInfo, error) {
	var available []PerfEventInfo

	// Test hardware events
	for _, event := range hardwareEvents {
		event.Available = isPerfEventAvailable(event.Type, event.Config)
		available = append(available, event)
	}

	// Test software events (these should always be available)
	for _, event := range softwareEvents {
		event.Available = isPerfEventAvailable(event.Type, event.Config)
		available = append(available, event)
	}

	// Discover PMU events from /sys/bus/event_source/devices/
	pmuEvents, err := discoverPMUEvents()
	if err == nil {
		available = append(available, pmuEvents...)
	}

	// Note: Tracepoint events discovery is disabled for now
	// They can be re-enabled by uncommenting the lines below:
	//
	// traceEvents, err := discoverTracepointEvents()
	// if err == nil {
	//     filtered := filterTracepointsByCategory(traceEvents)
	//     available = append(available, filtered...)
	// }

	// Discover cache events (HW_CACHE type)
	cacheEvents := generateCacheEvents()
	for _, event := range cacheEvents {
		event.Available = isPerfEventAvailable(event.Type, event.Config)
		available = append(available, event)
	}

	// Sort by source, then type, then name for consistent output
	sort.Slice(available, func(i, j int) bool {
		if available[i].Source != available[j].Source {
			return available[i].Source < available[j].Source
		}
		if available[i].Type != available[j].Type {
			return available[i].Type < available[j].Type
		}
		return available[i].Name < available[j].Name
	})

	return available, nil
}

// isPerfEventAvailable tests if a specific perf event can be opened
func isPerfEventAvailable(eventType uint32, config uint64) bool {
	attr := unix.PerfEventAttr{
		Type:   eventType,
		Config: config,
		Size:   uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
	}

	// Try to open the event on CPU 0 for the current process
	fd, err := unix.PerfEventOpen(&attr, 0, -1, -1, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		return false
	}

	unix.Close(fd)
	return true
}

// discoverPMUEvents discovers PMU events from /sys/bus/event_source/devices/ and /sys/devices/
func discoverPMUEvents() ([]PerfEventInfo, error) {
	var events []PerfEventInfo

	// Try both PMU discovery paths
	pmuPaths := []string{
		"/sys/bus/event_source/devices",
		"/sys/devices", // Direct device path
	}

	for _, basePath := range pmuPaths {
		entries, err := os.ReadDir(basePath)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			deviceName := entry.Name()
			deviceDir := filepath.Join(basePath, deviceName)

			// Get PMU type
			typeFile := filepath.Join(deviceDir, "type")
			typeData, err := os.ReadFile(typeFile)
			if err != nil {
				continue
			}

			pmuType, err := strconv.ParseUint(strings.TrimSpace(string(typeData)), 10, 32)
			if err != nil {
				continue
			}

			// Discover events for this PMU
			eventsDir := filepath.Join(deviceDir, "events")
			pmuEvents, err := discoverPMUDeviceEvents(deviceName, uint32(pmuType), eventsDir)
			if err == nil {
				events = append(events, pmuEvents...)
			}
		}
	}

	return events, nil
}

// discoverPMUDeviceEvents discovers events for a specific PMU device
func discoverPMUDeviceEvents(deviceName string, pmuType uint32, eventsDir string) ([]PerfEventInfo, error) {
	var events []PerfEventInfo

	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		return events, nil
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		eventName := entry.Name()
		eventPath := filepath.Join(eventsDir, eventName)

		// Parse event definition
		event, err := parsePMUEventFile(deviceName, pmuType, eventName, eventPath)
		if err != nil {
			continue
		}

		// Test if it's available
		event.Available = isPerfEventAvailable(event.Type, event.Config)
		events = append(events, *event)
	}

	return events, nil
}

// parsePMUEventFile parses a PMU event file
func parsePMUEventFile(deviceName string, pmuType uint32, eventName, eventPath string) (*PerfEventInfo, error) {
	data, err := os.ReadFile(eventPath)
	if err != nil {
		return nil, err
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil, fmt.Errorf("empty event file")
	}

	event := &PerfEventInfo{
		Name:        fmt.Sprintf("%s/%s/", deviceName, eventName),
		Type:        pmuType,
		Description: fmt.Sprintf("%s PMU event from %s", deviceName, eventPath),
		Source:      "pmu",
	}

	// Parse event configuration
	// Format can be: "event=0x3c" or "event=0x3c,umask=0x01" or more complex
	config, err := parsePMUEventConfig(content)
	if err != nil {
		return nil, err
	}

	event.Config = config

	return event, nil
}

// parsePMUEventConfig parses PMU event configuration string
func parsePMUEventConfig(content string) (uint64, error) {
	var config uint64

	// Parse comma-separated key=value pairs
	parts := strings.Split(content, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		keyValue := strings.SplitN(part, "=", 2)
		if len(keyValue) != 2 {
			continue
		}

		key := strings.TrimSpace(keyValue[0])
		valueStr := strings.TrimSpace(keyValue[1])

		// Parse hex value
		var value uint64
		var err error
		if strings.HasPrefix(valueStr, "0x") {
			value, err = strconv.ParseUint(valueStr[2:], 16, 64)
		} else {
			value, err = strconv.ParseUint(valueStr, 10, 64)
		}
		if err != nil {
			continue
		}

		// Map common PMU event fields
		switch key {
		case "event":
			config |= value
		case "umask":
			config |= value << 8
		case "cmask":
			config |= value << 24
		case "inv":
			if value != 0 {
				config |= 1 << 23
			}
		case "any":
			if value != 0 {
				config |= 1 << 21
			}
		case "edge":
			if value != 0 {
				config |= 1 << 18
			}
			// Note: "period" and other extended config fields are ignored for now
		}
	}

	return config, nil
}

// generateCacheEvents generates hardware cache events (PERF_TYPE_HW_CACHE)
func generateCacheEvents() []PerfEventInfo {
	var events []PerfEventInfo

	// Cache types
	cacheTypes := map[string]uint64{
		"L1-dcache": 0,
		"L1-icache": 1,
		"LLC":       2,
		"dTLB":      3,
		"iTLB":      4,
		"branch":    5,
		"node":      6,
	}

	// Cache operations
	cacheOps := map[string]uint64{
		"loads":    0,
		"stores":   1,
		"prefetch": 2,
	}

	// Cache results
	cacheResults := map[string]uint64{
		"refs":   0,
		"misses": 1,
	}

	// Generate all combinations
	for cacheTypeName, cacheTypeVal := range cacheTypes {
		for cacheOpName, cacheOpVal := range cacheOps {
			for cacheResultName, cacheResultVal := range cacheResults {
				// Skip invalid combinations
				if cacheOpName == "prefetch" && cacheResultName == "misses" {
					continue
				}
				if cacheTypeName == "node" && cacheOpName != "loads" {
					continue
				}

				config := (cacheTypeVal) | (cacheOpVal << 8) | (cacheResultVal << 16)
				name := fmt.Sprintf("%s-%s-%s", cacheTypeName, cacheOpName, cacheResultName)

				event := PerfEventInfo{
					Name:        name,
					Type:        unix.PERF_TYPE_HW_CACHE,
					Config:      config,
					Description: fmt.Sprintf("Hardware cache event: %s", name),
					Source:      "cache",
				}

				events = append(events, event)
			}
		}
	}

	return events
}

// GetAvailablePerfEventNames returns just the names of available events
func GetAvailablePerfEventNames() ([]string, error) {
	events, err := EnumerateAvailablePerfEvents()
	if err != nil {
		return nil, err
	}

	var names []string
	for _, event := range events {
		if event.Available {
			names = append(names, event.Name)
		}
	}

	return names, nil
}

// FindPerfEventByName looks up a perf event by name and returns its info
func FindPerfEventByName(name string) (*PerfEventInfo, error) {
	events, err := EnumerateAvailablePerfEvents()
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate perf events: %w", err)
	}

	for _, event := range events {
		if event.Name == name {
			return &event, nil
		}
	}

	return nil, fmt.Errorf("perf event %q not found", name)
}

// PerfEventSummary provides statistics about discovered events
type PerfEventSummary struct {
	TotalEvents     int            `json:"total_events"`
	AvailableEvents int            `json:"available_events"`
	BySource        map[string]int `json:"by_source"`
	ByType          map[uint32]int `json:"by_type"`
	AvailableByType map[uint32]int `json:"available_by_type"`
}

// GetPerfEventSummary returns statistics about discovered perf events
func GetPerfEventSummary() (*PerfEventSummary, error) {
	events, err := EnumerateAvailablePerfEvents()
	if err != nil {
		return nil, err
	}

	summary := &PerfEventSummary{
		BySource:        make(map[string]int),
		ByType:          make(map[uint32]int),
		AvailableByType: make(map[uint32]int),
	}

	for _, event := range events {
		summary.TotalEvents++
		if event.Available {
			summary.AvailableEvents++
			summary.AvailableByType[event.Type]++
		}
		summary.BySource[event.Source]++
		summary.ByType[event.Type]++
	}

	return summary, nil
}

// GetEventsBySource returns events filtered by source
func GetEventsBySource(source string) ([]PerfEventInfo, error) {
	events, err := EnumerateAvailablePerfEvents()
	if err != nil {
		return nil, err
	}

	var filtered []PerfEventInfo
	for _, event := range events {
		if event.Source == source {
			filtered = append(filtered, event)
		}
	}

	return filtered, nil
}

// GetEventsByType returns events filtered by PERF_TYPE_*
func GetEventsByType(eventType uint32) ([]PerfEventInfo, error) {
	events, err := EnumerateAvailablePerfEvents()
	if err != nil {
		return nil, err
	}

	var filtered []PerfEventInfo
	for _, event := range events {
		if event.Type == eventType {
			filtered = append(filtered, event)
		}
	}

	return filtered, nil
}

// PrintEventSummary prints a human-readable summary of discovered events
func PrintEventSummary() error {
	summary, err := GetPerfEventSummary()
	if err != nil {
		return err
	}

	fmt.Printf("Perf Event Discovery Summary:\n")
	fmt.Printf("  Total events discovered: %d\n", summary.TotalEvents)
	fmt.Printf("  Available events: %d\n", summary.AvailableEvents)
	fmt.Printf("\n")

	fmt.Printf("Events by source:\n")
	for source, count := range summary.BySource {
		fmt.Printf("  %s: %d events\n", source, count)
	}
	fmt.Printf("\n")

	typeNames := map[uint32]string{
		unix.PERF_TYPE_HARDWARE:   "HARDWARE",
		unix.PERF_TYPE_SOFTWARE:   "SOFTWARE",
		unix.PERF_TYPE_TRACEPOINT: "TRACEPOINT",
		unix.PERF_TYPE_HW_CACHE:   "HW_CACHE",
		4:                         "RAW", // PERF_TYPE_RAW
	}

	fmt.Printf("Events by type:\n")
	for eventType, count := range summary.ByType {
		typeName, ok := typeNames[eventType]
		if !ok {
			typeName = fmt.Sprintf("TYPE_%d", eventType)
		}
		available := summary.AvailableByType[eventType]
		fmt.Printf("  %s: %d total, %d available\n", typeName, count, available)
	}

	return nil
}
