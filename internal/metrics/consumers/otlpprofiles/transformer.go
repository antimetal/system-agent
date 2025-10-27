// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package otlpprofiles

import (
	"fmt"

	"github.com/go-logr/logr"

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

// OTLPProfile represents an OTLP profile (placeholder for actual OTLP proto)
// TODO: Replace with actual go.opentelemetry.io/proto/otlp/profiles/v1 types
type OTLPProfile struct {
	ProfileID      string
	StartTime      int64
	EndTime        int64
	Samples        []OTLPSample
	Locations      []OTLPLocation
	Functions      []OTLPFunction
	StringTable    []string
	EventName      string
	SamplePeriod   uint64
	ServiceName    string
	ServiceVersion string
	NodeName       string
	ClusterName    string
}

// OTLPSample represents a stack trace sample
type OTLPSample struct {
	LocationIndices []int
	Value           uint64
	PID             int32
	TID             int32
	CPU             int32
}

// OTLPLocation represents a location in the program
type OTLPLocation struct {
	Address       uint64
	FunctionIndex int
	IsKernel      bool
}

// OTLPFunction represents a function symbol
type OTLPFunction struct {
	Name      string
	Filename  string
	StartLine int
}

// Transform converts a MetricEvent containing ProfileStats to OTLP format
func (t *Transformer) Transform(event metrics.MetricEvent) (*OTLPProfile, error) {
	// Type assert to ProfileStats
	profileStats, ok := event.Data.(*performance.ProfileStats)
	if !ok {
		return nil, fmt.Errorf("expected *performance.ProfileStats, got %T", event.Data)
	}

	// Build OTLP profile structure
	profile := &OTLPProfile{
		ProfileID:      generateProfileID(profileStats),
		StartTime:      profileStats.CollectionTime.UnixNano(),
		EndTime:        profileStats.CollectionTime.Add(profileStats.Duration).UnixNano(),
		EventName:      profileStats.EventName,
		SamplePeriod:   profileStats.SamplePeriod,
		ServiceName:    t.serviceName,
		ServiceVersion: t.serviceVersion,
		NodeName:       event.NodeName,
		ClusterName:    event.ClusterName,
	}

	// Build string table (deduplicated strings)
	stringTable := newStringTable()
	stringTable.add("") // Index 0 must be empty string per OTLP spec

	// Build location and function tables
	locationTable := make(map[uint64]int) // address -> location index
	functionTable := make(map[string]int) // function name -> function index

	var locations []OTLPLocation
	var functions []OTLPFunction

	// Process each stack trace
	for _, stack := range profileStats.Stacks {
		var locationIndices []int

		// Process user stack
		for _, addr := range stack.UserStack {
			if addr == 0 {
				break
			}
			locIdx := t.addLocation(&locations, &functions, &locationTable, &functionTable, stringTable, addr, false)
			locationIndices = append(locationIndices, locIdx)
		}

		// Process kernel stack
		for _, addr := range stack.KernelStack {
			if addr == 0 {
				break
			}
			locIdx := t.addLocation(&locations, &functions, &locationTable, &functionTable, stringTable, addr, true)
			locationIndices = append(locationIndices, locIdx)
		}

		// Add sample
		sample := OTLPSample{
			LocationIndices: locationIndices,
			Value:           stack.SampleCount,
			PID:             stack.PID,
			TID:             stack.TID,
			CPU:             stack.CPU,
		}
		profile.Samples = append(profile.Samples, sample)
	}

	profile.Locations = locations
	profile.Functions = functions
	profile.StringTable = stringTable.strings

	t.logger.V(2).Info("transformed profile",
		"samples", len(profile.Samples),
		"locations", len(profile.Locations),
		"functions", len(profile.Functions),
		"strings", len(profile.StringTable))

	return profile, nil
}

// addLocation adds or reuses a location in the location table
func (t *Transformer) addLocation(
	locations *[]OTLPLocation,
	functions *[]OTLPFunction,
	locationTable *map[uint64]int,
	functionTable *map[string]int,
	stringTable *stringTable,
	address uint64,
	isKernel bool,
) int {
	// Check if we already have this location
	if idx, exists := (*locationTable)[address]; exists {
		return idx
	}

	// TODO: Resolve symbol for address using /proc/[pid]/maps and ELF parsing
	// For now, create placeholder function
	funcName := fmt.Sprintf("0x%x", address)
	if isKernel {
		funcName = fmt.Sprintf("[kernel] 0x%x", address)
	}

	// Add or reuse function
	funcIdx, funcExists := (*functionTable)[funcName]
	if !funcExists {
		funcIdx = len(*functions)
		(*functionTable)[funcName] = funcIdx
		*functions = append(*functions, OTLPFunction{
			Name:      funcName,
			Filename:  "",
			StartLine: 0,
		})
		stringTable.add(funcName)
	}

	// Add location
	locIdx := len(*locations)
	(*locationTable)[address] = locIdx
	*locations = append(*locations, OTLPLocation{
		Address:       address,
		FunctionIndex: funcIdx,
		IsKernel:      isKernel,
	})

	return locIdx
}

// stringTable helps build deduplicated string tables
type stringTable struct {
	strings []string
	index   map[string]int
}

func newStringTable() *stringTable {
	return &stringTable{
		strings: make([]string, 0, 100),
		index:   make(map[string]int),
	}
}

func (st *stringTable) add(s string) int {
	if idx, exists := st.index[s]; exists {
		return idx
	}
	idx := len(st.strings)
	st.strings = append(st.strings, s)
	st.index[s] = idx
	return idx
}

// generateProfileID creates a unique profile ID
func generateProfileID(stats *performance.ProfileStats) string {
	// TODO: Generate proper unique ID (UUID or similar)
	return fmt.Sprintf("profile-%d", stats.CollectionTime.UnixNano())
}
