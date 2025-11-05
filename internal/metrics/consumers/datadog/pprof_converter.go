// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package datadog

import (
	"fmt"
	"time"

	"github.com/google/pprof/profile"

	"github.com/antimetal/agent/internal/metrics"
	"github.com/antimetal/agent/pkg/performance"
)

// ConvertToPprof converts ProfileStats to pprof format
func ConvertToPprof(event metrics.MetricEvent, config Config) (*profile.Profile, error) {
	// Type assert to ProfileStats
	profileStats, ok := event.Data.(*performance.ProfileStats)
	if !ok {
		return nil, fmt.Errorf("expected *performance.ProfileStats, got %T", event.Data)
	}

	// Create pprof profile
	prof := &profile.Profile{
		TimeNanos:     profileStats.CollectionTime.UnixNano(),
		DurationNanos: profileStats.Duration.Nanoseconds(),
		PeriodType: &profile.ValueType{
			Type: "cpu",
			Unit: "nanoseconds",
		},
		Period: int64(profileStats.SamplePeriod),
	}

	// Add sample type based on event
	sampleType := &profile.ValueType{
		Type: "samples",
		Unit: "count",
	}
	prof.SampleType = []*profile.ValueType{sampleType}

	// Location cache: address -> *profile.Location
	locationCache := make(map[uint64]*profile.Location)

	// Function cache: address -> *profile.Function
	functionCache := make(map[uint64]*profile.Function)

	// Helper to get or create location
	getLocation := func(addr uint64, isKernel bool) *profile.Location {
		if loc, exists := locationCache[addr]; exists {
			return loc
		}

		// Create function for this address
		var fn *profile.Function
		if existingFn, exists := functionCache[addr]; exists {
			fn = existingFn
		} else {
			// Create placeholder function name
			funcName := fmt.Sprintf("0x%x", addr)
			if isKernel {
				funcName = fmt.Sprintf("[kernel] 0x%x", addr)
			}

			fn = &profile.Function{
				ID:   uint64(len(prof.Function) + 1),
				Name: funcName,
			}
			prof.Function = append(prof.Function, fn)
			functionCache[addr] = fn
		}

		// Create location
		loc := &profile.Location{
			ID:      uint64(len(prof.Location) + 1),
			Address: addr,
			Line: []profile.Line{
				{Function: fn},
			},
		}
		prof.Location = append(prof.Location, loc)
		locationCache[addr] = loc
		return loc
	}

	// Convert each stack to a pprof sample
	for _, stack := range profileStats.Stacks {
		var locations []*profile.Location

		// Add user stack (reversed for pprof - leaf first)
		for i := len(stack.UserStack) - 1; i >= 0; i-- {
			addr := stack.UserStack[i]
			if addr == 0 {
				break
			}
			loc := getLocation(addr, false)
			locations = append(locations, loc)
		}

		// Add kernel stack
		for i := len(stack.KernelStack) - 1; i >= 0; i-- {
			addr := stack.KernelStack[i]
			if addr == 0 {
				break
			}
			loc := getLocation(addr, true)
			locations = append(locations, loc)
		}

		// Skip empty stacks
		if len(locations) == 0 {
			continue
		}

		// Create sample with labels
		sample := &profile.Sample{
			Location: locations,
			Value:    []int64{int64(stack.SampleCount)},
			Label: map[string][]string{
				"pid": {fmt.Sprintf("%d", stack.PID)},
				"tid": {fmt.Sprintf("%d", stack.TID)},
			},
			NumLabel: map[string][]int64{
				"cpu": {int64(stack.CPU)},
			},
		}
		prof.Sample = append(prof.Sample, sample)
	}

	// Add metadata as comments (strings, not indices)
	prof.Comments = []string{
		fmt.Sprintf("event: %s", profileStats.EventName),
		fmt.Sprintf("sample_count: %d", profileStats.SampleCount),
		fmt.Sprintf("dropped_samples: %d", profileStats.DroppedSamples),
	}

	return prof, nil
}

// BuildTags creates Datadog tags from config and event metadata
func BuildTags(config Config, event metrics.MetricEvent) map[string]string {
	tags := make(map[string]string)

	// Add config tags
	for k, v := range config.Tags {
		tags[k] = v
	}

	// Add standard tags
	tags["service"] = config.Service
	tags["env"] = config.Env
	tags["version"] = config.Version

	// Add event metadata
	if event.NodeName != "" {
		tags["host"] = event.NodeName
	} else if config.Hostname != "" {
		tags["host"] = config.Hostname
	}

	if event.ClusterName != "" {
		tags["cluster_name"] = event.ClusterName
	}

	// Add profiling-specific tags
	if stats, ok := event.Data.(*performance.ProfileStats); ok {
		tags["profile_type"] = "cpu" // or derive from EventName
		tags["event_name"] = stats.EventName
	}

	return tags
}

// ProfileMetadata holds metadata for Datadog profile upload
type ProfileMetadata struct {
	Start    time.Time
	End      time.Time
	Family   string // "go", "ruby", etc. - use "ebpf" for our case
	Version  string
	Tags     map[string]string
	Hostname string
}

// GetProfileMetadata extracts metadata from event and config
func GetProfileMetadata(event metrics.MetricEvent, config Config) ProfileMetadata {
	stats, _ := event.Data.(*performance.ProfileStats)

	hostname := config.Hostname
	if event.NodeName != "" {
		hostname = event.NodeName
	}

	return ProfileMetadata{
		Start:    stats.CollectionTime,
		End:      stats.CollectionTime.Add(stats.Duration),
		Family:   "ebpf", // Indicate this is eBPF-based profiling
		Version:  config.Version,
		Tags:     BuildTags(config, event),
		Hostname: hostname,
	}
}
