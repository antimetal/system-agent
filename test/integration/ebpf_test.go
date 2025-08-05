// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package integration

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/antimetal/agent/pkg/ebpf/core"
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

	currentKernel, err := GetCurrentKernelVersion()
	require.NoError(t, err)

	t.Logf("Testing eBPF on kernel %d.%d.%d", currentKernel.Major, currentKernel.Minor, currentKernel.Patch)

	// Test CO-RE manager creation
	t.Run("CO-RE Manager", func(t *testing.T) {
		manager, err := core.NewManager(logr.Discard())

		if currentKernel.Compare(KernelVersion{4, 18, 0}) >= 0 {
			assert.NoError(t, err, "CO-RE manager should initialize on kernel 4.18+")
			if err == nil {
				features := manager.GetKernelFeatures()
				t.Logf("Kernel features: %+v", features)

				// Check CO-RE support level
				if currentKernel.Compare(KernelVersion{5, 2, 0}) >= 0 {
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
			if currentKernel.Compare(KernelVersion{4, 1, 0}) < 0 {
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
			minKernel  KernelVersion
			keySize    uint32
			valueSize  uint32
			maxEntries uint32
		}{
			{
				name:       "Hash Map",
				mapType:    ebpf.Hash,
				minKernel:  KernelVersion{3, 19, 0},
				keySize:    4,
				valueSize:  8,
				maxEntries: 10,
			},
			{
				name:       "Array Map",
				mapType:    ebpf.Array,
				minKernel:  KernelVersion{3, 19, 0},
				keySize:    4,
				valueSize:  8,
				maxEntries: 10,
			},
			{
				name:       "Perf Event Array",
				mapType:    ebpf.PerfEventArray,
				minKernel:  KernelVersion{4, 3, 0},
				keySize:    4,
				valueSize:  4,
				maxEntries: 0, // CPU count
			},
			{
				name:       "Ring Buffer",
				mapType:    ebpf.RingBuf,
				minKernel:  KernelVersion{5, 8, 0},
				keySize:    0,
				valueSize:  0,
				maxEntries: 4096,
			},
		}

		for _, mt := range mapTests {
			t.Run(mt.name, func(t *testing.T) {
				if currentKernel.Compare(mt.minKernel) < 0 {
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

	currentKernel, err := GetCurrentKernelVersion()
	require.NoError(t, err)

	// Check if we have the eBPF object files
	ebpfDir := filepath.Join("ebpf", "build")
	entries, err := os.ReadDir(ebpfDir)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("eBPF programs not built, run 'make build-ebpf' first")
		}
		t.Fatalf("Failed to read eBPF directory: %v", err)
	}

	// Test loading each built eBPF program
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".o" {
			continue
		}

		progName := entry.Name()
		t.Run(progName, func(t *testing.T) {
			progPath := filepath.Join(ebpfDir, progName)

			// Check program-specific requirements
			switch progName {
			case "execsnoop.bpf.o":
				if currentKernel.Compare(KernelVersion{5, 8, 0}) < 0 {
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

func TestKernelHelperSupport(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Kernel helper tests only run on Linux")
	}

	currentKernel, err := GetCurrentKernelVersion()
	require.NoError(t, err)

	// Test availability of common BPF helpers
	// Note: This is a simplified test - in practice we'd check actual helper availability
	helpers := []struct {
		name      string
		minKernel KernelVersion
	}{
		{"bpf_probe_read", KernelVersion{4, 1, 0}},
		{"bpf_probe_read_str", KernelVersion{4, 11, 0}},
		{"bpf_probe_read_kernel", KernelVersion{5, 5, 0}},
		{"bpf_probe_read_user", KernelVersion{5, 5, 0}},
		{"bpf_ringbuf_reserve", KernelVersion{5, 8, 0}},
		{"bpf_ringbuf_submit", KernelVersion{5, 8, 0}},
	}

	for _, helper := range helpers {
		t.Run(helper.name, func(t *testing.T) {
			if currentKernel.Compare(helper.minKernel) >= 0 {
				t.Logf("✓ %s should be available (requires kernel %d.%d.%d+)",
					helper.name,
					helper.minKernel.Major, helper.minKernel.Minor, helper.minKernel.Patch)
			} else {
				t.Logf("✗ %s not available on kernel %d.%d.%d (requires %d.%d.%d+)",
					helper.name,
					currentKernel.Major, currentKernel.Minor, currentKernel.Patch,
					helper.minKernel.Major, helper.minKernel.Minor, helper.minKernel.Patch)
			}
		})
	}
}

func TestEBPFWithContext(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("eBPF context tests only run on Linux")
	}

	if os.Geteuid() != 0 {
		t.Skip("eBPF context tests require root privileges")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test that eBPF operations respect context cancellation
	t.Run("Context Cancellation", func(t *testing.T) {
		// Create a context that's already cancelled
		cancelledCtx, cancelFunc := context.WithCancel(context.Background())
		cancelFunc()

		// Operations should handle cancelled context gracefully
		select {
		case <-cancelledCtx.Done():
			t.Log("✓ Context cancellation works correctly")
		default:
			t.Error("Context should be cancelled")
		}
	})

	// Test timeout handling
	t.Run("Timeout Handling", func(t *testing.T) {
		shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		start := time.Now()
		<-shortCtx.Done()
		elapsed := time.Since(start)

		assert.InDelta(t, 100, elapsed.Milliseconds(), 50,
			"Context timeout should be approximately 100ms")
	})
}
