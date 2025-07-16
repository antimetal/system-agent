// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"testing"
	"time"
)

func TestCollectionConfig_ApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    CollectionConfig
		expected CollectionConfig
	}{
		{
			name:  "empty config gets all defaults",
			input: CollectionConfig{},
			expected: CollectionConfig{
				Interval: time.Second,
				EnabledCollectors: map[MetricType]bool{
					MetricTypeLoad:    true,
					MetricTypeMemory:  true,
					MetricTypeCPU:     true,
					MetricTypeProcess: true,
					MetricTypeDisk:    true,
					MetricTypeNetwork: true,
					MetricTypeTCP:     true,
					MetricTypeKernel:  true,
					// Hardware configuration collectors
					MetricTypeCPUInfo:     true,
					MetricTypeMemoryInfo:  true,
					MetricTypeDiskInfo:    true,
					MetricTypeNetworkInfo: true,
				},
				HostProcPath: "/proc",
				HostSysPath:  "/sys",
				HostDevPath:  "/dev",
			},
		},
		{
			name: "partial config keeps user values",
			input: CollectionConfig{
				Interval:     5 * time.Second,
				HostProcPath: "/custom/proc",
			},
			expected: CollectionConfig{
				Interval: 5 * time.Second, // User value kept
				EnabledCollectors: map[MetricType]bool{ // Default applied
					MetricTypeLoad:    true,
					MetricTypeMemory:  true,
					MetricTypeCPU:     true,
					MetricTypeProcess: true,
					MetricTypeDisk:    true,
					MetricTypeNetwork: true,
					MetricTypeTCP:     true,
					MetricTypeKernel:  true,
					// Hardware configuration collectors
					MetricTypeCPUInfo:     true,
					MetricTypeMemoryInfo:  true,
					MetricTypeDiskInfo:    true,
					MetricTypeNetworkInfo: true,
				},
				HostProcPath: "/custom/proc", // User value kept
				HostSysPath:  "/sys",         // Default applied
				HostDevPath:  "/dev",         // Default applied
			},
		},
		{
			name: "enabled collectors partial override",
			input: CollectionConfig{
				EnabledCollectors: map[MetricType]bool{
					MetricTypeLoad: false,
					MetricTypeCPU:  true,
				},
			},
			expected: CollectionConfig{
				Interval: time.Second,
				EnabledCollectors: map[MetricType]bool{
					MetricTypeLoad: false, // User override
					MetricTypeCPU:  true,  // User value
				},
				HostProcPath: "/proc",
				HostSysPath:  "/sys",
				HostDevPath:  "/dev",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.input
			config.ApplyDefaults()

			// Check each field
			if config.Interval != tt.expected.Interval {
				t.Errorf("Interval = %v, want %v", config.Interval, tt.expected.Interval)
			}
			if config.HostProcPath != tt.expected.HostProcPath {
				t.Errorf("HostProcPath = %v, want %v", config.HostProcPath, tt.expected.HostProcPath)
			}
			if config.HostSysPath != tt.expected.HostSysPath {
				t.Errorf("HostSysPath = %v, want %v", config.HostSysPath, tt.expected.HostSysPath)
			}
			if config.HostDevPath != tt.expected.HostDevPath {
				t.Errorf("HostDevPath = %v, want %v", config.HostDevPath, tt.expected.HostDevPath)
			}

			// Check EnabledCollectors map
			if len(config.EnabledCollectors) != len(tt.expected.EnabledCollectors) {
				t.Errorf("EnabledCollectors length = %v, want %v", len(config.EnabledCollectors), len(tt.expected.EnabledCollectors))
			}
			for k, v := range tt.expected.EnabledCollectors {
				if config.EnabledCollectors[k] != v {
					t.Errorf("EnabledCollectors[%v] = %v, want %v", k, config.EnabledCollectors[k], v)
				}
			}
		})
	}
}
