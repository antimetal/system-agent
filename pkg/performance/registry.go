// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"slices"

	"github.com/antimetal/agent/pkg/performance/capabilities"
	"github.com/go-logr/stdr"
)

var (
	registry              = make(map[MetricType]NewContinuousCollector)
	unavailableCollectors = make(map[MetricType]UnavailableCollector)
	registryLogger        = stdr.New(log.New(os.Stderr, "[performance/registry] ", log.LstdFlags))
)

// UnavailableCollector tracks collectors that cannot run on the current platform
type UnavailableCollector struct {
	MetricType           MetricType
	Reason               string
	MissingCapabilities  []capabilities.Capability
	MinKernelVersion     string
	CurrentKernelVersion string
}

// Register adds a NewCollector factory to the global registry for metricType.
// It checks if the collector can run on the current platform based on capabilities.
// If the collector cannot run due to missing capabilities, it is tracked in the
// unavailable collectors list with the reason, and an error is logged.
//
// This function is usually called during package initialization (typically in init() functions)
// to register collector implementations before they can be instantiated via the registry.
//
// It will panic if a collector for the given metricType is already registered.
func Register(metricType MetricType, collector NewContinuousCollector) {
	// Check if already registered
	if _, exists := registry[metricType]; exists {
		panic(fmt.Sprintf("Collector for %s already registered", metricType))
	}

	// Create a temporary collector instance to check capabilities
	config := CollectionConfig{
		HostProcPath: "/proc",
		HostSysPath:  "/sys",
		HostDevPath:  "/dev",
	}
	tempLogger := registryLogger.WithName(string(metricType))

	tempCollector, err := collector(tempLogger, config)
	if err != nil {
		// If we can't even create the collector, it's likely a platform issue
		unavailableCollectors[metricType] = UnavailableCollector{
			MetricType: metricType,
			Reason:     fmt.Sprintf("Failed to create collector: %v", err),
		}
		registryLogger.Info("Collector not available on this platform",
			"metric_type", metricType, "reason", err.Error())
		return
	}

	// Get collector capabilities
	caps := tempCollector.Capabilities()

	// Get current kernel version for debugging
	var currentKernelVersion string
	if kv, err := capabilities.DetectKernelVersion(); err == nil {
		currentKernelVersion = fmt.Sprintf("%s", kv)
	}

	err = caps.CanRun()
	if err != nil {
		var missing []capabilities.Capability

		if unwrapper, ok := err.(interface{ Unwrap() []error }); ok {
			for _, e := range unwrapper.Unwrap() {
				var capErr capabilities.MissingCapabilityError
				if errors.As(e, &capErr) {
					missing = append(missing, capErr.Capability)
				}
			}
		}

		if len(missing) > 0 {
			unavailableCollectors[metricType] = UnavailableCollector{
				MetricType:           metricType,
				Reason:               err.Error(),
				MissingCapabilities:  missing,
				MinKernelVersion:     caps.MinKernelVersion,
				CurrentKernelVersion: currentKernelVersion,
			}

			capNames := make([]string, len(missing))
			for i, cap := range missing {
				capNames[i] = cap.String()
			}

			registryLogger.Info("Collector requires additional capabilities",
				"metric_type", metricType,
				"missing_capabilities", capNames,
				"min_kernel_version", caps.MinKernelVersion)
		} else {
			unavailableCollectors[metricType] = UnavailableCollector{
				MetricType:           metricType,
				Reason:               fmt.Sprintf("Failed to check capabilities: %v", err),
				CurrentKernelVersion: currentKernelVersion,
			}
			registryLogger.Info("Failed to check collector capabilities",
				"metric_type", metricType, "error", err.Error())
		}
		return
	}

	// All checks passed, register the collector
	registry[metricType] = collector
	registryLogger.V(1).Info("Collector registered successfully", "metric_type", metricType)
}

// GetCollector returns the collector factory for the given metric type.
// The returned factory function can be used to create new collector instances.
func GetCollector(metricType MetricType) (NewContinuousCollector, error) {
	collector, exists := registry[metricType]
	if !exists {
		return nil, fmt.Errorf("Collector for %s not found", metricType)
	}
	return collector, nil
}

// GetAvailableCollectors returns a list of metric types for collectors that are registered
// and can run on the current platform.
func GetAvailableCollectors() []MetricType {
	return slices.Collect(maps.Keys(registry))
}

// GetUnavailableCollectors returns information about collectors that cannot run on the
// current platform due to missing capabilities or other requirements.
func GetUnavailableCollectors() map[MetricType]UnavailableCollector {
	return unavailableCollectors
}

// GetCollectorStatus returns whether a collector is available and a reason string.
// If the collector is available, the reason will be "available".
// If the collector is unavailable, the reason will describe why.
// If the collector doesn't exist, the reason will be "not found".
func GetCollectorStatus(metricType MetricType) (bool, string) {
	// Check if it's available
	if _, exists := registry[metricType]; exists {
		return true, "available"
	}

	// Check if it's unavailable with a known reason
	if unavail, exists := unavailableCollectors[metricType]; exists {
		return false, unavail.Reason
	}

	// Not found at all
	return false, "not found"
}
