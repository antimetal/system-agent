// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/antimetal/agent/pkg/performance/collectors"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestNetworkInfoCollector(t *testing.T) (*collectors.NetworkInfoCollector, string) {
	tmpDir := t.TempDir()
	sysPath := filepath.Join(tmpDir, "sys")

	require.NoError(t, os.MkdirAll(sysPath, 0755))

	config := performance.CollectionConfig{
		HostSysPath: sysPath,
	}

	collector, err := collectors.NewNetworkInfoCollector(logr.Discard(), config)
	require.NoError(t, err)
	return collector, tmpDir
}

func setupNetworkInterface(t *testing.T, netPath, iface string, props map[string]string) {
	ifacePath := filepath.Join(netPath, iface)
	require.NoError(t, os.MkdirAll(ifacePath, 0755))

	// Make it a symlink to simulate real /sys/class/net behavior
	// For testing purposes, we'll just create regular files

	// Write properties
	for key, value := range props {
		filePath := filepath.Join(ifacePath, key)
		require.NoError(t, os.WriteFile(filePath, []byte(value), 0644))
	}

	// Create device directory for physical interfaces
	if driver, ok := props["driver"]; ok {
		devicePath := filepath.Join(ifacePath, "device")
		require.NoError(t, os.MkdirAll(devicePath, 0755))

		// Create driver symlink
		driverPath := filepath.Join(devicePath, "driver")
		driverTarget := filepath.Join("/sys/bus/pci/drivers", driver)
		_ = os.Symlink(driverTarget, driverPath)
	}
}

func TestNetworkInfoCollector_Constructor(t *testing.T) {
	tests := []struct {
		name    string
		config  performance.CollectionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid absolute path",
			config: performance.CollectionConfig{
				HostSysPath: "/sys",
			},
			wantErr: false,
		},
		{
			name: "invalid relative path",
			config: performance.CollectionConfig{
				HostSysPath: "sys",
			},
			wantErr: true,
			errMsg:  "HostSysPath must be an absolute path",
		},
		{
			name: "empty path (allowed for defaults)",
			config: performance.CollectionConfig{
				HostSysPath: "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := collectors.NewNetworkInfoCollector(logr.Discard(), tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, collector)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, collector)
			}
		})
	}
}

