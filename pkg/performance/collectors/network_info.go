// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

// NetworkInfoCollector collects network interface hardware configuration from the Linux sysfs filesystem.
//
// Data Sources and Standardization:
//
// This collector reads network interface information from /sys/class/net/, which is part of the
// Linux sysfs virtual filesystem. Unlike some other sysfs areas, network interface attributes
// have varying levels of standardization:
//
// KERNEL-GUARANTEED DATA (standardized format):
// These files are managed by the kernel networking subsystem and have consistent formats:
//   - /sys/class/net/[interface]/address        - MAC address (kernel-standardized)
//   - /sys/class/net/[interface]/mtu            - Maximum Transmission Unit (kernel-standardized)
//   - /sys/class/net/[interface]/operstate      - Operational state (kernel-standardized)
//   - /sys/class/net/[interface]/carrier        - Link carrier status (kernel-standardized)
//   - /sys/class/net/[interface]/type           - Interface type number (kernel-standardized)
//
// DRIVER-DEPENDENT DATA (vendor/driver-specific):
// These files depend on driver implementation and may vary between vendors:
//   - /sys/class/net/[interface]/speed          - Link speed (driver-dependent, may be unavailable)
//   - /sys/class/net/[interface]/duplex         - Link duplex mode (driver-dependent)
//   - /sys/class/net/[interface]/device/driver  - Driver name (driver-dependent)
//   - Driver version information                - Highly driver-specific, often unavailable
//
// Interface Type Detection:
// The collector uses multiple methods to determine interface type:
// 1. Kernel type numbers (standardized): /sys/class/net/[interface]/type
// 2. Directory presence (e.g., wireless/ subdirectory for WiFi)
// 3. Naming conventions (eth*, wlan*, etc.) - NOT standardized, but common
// 4. Device symlinks to determine if hardware-backed
//
// Important Notes:
// - Speed/duplex may be unavailable for down interfaces or virtual interfaces
// - Driver information depends on hardware abstraction layer
// - Interface naming is distribution/udev-dependent, not kernel-guaranteed
// - Virtual interfaces (bridges, tunnels) may have limited hardware info
//
// References:
// - Linux netdev sysfs documentation: https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-class-net
// - Network interface types: https://www.kernel.org/doc/html/latest/networking/netdevices.html
// - Driver model: https://www.kernel.org/doc/html/latest/driver-api/driver-model/
type NetworkInfoCollector struct {
	performance.BaseCollector
	netClassPath string
}

// Compile-time interface check
var _ performance.PointCollector = (*NetworkInfoCollector)(nil)

func NewNetworkInfoCollector(logger logr.Logger, config performance.CollectionConfig) (*NetworkInfoCollector, error) {
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:    true,
		SupportsContinuous: false,
		RequiresRoot:       false,
		RequiresEBPF:       false,
		MinKernelVersion:   "2.6.0",
	}

	return &NetworkInfoCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeNetworkInfo,
			"Network Interface Hardware Info Collector",
			logger,
			config,
			capabilities,
		),
		netClassPath: filepath.Join(config.HostSysPath, "class", "net"),
	}, nil
}

func (c *NetworkInfoCollector) Collect(ctx context.Context) (any, error) {
	return c.collectNetworkInfo()
}

// collectNetworkInfo discovers and collects information for all network interfaces.
//
// This method implements the core discovery logic:
// 1. Lists all entries in /sys/class/net/ (each represents a network interface)
// 2. For each interface, determines its type using multiple detection methods
// 3. Collects both kernel-standardized and driver-specific properties
//
// Interface Discovery:
// The kernel creates a directory/symlink in /sys/class/net/ for every network interface,
// including physical interfaces, virtual interfaces, bridges, tunnels, and loopbacks.
// The directory name is the interface name (e.g., eth0, wlan0, docker0).
//
// Note: Interface names are NOT kernel-standardized - they're assigned by:
// - udev rules (distribution-specific)
// - systemd network naming (predictable names like enp0s3)
// - Manual configuration
// - Driver defaults
//
// See: https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-class-net
func (c *NetworkInfoCollector) collectNetworkInfo() ([]performance.NetworkInfo, error) {
	interfaces := make([]performance.NetworkInfo, 0)

	entries, err := os.ReadDir(c.netClassPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read network interfaces: %w", err)
	}

	for _, entry := range entries {
		interfaceName := entry.Name()
		interfacePath := filepath.Join(c.netClassPath, interfaceName)

		// Check if it's a valid interface directory or symlink
		// Most entries are symlinks to actual device directories
		if stat, err := os.Stat(interfacePath); err == nil && stat.IsDir() {
			info := performance.NetworkInfo{
				Interface: interfaceName,
			}
			info.Type = c.getInterfaceType(interfaceName, interfacePath)
			c.parseInterfaceProperties(&info, interfacePath)

			interfaces = append(interfaces, info)
		}
	}

	return interfaces, nil
}

