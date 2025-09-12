// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux

// Package host provides utilities for host and machine identification
package host

import (
	"os"
	"strings"
)

// GetMachineID returns a stable, unique identifier for the host machine.
// It attempts multiple sources in order of preference:
// 1. /etc/machine-id (systemd standard, most reliable)
// 2. /var/lib/dbus/machine-id (D-Bus machine ID, fallback)
// 3. /sys/class/dmi/id/product_uuid (hardware UUID, requires root)
// 4. Hostname (fallback, less stable but always available)
//
// This function is used to ensure resource uniqueness across multi-node clusters,
// as container IDs and process PIDs are only unique within a single host.
func GetMachineID() string {
	// Try /etc/machine-id (OS-specific identifier, most common on Linux)
	// This is the systemd standard and is persistent across reboots
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	}

	// Try /var/lib/dbus/machine-id as fallback
	if data, err := os.ReadFile("/var/lib/dbus/machine-id"); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	}

	// Fall back to system UUID if machine-id not available
	// This requires root access but provides a hardware-based identifier
	if data, err := os.ReadFile("/sys/class/dmi/id/product_uuid"); err == nil {
		if uuid := strings.TrimSpace(string(data)); uuid != "" {
			return uuid
		}
	}

	// Last resort: use hostname
	// This is less stable (can be changed) but always available
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		return hostname
	}

	return "unknown"
}

// GetSystemUUID reads the hardware UUID from DMI.
// This is distinct from the machine ID and represents the hardware platform.
// Returns empty string if not available or not accessible (requires root).
func GetSystemUUID() string {
	// Try /sys/class/dmi/id/product_uuid (hardware-based, requires root)
	if data, err := os.ReadFile("/sys/class/dmi/id/product_uuid"); err == nil {
		if uuid := strings.TrimSpace(string(data)); uuid != "" {
			return uuid
		}
	}
	return ""
}
