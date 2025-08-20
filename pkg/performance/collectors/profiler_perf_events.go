//go:build linux

package collectors

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// PerfEventInfo contains information about a perf event
type PerfEventInfo struct {
	Name        string // Human-readable name
	Type        uint32 // PERF_TYPE_* constant
	Config      uint64 // Event-specific configuration
	Description string // Description of what this measures
	Category    string // "hardware", "software", "pmu"
}

// PerfEventSummary provides an overview of available perf events
type PerfEventSummary struct {
	HardwareEvents []PerfEventInfo // Hardware PMU events
	SoftwareEvents []PerfEventInfo // Software events (virtualization-friendly)
	PMUEvents      []PerfEventInfo // Raw PMU events from /sys
	TotalCount     int             // Total number of events
}

// Hardware perf event constants (PERF_TYPE_HARDWARE)
const (
	PERF_TYPE_HARDWARE   = 0
	PERF_TYPE_SOFTWARE   = 1
	PERF_TYPE_TRACEPOINT = 2
	PERF_TYPE_HW_CACHE   = 3
	PERF_TYPE_RAW        = 4

	// Hardware event IDs
	PERF_COUNT_HW_CPU_CYCLES              = 0
	PERF_COUNT_HW_INSTRUCTIONS            = 1
	PERF_COUNT_HW_CACHE_REFERENCES        = 2
	PERF_COUNT_HW_CACHE_MISSES            = 3
	PERF_COUNT_HW_BRANCH_INSTRUCTIONS     = 4
	PERF_COUNT_HW_BRANCH_MISSES           = 5
	PERF_COUNT_HW_BUS_CYCLES              = 6
	PERF_COUNT_HW_STALLED_CYCLES_FRONTEND = 7
	PERF_COUNT_HW_STALLED_CYCLES_BACKEND  = 8
	PERF_COUNT_HW_REF_CPU_CYCLES          = 9

	// Software event IDs
	PERF_COUNT_SW_CPU_CLOCK        = 0
	PERF_COUNT_SW_TASK_CLOCK       = 1
	PERF_COUNT_SW_PAGE_FAULTS      = 2
	PERF_COUNT_SW_CONTEXT_SWITCHES = 3
	PERF_COUNT_SW_CPU_MIGRATIONS   = 4
	PERF_COUNT_SW_PAGE_FAULTS_MIN  = 5
	PERF_COUNT_SW_PAGE_FAULTS_MAJ  = 6
	PERF_COUNT_SW_ALIGNMENT_FAULTS = 7
	PERF_COUNT_SW_EMULATION_FAULTS = 8
)

// Predefined hardware events
var hardwareEvents = []PerfEventInfo{
	{
		Name:        "cpu-cycles",
		Type:        PERF_TYPE_HARDWARE,
		Config:      PERF_COUNT_HW_CPU_CYCLES,
		Description: "CPU cycles consumed by the task",
		Category:    "hardware",
	},
	{
		Name:        "instructions",
		Type:        PERF_TYPE_HARDWARE,
		Config:      PERF_COUNT_HW_INSTRUCTIONS,
		Description: "Instructions executed by the task",
		Category:    "hardware",
	},
	{
		Name:        "cache-references",
		Type:        PERF_TYPE_HARDWARE,
		Config:      PERF_COUNT_HW_CACHE_REFERENCES,
		Description: "Cache references by the task",
		Category:    "hardware",
	},
	{
		Name:        "cache-misses",
		Type:        PERF_TYPE_HARDWARE,
		Config:      PERF_COUNT_HW_CACHE_MISSES,
		Description: "Cache misses by the task",
		Category:    "hardware",
	},
	{
		Name:        "branch-instructions",
		Type:        PERF_TYPE_HARDWARE,
		Config:      PERF_COUNT_HW_BRANCH_INSTRUCTIONS,
		Description: "Branch instructions executed",
		Category:    "hardware",
	},
	{
		Name:        "branch-misses",
		Type:        PERF_TYPE_HARDWARE,
		Config:      PERF_COUNT_HW_BRANCH_MISSES,
		Description: "Branch mispredictions",
		Category:    "hardware",
	},
	{
		Name:        "bus-cycles",
		Type:        PERF_TYPE_HARDWARE,
		Config:      PERF_COUNT_HW_BUS_CYCLES,
		Description: "Bus cycles consumed",
		Category:    "hardware",
	},
	{
		Name:        "stalled-cycles-frontend",
		Type:        PERF_TYPE_HARDWARE,
		Config:      PERF_COUNT_HW_STALLED_CYCLES_FRONTEND,
		Description: "Stalled cycles during instruction fetch",
		Category:    "hardware",
	},
	{
		Name:        "stalled-cycles-backend",
		Type:        PERF_TYPE_HARDWARE,
		Config:      PERF_COUNT_HW_STALLED_CYCLES_BACKEND,
		Description: "Stalled cycles during instruction execution",
		Category:    "hardware",
	},
	{
		Name:        "ref-cycles",
		Type:        PERF_TYPE_HARDWARE,
		Config:      PERF_COUNT_HW_REF_CPU_CYCLES,
		Description: "Reference CPU cycles",
		Category:    "hardware",
	},
}

