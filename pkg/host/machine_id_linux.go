// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux

// Package host provides utilities for host and machine identification
package host

import (
	"fmt"
	"os"
	"strings"
)

// MachineID returns a unique machine ID of the local system that is set
// during installation or boot.
// It attempts multiple sources in order of preference:
// 1. /etc/machine-id (systemd standard, most reliable)
// 2. /var/lib/dbus/machine-id (D-Bus machine ID, fallback)
//
// This function can be used to ensure resource uniqueness across multi-node clusters,
// as container IDs and process PIDs are only unique within a single host.
func MachineID() (string, error) {
	// Try /etc/machine-id (OS-specific identifier, most common on Linux)
	// This is the systemd standard and is persistent across reboots
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id, nil
		}
	}

	// Try /var/lib/dbus/machine-id as fallback
	if data, err := os.ReadFile("/var/lib/dbus/machine-id"); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id, nil
		}
	}

	return "", fmt.Errorf("machine-id not found")
}

// SystemUUID reads the hardware UUID from DMI.
// This is distinct from the machine ID and represents the hardware platform.
// Returns empty string if not available or not accessible (requires root).
func SystemUUID() (string, error) {
	// Try /sys/class/dmi/id/product_uuid (hardware-based, requires root)
	if data, err := os.ReadFile("/sys/class/dmi/id/product_uuid"); err == nil {
		if uuid := strings.TrimSpace(string(data)); uuid != "" {
			return uuid, nil
		}
	}
	return "", fmt.Errorf("system uuid not found")
}
