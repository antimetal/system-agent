// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build linux

package host

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/antimetal/agent/pkg/config/environment"
)

func hostname() (string, error) {
	hostPaths := environment.GetHostPaths()
	hostFile := filepath.Join(hostPaths.Proc, "sys/kernel/hostname")
	f, err := os.Open(hostFile)
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := make([]byte, 512) // enough for a DNS name
	n, err := f.Read(buf)
	if err != nil {
		return "", err
	}

	if n > 0 && buf[n-1] == '\n' {
		n--
	}
	return string(buf[:n]), nil
}

// machineID returns a unique machine ID of the local system that is set
// during installation or boot.
// It attempts multiple sources in order of preference:
// 1. /etc/machine-id (systemd standard, most reliable)
// 2. /var/lib/dbus/machine-id (D-Bus machine ID, fallback)
func machineID() (string, error) {
	hostPaths := environment.GetHostPaths()

	machineIDPath := filepath.Join(hostPaths.Etc, "machine-id")
	if data, err := os.ReadFile(machineIDPath); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id, nil
		}
	}

	dbusMachineIDPath := filepath.Join(hostPaths.Var, "lib/dbus/machine-id")
	if data, err := os.ReadFile(dbusMachineIDPath); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id, nil
		}
	}

	return "", fmt.Errorf("machine-id not found")
}

// systemUUID reads the hardware UUID from DMI.
// Returns empty string if not available or not accessible (requires root).
func systemUUID() (string, error) {
	hostPaths := environment.GetHostPaths()
	productUUID := filepath.Join(hostPaths.Sys, "class/dmi/id/product_uuid")
	if data, err := os.ReadFile(productUUID); err == nil {
		if uuid := strings.TrimSpace(string(data)); uuid != "" {
			return uuid, nil
		}
	}
	return "", fmt.Errorf("system uuid not found")
}