// Predefined software events (work in virtualized environments)
var softwareEvents = []PerfEventInfo{
	{
		Name:        "cpu-clock",
		Type:        PERF_TYPE_SOFTWARE,
		Config:      PERF_COUNT_SW_CPU_CLOCK,
		Description: "High-resolution CPU timer",
		Category:    "software",
	},
	{
		Name:        "task-clock",
		Type:        PERF_TYPE_SOFTWARE,
		Config:      PERF_COUNT_SW_TASK_CLOCK,
		Description: "Task clock time",
		Category:    "software",
	},
	{
		Name:        "page-faults",
		Type:        PERF_TYPE_SOFTWARE,
		Config:      PERF_COUNT_SW_PAGE_FAULTS,
		Description: "Total page faults",
		Category:    "software",
	},
	{
		Name:        "context-switches",
		Type:        PERF_TYPE_SOFTWARE,
		Config:      PERF_COUNT_SW_CONTEXT_SWITCHES,
		Description: "Context switches",
		Category:    "software",
	},
	{
		Name:        "cpu-migrations",
		Type:        PERF_TYPE_SOFTWARE,
		Config:      PERF_COUNT_SW_CPU_MIGRATIONS,
		Description: "CPU migrations",
		Category:    "software",
	},
	{
		Name:        "minor-faults",
		Type:        PERF_TYPE_SOFTWARE,
		Config:      PERF_COUNT_SW_PAGE_FAULTS_MIN,
		Description: "Minor page faults",
		Category:    "software",
	},
	{
		Name:        "major-faults",
		Type:        PERF_TYPE_SOFTWARE,
		Config:      PERF_COUNT_SW_PAGE_FAULTS_MAJ,
		Description: "Major page faults",
		Category:    "software",
	},
	{
		Name:        "alignment-faults",
		Type:        PERF_TYPE_SOFTWARE,
		Config:      PERF_COUNT_SW_ALIGNMENT_FAULTS,
		Description: "Alignment faults",
		Category:    "software",
	},
	{
		Name:        "emulation-faults",
		Type:        PERF_TYPE_SOFTWARE,
		Config:      PERF_COUNT_SW_EMULATION_FAULTS,
		Description: "Emulation faults",
		Category:    "software",
	},
}

// EnumerateAvailablePerfEvents discovers all perf events available on this system
func EnumerateAvailablePerfEvents() ([]PerfEventInfo, error) {
	var allEvents []PerfEventInfo

	// Add hardware events that are available
	for _, event := range hardwareEvents {
		if isPerfEventAvailable(event.Type, event.Config) {
			allEvents = append(allEvents, event)
		}
	}

	// Add software events (should always be available)
	for _, event := range softwareEvents {
		if isPerfEventAvailable(event.Type, event.Config) {
			allEvents = append(allEvents, event)
		}
	}

	// Add PMU events from /sys
	pmuEvents, err := enumeratePMUEvents()
	if err == nil {
		allEvents = append(allEvents, pmuEvents...)
	}

	return allEvents, nil
}

// GetAvailablePerfEventNames returns just the names of available events
func GetAvailablePerfEventNames() ([]string, error) {
	events, err := EnumerateAvailablePerfEvents()
	if err != nil {
		return nil, err
	}

	names := make([]string, len(events))
	for i, event := range events {
		names[i] = event.Name
	}

	sort.Strings(names)
	return names, nil
}

// GetPerfEventSummary returns a categorized summary of available events
func GetPerfEventSummary() (*PerfEventSummary, error) {
	events, err := EnumerateAvailablePerfEvents()
	if err != nil {
		return nil, err
	}

	summary := &PerfEventSummary{}
	for _, event := range events {
		switch event.Category {
		case "hardware":
			summary.HardwareEvents = append(summary.HardwareEvents, event)
		case "software":
			summary.SoftwareEvents = append(summary.SoftwareEvents, event)
		case "pmu":
			summary.PMUEvents = append(summary.PMUEvents, event)
		}
	}

	summary.TotalCount = len(events)
	return summary, nil
}

