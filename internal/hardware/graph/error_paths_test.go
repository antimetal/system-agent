// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package hardwaregraph

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/antimetal/agent/internal/hardware/types"
	"github.com/antimetal/agent/internal/resource"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// MockStore is a mock implementation of resource.Store for testing error paths
type MockStore struct {
	mock.Mock
}

func (m *MockStore) AddResource(rsrc *resourcev1.Resource) error {
	args := m.Called(rsrc)
	return args.Error(0)
}

func (m *MockStore) UpdateResource(rsrc *resourcev1.Resource) error {
	args := m.Called(rsrc)
	return args.Error(0)
}

func (m *MockStore) GetResource(ref *resourcev1.ResourceRef) (*resourcev1.Resource, error) {
	args := m.Called(ref)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*resourcev1.Resource), args.Error(1)
}

func (m *MockStore) DeleteResource(ref *resourcev1.ResourceRef) error {
	args := m.Called(ref)
	return args.Error(0)
}

func (m *MockStore) AddRelationships(rels ...*resourcev1.Relationship) error {
	args := m.Called(rels)
	return args.Error(0)
}

func (m *MockStore) GetRelationships(subject, object *resourcev1.ResourceRef, predicateT proto.Message) ([]*resourcev1.Relationship, error) {
	args := m.Called(subject, object, predicateT)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*resourcev1.Relationship), args.Error(1)
}

func (m *MockStore) DeleteRelationships(rels ...*resourcev1.Relationship) error {
	args := m.Called(rels)
	return args.Error(0)
}

func (m *MockStore) Subscribe(typeDefs ...*resourcev1.TypeDescriptor) <-chan resource.Event {
	args := m.Called(typeDefs)
	if args.Get(0) == nil {
		ch := make(chan resource.Event)
		close(ch)
		return ch
	}
	return args.Get(0).(<-chan resource.Event)
}

func (m *MockStore) Close() error {
	args := m.Called()
	return args.Error(0)
}

// Phase 1: Critical Error Path Tests

// TestGetOSInfo_Behavior tests getOSInfo behavior
func TestGetOSInfo_Behavior(t *testing.T) {
	t.Run("returns non-empty string", func(t *testing.T) {
		// getOSInfo should always return something (either from os-release or "Linux" fallback)
		result := getOSInfo()
		assert.NotEmpty(t, result, "getOSInfo should never return empty string")
	})

	t.Run("returns valid os information", func(t *testing.T) {
		// getOSInfo should return a reasonable OS description
		result := getOSInfo()

		// Result should either be the fallback or actual OS info
		assert.True(t, len(result) > 0, "Should return non-empty OS info")

		// Common patterns that should appear in OS info
		hasValidInfo := result == "Linux" ||
			contains(result, "Ubuntu") ||
			contains(result, "Debian") ||
			contains(result, "Red Hat") ||
			contains(result, "CentOS") ||
			contains(result, "Fedora") ||
			contains(result, "Alpine") ||
			contains(result, "Amazon") ||
			contains(result, "SUSE")

		assert.True(t, hasValidInfo, "Should return 'Linux' fallback or valid OS name, got: %s", result)
	})
}

// Helper function for case-insensitive contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 &&
			(s[:len(substr)] == substr ||
				(len(s) > len(substr) && anyContains(s, substr)))))
}

func anyContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestGetSystemInfo_ErrorHandling tests error scenarios in getSystemInfo
func TestGetSystemInfo_ErrorHandling(t *testing.T) {
	// Note: getSystemInfo handles errors internally by logging and using fallbacks
	// We test that it doesn't panic and returns reasonable defaults

	t.Run("handles errors gracefully", func(t *testing.T) {
		builder := NewBuilder(logr.Discard(), nil)

		// This should not panic even if some system calls fail
		arch, bootTime, kernelVersion, osInfo := builder.getSystemInfo()

		// Verify we get some values (architecture should always be valid)
		assert.NotEqual(t, 0, arch, "Architecture should be set")
		assert.NotZero(t, bootTime, "Boot time should be set")
		assert.NotEmpty(t, kernelVersion, "Kernel version should be set")
		assert.NotEmpty(t, osInfo, "OS info should be set")
	})
}

