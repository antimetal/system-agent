// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package graph

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	hardwarev1 "github.com/antimetal/agent/pkg/api/antimetal/hardware/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/kernel"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/proc"
)

var (
	kindResource = string((&resourcev1.Resource{}).ProtoReflect().Descriptor().FullName())

	sysDir     string
	etcDir     string
	varDir     string
	machineID  string
	systemUUID string
)

func init() {
	sysDir = os.Getenv("HOST_SYS")
	if sysDir == "" {
		sysDir = "/sys"
	}

	etcDir = os.Getenv("HOST_ETC")
	if etcDir == "" {
		etcDir = "/etc"
	}

	varDir = os.Getenv("HOST_VAR")
	if varDir == "" {
		varDir = "/var"
	}

	// assume these are static and set at boot time
	machineID = getMachineID()
	systemUUID = getSystemUUID()
}

// getSystemInfo gathers system information using existing utilities
func (b *Builder) getSystemInfo() (arch hardwarev1.Architecture, bootTime time.Time, kernelVersion string, osInfo string) {
	// Get architecture from runtime - this is what the agent was compiled for
	// and matches the actual system architecture
	switch runtime.GOARCH {
	case "amd64":
		arch = hardwarev1.Architecture_ARCHITECTURE_X86_64
	case "arm64":
		arch = hardwarev1.Architecture_ARCHITECTURE_ARM64
	case "386":
		arch = hardwarev1.Architecture_ARCHITECTURE_X86
	default:
		arch = hardwarev1.Architecture_ARCHITECTURE_UNKNOWN
	}

	bootTime, err := proc.BootTime()
	if err != nil {
		b.logger.Error(err, "Failed to get boot time, using current time")
		bootTime = time.Now()
	}

	// Use existing kernel version detection from kernel package
	if kv, err := kernel.GetCurrentVersion(); err == nil {
		kernelVersion = kv.Raw
	} else {
		b.logger.Error(err, "Failed to get kernel version")
		kernelVersion = "unknown"
	}

	// Get OS info from /etc/os-release - keep this as is since there's no existing utility
	osInfo = getOSInfo()

	return
}

// getOSInfo reads OS information from os-release files according to freedesktop.org standard
func getOSInfo() string {
	// Try /etc/os-release first (primary location per freedesktop.org spec)
	file, err := os.Open(path.Join(etcDir, "os-release"))
	if err != nil {
		// Fall back to /usr/lib/os-release (secondary location)
		file, err = os.Open("/usr/lib/os-release")
		if err != nil {
			return "Linux" // Final fallback
		}
	}
	defer file.Close()

	var prettyName string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			// Remove PRETTY_NAME= and quotes
			prettyName = strings.TrimPrefix(line, "PRETTY_NAME=")
			prettyName = strings.Trim(prettyName, `"`)
			return prettyName
		}
	}
	return "Linux" // Fallback
}

// createSystemNode creates the root system node representing the machine
func (b *Builder) createSystemNode() (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	// Get system information
	arch, bootTime, kernelVersion, osInfo := b.getSystemInfo()

	// Get hostname (network identifier)
	hostname, err := os.Hostname()
	if err != nil {
		b.logger.Error(err, "Failed to get hostname, using 'unknown'")
		hostname = "unknown"
	}

	// Create system node spec
	systemSpec := &hardwarev1.SystemNode{
		Hostname:      hostname,
		Architecture:  arch,
		BootTime:      timestamppb.New(bootTime),
		KernelVersion: kernelVersion,
		OsInfo:        osInfo,
	}

	// Marshal the spec
	specAny, err := anypb.New(systemSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal system spec: %w", err)
	}

	// Build tags list
	tags := []*resourcev1.Tag{
		{Key: "hostname", Value: hostname},
		{Key: "machine-id", Value: machineID},
	}

	// Add system UUID tag if available
	if systemUUID != "" {
		tags = append(tags, &resourcev1.Tag{Key: "system-uuid", Value: systemUUID})
	}

	name := machineID
	if name == "" {
		name = systemUUID
	}

	// Create resource with machine ID as the name (globally unique)
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindResource,
			Type: string(systemSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_ANTIMETAL,
			ProviderId: name,
			Name:       name,
			Tags:       tags,
		},
		Spec: specAny,
	}

	// Create reference
	ref := &resourcev1.ResourceRef{
		TypeUrl: string(systemSpec.ProtoReflect().Descriptor().FullName()),
		Name:    hostname, // Keep hostname for readability, machine ID is in ProviderId
	}

	return resource, ref, nil
}