// getInterfaceType determines the type of network interface using multiple detection methods.
//
// This method uses a hierarchical approach combining kernel-standardized data with heuristics:
//
// 1. KERNEL-STANDARDIZED Detection:
//   - wireless/ subdirectory presence (kernel creates this for wireless interfaces)
//   - type file with standard ARPHRD_* constants (kernel-guaranteed)
//   - device/ symlink presence (indicates hardware-backed interface)
//
// 2. WELL-KNOWN Interface Names:
//   - "lo" is universally the loopback interface
//
// 3. HEURISTIC Detection (naming conventions):
//   - These are common but NOT kernel-guaranteed
//   - Different distributions/configurations may use different naming
//
// Interface Type Numbers (from if_arp.h):
// - 1 (ARPHRD_ETHER): Ethernet
// - 772 (ARPHRD_LOOPBACK): Loopback
// - 776 (ARPHRD_SIT): IPv6-in-IPv4 tunnel
// - 778 (ARPHRD_IPGRE): GRE tunnel
//
// Naming Convention Heuristics (distribution-dependent):
// - eth*: Traditional ethernet naming
// - wlan*: Common wireless naming
// - tun*/tap*: Tunnel interfaces
// - veth*: Virtual ethernet pairs
// - docker*/br-*: Bridge interfaces
// - virbr*: Virtualization bridges
//
// Fallback Logic:
// - If device/ symlink exists → physical "ethernet"
// - Otherwise → "virtual" interface
//
// References:
// - ARPHRD constants: https://elixir.bootlin.com/linux/latest/source/include/uapi/linux/if_arp.h
// - Network device types: https://www.kernel.org/doc/html/latest/networking/netdevices.html
func (c *NetworkInfoCollector) getInterfaceType(name string, path string) string {
	// KERNEL-STANDARDIZED: Check for wireless subdirectory
	// The kernel creates this directory for all wireless interfaces
	wirelessPath := filepath.Join(path, "wireless")
	if _, err := os.Stat(wirelessPath); err == nil {
		return "wireless"
	}

	// WELL-KNOWN: Loopback interface is universally named "lo"
	if name == "lo" {
		return "loopback"
	}

	// KERNEL-STANDARDIZED: Check interface type number
	// Path: /sys/class/net/[interface]/type
	// Contains ARPHRD_* constants from if_arp.h (kernel-guaranteed format)
	typePath := filepath.Join(path, "type")
	if data, err := os.ReadFile(typePath); err == nil {
		typeNum := strings.TrimSpace(string(data))
		switch typeNum {
		case "1": // ARPHRD_ETHER
			return "ethernet"
		case "772": // ARPHRD_LOOPBACK
			return "loopback"
		case "776": // ARPHRD_SIT (IPv6-in-IPv4)
			return "tunnel"
		case "778": // ARPHRD_IPGRE (GRE tunnel)
			return "tunnel"
		}
	}

	// HEURISTIC: Check common naming patterns
	// WARNING: These are NOT kernel-guaranteed - they're distribution/configuration-dependent
	switch {
	case strings.HasPrefix(name, "eth"):
		return "ethernet" // Traditional ethernet naming
	case strings.HasPrefix(name, "wlan"):
		return "wireless" // Common wireless naming
	case strings.HasPrefix(name, "tun"):
		return "tunnel" // TUN interfaces
	case strings.HasPrefix(name, "tap"):
		return "tap" // TAP interfaces
	case strings.HasPrefix(name, "veth"):
		return "virtual" // Virtual ethernet pairs (containers)
	case strings.HasPrefix(name, "docker"):
		return "bridge" // Docker bridge interfaces
	case strings.HasPrefix(name, "br-"):
		return "bridge" // Bridge interfaces
	case strings.HasPrefix(name, "virbr"):
		return "bridge" // Virtualization bridges (libvirt)
	}

	// FALLBACK: Check if hardware-backed
	// Physical interfaces have device/ symlink to hardware
	devicePath := filepath.Join(path, "device")
	if _, err := os.Stat(devicePath); err == nil {
		return "ethernet" // Physical interface, assume ethernet
	}

	// Default: Virtual interface
	return "virtual"
}

