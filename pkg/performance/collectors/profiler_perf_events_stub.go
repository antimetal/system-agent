//go:build !linux

package collectors

import "errors"

// PerfEventInfo contains information about a perf event (stub for non-Linux)
type PerfEventInfo struct {
	Name        string
	Type        uint32
	Config      uint64
	Description string
	Category    string
}

// PerfEventSummary provides an overview of available perf events (stub for non-Linux)
type PerfEventSummary struct {
	HardwareEvents []PerfEventInfo
	SoftwareEvents []PerfEventInfo
	PMUEvents      []PerfEventInfo
	TotalCount     int
}

// EnumerateAvailablePerfEvents returns an error on non-Linux platforms
func EnumerateAvailablePerfEvents() ([]PerfEventInfo, error) {
	return nil, errors.New("perf event enumeration is only supported on Linux")
}

// GetAvailablePerfEventNames returns an error on non-Linux platforms
func GetAvailablePerfEventNames() ([]string, error) {
	return nil, errors.New("perf event enumeration is only supported on Linux")
}

// GetPerfEventSummary returns an error on non-Linux platforms
func GetPerfEventSummary() (*PerfEventSummary, error) {
	return nil, errors.New("perf event enumeration is only supported on Linux")
}

// FindPerfEventByName returns an error on non-Linux platforms
func FindPerfEventByName(name string) (*PerfEventInfo, error) {
	return nil, errors.New("perf event enumeration is only supported on Linux")
}

// isPerfEventAvailable always returns false on non-Linux platforms
func isPerfEventAvailable(eventType uint32, config uint64) bool {
	return false
}
