// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/antimetal/agent/pkg/performance/capabilities"
	"github.com/go-logr/logr"
	"github.com/go-logr/stdr"
)

var (
	registry              = make(map[MetricType]NewContinuousCollector)
	unavailableCollectors = make(map[MetricType]UnavailableCollector)
	registryLogger        = stdr.New(log.New(os.Stderr, "[performance.registry] ", log.LstdFlags))
)

// UnavailableCollector represents a collector that cannot run on this platform
type UnavailableCollector struct {
	MetricType           MetricType
	Reason               string
	MissingCapabilities  []capabilities.Capability
	MinKernelVersion     string
	CurrentKernelVersion string
}

// Register adds a NewCollector factory to the global registry for metricType.
// collector is used to create new collector instances with the provided logger and
// configuration.
//
// This function is usually called during package initialization (typically in init() functions)
// to register collector implementations before they can be instantiated by performance.Manager.
//
// On non-Linux platforms, this is a no-op to allow unit tests to run on macOS/Windows.
// It will panic if a collector for the given metricType is already registered on Linux.
func Register(metricType MetricType, collector NewContinuousCollector) {
	// No-op on non-Linux platforms
	if runtime.GOOS != "linux" {
		registryLogger.V(1).Info("Skipping collector registration on non-Linux platform",
			"metric_type", metricType, "platform", runtime.GOOS)
		return
	}

	_, exists := registry[metricType]
	if exists {
		panic(fmt.Sprintf("Collector for %s already registered", metricType))
	}
	registry[metricType] = collector
}

// TryRegister attempts to register a collector after checking if it can run on the current platform.
// If the collector cannot run due to missing capabilities or incompatible platform, it is tracked
// in the unavailable collectors list with the reason.
//
// On non-Linux platforms, this is a no-op to allow unit tests to run on macOS/Windows.
// This function is called during package initialization and will not panic on capability failures.
func TryRegister(metricType MetricType, collector NewContinuousCollector) {
	// No-op on non-Linux platforms
	if runtime.GOOS != "linux" {
		registryLogger.V(1).Info("Skipping collector registration on non-Linux platform",
			"metric_type", metricType, "platform", runtime.GOOS)
		return
	}

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

	// Check if collector can run with current capabilities
	canRun, missing, err := caps.CanRun()
	if err != nil {
		unavailableCollectors[metricType] = UnavailableCollector{
			MetricType: metricType,
			Reason:     fmt.Sprintf("Failed to check capabilities: %v", err),
		}
		registryLogger.Info("Failed to check collector capabilities",
			"metric_type", metricType, "error", err.Error())
		return
	}

	if !canRun {
		unavailableCollectors[metricType] = UnavailableCollector{
			MetricType:          metricType,
			Reason:              "Missing required capabilities",
			MissingCapabilities: missing,
			MinKernelVersion:    caps.MinKernelVersion,
		}

		capNames := make([]string, len(missing))
		for i, cap := range missing {
			capNames[i] = cap.String()
		}

		registryLogger.Info("Collector requires additional capabilities",
			"metric_type", metricType,
			"missing_capabilities", capNames,
			"min_kernel_version", caps.MinKernelVersion)
		return
	}

	// All checks passed, register the collector
	registry[metricType] = collector
	registryLogger.V(1).Info("Successfully registered collector", "metric_type", metricType)
}

// GetCollector retrieves the collector factory function from the global registry for metricType.
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
	types := make([]MetricType, 0, len(registry))
	for metricType := range registry {
		types = append(types, metricType)
	}
	return types
}

// GetUnavailableCollectors returns information about collectors that cannot run on the
// current platform due to missing capabilities or other requirements.
func GetUnavailableCollectors() map[MetricType]UnavailableCollector {
	// Return a copy to prevent external modification
	result := make(map[MetricType]UnavailableCollector, len(unavailableCollectors))
	for k, v := range unavailableCollectors {
		result[k] = v
	}
	return result
}

// GetCollectorStatus returns detailed status information for a specific collector type.
// It returns whether the collector is available, and if not, why it cannot run.
func GetCollectorStatus(metricType MetricType) (available bool, reason string) {
	if _, exists := registry[metricType]; exists {
		return true, "Collector is registered and available"
	}

	if unavail, exists := unavailableCollectors[metricType]; exists {
		reason = unavail.Reason
		if len(unavail.MissingCapabilities) > 0 {
			capNames := make([]string, len(unavail.MissingCapabilities))
			for i, cap := range unavail.MissingCapabilities {
				capNames[i] = cap.String()
			}
			reason = fmt.Sprintf("%s (missing: %v)", reason, capNames)
		}
		return false, reason
	}

	return false, "Collector not found"
}

// SetRegistryLogger allows setting a custom logger for the registry.
// This should be called before any collectors are registered.
func SetRegistryLogger(logger logr.Logger) {
	registryLogger = logger
}