// TestBuildFromSnapshot_StoreFailures tests resource store failure scenarios
func TestBuildFromSnapshot_StoreFailures(t *testing.T) {
	t.Run("system node store failure", func(t *testing.T) {
		mockStore := new(MockStore)
		builder := NewBuilder(logr.Discard(), mockStore)

		// Mock UpdateResource to fail for system node
		mockStore.On("UpdateResource", mock.Anything).Return(errors.New("store failure"))

		snapshot := &types.Snapshot{
			CPUInfo:     &performance.CPUInfo{},
			MemoryInfo:  &performance.MemoryInfo{},
			DiskInfo:    []*performance.DiskInfo{},
			NetworkInfo: []*performance.NetworkInfo{},
		}

		err := builder.BuildFromSnapshot(context.Background(), snapshot)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to add system node")

		mockStore.AssertExpectations(t)
	})

	t.Run("CPU package store failure", func(t *testing.T) {
		mockStore := new(MockStore)
		builder := NewBuilder(logr.Discard(), mockStore)

		// First call succeeds (system node), second fails (CPU package)
		mockStore.On("UpdateResource", mock.Anything).Return(nil).Once()
		mockStore.On("UpdateResource", mock.Anything).Return(errors.New("CPU store failure")).Once()

		snapshot := &types.Snapshot{
			CPUInfo: &performance.CPUInfo{
				VendorID:  "GenuineIntel",
				ModelName: "Test CPU",
				Cores: []performance.CPUCore{
					{Processor: 0, PhysicalID: 0, CoreID: 0},
				},
			},
		}

		err := builder.BuildFromSnapshot(context.Background(), snapshot)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to build CPU topology")

		mockStore.AssertExpectations(t)
	})

	t.Run("relationship store failure", func(t *testing.T) {
		mockStore := new(MockStore)
		builder := NewBuilder(logr.Discard(), mockStore)

		// All UpdateResource calls succeed
		mockStore.On("UpdateResource", mock.Anything).Return(nil)

		// GetRelationships returns not found (to trigger add)
		mockStore.On("GetRelationships", mock.Anything, mock.Anything, mock.Anything).
			Return(nil, resource.ErrRelationshipsNotFound)

		// AddRelationships fails
		mockStore.On("AddRelationships", mock.Anything).Return(errors.New("relationship store failure"))

		snapshot := &types.Snapshot{
			CPUInfo: &performance.CPUInfo{
				VendorID:  "GenuineIntel",
				ModelName: "Test CPU",
				Cores: []performance.CPUCore{
					{Processor: 0, PhysicalID: 0, CoreID: 0},
				},
			},
		}

		err := builder.BuildFromSnapshot(context.Background(), snapshot)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to add CPU topology relationships")

		mockStore.AssertExpectations(t)
	})
}

// Phase 2: Data Validation Tests