func TestNetworkInfoCollector_Collect(t *testing.T) {
	tests := []struct {
		name     string
		setupSys func(t *testing.T, sysPath string)
		wantInfo func(t *testing.T, interfaces []performance.NetworkInfo)
		wantErr  bool
	}{
		{
			name: "ethernet interface",
			setupSys: func(t *testing.T, sysPath string) {
				netPath := filepath.Join(sysPath, "class", "net")
				require.NoError(t, os.MkdirAll(netPath, 0755))

				setupNetworkInterface(t, netPath, "eth0", map[string]string{
					"address":   "aa:bb:cc:dd:ee:ff",
					"speed":     "1000",
					"duplex":    "full",
					"mtu":       "1500",
					"operstate": "up",
					"carrier":   "1",
					"type":      "1",
					"driver":    "e1000e",
				})
			},
			wantInfo: func(t *testing.T, interfaces []performance.NetworkInfo) {
				assert.Len(t, interfaces, 1)
				assert.Equal(t, "eth0", interfaces[0].Interface)
				assert.Equal(t, "ethernet", interfaces[0].Type)
				assert.Equal(t, "aa:bb:cc:dd:ee:ff", interfaces[0].MACAddress)
				assert.Equal(t, uint64(1000), interfaces[0].Speed)
				assert.Equal(t, "full", interfaces[0].Duplex)
				assert.Equal(t, uint32(1500), interfaces[0].MTU)
				assert.Equal(t, "up", interfaces[0].OperState)
				assert.True(t, interfaces[0].Carrier)
				assert.Equal(t, "e1000e", interfaces[0].Driver)
			},
		},
		{
			name: "wireless interface",
			setupSys: func(t *testing.T, sysPath string) {
				netPath := filepath.Join(sysPath, "class", "net")
				require.NoError(t, os.MkdirAll(netPath, 0755))

				setupNetworkInterface(t, netPath, "wlan0", map[string]string{
					"address":   "11:22:33:44:55:66",
					"mtu":       "1500",
					"operstate": "up",
					"carrier":   "1",
				})

				// Create wireless directory to identify as wireless
				wirelessPath := filepath.Join(netPath, "wlan0", "wireless")
				require.NoError(t, os.MkdirAll(wirelessPath, 0755))
			},
			wantInfo: func(t *testing.T, interfaces []performance.NetworkInfo) {
				assert.Len(t, interfaces, 1)
				assert.Equal(t, "wlan0", interfaces[0].Interface)
				assert.Equal(t, "wireless", interfaces[0].Type)
				assert.Equal(t, "11:22:33:44:55:66", interfaces[0].MACAddress)
			},
		},
		{
			name: "loopback interface",
			setupSys: func(t *testing.T, sysPath string) {
				netPath := filepath.Join(sysPath, "class", "net")
				require.NoError(t, os.MkdirAll(netPath, 0755))

				setupNetworkInterface(t, netPath, "lo", map[string]string{
					"address":   "00:00:00:00:00:00",
					"mtu":       "65536",
					"operstate": "unknown",
					"carrier":   "1",
					"type":      "772",
				})
			},
			wantInfo: func(t *testing.T, interfaces []performance.NetworkInfo) {
				assert.Len(t, interfaces, 1)
				assert.Equal(t, "lo", interfaces[0].Interface)
				assert.Equal(t, "loopback", interfaces[0].Type)
				assert.Equal(t, uint32(65536), interfaces[0].MTU)
			},
		},
		{
			name: "multiple interfaces",
			setupSys: func(t *testing.T, sysPath string) {
				netPath := filepath.Join(sysPath, "class", "net")
				require.NoError(t, os.MkdirAll(netPath, 0755))

				// Physical ethernet
				setupNetworkInterface(t, netPath, "eth0", map[string]string{
					"address":   "aa:bb:cc:dd:ee:ff",
					"speed":     "1000",
					"operstate": "up",
					"carrier":   "1",
				})

				// Docker bridge
				setupNetworkInterface(t, netPath, "docker0", map[string]string{
					"address":   "02:42:ac:11:00:01",
					"mtu":       "1500",
					"operstate": "down",
					"carrier":   "0",
				})

				// Virtual ethernet
				setupNetworkInterface(t, netPath, "veth1234", map[string]string{
					"address":   "fe:fe:fe:fe:fe:fe",
					"mtu":       "1500",
					"operstate": "up",
				})
			},
			wantInfo: func(t *testing.T, interfaces []performance.NetworkInfo) {
				assert.Len(t, interfaces, 3)

				// Find interfaces by name
				var eth0, docker0, veth *performance.NetworkInfo
				for i := range interfaces {
					switch interfaces[i].Interface {
					case "eth0":
						eth0 = &interfaces[i]
					case "docker0":
						docker0 = &interfaces[i]
					case "veth1234":
						veth = &interfaces[i]
					}
				}

				require.NotNil(t, eth0)
				require.NotNil(t, docker0)
				require.NotNil(t, veth)

				assert.Equal(t, "ethernet", eth0.Type)
				assert.Equal(t, "bridge", docker0.Type)
				assert.Equal(t, "virtual", veth.Type)
			},
		},
		{
			name: "interface with invalid speed",
			setupSys: func(t *testing.T, sysPath string) {
				netPath := filepath.Join(sysPath, "class", "net")
				require.NoError(t, os.MkdirAll(netPath, 0755))

				setupNetworkInterface(t, netPath, "eth0", map[string]string{
					"address":   "aa:bb:cc:dd:ee:ff",
					"speed":     "-1", // Common for down interfaces
					"operstate": "down",
					"carrier":   "0",
				})
			},
			wantInfo: func(t *testing.T, interfaces []performance.NetworkInfo) {
				assert.Len(t, interfaces, 1)
				assert.Equal(t, uint64(0), interfaces[0].Speed) // Should be 0, not -1
				assert.Equal(t, "down", interfaces[0].OperState)
				assert.False(t, interfaces[0].Carrier)
			},
		},
		{
			name: "missing net class directory",
			setupSys: func(t *testing.T, sysPath string) {
				// Don't create class/net directory
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, tmpDir := createTestNetworkInfoCollector(t)

			if tt.setupSys != nil {
				tt.setupSys(t, filepath.Join(tmpDir, "sys"))
			}

			result, err := collector.Collect(context.Background())

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			interfaces, ok := result.([]performance.NetworkInfo)
			require.True(t, ok, "Expected []performance.NetworkInfo, got %T", result)

			if tt.wantInfo != nil {
				tt.wantInfo(t, interfaces)
			}
		})
	}
}

