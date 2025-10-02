// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

// Package host provides utilities for host and machine identification
package host

import (
	"fmt"
	"net"
	"strings"
)

// MachineID returns a unique machine ID of the local system that is set
// during installation or boot by systemd.
func MachineID() (string, error) {
	return machineID()
}

// SystemUUID reads the hardware UUID.
// This is distinct from the machine ID and represents the hardware or
// virtual machine platform.
func SystemUUID() (string, error) {
	return systemUUID()
}

// Hostname returns the hostname reported by the kernel.
// In particular it returns the hostname of the host machine
// when inside a container.
func Hostname() (string, error) {
	return hostname()
}

// FQDN returns the full FQDN of the hostname reported by the kernel.
// In particular it returns the fqdn of the host machine when
// inside a container.
func FQDN() (string, error) {
	host, err := Hostname()
	if err != nil {
		return "", fmt.Errorf("could not get hostname: %w", err)
	}

	fqdn, err := net.LookupCNAME(host)
	if err == nil || len(fqdn) > 0 {
		return strings.TrimSuffix(fqdn, "."), nil
	}

	addrs, err := net.LookupIP(host)
	if err != nil {
		return "", fmt.Errorf("could not find fqdn: %w", err)
	}

	var hosts []string
	for _, addr := range addrs {
		hosts, err = net.LookupAddr(addr.String())
		if err == nil || len(hosts) > 0 {
			fqdn = hosts[0]
			break
		}
	}

	if err != nil {
		return "", fmt.Errorf("could not find fqdn: %w", err)
	}
	return fqdn, err
}
