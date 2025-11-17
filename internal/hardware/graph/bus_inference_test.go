// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package hardwaregraph

import (
	"testing"

	hardwarev1 "github.com/antimetal/agent/pkg/api/antimetal/hardware/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Phase 1: Core Disk Bus Type Pattern Tests

func TestInferDiskBusType_NVMePatterns(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name     string
		device   string
		expected string
	}{
		{"nvme base device", "nvme0n1", "nvme"},
		{"nvme with partition", "nvme0n1p1", "nvme"},
		{"nvme second device", "nvme1n1", "nvme"},
		{"nvme with multiple partitions", "nvme1n2p5", "nvme"},
		{"nvme namespace variant", "nvme2n10", "nvme"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := builder.inferDiskBusType(tc.device)
			assert.Equal(t, tc.expected, result,
				"Device %s should be detected as %s", tc.device, tc.expected)
		})
	}
}

func TestInferDiskBusType_SATAPatterns(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name     string
		device   string
		expected string
	}{
		{"sda base", "sda", "sata"},
		{"sdb device", "sdb", "sata"},
		{"sdz last letter", "sdz", "sata"},
		{"sdaa extended", "sdaa", "sata"},
		{"sda with partition", "sda1", "sata"},
		{"sdb with multiple partitions", "sdb5", "sata"},
		{"sd prefix only", "sd", "sata"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := builder.inferDiskBusType(tc.device)
			assert.Equal(t, tc.expected, result,
				"Device %s should be detected as SATA", tc.device)
		})
	}
}

func TestInferDiskBusType_IDEPatterns(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name     string
		device   string
		expected string
	}{
		{"hda primary master", "hda", "ide"},
		{"hdb primary slave", "hdb", "ide"},
		{"hdc secondary master", "hdc", "ide"},
		{"hdd secondary slave", "hdd", "ide"},
		{"hda with partition", "hda1", "ide"},
		{"hd prefix only", "hd", "ide"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := builder.inferDiskBusType(tc.device)
			assert.Equal(t, tc.expected, result,
				"Device %s should be detected as IDE", tc.device)
		})
	}
}

func TestInferDiskBusType_VirtIOPatterns(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name     string
		device   string
		expected string
	}{
		// KVM/QEMU virtio
		{"vda KVM disk", "vda", "virtio"},
		{"vdb second disk", "vdb", "virtio"},
		{"vda with partition", "vda1", "virtio"},
		{"vdz extended", "vdz", "virtio"},

		// Xen virtio
		{"xvda Xen disk", "xvda", "virtio"},
		{"xvdb second Xen disk", "xvdb", "virtio"},
		{"xvda with partition", "xvda1", "virtio"},
		{"xvdz Xen extended", "xvdz", "virtio"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := builder.inferDiskBusType(tc.device)
			assert.Equal(t, tc.expected, result,
				"Device %s should be detected as virtio", tc.device)
		})
	}
}

func TestInferDiskBusType_UnknownPatterns(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name   string
		device string
	}{
		{"empty string", ""},
		{"loop device", "loop0"},
		{"device mapper", "dm-0"},
		{"ram disk", "ram0"},
		{"md raid", "md0"},
		{"mmcblk eMMC", "mmcblk0"},
		{"nbd network block", "nbd0"},
		{"sr optical", "sr0"},
		{"completely unknown", "unknown-device-123"},
		{"numeric only", "12345"},
		{"special chars", "dev@#$"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := builder.inferDiskBusType(tc.device)
			assert.Equal(t, "unknown", result,
				"Device %s should be detected as unknown", tc.device)
		})
	}
}

// Phase 2: Network Driver Detection Tests

func TestInferNetworkBusType_VirtualInterfaces(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name     string
		iface    *performance.NetworkInfo
		expected string
	}{
		{
			name:     "loopback",
			iface:    &performance.NetworkInfo{Interface: "lo", Type: "loopback"},
			expected: "virtual",
		},
		{
			name:     "bridge",
			iface:    &performance.NetworkInfo{Interface: "br0", Type: "bridge"},
			expected: "virtual",
		},
		{
			name:     "vlan",
			iface:    &performance.NetworkInfo{Interface: "eth0.100", Type: "vlan"},
			expected: "virtual",
		},
		{
			name:     "bond",
			iface:    &performance.NetworkInfo{Interface: "bond0", Type: "bond"},
			expected: "virtual",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := builder.inferNetworkBusType(tc.iface)
			assert.Equal(t, tc.expected, result,
				"Interface %s (type: %s) should be detected as virtual", tc.iface.Interface, tc.iface.Type)
		})
	}
}

