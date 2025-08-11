// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package core_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/antimetal/agent/pkg/ebpf/core"
	"github.com/antimetal/agent/pkg/kernel"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/rlimit"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEBPFProgramLoading(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF tests only run on Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("eBPF tests require root privileges")
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		t.Logf("Warning: Failed to remove memlock limit: %v", err)
	}

	currentKernel, err := kernel.GetCurrentVersion()
	require.NoError(t, err)

	t.Logf("Testing eBPF on kernel %d.%d.%d", currentKernel.Major, currentKernel.Minor, currentKernel.Patch)

	// Test CO-RE manager creation
	t.Run("CO-RE Manager", func(t *testing.T) {
		manager, err := core.NewManager(logr.Discard())

		if currentKernel.IsAtLeast(4, 18) {
			assert.NoError(t, err, "CO-RE manager should initialize on kernel 4.18+")
			if err == nil {
				features := manager.GetKernelFeatures()
				t.Logf("Kernel features: %+v", features)

				// Check CO-RE support level
				if currentKernel.IsAtLeast(5, 2) {
					assert.True(t, manager.HasFullCORESupport(), "Should have full CO-RE support on kernel 5.2+")
				}
			}
		} else {
			t.Logf("Kernel %d.%d.%d is below minimum for CO-RE (4.18)",
				currentKernel.Major, currentKernel.Minor, currentKernel.Patch)
		}
	})

	// Test basic eBPF program loading
	t.Run("Basic Program Loading", func(t *testing.T) {
		// Create a minimal eBPF program for testing
		spec := &ebpf.ProgramSpec{
			Type:    ebpf.TracePoint,
			License: "GPL",
			Instructions: asm.Instructions{
				// mov r0, 0
				asm.Mov.Imm(asm.R0, 0),
				// exit
				asm.Return(),
			},
		}

		prog, err := ebpf.NewProgram(spec)
		if err != nil {
			t.Logf("Failed to load basic eBPF program: %v", err)
			// This might fail on older kernels
			if !currentKernel.IsAtLeast(4, 1) {
				t.Skipf("eBPF not fully supported on kernel %d.%d.%d",
					currentKernel.Major, currentKernel.Minor, currentKernel.Patch)
			}
			t.Errorf("Failed to load eBPF program on supported kernel: %v", err)
		} else {
			defer prog.Close()
			t.Log("✓ Successfully loaded basic eBPF program")
		}
	})

	// Test map types
	t.Run("Map Type Support", func(t *testing.T) {
		mapTests := []struct {
			name       string
			mapType    ebpf.MapType
			minKernel  kernel.Version
			keySize    uint32
			valueSize  uint32
			maxEntries uint32
		}{
			{
				name:       "Hash Map",
				mapType:    ebpf.Hash,
				minKernel:  kernel.Version{Major: 3, Minor: 19, Patch: 0},
				keySize:    4,
				valueSize:  8,
				maxEntries: 10,
			},
			{
				name:       "Array Map",
				mapType:    ebpf.Array,
				minKernel:  kernel.Version{Major: 3, Minor: 19, Patch: 0},
				keySize:    4,
				valueSize:  8,
				maxEntries: 10,
			},
			{
				name:       "Perf Event Array",
				mapType:    ebpf.PerfEventArray,
				minKernel:  kernel.Version{Major: 4, Minor: 3, Patch: 0},
				keySize:    4,
				valueSize:  4,
				maxEntries: 0, // CPU count
			},
			{
				name:       "Ring Buffer",
				mapType:    ebpf.RingBuf,
				minKernel:  kernel.Version{Major: 5, Minor: 8, Patch: 0},
				keySize:    0,
				valueSize:  0,
				maxEntries: 4096,
			},
		}

		for _, mt := range mapTests {
			t.Run(mt.name, func(t *testing.T) {
				if !currentKernel.IsAtLeast(mt.minKernel.Major, mt.minKernel.Minor) {
					t.Skipf("%s requires kernel %d.%d.%d+, current is %d.%d.%d",
						mt.name,
						mt.minKernel.Major, mt.minKernel.Minor, mt.minKernel.Patch,
						currentKernel.Major, currentKernel.Minor, currentKernel.Patch)
				}

				spec := &ebpf.MapSpec{
					Type:       mt.mapType,
					KeySize:    mt.keySize,
					ValueSize:  mt.valueSize,
					MaxEntries: mt.maxEntries,
				}

				m, err := ebpf.NewMap(spec)
				if err != nil {
					t.Errorf("Failed to create %s: %v", mt.name, err)
				} else {
					defer m.Close()
					t.Logf("✓ %s supported", mt.name)
				}
			})
		}
	})
}

func TestEBPFCollectorPrograms(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF tests only run on Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("eBPF collector tests require root privileges")
	}

	currentKernel, err := kernel.GetCurrentVersion()
	require.NoError(t, err)

	// Look for eBPF programs - default to build directory for development
	ebpfBuildDir := "ebpf/build"

	// Allow override via environment variable (e.g., for CI/production)
	if envDir := os.Getenv("EBPF_BUILD_DIR"); envDir != "" {
		ebpfBuildDir = envDir
	}

	// In CI/production, eBPF programs are installed to standard location
	if _, err := os.Stat(ebpfBuildDir); os.IsNotExist(err) {
		if _, err := os.Stat("/usr/local/lib/antimetal/ebpf"); err == nil {
			ebpfBuildDir = "/usr/local/lib/antimetal/ebpf"
		}
	}

	// Check if we have the eBPF object files - fail if not found
	entries, err := os.ReadDir(ebpfBuildDir)
	if err != nil {
		t.Fatalf("Failed to read eBPF directory %s: %v - integration test environment not properly set up", ebpfBuildDir, err)
	}

	// Test loading each built eBPF program
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".o" {
			continue
		}

		progName := entry.Name()
		t.Run(progName, func(t *testing.T) {
			progPath := filepath.Join(ebpfBuildDir, progName)

			// Check program-specific requirements
			switch progName {
			case "execsnoop.bpf.o":
				if !currentKernel.IsAtLeast(5, 8) {
					t.Skipf("execsnoop requires kernel 5.8+ for ring buffer, current is %d.%d.%d",
						currentKernel.Major, currentKernel.Minor, currentKernel.Patch)
				}
			}

			// Try to load the collection spec
			spec, err := ebpf.LoadCollectionSpec(progPath)
			if err != nil {
				t.Errorf("Failed to load spec for %s: %v", progName, err)
				return
			}

			// Log the programs and maps found
			t.Logf("Found %d programs in %s", len(spec.Programs), progName)
			for name := range spec.Programs {
				t.Logf("  - Program: %s", name)
			}

			t.Logf("Found %d maps in %s", len(spec.Maps), progName)
			for name, mapSpec := range spec.Maps {
				t.Logf("  - Map: %s (type: %s)", name, mapSpec.Type)
			}

			// Try to instantiate the collection
			coll, err := ebpf.NewCollection(spec)
			if err != nil {
				t.Errorf("Failed to create collection for %s: %v", progName, err)
				return
			}
			defer coll.Close()

			t.Logf("✓ Successfully loaded %s", progName)
		})
	}
}