// createCPUPackageNode creates a CPU package (socket) node
func (b *Builder) createCPUPackageNode(cpuInfo *performance.CPUInfo, physicalID int32) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	// Find the first core with this physical ID to get package info
	var sampleCore *performance.CPUCore
	for _, core := range cpuInfo.Cores {
		if core.PhysicalID == physicalID {
			sampleCore = &core
			break
		}
	}

	if sampleCore == nil {
		return nil, nil, fmt.Errorf("no core found for physical ID %d", physicalID)
	}

	// Count cores for this package
	var physicalCores, logicalCores int32
	for _, core := range cpuInfo.Cores {
		if core.PhysicalID == physicalID {
			logicalCores++
			// Count unique core IDs for physical cores
			// This is a simplification - in reality we'd need to track unique core IDs
			physicalCores++
		}
	}

	// Create CPU package spec
	packageSpec := &hardwarev1.CPUPackageNode{
		SocketId:      physicalID,
		VendorId:      cpuInfo.VendorID,
		ModelName:     cpuInfo.ModelName,
		CpuFamily:     cpuInfo.CPUFamily,
		Model:         cpuInfo.Model,
		Stepping:      cpuInfo.Stepping,
		Microcode:     cpuInfo.Microcode,
		CacheSize:     cpuInfo.CacheSize,
		PhysicalCores: physicalCores,
		LogicalCores:  logicalCores,
	}

	// Marshal the spec
	specAny, err := anypb.New(packageSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal CPU package spec: %w", err)
	}

	// Create resource
	packageName := fmt.Sprintf("cpu-package-%d", physicalID)
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindResource,
			Type: string(packageSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_ANTIMETAL,
			ProviderId: packageName,
			Name:       packageName,
		},
		Spec: specAny,
	}

	// Create reference
	ref := &resourcev1.ResourceRef{
		TypeUrl: string(packageSpec.ProtoReflect().Descriptor().FullName()),
		Name:    packageName,
	}

	return resource, ref, nil
}

// createCPUCoreNode creates a CPU core node
func (b *Builder) createCPUCoreNode(core *performance.CPUCore) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	// Create CPU core spec
	coreSpec := &hardwarev1.CPUCoreNode{
		ProcessorId:  int32(core.Processor),
		CoreId:       int32(core.CoreID),
		PhysicalId:   core.PhysicalID,
		FrequencyMhz: core.CPUMHz,
		Siblings:     int32(core.Siblings),
	}

	// Marshal the spec
	specAny, err := anypb.New(coreSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal CPU core spec: %w", err)
	}

	// Create resource
	coreName := fmt.Sprintf("cpu-core-%d", core.Processor)
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindResource,
			Type: string(coreSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_ANTIMETAL,
			ProviderId: coreName,
			Name:       coreName,
		},
		Spec: specAny,
	}

	// Create reference
	ref := &resourcev1.ResourceRef{
		TypeUrl: string(coreSpec.ProtoReflect().Descriptor().FullName()),
		Name:    coreName,
	}

	return resource, ref, nil
}

