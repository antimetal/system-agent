// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package otlpprofiles

import (
	"bytes"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/pprof/profile"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pprofile"

	"github.com/antimetal/agent/internal/metrics"
	"github.com/antimetal/agent/pkg/performance"
)

// Transformer converts ProfileStats to OTLP profiling format
type Transformer struct {
	logger         logr.Logger
	serviceName    string
	serviceVersion string
}

// NewTransformer creates a new OTLP profile transformer
func NewTransformer(logger logr.Logger, serviceName, serviceVersion string) *Transformer {
	return &Transformer{
		logger:         logger.WithName("otlp-transformer"),
		serviceName:    serviceName,
		serviceVersion: serviceVersion,
	}
}

// Transform converts a MetricEvent containing ProfileStats to OTLP format
// Following the "OriginalPayload" approach from GitHub issue 239
func (t *Transformer) Transform(event metrics.MetricEvent) (pprofile.Profiles, error) {
	// Type assert to ProfileStats
	profileStats, ok := event.Data.(*performance.ProfileStats)
	if !ok {
		return pprofile.Profiles{}, fmt.Errorf("expected *performance.ProfileStats, got %T", event.Data)
	}

	// Convert ProfileStats to pprof format
	pprofProfile, err := t.convertToPprof(profileStats)
	if err != nil {
		return pprofile.Profiles{}, fmt.Errorf("failed to convert to pprof: %w", err)
	}

	// Serialize pprof to gzipped bytes
	// Note: profile.Write() already returns gzip-compressed data
	var buf bytes.Buffer
	if err := pprofProfile.Write(&buf); err != nil {
		return pprofile.Profiles{}, fmt.Errorf("failed to write pprof: %w", err)
	}

	// Create OTLP profiles structure
	profiles := pprofile.NewProfiles()

	// Add resource profile
	rp := profiles.ResourceProfiles().AppendEmpty()

	// Set resource attributes
	resource := rp.Resource()
	attrs := resource.Attributes()
	attrs.PutStr("host.name", event.NodeName)
	attrs.PutStr("service.name", t.serviceName)
	attrs.PutStr("service.version", t.serviceVersion)
	attrs.PutStr("profile.event_name", profileStats.EventName)
	if event.ClusterName != "" {
		attrs.PutStr("k8s.cluster.name", event.ClusterName)
	}

	// Add scope profile
	sp := rp.ScopeProfiles().AppendEmpty()
	sp.Scope().SetName("antimetal-system-agent")
	sp.Scope().SetVersion(t.serviceVersion)

	// Add profile with OriginalPayload
	otlpProfile := sp.Profiles().AppendEmpty()

	// Set profile metadata
	otlpProfile.SetProfileID(pprofile.ProfileID(generateProfileID(profileStats)))
	otlpProfile.SetTime(pcommon.NewTimestampFromTime(profileStats.CollectionTime))
	otlpProfile.SetDuration(pcommon.Timestamp(profileStats.Duration.Nanoseconds()))

	// Store original pprof data (recommended approach from issue 239)
	otlpProfile.SetOriginalPayloadFormat("pprof")
	otlpProfile.OriginalPayload().FromRaw(buf.Bytes())

	// Set period information
	otlpProfile.SetPeriod(int64(profileStats.SamplePeriod))

	// Set sample type - use string indices (0 for empty, we'll add to profile string table if needed)
	// For now, use basic type without string table management
	sampleType := otlpProfile.SampleType()
	// Note: In production, these should reference proper string table indices
	sampleType.SetTypeStrindex(0) // TODO: Manage string table properly
	sampleType.SetUnitStrindex(0)

	t.logger.V(2).Info("transformed profile to OTLP",
		"profile_id", otlpProfile.ProfileID().String(),
		"time", profileStats.CollectionTime,
		"duration", profileStats.Duration,
		"samples", profileStats.SampleCount,
		"stacks", len(profileStats.Stacks),
		"pprof_size_bytes", buf.Len())

	return profiles, nil
}