// FindPerfEventByName looks up an event by name with fuzzy matching
func FindPerfEventByName(name string) (*PerfEventInfo, error) {
	events, err := EnumerateAvailablePerfEvents()
	if err != nil {
		return nil, err
	}

	// Exact match first
	for _, event := range events {
		if event.Name == name {
			return &event, nil
		}
	}

	// Fuzzy match (case-insensitive, partial match)
	name = strings.ToLower(name)
	var candidates []PerfEventInfo
	for _, event := range events {
		if strings.Contains(strings.ToLower(event.Name), name) {
			candidates = append(candidates, event)
		}
	}

	if len(candidates) == 1 {
		return &candidates[0], nil
	} else if len(candidates) > 1 {
		names := make([]string, len(candidates))
		for i, c := range candidates {
			names[i] = c.Name
		}
		return nil, fmt.Errorf("multiple events match %q: %v", name, names)
	}

	// No match - provide suggestions
	suggestions := getSimilarEventNames(name, events)
	if len(suggestions) > 0 {
		return nil, fmt.Errorf("event %q not found. Similar events: %v", name, suggestions)
	}

	return nil, fmt.Errorf("event %q not found", name)
}

// isPerfEventAvailable tests if a perf event is available by trying to open it
func isPerfEventAvailable(eventType uint32, config uint64) bool {
	// Try to open the perf event to test availability
	attr := &perfEventAttr{
		Type:   eventType,
		Config: config,
		Size:   uint32(unsafe.Sizeof(perfEventAttr{})),
	}

	fd, _, _ := syscall.RawSyscall6(
		syscall.SYS_PERF_EVENT_OPEN,
		uintptr(unsafe.Pointer(attr)),
		0,           // pid (0 = current process)
		^uintptr(0), // cpu (-1 = all CPUs)
		^uintptr(0), // group_fd (-1 = no group)
		0,           // flags
		0,
	)

	if fd > 0 {
		syscall.Close(int(fd))
		return true
	}

	return false
}

// enumeratePMUEvents discovers PMU events from /sys/bus/event_source/devices/
func enumeratePMUEvents() ([]PerfEventInfo, error) {
	var events []PerfEventInfo

	// Look for CPU PMU events in /sys/devices/cpu/events/
	cpuEventsPath := "/sys/devices/cpu/events"
	if entries, err := os.ReadDir(cpuEventsPath); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			eventName := entry.Name()
			eventPath := filepath.Join(cpuEventsPath, eventName)

			// Read event configuration
			if configData, err := os.ReadFile(eventPath); err == nil {
				configStr := strings.TrimSpace(string(configData))
				if config, err := parseEventConfig(configStr); err == nil {
					events = append(events, PerfEventInfo{
						Name:        eventName,
						Type:        4, // PERF_TYPE_RAW
						Config:      config,
						Description: fmt.Sprintf("PMU event: %s", eventName),
						Category:    "pmu",
					})
				}
			}
		}
	}

	return events, nil
}

// parseEventConfig parses event configuration strings like "event=0x3c"
func parseEventConfig(configStr string) (uint64, error) {
	// Handle formats like "event=0x3c", "event=0x3c,umask=0x00"
	if strings.Contains(configStr, "event=") {
		re := regexp.MustCompile(`event=(0x[0-9a-fA-F]+)`)
		matches := re.FindStringSubmatch(configStr)
		if len(matches) > 1 {
			return strconv.ParseUint(matches[1], 0, 64)
		}
	}

	// Try parsing as hex directly
	if strings.HasPrefix(configStr, "0x") {
		return strconv.ParseUint(configStr, 0, 64)
	}

	// Try parsing as decimal
	return strconv.ParseUint(configStr, 10, 64)
}

// getSimilarEventNames returns event names similar to the query using simple string similarity
func getSimilarEventNames(query string, events []PerfEventInfo) []string {
	var suggestions []string

	for _, event := range events {
		eventName := strings.ToLower(event.Name)

		// Check for common substrings
		if len(query) >= 3 {
			if strings.Contains(eventName, query[:3]) || strings.Contains(query, eventName[:min(3, len(eventName))]) {
				suggestions = append(suggestions, event.Name)
			}
		}
	}

	// Limit suggestions to avoid overwhelming output
	if len(suggestions) > 5 {
		suggestions = suggestions[:5]
	}

	return suggestions
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// perfEventAttr represents the perf_event_attr structure for syscalls
type perfEventAttr struct {
	Type   uint32
	Size   uint32
	Config uint64
	// ... other fields omitted for brevity in this basic implementation
}