func TestNetworkInfoCollector_InterfaceTypes(t *testing.T) {
	tests := []struct {
		name          string
		interfaceName string
		hasWireless   bool
		typeFile      string
		hasDevice     bool
		wantType      string
	}{
		// Test KERNEL-STANDARDIZED detection: wireless/ subdirectory presence
		{"wireless with dir", "wlan0", true, "", false, "wireless"},

		// Test WELL-KNOWN interface name: "lo" is universally loopback
		{"loopback by name", "lo", false, "", false, "loopback"},

		// Test KERNEL-STANDARDIZED detection: type file with ARPHRD_ETHER (1)
		{"ethernet by type", "enp0s3", false, "1", false, "ethernet"},

		// Test KERNEL-STANDARDIZED detection: type file with ARPHRD_SIT (776)
		{"tunnel sit", "sit0", false, "776", false, "tunnel"},

		// Test KERNEL-STANDARDIZED detection: type file with ARPHRD_IPGRE (778)
		{"tunnel gre", "gre0", false, "778", false, "tunnel"},

		// Test HEURISTIC detection: "eth*" prefix naming convention
		{"ethernet by prefix", "eth1", false, "", false, "ethernet"},

		// Test HEURISTIC detection: "tap*" prefix naming convention
		{"tap device", "tap0", false, "", false, "tap"},

		// Test HEURISTIC detection: "veth*" prefix naming convention (container interfaces)
		{"virtual eth", "veth123", false, "", false, "virtual"},

		// Test HEURISTIC detection: "docker*" prefix naming convention
		{"docker bridge", "docker0", false, "", false, "bridge"},

		// Test HEURISTIC detection: "br-*" prefix naming convention
		{"bridge by prefix", "br-abcd", false, "", false, "bridge"},

		// Test HEURISTIC detection: "virbr*" prefix naming convention (libvirt bridges)
		{"libvirt bridge", "virbr0", false, "", false, "bridge"},

		// Test FALLBACK detection: device/ symlink indicates physical interface
		{"physical with device", "eno1", false, "", true, "ethernet"},

		// Test FALLBACK detection: no device/ symlink defaults to virtual
		{"unknown virtual", "myiface", false, "", false, "virtual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, tmpDir := createTestNetworkInfoCollector(t)
			netPath := filepath.Join(tmpDir, "sys", "class", "net")
			require.NoError(t, os.MkdirAll(netPath, 0755))

			props := map[string]string{
				"address": "00:11:22:33:44:55",
			}

			if tt.typeFile != "" {
				props["type"] = tt.typeFile
			}

			setupNetworkInterface(t, netPath, tt.interfaceName, props)

			ifacePath := filepath.Join(netPath, tt.interfaceName)

			if tt.hasWireless {
				wirelessPath := filepath.Join(ifacePath, "wireless")
				require.NoError(t, os.MkdirAll(wirelessPath, 0755))
			}

			if tt.hasDevice {
				devicePath := filepath.Join(ifacePath, "device")
				require.NoError(t, os.MkdirAll(devicePath, 0755))
			}

			result, err := collector.Collect(context.Background())
			require.NoError(t, err)

			interfaces, ok := result.([]performance.NetworkInfo)
			require.True(t, ok)
			require.Len(t, interfaces, 1)

			assert.Equal(t, tt.wantType, interfaces[0].Type)
		})
	}
}