// convertToPprof converts ProfileStats to pprof.Profile format
func (t *Transformer) convertToPprof(stats *performance.ProfileStats) (*profile.Profile, error) {
	p := &profile.Profile{
		TimeNanos:     stats.CollectionTime.UnixNano(),
		DurationNanos: stats.Duration.Nanoseconds(),
		PeriodType: &profile.ValueType{
			Type: "cpu",
			Unit: "nanoseconds",
		},
		Period: int64(stats.SamplePeriod),
	}

	// Add sample type
	p.SampleType = []*profile.ValueType{
		{
			Type: "cpu",
			Unit: "nanoseconds",
		},
	}

	// Build location and function maps
	locationMap := make(map[uint64]*profile.Location)
	functionMap := make(map[string]*profile.Function)

	// Track next IDs
	nextFunctionID := uint64(1)
	nextLocationID := uint64(1)

	// Process each stack trace
	for _, stack := range stats.Stacks {
		var locations []*profile.Location

		// Process user stack (reverse order for pprof - deepest first)
		for i := len(stack.UserStack) - 1; i >= 0; i-- {
			addr := stack.UserStack[i]
			if addr == 0 {
				continue
			}
			loc := t.getOrCreateLocation(p, locationMap, functionMap, &nextFunctionID, &nextLocationID, addr, false, stack.PID)
			locations = append(locations, loc)
		}

		// Process kernel stack
		for i := len(stack.KernelStack) - 1; i >= 0; i-- {
			addr := stack.KernelStack[i]
			if addr == 0 {
				continue
			}
			loc := t.getOrCreateLocation(p, locationMap, functionMap, &nextFunctionID, &nextLocationID, addr, true, stack.PID)
			locations = append(locations, loc)
		}

		// Add sample
		sample := &profile.Sample{
			Location: locations,
			Value:    []int64{int64(stack.SampleCount)},
			Label: map[string][]string{
				"pid": {fmt.Sprintf("%d", stack.PID)},
				"tid": {fmt.Sprintf("%d", stack.TID)},
				"cpu": {fmt.Sprintf("%d", stack.CPU)},
			},
		}
		p.Sample = append(p.Sample, sample)
	}

	return p, nil
}

// getOrCreateLocation gets or creates a location for an address
func (t *Transformer) getOrCreateLocation(
	p *profile.Profile,
	locationMap map[uint64]*profile.Location,
	functionMap map[string]*profile.Function,
	nextFunctionID *uint64,
	nextLocationID *uint64,
	address uint64,
	isKernel bool,
	pid int32,
) *profile.Location {
	// Check if location already exists
	if loc, exists := locationMap[address]; exists {
		return loc
	}

	// TODO: Resolve symbol for address using /proc/[pid]/maps and symbol resolution
	// For now, create placeholder function with hex address
	funcName := fmt.Sprintf("0x%x", address)
	if isKernel {
		funcName = fmt.Sprintf("[kernel] 0x%x", address)
	}

	// Get or create function
	fn, exists := functionMap[funcName]
	if !exists {
		fn = &profile.Function{
			ID:   *nextFunctionID,
			Name: funcName,
		}
		*nextFunctionID++
		p.Function = append(p.Function, fn)
		functionMap[funcName] = fn
	}

	// Create location
	loc := &profile.Location{
		ID:      *nextLocationID,
		Address: address,
		Line: []profile.Line{
			{
				Function: fn,
			},
		},
	}
	*nextLocationID++
	p.Location = append(p.Location, loc)
	locationMap[address] = loc

	return loc
}

// generateProfileID creates a unique profile ID
func generateProfileID(stats *performance.ProfileStats) [16]byte {
	// Use timestamp and event name to generate ID
	// TODO: Use proper UUID generation
	var id [16]byte
	timestamp := uint64(stats.CollectionTime.UnixNano())
	for i := 0; i < 8; i++ {
		id[i] = byte(timestamp >> (i * 8))
	}
	// Use hash of event name for remaining bytes
	for i, c := range []byte(stats.EventName) {
		if i >= 8 {
			break
		}
		id[8+i] = c
	}
	return id
}