// createMemoryModuleNode creates a memory module node
func (b *Builder) createMemoryModuleNode(memInfo *performance.MemoryInfo) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	// Create memory module spec
	memSpec := &hardwarev1.MemoryModuleNode{
		TotalBytes:             memInfo.TotalBytes,
		NumaEnabled:            memInfo.NUMAEnabled,
		NumaBalancingAvailable: memInfo.NUMABalancingAvailable,
		NumaNodeCount:          int32(len(memInfo.NUMANodes)),
	}

	// Marshal the spec
	specAny, err := anypb.New(memSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal memory spec: %w", err)
	}

	// Create resource
	memName := "system-memory"
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindResource,
			Type: string(memSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_ANTIMETAL,
			ProviderId: memName,
			Name:       memName,
		},
		Spec: specAny,
	}

	// Create reference
	ref := &resourcev1.ResourceRef{
		TypeUrl: string(memSpec.ProtoReflect().Descriptor().FullName()),
		Name:    memName,
	}

	return resource, ref, nil
}

// createNUMANode creates a NUMA node
func (b *Builder) createNUMANode(numa *performance.NUMANode) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	// Convert CPU list to int32 slice
	cpus := make([]int32, len(numa.CPUs))
	for i, cpu := range numa.CPUs {
		cpus[i] = int32(cpu)
	}

	// Convert distances map to slice
	distances := make([]int32, len(numa.Distances))
	for nodeID, distance := range numa.Distances {
		if nodeID < len(distances) {
			distances[nodeID] = int32(distance)
		}
	}

	// Create NUMA node spec
	numaSpec := &hardwarev1.NUMANode{
		NodeId:     numa.NodeID,
		TotalBytes: numa.TotalBytes,
		Cpus:       cpus,
		Distances:  distances,
	}

	// Marshal the spec
	specAny, err := anypb.New(numaSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal NUMA spec: %w", err)
	}

	// Create resource
	numaName := fmt.Sprintf("numa-node-%d", numa.NodeID)
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindResource,
			Type: string(numaSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_ANTIMETAL,
			ProviderId: numaName,
			Name:       numaName,
		},
		Spec: specAny,
	}

	// Create reference
	ref := &resourcev1.ResourceRef{
		TypeUrl: string(numaSpec.ProtoReflect().Descriptor().FullName()),
		Name:    numaName,
	}

	return resource, ref, nil
}

// createDiskDeviceNode creates a disk device node
func (b *Builder) createDiskDeviceNode(disk *performance.DiskInfo) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	// Create disk device spec
	diskSpec := &hardwarev1.DiskDeviceNode{
		Device:            disk.Device,
		Model:             disk.Model,
		Vendor:            disk.Vendor,
		SizeBytes:         disk.SizeBytes,
		Rotational:        disk.Rotational,
		BlockSize:         uint32(disk.BlockSize),
		PhysicalBlockSize: uint32(disk.PhysicalBlockSize),
		Scheduler:         disk.Scheduler,
		QueueDepth:        uint32(disk.QueueDepth),
	}

	// Marshal the spec
	specAny, err := anypb.New(diskSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal disk spec: %w", err)
	}

	// Create resource
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindResource,
			Type: string(diskSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_ANTIMETAL,
			ProviderId: disk.Device,
			Name:       disk.Device,
		},
		Spec: specAny,
	}

	// Create reference
	ref := &resourcev1.ResourceRef{
		TypeUrl: string(diskSpec.ProtoReflect().Descriptor().FullName()),
		Name:    disk.Device,
	}

	return resource, ref, nil
}

// createDiskPartitionNode creates a disk partition node
func (b *Builder) createDiskPartitionNode(partition *performance.PartitionInfo, parentDevice string) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	// Create disk partition spec
	partSpec := &hardwarev1.DiskPartitionNode{
		Name:         partition.Name,
		ParentDevice: parentDevice,
		SizeBytes:    partition.SizeBytes,
		StartSector:  partition.StartSector,
	}

	// Marshal the spec
	specAny, err := anypb.New(partSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal partition spec: %w", err)
	}

	// Create resource
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindResource,
			Type: string(partSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_ANTIMETAL,
			ProviderId: partition.Name,
			Name:       partition.Name,
		},
		Spec: specAny,
	}

	// Create reference
	ref := &resourcev1.ResourceRef{
		TypeUrl: string(partSpec.ProtoReflect().Descriptor().FullName()),
		Name:    partition.Name,
	}

	return resource, ref, nil
}