func TestInferNetworkBusType_VirtIODrivers(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name   string
		driver string
	}{
		{"virtio_net", "virtio_net"},
		{"virtio in driver name", "virtio_pci"},
		{"virtio variant", "vhost_virtio"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			iface := &performance.NetworkInfo{
				Interface: "eth0",
				Type:      "ethernet",
				Driver:    tc.driver,
			}
			result := builder.inferNetworkBusType(iface)
			assert.Equal(t, "virtio", result,
				"Driver %s should be detected as virtio", tc.driver)
		})
	}
}

func TestInferNetworkBusType_USBDrivers(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name     string
		driver   string
		expected string
	}{
		// Currently detected as USB (contains "usb" in driver name)
		{"usbnet", "usbnet", "usb"},
		{"generic usb in name", "usb_ethernet", "usb"},
		{"rndis_host", "rndis_host_usb", "usb"},

		// NOTE: These drivers are USB but don't contain "usb" in the name
		// Current implementation defaults to PCI for these
		{"r8152 Realtek USB", "r8152", "pci"},   // Realtek USB Ethernet - not detected
		{"asix USB Ethernet", "asix", "pci"},    // ASIX USB Ethernet - not detected
		{"ax88179_178a", "ax88179_178a", "pci"}, // ASIX USB 3.0 - not detected
		{"cdc_ether", "cdc_ether", "pci"},       // CDC Ethernet - not detected
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			iface := &performance.NetworkInfo{
				Interface: "eth0",
				Type:      "ethernet",
				Driver:    tc.driver,
			}
			result := builder.inferNetworkBusType(iface)
			assert.Equal(t, tc.expected, result,
				"Driver %s should be detected as %s (current implementation)", tc.driver, tc.expected)
		})
	}
}

func TestInferNetworkBusType_PCIDrivers(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name   string
		driver string
	}{
		// Intel drivers
		{"e1000", "e1000"},
		{"e1000e", "e1000e"},
		{"igb", "igb"},
		{"igc", "igc"},
		{"ixgbe 10GbE", "ixgbe"},
		{"i40e", "i40e"},
		{"ice", "ice"},

		// Broadcom
		{"bnxt_en", "bnxt_en"},
		{"tg3", "tg3"},

		// Realtek
		{"r8169", "r8169"},

		// Mellanox
		{"mlx4_en", "mlx4_en"},
		{"mlx5_core", "mlx5_core"},

		// Amazon ENA
		{"ena", "ena"},

		// VMware vmxnet3
		{"vmxnet3", "vmxnet3"},

		// Wireless (typically PCI)
		{"iwlwifi", "iwlwifi"},
		{"ath9k", "ath9k"},
		{"ath10k", "ath10k"},

		// Generic/unknown - defaults to PCI
		{"unknown_driver", "unknown_driver"},
		{"empty driver", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			iface := &performance.NetworkInfo{
				Interface: "eth0",
				Type:      "ethernet",
				Driver:    tc.driver,
			}
			result := builder.inferNetworkBusType(iface)
			assert.Equal(t, "pci", result,
				"Driver %s should default to PCI", tc.driver)
		})
	}
}

// Phase 3: Bus Connection Relationship Tests

func TestCreateBusConnectionRelationship_AllBusTypes(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	deviceRef := &resourcev1.ResourceRef{
		TypeUrl: "test.device",
		Name:    "test-device",
	}
	systemRef := &resourcev1.ResourceRef{
		TypeUrl: "test.system",
		Name:    "test-system",
	}

	testCases := []struct {
		busType      string
		expectedEnum hardwarev1.BusType
		busAddress   string
	}{
		{"pci", hardwarev1.BusType_BUS_TYPE_PCI, "0000:00:1f.2"},
		{"pcie", hardwarev1.BusType_BUS_TYPE_PCIE, "0000:01:00.0"},
		{"usb", hardwarev1.BusType_BUS_TYPE_USB, "1-1.2"},
		{"sata", hardwarev1.BusType_BUS_TYPE_SATA, ""},
		{"nvme", hardwarev1.BusType_BUS_TYPE_NVME, ""},
		{"sas", hardwarev1.BusType_BUS_TYPE_SAS, ""},
		{"ide", hardwarev1.BusType_BUS_TYPE_IDE, ""},
		{"scsi", hardwarev1.BusType_BUS_TYPE_SCSI, ""},
		{"virtio", hardwarev1.BusType_BUS_TYPE_VIRTIO, ""},
		{"virtual", hardwarev1.BusType_BUS_TYPE_UNKNOWN, ""},
		{"unknown", hardwarev1.BusType_BUS_TYPE_UNKNOWN, ""},
		{"", hardwarev1.BusType_BUS_TYPE_UNKNOWN, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.busType, func(t *testing.T) {
			rel, err := builder.createBusConnectionRelationship(deviceRef, systemRef, tc.busType, tc.busAddress)
			require.NoError(t, err, "Should create relationship for bus type %s", tc.busType)
			require.NotNil(t, rel)

			// Verify relationship structure
			assert.Equal(t, deviceRef, rel.Subject)
			assert.Equal(t, systemRef, rel.Object)
			assert.NotNil(t, rel.Predicate)
			assert.NotNil(t, rel.Type)

			// Verify predicate contains correct bus type
			var connectedTo hardwarev1.ConnectedTo
			err = rel.Predicate.UnmarshalTo(&connectedTo)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedEnum, connectedTo.BusType,
				"Bus type %s should map to enum %v", tc.busType, tc.expectedEnum)

			if tc.busAddress != "" {
				assert.Equal(t, tc.busAddress, connectedTo.BusAddress)
			}
		})
	}
}