// parseInterfaceProperties reads interface properties from sysfs files.
//
// This method collects both kernel-standardized and driver-dependent information:
//
// KERNEL-STANDARDIZED Properties (consistent format across all drivers):
// - address: MAC address in standard format (XX:XX:XX:XX:XX:XX)
// - mtu: Maximum Transmission Unit in bytes
// - operstate: Operational state (up, down, dormant, etc.)
// - carrier: Physical link status (1=up, 0=down)
//
// DRIVER-DEPENDENT Properties (may vary or be unavailable):
// - speed: Link speed in Mbps (driver must support and interface must be up)
// - duplex: Link duplex mode (full, half, unknown)
// - driver: Driver name (only for hardware interfaces)
// - driver version: Highly driver-specific, often unavailable
//
// Important Behavior Notes:
// - speed may be -1 for down interfaces or unsupported hardware
// - duplex may be "unknown" for virtual interfaces or when link is down
// - driver information only available for hardware-backed interfaces
// - All reads are gracefully handled - missing files result in zero/empty values
//
// References:
// - Network sysfs ABI: https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-class-net
// - Operational states: https://www.kernel.org/doc/html/latest/networking/operstates.html
func (c *NetworkInfoCollector) parseInterfaceProperties(info *performance.NetworkInfo, interfacePath string) {
	// KERNEL-STANDARDIZED: Read MAC address
	addressPath := filepath.Join(interfacePath, "address")
	if data, err := os.ReadFile(addressPath); err == nil {
		info.MACAddress = strings.TrimSpace(string(data))
	}

	// DRIVER-DEPENDENT: Read link speed in Mbps
	speedPath := filepath.Join(interfacePath, "speed")
	if data, err := os.ReadFile(speedPath); err == nil {
		speedStr := strings.TrimSpace(string(data))
		// Speed might be -1 for down interfaces or unsupported
		if speed, err := strconv.ParseInt(speedStr, 10, 64); err == nil && speed > 0 {
			info.Speed = uint64(speed)
		}
	}

	// DRIVER-DEPENDENT: Read duplex mode
	// Values: "full", "half", "unknown" (driver-dependent availability)
	duplexPath := filepath.Join(interfacePath, "duplex")
	if data, err := os.ReadFile(duplexPath); err == nil {
		info.Duplex = strings.TrimSpace(string(data))
	}

	// KERNEL-STANDARDIZED: Read Maximum Transmission Unit
	// Format: Decimal number in bytes (kernel-guaranteed)
	mtuPath := filepath.Join(interfacePath, "mtu")
	if data, err := os.ReadFile(mtuPath); err == nil {
		if mtu, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32); err == nil {
			info.MTU = uint32(mtu)
		}
	}

	// KERNEL-STANDARDIZED: Read operational state
	// Values: "up", "down", "dormant", "testing", "unknown", "notpresent", "lowerlayerdown"
	// See: https://www.kernel.org/doc/html/latest/networking/operstates.html
	operstatePath := filepath.Join(interfacePath, "operstate")
	if data, err := os.ReadFile(operstatePath); err == nil {
		info.OperState = strings.TrimSpace(string(data))
	}

	// KERNEL-STANDARDIZED: Read carrier (physical link) status
	// Values: "1" (link up), "0" (link down) - kernel-guaranteed format
	carrierPath := filepath.Join(interfacePath, "carrier")
	if data, err := os.ReadFile(carrierPath); err == nil {
		info.Carrier = strings.TrimSpace(string(data)) == "1"
	}

	// DRIVER-DEPENDENT: Try to get driver name
	// Only available for hardware-backed interfaces
	driverPath := filepath.Join(interfacePath, "device", "driver")
	if target, err := os.Readlink(driverPath); err == nil {
		info.Driver = filepath.Base(target)
	}
}