// createNetworkInterfaceNode creates a network interface node
func (b *Builder) createNetworkInterfaceNode(iface *performance.NetworkInfo) (*resourcev1.Resource, *resourcev1.ResourceRef, error) {
	// Map duplex mode
	var duplexMode hardwarev1.DuplexMode
	switch strings.ToLower(iface.Duplex) {
	case "full":
		duplexMode = hardwarev1.DuplexMode_DUPLEX_MODE_FULL
	case "half":
		duplexMode = hardwarev1.DuplexMode_DUPLEX_MODE_HALF
	default:
		duplexMode = hardwarev1.DuplexMode_DUPLEX_MODE_UNKNOWN
	}

	// Map interface type
	var ifaceType hardwarev1.InterfaceType
	switch iface.Type {
	case "ethernet":
		ifaceType = hardwarev1.InterfaceType_INTERFACE_TYPE_ETHERNET
	case "wireless":
		ifaceType = hardwarev1.InterfaceType_INTERFACE_TYPE_WIRELESS
	case "loopback":
		ifaceType = hardwarev1.InterfaceType_INTERFACE_TYPE_LOOPBACK
	case "bridge":
		ifaceType = hardwarev1.InterfaceType_INTERFACE_TYPE_BRIDGE
	case "vlan":
		ifaceType = hardwarev1.InterfaceType_INTERFACE_TYPE_VLAN
	case "bond":
		ifaceType = hardwarev1.InterfaceType_INTERFACE_TYPE_BOND
	default:
		ifaceType = hardwarev1.InterfaceType_INTERFACE_TYPE_UNKNOWN
	}

	// Map operational state
	var operState hardwarev1.OperationalState
	switch strings.ToLower(iface.OperState) {
	case "up":
		operState = hardwarev1.OperationalState_OPERATIONAL_STATE_UP
	case "down":
		operState = hardwarev1.OperationalState_OPERATIONAL_STATE_DOWN
	case "testing":
		operState = hardwarev1.OperationalState_OPERATIONAL_STATE_TESTING
	case "dormant":
		operState = hardwarev1.OperationalState_OPERATIONAL_STATE_DORMANT
	case "notpresent":
		operState = hardwarev1.OperationalState_OPERATIONAL_STATE_NOT_PRESENT
	case "lowerlayerdown":
		operState = hardwarev1.OperationalState_OPERATIONAL_STATE_LOWER_LAYER_DOWN
	default:
		operState = hardwarev1.OperationalState_OPERATIONAL_STATE_UNKNOWN
	}

	// Create network interface spec
	netSpec := &hardwarev1.NetworkInterfaceNode{
		Interface:     iface.Interface,
		MacAddress:    iface.MACAddress,
		Speed:         iface.Speed,
		Duplex:        duplexMode,
		Mtu:           uint32(iface.MTU),
		Driver:        iface.Driver,
		Type:          ifaceType,
		Ipv4Addresses: []string{}, // TODO: Add IP address collection to NetworkInfo
		Ipv6Addresses: []string{}, // TODO: Add IP address collection to NetworkInfo
		OperState:     operState,
		Carrier:       iface.Carrier,
	}

	// Marshal the spec
	specAny, err := anypb.New(netSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal network spec: %w", err)
	}

	// Create resource
	resource := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindResource,
			Type: string(netSpec.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_ANTIMETAL,
			ProviderId: iface.Interface,
			Name:       iface.Interface,
		},
		Spec: specAny,
	}

	// Create reference
	ref := &resourcev1.ResourceRef{
		TypeUrl: string(netSpec.ProtoReflect().Descriptor().FullName()),
		Name:    iface.Interface,
	}

	return resource, ref, nil
}