func TestCreateBusConnectionRelationship_WithBusAddresses(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	deviceRef := &resourcev1.ResourceRef{
		TypeUrl: "test.device",
		Name:    "test-device",
	}
	systemRef := &resourcev1.ResourceRef{
		TypeUrl: "test.system",
		Name:    "test-system",
	}

	testCases := []struct {
		name       string
		busType    string
		busAddress string
	}{
		{"PCI address format", "pci", "0000:00:1f.2"},
		{"PCI domain included", "pci", "0001:05:00.0"},
		{"USB bus location", "usb", "1-1.2:1.0"},
		{"USB simple", "usb", "2-1"},
		{"empty address", "pci", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rel, err := builder.createBusConnectionRelationship(deviceRef, systemRef, tc.busType, tc.busAddress)
			require.NoError(t, err)

			var connectedTo hardwarev1.ConnectedTo
			err = rel.Predicate.UnmarshalTo(&connectedTo)
			require.NoError(t, err)
			assert.Equal(t, tc.busAddress, connectedTo.BusAddress)
		})
	}
}

// Phase 4: Real-World Device Pattern Validation

func TestBusInference_AWSDevices(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name            string
		device          string
		expectedBusType string
		description     string
	}{
		{
			name:            "EC2 NVMe EBS",
			device:          "nvme0n1",
			expectedBusType: "nvme",
			description:     "AWS EBS volumes presented as NVMe devices",
		},
		{
			name:            "EC2 NVMe instance store",
			device:          "nvme1n1",
			expectedBusType: "nvme",
			description:     "AWS instance store NVMe SSDs",
		},
		{
			name:            "Xen paravirtual",
			device:          "xvda",
			expectedBusType: "virtio",
			description:     "Legacy EC2 Xen instances",
		},
		{
			name:            "Xen additional volume",
			device:          "xvdf",
			expectedBusType: "virtio",
			description:     "Additional EBS volumes on Xen",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := builder.inferDiskBusType(tc.device)
			assert.Equal(t, tc.expectedBusType, result, tc.description)
		})
	}
}

func TestBusInference_GCPDevices(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name            string
		device          string
		expectedBusType string
		description     string
	}{
		{
			name:            "GCP persistent disk",
			device:          "sda",
			expectedBusType: "sata",
			description:     "GCP standard persistent disks",
		},
		{
			name:            "GCP local SSD",
			device:          "nvme0n1",
			expectedBusType: "nvme",
			description:     "GCP local NVMe SSDs",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := builder.inferDiskBusType(tc.device)
			assert.Equal(t, tc.expectedBusType, result, tc.description)
		})
	}
}

func TestBusInference_AzureDevices(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name            string
		device          string
		expectedBusType string
		description     string
	}{
		{
			name:            "Azure managed disk",
			device:          "sda",
			expectedBusType: "sata",
			description:     "Azure standard managed disks",
		},
		{
			name:            "Azure NVMe",
			device:          "nvme0n1",
			expectedBusType: "nvme",
			description:     "Azure premium NVMe",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := builder.inferDiskBusType(tc.device)
			assert.Equal(t, tc.expectedBusType, result, tc.description)
		})
	}
}