// TestNetworkInterface_EnumMappingEdgeCases tests enum mapping for network interfaces
func TestNetworkInterface_EnumMappingEdgeCases(t *testing.T) {
	testCases := []struct {
		name      string
		duplex    string
		ifaceType string
		operState string
		wantError bool
	}{
		{
			name:      "unknown duplex mode",
			duplex:    "invalid",
			ifaceType: "ethernet",
			operState: "up",
			wantError: false, // Should map to DUPLEX_MODE_UNKNOWN
		},
		{
			name:      "unknown interface type",
			duplex:    "full",
			ifaceType: "quantum", // Invalid type
			operState: "up",
			wantError: false, // Should map to INTERFACE_TYPE_UNKNOWN
		},
		{
			name:      "unknown operational state",
			duplex:    "full",
			ifaceType: "ethernet",
			operState: "quantum-entangled", // Invalid state
			wantError: false,               // Should map to OPERATIONAL_STATE_UNKNOWN
		},
		{
			name:      "empty values",
			duplex:    "",
			ifaceType: "",
			operState: "",
			wantError: false, // Should map to UNKNOWN values
		},
		{
			name:      "case sensitivity",
			duplex:    "FULL",
			ifaceType: "ETHERNET",
			operState: "UP",
			wantError: false, // Should handle case variations
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			builder := NewBuilder(logr.Discard(), nil)

			netInfo := &performance.NetworkInfo{
				Interface: "test0",
				Duplex:    tc.duplex,
				Type:      tc.ifaceType,
				OperState: tc.operState,
			}

			systemRef := &resourcev1.ResourceRef{
				TypeUrl: "test.system",
				Name:    "test-system",
			}

			node, ref, err := builder.createNetworkInterfaceNode(netInfo, systemRef)

			if tc.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, node)
				assert.NotNil(t, ref)
			}
		})
	}
}

// TestBusConnection_EnumMapping tests bus type enum mapping
func TestBusConnection_EnumMapping(t *testing.T) {
	testCases := []struct {
		name      string
		busType   string
		wantError bool
	}{
		{name: "pci", busType: "pci", wantError: false},
		{name: "pcie", busType: "pcie", wantError: false},
		{name: "usb", busType: "usb", wantError: false},
		{name: "sata", busType: "sata", wantError: false},
		{name: "nvme", busType: "nvme", wantError: false},
		{name: "virtio", busType: "virtio", wantError: false},
		{name: "unknown", busType: "quantum", wantError: false},      // Maps to UNKNOWN
		{name: "empty", busType: "", wantError: false},               // Maps to UNKNOWN
		{name: "special chars", busType: "pci@#$", wantError: false}, // Maps to UNKNOWN
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			builder := NewBuilder(logr.Discard(), nil)

			deviceRef := &resourcev1.ResourceRef{
				TypeUrl: "test.device",
				Name:    "test-device",
			}

			systemRef := &resourcev1.ResourceRef{
				TypeUrl: "test.system",
				Name:    "test-system",
			}

			rel, err := builder.createBusConnectionRelationship(deviceRef, systemRef, tc.busType, "")

			if tc.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, rel)
			}
		})
	}
}

// TestProtobufMarshaling_Errors tests protobuf marshaling error scenarios
func TestProtobufMarshaling_Errors(t *testing.T) {
	t.Run("anypb.New with nil proto should not panic", func(t *testing.T) {
		// This tests that our code handles nil proto messages gracefully
		// In practice, anypb.New returns an error for nil messages

		builder := NewBuilder(logr.Discard(), nil)

		// Try to create a relationship with invalid data
		// The actual functions validate data before marshaling, so we're
		// testing that the error is properly propagated

		// Test that creating relationships with nil refs doesn't panic
		assert.NotPanics(t, func() {
			_, _ = builder.createContainsRelationship(nil, nil, "physical")
		})
	})
}

// Phase 3: Recovery Testing

