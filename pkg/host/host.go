// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

// Package host provides utilities for host and machine identification
package host

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/antimetal/agent/pkg/aws"
)

// MachineInformation contains metadata about the host machine.
type MachineInformation struct {
	PrettyHostname  string
	IconName        string
	Chassis         string
	Deployment      string
	Location        string
	HardwareVendor  string
	HardwareModel   string
	HardwareSKU     string
	HardwareVersion string
}

// CanonicalName gets the name of the host using the following preference order:
//
// 1. Human defined named using MachineInfo
// 2. CloudProviderID
// 3. FQDN
// 4. MachineID
//
// Note that we omit Hostname from the list because we cannot guarantee uniqueness
// across connected networks.
func CanonicalName() (string, error) {
	info, err := MachineInfo()
	if err == nil {
		if name := createHostNameFromMachineInfo(info); name != "" {
			return name, nil
		}
	}

	if cloudID, err := CloudProviderID(); err == nil && cloudID != "" {
		return cloudID, nil
	}

	if fqdn, err := FQDN(); err == nil && fqdn != "" {
		return fqdn, nil
	}

	if machineID, err := MachineID(); err == nil && machineID != "" {
		return machineID, nil
	}

	return "", fmt.Errorf("could not come up with canonical name for the host")
}

// MachineID returns a unique machine ID of the local system that is set
// during installation or boot by systemd.
func MachineID() (string, error) {
	return machineID()
}

// MachineInfo returns machine metadata info.
func MachineInfo() (*MachineInformation, error) {
	return machineInfo()
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

// CloudProviderID returns the id of the host if managed by a cloud provider (e.g. AWS).
// If the host is not managed by the cloud provider then it return an error.
func CloudProviderID() (string, error) {
	ctx := context.Background()

	return ec2InstanceID(ctx)
}

func ec2InstanceID(ctx context.Context) (string, error) {
	client, err := aws.NewClient(aws.WithAutoDiscovery(ctx))
	if err != nil {
		return "", fmt.Errorf("failed to create AWS client: %w", err)
	}

	instanceID, err := client.GetEC2InstanceID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get EC2 instance ID: %w", err)
	}

	return instanceID, nil
}

func createHostNameFromMachineInfo(info *MachineInformation) string {
	if info == nil {
		return ""
	}

	if info.Location != "" && info.Deployment != "" && info.PrettyHostname != "" {
		return fmt.Sprintf("%s-%s-%s", info.Location, info.Deployment, info.PrettyHostname)
	}

	if info.Deployment != "" && info.PrettyHostname != "" {
		return fmt.Sprintf("%s-%s", info.Deployment, info.PrettyHostname)
	}

	return info.PrettyHostname
}