func TestBusInference_VMwareDevices(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name            string
		device          string
		expectedBusType string
		description     string
	}{
		{
			name:            "VMware SCSI",
			device:          "sda",
			expectedBusType: "sata",
			description:     "VMware paravirtual SCSI",
		},
		{
			name:            "VMware IDE",
			device:          "hda",
			expectedBusType: "ide",
			description:     "VMware IDE controller",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := builder.inferDiskBusType(tc.device)
			assert.Equal(t, tc.expectedBusType, result, tc.description)
		})
	}
}

func TestBusInference_ContainerEnvironments(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name            string
		device          string
		expectedBusType string
		description     string
	}{
		{
			name:            "loop device",
			device:          "loop0",
			expectedBusType: "unknown",
			description:     "Container loop devices",
		},
		{
			name:            "device mapper",
			device:          "dm-0",
			expectedBusType: "unknown",
			description:     "LVM/docker overlay",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := builder.inferDiskBusType(tc.device)
			assert.Equal(t, tc.expectedBusType, result, tc.description)
		})
	}
}

func TestBusInference_NetworkCloudProviders(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name          string
		driver        string
		interfaceType string
		expected      string
		description   string
	}{
		{
			name:          "AWS ENA",
			driver:        "ena",
			interfaceType: "ethernet",
			expected:      "pci",
			description:   "AWS Elastic Network Adapter",
		},
		{
			name:          "GCP virtio",
			driver:        "virtio_net",
			interfaceType: "ethernet",
			expected:      "virtio",
			description:   "GCP VirtIO network",
		},
		{
			name:          "Azure Mellanox",
			driver:        "mlx5_core",
			interfaceType: "ethernet",
			expected:      "pci",
			description:   "Azure accelerated networking",
		},
		{
			name:          "VMware vmxnet3",
			driver:        "vmxnet3",
			interfaceType: "ethernet",
			expected:      "pci",
			description:   "VMware paravirtual NIC",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			iface := &performance.NetworkInfo{
				Interface: "eth0",
				Type:      tc.interfaceType,
				Driver:    tc.driver,
			}
			result := builder.inferNetworkBusType(iface)
			assert.Equal(t, tc.expected, result, tc.description)
		})
	}
}

func TestBusInference_PhysicalServerDisks(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		name            string
		device          string
		expectedBusType string
		description     string
	}{
		// Large server with many disks
		{"disk 1", "sda", "sata", "First SATA disk"},
		{"disk 10", "sdj", "sata", "Tenth SATA disk"},
		{"disk 20", "sdt", "sata", "Twentieth SATA disk"},
		{"disk 26", "sdz", "sata", "26th disk (last single letter)"},
		{"disk 27", "sdaa", "sata", "27th disk (first double letter)"},
		{"disk 52", "sdaz", "sata", "52nd disk"},

		// Mixed NVMe/SATA
		{"nvme ssd 1", "nvme0n1", "nvme", "First NVMe SSD"},
		{"nvme ssd 2", "nvme1n1", "nvme", "Second NVMe SSD"},
		{"nvme ssd 4", "nvme3n1", "nvme", "Fourth NVMe SSD"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := builder.inferDiskBusType(tc.device)
			assert.Equal(t, tc.expectedBusType, result, tc.description)
		})
	}
}

// Edge case: test case sensitivity
func TestInferDiskBusType_CaseSensitivity(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		device   string
		expected string
	}{
		{"NVME0N1", "unknown"}, // Uppercase should not match
		{"SDA", "unknown"},
		{"Sda", "unknown"},
		{"nvme", "nvme"}, // Exact prefix match
		{"NVMe0n1", "unknown"},
	}

	for _, tc := range testCases {
		t.Run(tc.device, func(t *testing.T) {
			result := builder.inferDiskBusType(tc.device)
			assert.Equal(t, tc.expected, result,
				"Case sensitivity test for %s", tc.device)
		})
	}
}

// Edge case: test substring matching behavior
func TestInferDiskBusType_SubstringMatching(t *testing.T) {
	builder := NewBuilder(logr.Discard(), nil)

	testCases := []struct {
		device   string
		expected string
	}{
		// Prefix matching should work
		{"nvme", "nvme"},
		{"sd", "sata"},
		{"hd", "ide"},
		{"vd", "virtio"},
		{"xvd", "virtio"},

		// Should not match if prefix doesn't match
		{"mynvme", "unknown"},
		{"thesda", "unknown"},
	}

	for _, tc := range testCases {
		t.Run(tc.device, func(t *testing.T) {
			result := builder.inferDiskBusType(tc.device)
			assert.Equal(t, tc.expected, result,
				"Substring matching for %s", tc.device)
		})
	}
}
