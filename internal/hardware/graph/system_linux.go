// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux

package graph

import (
	"os"
	"path"
	"strings"
)

// getMachineID reads the OS-specific machine ID from /etc/machine-id
// This is distinct from the hardware UUID and hostname
func getMachineID() string {
	// Try /etc/machine-id and /var/lib/dbus/machine-id

	if data, err := os.ReadFile(path.Join(etcDir, "machine-id")); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	}

	if data, err := os.ReadFile(path.Join(varDir, "lib", "dbus", "machine-id")); err == nil {
		if uuid := strings.TrimSpace(string(data)); uuid != "" {
			return uuid
		}
	}

	return ""
}

// getSystemUUID reads the hardware UUID from DMI
func getSystemUUID() string {
	// Try /sys/class/dmi/id/product_uuid (hardware-based, requires root)
	if data, err := os.ReadFile(path.Join(sysDir, "class", "dmi", "id", "product_uuid")); err == nil {
		if uuid := strings.TrimSpace(string(data)); uuid != "" {
			return uuid
		}
	}
	return ""
}
