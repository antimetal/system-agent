// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !linux
// +build !linux

package collectors

import "errors"

// PerfEventInfo represents information about an available perf event
type PerfEventInfo struct {
	Name        string // Human readable name
	Type        uint32 // PERF_TYPE_* constant
	Config      uint64 // Event-specific config value
	Description string // Optional description
	Available   bool   // Whether this event is actually available
	Source      string // Source of event discovery (hardware, software, pmu, tracepoint)
}

// PerfEventSummary provides statistics about discovered events (stub for non-Linux)
type PerfEventSummary struct {
	TotalEvents     int            `json:"total_events"`
	AvailableEvents int            `json:"available_events"`
	BySource        map[string]int `json:"by_source"`
	ByType          map[uint32]int `json:"by_type"`
	AvailableByType map[uint32]int `json:"available_by_type"`
}

// EnumerateAvailablePerfEvents is not available on non-Linux platforms
func EnumerateAvailablePerfEvents() ([]PerfEventInfo, error) {
	return nil, errors.New("perf event enumeration only supported on Linux")
}

// GetPerfEventSummary returns an error on non-Linux platforms
func GetPerfEventSummary() (*PerfEventSummary, error) {
	return nil, errors.New("perf event enumeration only supported on Linux")
}

// GetAvailablePerfEventNames is not available on non-Linux platforms
func GetAvailablePerfEventNames() ([]string, error) {
	return nil, errors.New("perf event enumeration only supported on Linux")
}

// FindPerfEventByName is not available on non-Linux platforms
func FindPerfEventByName(name string) (*PerfEventInfo, error) {
	return nil, errors.New("perf event enumeration only supported on Linux")
}

// isPerfEventAvailable always returns false on non-Linux platforms
func isPerfEventAvailable(eventType uint32, config uint64) bool {
	return false
}