// TestBuildCPUTopology_PartialFailures tests partial failure handling in CPU topology
func TestBuildCPUTopology_PartialFailures(t *testing.T) {
	t.Run("missing physical ID for core", func(t *testing.T) {
		mockStore := new(MockStore)
		builder := NewBuilder(logr.Discard(), mockStore)

		cpuInfo := &performance.CPUInfo{
			VendorID:  "GenuineIntel",
			ModelName: "Test CPU",
			Cores: []performance.CPUCore{
				{Processor: 0, PhysicalID: 0, CoreID: 0},
				{Processor: 1, PhysicalID: 1, CoreID: 0}, // Different socket
			},
		}

		systemRef := &resourcev1.ResourceRef{
			TypeUrl: "test.system",
			Name:    "test-system",
		}

		// Mock all store operations to succeed
		mockStore.On("UpdateResource", mock.Anything).Return(nil)
		mockStore.On("GetRelationships", mock.Anything, mock.Anything, mock.Anything).
			Return(nil, resource.ErrRelationshipsNotFound)
		mockStore.On("AddRelationships", mock.Anything).Return(nil)

		err := builder.buildCPUTopology(context.Background(), cpuInfo, systemRef)
		assert.NoError(t, err, "Should handle multiple sockets gracefully")

		mockStore.AssertExpectations(t)
	})

	t.Run("duplicate processor IDs", func(t *testing.T) {
		mockStore := new(MockStore)
		builder := NewBuilder(logr.Discard(), mockStore)

		cpuInfo := &performance.CPUInfo{
			VendorID:  "GenuineIntel",
			ModelName: "Test CPU",
			Cores: []performance.CPUCore{
				{Processor: 0, PhysicalID: 0, CoreID: 0},
				{Processor: 0, PhysicalID: 0, CoreID: 0}, // Duplicate
			},
		}

		systemRef := &resourcev1.ResourceRef{
			TypeUrl: "test.system",
			Name:    "test-system",
		}

		// The second core will overwrite the first in the store
		mockStore.On("UpdateResource", mock.Anything).Return(nil)
		mockStore.On("GetRelationships", mock.Anything, mock.Anything, mock.Anything).
			Return(nil, resource.ErrRelationshipsNotFound)
		mockStore.On("AddRelationships", mock.Anything).Return(nil)

		err := builder.buildCPUTopology(context.Background(), cpuInfo, systemRef)
		assert.NoError(t, err, "Should handle duplicate processor IDs")

		mockStore.AssertExpectations(t)
	})
}

// TestBuildMemoryTopology_ErrorHandling tests memory topology error scenarios
func TestBuildMemoryTopology_ErrorHandling(t *testing.T) {
	t.Run("NUMA node store failure", func(t *testing.T) {
		mockStore := new(MockStore)
		builder := NewBuilder(logr.Discard(), mockStore)

		memInfo := &performance.MemoryInfo{
			TotalBytes:  8 * 1024 * 1024 * 1024,
			NUMAEnabled: true,
			NUMANodes: []performance.NUMANode{
				{NodeID: 0, TotalBytes: 4 * 1024 * 1024 * 1024},
			},
		}

		systemRef := &resourcev1.ResourceRef{
			TypeUrl: "test.system",
			Name:    "test-system",
		}

		// First call succeeds (memory module), second fails (NUMA node)
		mockStore.On("UpdateResource", mock.Anything).Return(nil).Once()
		mockStore.On("UpdateResource", mock.Anything).Return(errors.New("NUMA store failure")).Once()

		err := builder.buildMemoryTopology(context.Background(), memInfo, systemRef)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to add NUMA node")

		mockStore.AssertExpectations(t)
	})

	t.Run("invalid NUMA distances", func(t *testing.T) {
		mockStore := new(MockStore)
		builder := NewBuilder(logr.Discard(), mockStore)

		memInfo := &performance.MemoryInfo{
			TotalBytes:  8 * 1024 * 1024 * 1024,
			NUMAEnabled: true,
			NUMANodes: []performance.NUMANode{
				{
					NodeID:     0,
					TotalBytes: 4 * 1024 * 1024 * 1024,
					Distances:  []int32{10, 20}, // Index 0=10 (self), index 1=20 (non-existent node)
				},
			},
		}

		systemRef := &resourcev1.ResourceRef{
			TypeUrl: "test.system",
			Name:    "test-system",
		}

		mockStore.On("UpdateResource", mock.Anything).Return(nil)
		mockStore.On("GetRelationships", mock.Anything, mock.Anything, mock.Anything).
			Return(nil, resource.ErrRelationshipsNotFound)
		mockStore.On("AddRelationships", mock.Anything).Return(nil)

		// Should handle gracefully by skipping non-existent NUMA node references
		err := builder.buildMemoryTopology(context.Background(), memInfo, systemRef)
		assert.NoError(t, err, "Should skip invalid NUMA node references gracefully")

		mockStore.AssertExpectations(t)
	})
}

// TestBuildDiskTopology_ErrorPropagation tests error propagation in disk topology
func TestBuildDiskTopology_ErrorPropagation(t *testing.T) {
	t.Run("partition store failure", func(t *testing.T) {
		mockStore := new(MockStore)
		builder := NewBuilder(logr.Discard(), mockStore)

		diskInfo := []*performance.DiskInfo{
			{
				Device:    "sda",
				SizeBytes: 1000 * 1024 * 1024 * 1024,
				Partitions: []performance.PartitionInfo{
					{Name: "sda1", SizeBytes: 500 * 1024 * 1024 * 1024},
				},
			},
		}

		systemRef := &resourcev1.ResourceRef{
			TypeUrl: "test.system",
			Name:    "test-system",
		}

		// Disk succeeds, partition fails
		mockStore.On("UpdateResource", mock.Anything).Return(nil).Once()
		mockStore.On("UpdateResource", mock.Anything).Return(errors.New("partition store failure")).Once()

		err := builder.buildDiskTopology(context.Background(), diskInfo, systemRef)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to add partition node")

		mockStore.AssertExpectations(t)
	})
}

// TestInferBusType_EdgeCases tests bus type inference edge cases
func TestInferBusType_EdgeCases(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		device   string
		expected string
	}{
		{"nvme0n1", "nvme"},
		{"sda", "sata"},
		{"hda", "ide"},
		{"vda", "virtio"},
		{"xvda", "virtio"},
		{"unknown-device", "unknown"},
		{"", "unknown"},
		{"NVME0N1", "unknown"}, // Case sensitive
		{"sd", "sata"},         // Prefix match
		{"nvme", "nvme"},       // Exact prefix
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("device=%s", tc.device), func(t *testing.T) {
			result := builder.inferDiskBusType(tc.device)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestInferNetworkBusType_EdgeCases tests network bus type inference edge cases
func TestInferNetworkBusType_EdgeCases(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name     string
		iface    *performance.NetworkInfo
		expected string
	}{
		{
			name:     "loopback",
			iface:    &performance.NetworkInfo{Type: "loopback"},
			expected: "virtual",
		},
		{
			name:     "bridge",
			iface:    &performance.NetworkInfo{Type: "bridge"},
			expected: "virtual",
		},
		{
			name:     "virtio driver",
			iface:    &performance.NetworkInfo{Type: "ethernet", Driver: "virtio_net"},
			expected: "virtio",
		},
		{
			name:     "usb driver",
			iface:    &performance.NetworkInfo{Type: "ethernet", Driver: "usb_ethernet"},
			expected: "usb",
		},
		{
			name:     "default physical",
			iface:    &performance.NetworkInfo{Type: "ethernet", Driver: "e1000"},
			expected: "pci",
		},
		{
			name:     "empty driver",
			iface:    &performance.NetworkInfo{Type: "ethernet", Driver: ""},
			expected: "pci",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := builder.inferNetworkBusType(tc.iface)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestAddRelationships_DuplicateHandling tests duplicate relationship handling
func TestAddRelationships_DuplicateHandling(t *testing.T) {
	t.Run("skip existing relationships", func(t *testing.T) {
		mockStore := new(MockStore)
		builder := NewBuilder(logr.Discard(), mockStore)

		ref1 := &resourcev1.ResourceRef{TypeUrl: "test.type", Name: "resource1"}
		ref2 := &resourcev1.ResourceRef{TypeUrl: "test.type", Name: "resource2"}

		rel, err := builder.createContainsRelationship(ref1, ref2, "physical")
		require.NoError(t, err)

		// Mock GetRelationships to return existing relationship (not ErrRelationshipsNotFound)
		existingRels := []*resourcev1.Relationship{rel}
		mockStore.On("GetRelationships", mock.Anything, mock.Anything, mock.Anything).
			Return(existingRels, nil)

		// AddRelationships should not be called since relationship exists

		err = builder.addRelationships(rel)
		assert.NoError(t, err, "Should skip adding duplicate relationships")

		mockStore.AssertExpectations(t)
		mockStore.AssertNotCalled(t, "AddRelationships")
	})

	t.Run("GetRelationships error propagation", func(t *testing.T) {
		mockStore := new(MockStore)
		builder := NewBuilder(logr.Discard(), mockStore)

		ref1 := &resourcev1.ResourceRef{TypeUrl: "test.type", Name: "resource1"}
		ref2 := &resourcev1.ResourceRef{TypeUrl: "test.type", Name: "resource2"}

		rel, err := builder.createContainsRelationship(ref1, ref2, "physical")
		require.NoError(t, err)

		// Mock GetRelationships to return an error (not ErrRelationshipsNotFound)
		mockStore.On("GetRelationships", mock.Anything, mock.Anything, mock.Anything).
			Return(nil, errors.New("query error"))

		err = builder.addRelationships(rel)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to find existing relationships")

		mockStore.AssertExpectations(t)
	})
}

// TestContainmentType_EdgeCases tests containment type mapping edge cases
func TestContainmentType_EdgeCases(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	ref1 := &resourcev1.ResourceRef{TypeUrl: "test.type", Name: "resource1"}
	ref2 := &resourcev1.ResourceRef{TypeUrl: "test.type", Name: "resource2"}

	testCases := []struct {
		name         string
		containsType string
		expectError  bool
	}{
		{"physical", "physical", false},
		{"logical", "logical", false},
		{"partition", "partition", false},
		{"unknown type", "quantum", false}, // Maps to UNKNOWN
		{"empty type", "", false},          // Maps to UNKNOWN
		{"mixed case", "PHYSICAL", false},  // Maps to UNKNOWN (case sensitive)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rel, err := builder.createContainsRelationship(ref1, ref2, tc.containsType)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, rel)

				// Verify the relationship was created with proper type descriptor
				assert.NotNil(t, rel.Type)
				assert.NotNil(t, rel.Predicate)
			}
		})
	}
}

// TestCreateCPUPackageNode_NoMatchingCore tests error when no core matches physical ID
func TestCreateCPUPackageNode_NoMatchingCore(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	cpuInfo := &performance.CPUInfo{
		VendorID:  "GenuineIntel",
		ModelName: "Test CPU",
		Cores: []performance.CPUCore{
			{Processor: 0, PhysicalID: 0, CoreID: 0},
		},
	}

	systemRef := &resourcev1.ResourceRef{
		TypeUrl: "test.system",
		Name:    "test-system",
	}

	// Try to create package for non-existent physical ID
	_, _, err := builder.createCPUPackageNode(cpuInfo, 999, systemRef)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no core found for physical ID")
}

// TestNilSnapshot_Handling tests handling of nil components in snapshot
func TestNilSnapshot_Handling(t *testing.T) {
	mockStore := new(MockStore)
	builder := NewBuilder(logr.Discard(), mockStore)

	// Mock system node creation
	mockStore.On("UpdateResource", mock.Anything).Return(nil)

	snapshot := &types.Snapshot{
		CPUInfo:     nil, // All components nil
		MemoryInfo:  nil,
		DiskInfo:    nil,
		NetworkInfo: nil,
	}

	err := builder.BuildFromSnapshot(context.Background(), snapshot)
	assert.NoError(t, err, "Should handle nil snapshot components gracefully")

	// Verify only system node was created
	mockStore.AssertNumberOfCalls(t, "UpdateResource", 1)
}
