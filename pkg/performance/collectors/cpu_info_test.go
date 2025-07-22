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

const testCPUInfo = `processor	: 0
vendor_id	: GenuineIntel
cpu family	: 6
model		: 158
model name	: Intel(R) Core(TM) i7-8700K CPU @ 3.70GHz
stepping	: 10
microcode	: 0xde
cpu MHz		: 3700.000
cache size	: 12288 KB
physical id	: 0
siblings	: 12
core id		: 0
cpu cores	: 6
apicid		: 0
initial apicid	: 0
fpu		: yes
fpu_exception	: yes
cpuid level	: 22
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ht
bogomips	: 7399.70

processor	: 1
vendor_id	: GenuineIntel
cpu family	: 6
model		: 158
model name	: Intel(R) Core(TM) i7-8700K CPU @ 3.70GHz
stepping	: 10
microcode	: 0xde
cpu MHz		: 3700.000
cache size	: 12288 KB
physical id	: 0
siblings	: 12
core id		: 1
cpu cores	: 6
apicid		: 2
initial apicid	: 2
fpu		: yes
fpu_exception	: yes
cpuid level	: 22
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ht
bogomips	: 7399.70
`

const testCPUInfoVirtual = `processor	: 0
vendor_id	: GenuineIntel
cpu family	: 6
model		: 85
model name	: Intel(R) Xeon(R) Platinum 8370C CPU @ 2.80GHz
stepping	: 7
microcode	: 0xffffffff
cpu MHz		: 2800.000
cache size	: 49152 KB
fpu		: yes
fpu_exception	: yes
cpuid level	: 21
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ss
bogomips	: 5586.83

processor	: 1
vendor_id	: GenuineIntel
cpu family	: 6
model		: 85
model name	: Intel(R) Xeon(R) Platinum 8370C CPU @ 2.80GHz
stepping	: 7
microcode	: 0xffffffff
cpu MHz		: 2800.000
cache size	: 49152 KB
fpu		: yes
fpu_exception	: yes
cpuid level	: 21
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ss
bogomips	: 5586.83
`

const testCPUInfoARM64 = `processor	: 0
BogoMIPS	: 48.00
Features	: fp asimd evtstrm aes pmull sha1 sha2 crc32 atomics fphp asimdhp cpuid asimdrdm jscvt fcma lrcpc dcpop sha3 asimddp sha512 asimdfhm dit uscat ilrcpc flagm sb paca pacg dcpodp flagm2 frint
CPU implementer	: 0x61
CPU architecture: 8
CPU variant	: 0x0
CPU part	: 0x000
CPU revision	: 0

processor	: 1
BogoMIPS	: 48.00
Features	: fp asimd evtstrm aes pmull sha1 sha2 crc32 atomics fphp asimdhp cpuid asimdrdm jscvt fcma lrcpc dcpop sha3 asimddp sha512 asimdfhm dit uscat ilrcpc flagm sb paca pacg dcpodp flagm2 frint
CPU implementer	: 0x61
CPU architecture: 8
CPU variant	: 0x0
CPU part	: 0x000
CPU revision	: 0

processor	: 2
BogoMIPS	: 48.00
Features	: fp asimd evtstrm aes pmull sha1 sha2 crc32 atomics fphp asimdhp cpuid asimdrdm jscvt fcma lrcpc dcpop sha3 asimddp sha512 asimdfhm dit uscat ilrcpc flagm sb paca pacg dcpodp flagm2 frint
CPU implementer	: 0x61
CPU architecture: 8
CPU variant	: 0x0
CPU part	: 0x000
CPU revision	: 0

processor	: 3
BogoMIPS	: 48.00
Features	: fp asimd evtstrm aes pmull sha1 sha2 crc32 atomics fphp asimdhp cpuid asimdrdm jscvt fcma lrcpc dcpop sha3 asimddp sha512 asimdfhm dit uscat ilrcpc flagm sb paca pacg dcpodp flagm2 frint
CPU implementer	: 0x61
CPU architecture: 8
CPU variant	: 0x0
CPU part	: 0x000
CPU revision	: 0
`

const testCPUInfoProxmoxVM = `processor	: 0
vendor_id	: GenuineIntel
cpu family	: 15
model		: 6
model name	: Common KVM processor
stepping	: 1
microcode	: 0x1
cpu MHz		: 1699.999
cache size	: 16384 KB
physical id	: 0
siblings	: 1
core id		: 0
cpu cores	: 1
apicid		: 0
initial apicid	: 0
fpu		: yes
fpu_exception	: yes
cpuid level	: 13
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 syscall nx lm constant_tsc nopl xtopology cpuid tsc_known_freq pni cx16 x2apic hypervisor lahf_lm cpuid_fault pti
bugs		: cpu_meltdown spectre_v1 spectre_v2 spec_store_bypass l1tf mds swapgs itlb_multihit mmio_unknown bhi
bogomips	: 3399.99
clflush size	: 64
cache_alignment	: 128
address sizes	: 40 bits physical, 48 bits virtual
power management:

`

const testCPUInfoAWST3Micro = `processor	: 0
vendor_id	: GenuineIntel
cpu family	: 6
model		: 85
model name	: Intel(R) Xeon(R) Platinum 8259CL CPU @ 2.50GHz
stepping	: 7
microcode	: 0x5003901
cpu MHz		: 2499.998
cache size	: 36608 KB
physical id	: 0
siblings	: 2
core id		: 0
cpu cores	: 1
apicid		: 0
initial apicid	: 0
fpu		: yes
fpu_exception	: yes
cpuid level	: 13
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ss ht syscall nx pdpe1gb rdtscp lm constant_tsc rep_good nopl xtopology nonstop_tsc cpuid tsc_known_freq pni pclmulqdq ssse3 fma cx16 pcid sse4_1 sse4_2 x2apic movbe popcnt tsc_deadline_timer aes xsave avx f16c rdrand hypervisor lahf_lm abm 3dnowprefetch invpcid_single pti fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid mpx avx512f avx512dq rdseed adx smap clflushopt clwb avx512cd avx512bw avx512vl xsaveopt xsavec xgetbv1 xsaves ida arat pku ospke
bugs		: cpu_meltdown spectre_v1 spectre_v2 spec_store_bypass l1tf mds swapgs itlb_multihit mmio_stale_data retbleed gds bhi its
bogomips	: 4999.99
clflush size	: 64
cache_alignment	: 64
address sizes	: 46 bits physical, 48 bits virtual
power management:

processor	: 1
vendor_id	: GenuineIntel
cpu family	: 6
model		: 85
model name	: Intel(R) Xeon(R) Platinum 8259CL CPU @ 2.50GHz
stepping	: 7
microcode	: 0x5003901
cpu MHz		: 2499.998
cache size	: 36608 KB
physical id	: 0
siblings	: 2
core id		: 0
cpu cores	: 1
apicid		: 1
initial apicid	: 1
fpu		: yes
fpu_exception	: yes
cpuid level	: 13
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ss ht syscall nx pdpe1gb rdtscp lm constant_tsc rep_good nopl xtopology nonstop_tsc cpuid tsc_known_freq pni pclmulqdq ssse3 fma cx16 pcid sse4_1 sse4_2 x2apic movbe popcnt tsc_deadline_timer aes xsave avx f16c rdrand hypervisor lahf_lm abm 3dnowprefetch invpcid_single pti fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid mpx avx512f avx512dq rdseed adx smap clflushopt clwb avx512cd avx512bw avx512vl xsaveopt xsavec xgetbv1 xsaves ida arat pku ospke
bugs		: cpu_meltdown spectre_v1 spectre_v2 spec_store_bypass l1tf mds swapgs itlb_multihit mmio_stale_data retbleed gds bhi its
bogomips	: 4999.99
clflush size	: 64
cache_alignment	: 64
address sizes	: 46 bits physical, 48 bits virtual
power management:

`

func createTestCPUInfoCollector(t *testing.T) (*collectors.CPUInfoCollector, string) {
	tmpDir := t.TempDir()
	procPath := filepath.Join(tmpDir, "proc")
	sysPath := filepath.Join(tmpDir, "sys")

	require.NoError(t, os.MkdirAll(procPath, 0755))
	require.NoError(t, os.MkdirAll(sysPath, 0755))

	config := performance.CollectionConfig{
		HostProcPath: procPath,
		HostSysPath:  sysPath,
	}

	collector, err := collectors.NewCPUInfoCollector(logr.Discard(), config)
	require.NoError(t, err)
	return collector, tmpDir
}

func TestCPUInfoCollector_Constructor(t *testing.T) {
	tests := []struct {
		name    string
		config  performance.CollectionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid absolute paths",
			config: performance.CollectionConfig{
				HostProcPath: "/proc",
				HostSysPath:  "/sys",
			},
			wantErr: false,
		},
		{
			name: "invalid relative proc path",
			config: performance.CollectionConfig{
				HostProcPath: "proc",
				HostSysPath:  "/sys",
			},
			wantErr: true,
			errMsg:  "HostProcPath must be an absolute path",
		},
		{
			name: "invalid relative sys path",
			config: performance.CollectionConfig{
				HostProcPath: "/proc",
				HostSysPath:  "sys",
			},
			wantErr: true,
			errMsg:  "HostSysPath must be an absolute path",
		},
		{
			name: "empty paths (allowed for defaults)",
			config: performance.CollectionConfig{
				HostProcPath: "",
				HostSysPath:  "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := collectors.NewCPUInfoCollector(logr.Discard(), tt.config)
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

func TestCPUInfoCollector_Collect(t *testing.T) {
	tests := []struct {
		name       string
		cpuInfo    string
		setupSysfs func(t *testing.T, sysPath string)
		wantInfo   func(t *testing.T, info *performance.CPUInfo)
		wantErr    bool
	}{
		{
			name:    "physical CPU with HT",
			cpuInfo: testCPUInfo,
			setupSysfs: func(t *testing.T, sysPath string) {
				// Create cpufreq paths
				cpu0Path := filepath.Join(sysPath, "devices", "system", "cpu", "cpu0", "cpufreq")
				require.NoError(t, os.MkdirAll(cpu0Path, 0755))
				require.NoError(t, os.WriteFile(
					filepath.Join(cpu0Path, "cpuinfo_min_freq"),
					[]byte("800000\n"),
					0644,
				))
				require.NoError(t, os.WriteFile(
					filepath.Join(cpu0Path, "cpuinfo_max_freq"),
					[]byte("4700000\n"),
					0644,
				))

				// Create NUMA node
				nodePath := filepath.Join(sysPath, "devices", "system", "node", "node0")
				require.NoError(t, os.MkdirAll(nodePath, 0755))
			},
			wantInfo: func(t *testing.T, info *performance.CPUInfo) {
				assert.Equal(t, int32(2), info.LogicalCores)
				assert.Equal(t, int32(2), info.PhysicalCores)
				assert.Equal(t, "GenuineIntel", info.VendorID)
				assert.Equal(t, "Intel(R) Core(TM) i7-8700K CPU @ 3.70GHz", info.ModelName)
				assert.Equal(t, int32(6), info.CPUFamily)
				assert.Equal(t, int32(158), info.Model)
				assert.Equal(t, int32(10), info.Stepping)
				assert.Equal(t, "0xde", info.Microcode)
				assert.Equal(t, 3700.0, info.CPUMHz)
				assert.Equal(t, 800.0, info.CPUMinMHz)
				assert.Equal(t, 4700.0, info.CPUMaxMHz)
				assert.Equal(t, "12288 KB", info.CacheSize)
				assert.Equal(t, 7399.70, info.BogoMIPS)
				assert.Equal(t, int32(1), info.NUMANodes)
				assert.Contains(t, info.Flags, "fpu")
				assert.Contains(t, info.Flags, "sse2")
				assert.Len(t, info.Cores, 2)
				assert.Equal(t, int32(0), info.Cores[0].CoreID)
				assert.Equal(t, int32(1), info.Cores[1].CoreID)
			},
		},
		{
			name:    "virtual CPU without physical IDs",
			cpuInfo: testCPUInfoVirtual,
			setupSysfs: func(t *testing.T, sysPath string) {
				// No cpufreq or NUMA setup
			},
			wantInfo: func(t *testing.T, info *performance.CPUInfo) {
				assert.Equal(t, int32(2), info.LogicalCores)
				assert.Equal(t, int32(2), info.PhysicalCores) // Falls back to logical count
				assert.Equal(t, "GenuineIntel", info.VendorID)
				assert.Equal(t, "Intel(R) Xeon(R) Platinum 8370C CPU @ 2.80GHz", info.ModelName)
				assert.Equal(t, 2800.0, info.CPUMHz)
				assert.Equal(t, 0.0, info.CPUMinMHz)      // Not available
				assert.Equal(t, 0.0, info.CPUMaxMHz)      // Not available
				assert.Equal(t, int32(1), info.NUMANodes) // Default
			},
		},
		{
			name:    "ARM64 architecture",
			cpuInfo: testCPUInfoARM64,
			setupSysfs: func(t *testing.T, sysPath string) {
				// No cpufreq setup for ARM test
			},
			wantInfo: func(t *testing.T, info *performance.CPUInfo) {
				assert.Equal(t, int32(4), info.LogicalCores)
				assert.Equal(t, int32(4), info.PhysicalCores) // Falls back to logical count
				assert.Equal(t, "", info.VendorID)            // Not present in ARM64
				assert.Equal(t, "", info.ModelName)           // Not present in ARM64
				assert.Equal(t, 48.00, info.BogoMIPS)         // Different format but parsed
				assert.Equal(t, 0.0, info.CPUMHz)             // Not present in ARM64 cpuinfo
				assert.NotEmpty(t, info.Flags)                // ARM64 Features are parsed as flags
				assert.Contains(t, info.Flags, "fp")
				assert.Contains(t, info.Flags, "asimd")
				assert.Contains(t, info.Flags, "evtstrm")
				assert.Equal(t, int32(1), info.NUMANodes) // Default

				// Verify all cores detected
				assert.Len(t, info.Cores, 4)
				for i, core := range info.Cores {
					assert.Equal(t, int32(i), core.Processor)
					assert.Equal(t, int32(0), core.CoreID)     // Not present, defaults to 0
					assert.Equal(t, int32(0), core.PhysicalID) // Not present, defaults to 0
				}
			},
		},
		{
			name:    "Proxmox VM",
			cpuInfo: testCPUInfoProxmoxVM,
			wantInfo: func(t *testing.T, info *performance.CPUInfo) {
				assert.Equal(t, int32(1), info.LogicalCores)
				assert.Equal(t, int32(1), info.PhysicalCores)
				assert.Equal(t, "GenuineIntel", info.VendorID)
				assert.Equal(t, "Common KVM processor", info.ModelName)
				assert.Equal(t, int32(15), info.CPUFamily)
				assert.Equal(t, int32(6), info.Model)
				assert.Equal(t, int32(1), info.Stepping)
				assert.Equal(t, "0x1", info.Microcode)
				assert.Equal(t, 1699.999, info.CPUMHz)
				assert.Equal(t, "16384 KB", info.CacheSize)
				assert.Equal(t, int32(128), info.CacheAlignment)
				assert.Equal(t, 3399.99, info.BogoMIPS)
				assert.Equal(t, int32(1), info.NUMANodes)
				assert.Contains(t, info.Flags, "hypervisor")
				assert.Contains(t, info.Flags, "x2apic")

				// Single core
				assert.Len(t, info.Cores, 1)
				assert.Equal(t, int32(0), info.Cores[0].Processor)
				assert.Equal(t, int32(0), info.Cores[0].CoreID)
				assert.Equal(t, int32(0), info.Cores[0].PhysicalID)
			},
		},
		{
			name:    "AWS t3.micro instance",
			cpuInfo: testCPUInfoAWST3Micro,
			wantInfo: func(t *testing.T, info *performance.CPUInfo) {
				assert.Equal(t, int32(2), info.LogicalCores)
				assert.Equal(t, int32(2), info.PhysicalCores) // Falls back to logical count when all cores have same ID
				assert.Equal(t, "GenuineIntel", info.VendorID)
				assert.Equal(t, "Intel(R) Xeon(R) Platinum 8259CL CPU @ 2.50GHz", info.ModelName)
				assert.Equal(t, int32(6), info.CPUFamily)
				assert.Equal(t, int32(85), info.Model)
				assert.Equal(t, int32(7), info.Stepping)
				assert.Equal(t, "0x5003901", info.Microcode)
				assert.Equal(t, 2499.998, info.CPUMHz)
				assert.Equal(t, "36608 KB", info.CacheSize)
				assert.Equal(t, int32(64), info.CacheAlignment)
				assert.Equal(t, 4999.99, info.BogoMIPS)
				assert.Equal(t, int32(1), info.NUMANodes)

				// Check for AVX-512 support
				assert.Contains(t, info.Flags, "avx512f")
				assert.Contains(t, info.Flags, "avx512dq")
				assert.Contains(t, info.Flags, "avx512cd")
				assert.Contains(t, info.Flags, "avx512bw")
				assert.Contains(t, info.Flags, "avx512vl")

				// Two logical cores on same physical core
				assert.Len(t, info.Cores, 2)
				for i, core := range info.Cores {
					assert.Equal(t, int32(i), core.Processor)
					assert.Equal(t, int32(0), core.CoreID)     // Same core ID
					assert.Equal(t, int32(0), core.PhysicalID) // Same physical ID
					assert.Equal(t, int32(2), core.Siblings)   // HyperThreading
				}
			},
		},
		{
			name:    "empty cpuinfo",
			cpuInfo: "",
			wantInfo: func(t *testing.T, info *performance.CPUInfo) {
				assert.Equal(t, int32(0), info.LogicalCores)
				assert.Equal(t, int32(0), info.PhysicalCores)
			},
		},
		{
			name:    "missing cpuinfo file",
			cpuInfo: "", // Won't create the file
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, tmpDir := createTestCPUInfoCollector(t)

			if tt.cpuInfo != "" || !tt.wantErr {
				cpuinfoPath := filepath.Join(tmpDir, "proc", "cpuinfo")
				require.NoError(t, os.WriteFile(cpuinfoPath, []byte(tt.cpuInfo), 0644))
			}

			if tt.setupSysfs != nil {
				tt.setupSysfs(t, filepath.Join(tmpDir, "sys"))
			}

			result, err := collector.Collect(context.Background())

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			info, ok := result.(*performance.CPUInfo)
			require.True(t, ok, "Expected *performance.CPUInfo, got %T", result)

			if tt.wantInfo != nil {
				tt.wantInfo(t, info)
			}
		})
	}
}

func TestCPUInfoCollector_ParseMultiSocket(t *testing.T) {
	multiSocketCPUInfo := `processor	: 0
physical id	: 0
core id		: 0
cpu cores	: 4

processor	: 1
physical id	: 0
core id		: 1
cpu cores	: 4

processor	: 2
physical id	: 1
core id		: 0
cpu cores	: 4

processor	: 3
physical id	: 1
core id		: 1
cpu cores	: 4
`

	collector, tmpDir := createTestCPUInfoCollector(t)
	cpuinfoPath := filepath.Join(tmpDir, "proc", "cpuinfo")
	require.NoError(t, os.WriteFile(cpuinfoPath, []byte(multiSocketCPUInfo), 0644))

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	info, ok := result.(*performance.CPUInfo)
	require.True(t, ok)

	assert.Equal(t, int32(4), info.LogicalCores)
	assert.Equal(t, int32(4), info.PhysicalCores) // 2 cores on each of 2 sockets
}

func TestCPUInfoCollector_MalformedData(t *testing.T) {
	malformedCPUInfo := `processor: 0
vendor_id: Intel
cpu MHz: not-a-number
physical id: also-not-a-number
cache_alignment: 64
flags: fpu vme de

processor: 1
vendor_id: Intel
`

	collector, tmpDir := createTestCPUInfoCollector(t)
	cpuinfoPath := filepath.Join(tmpDir, "proc", "cpuinfo")
	require.NoError(t, os.WriteFile(cpuinfoPath, []byte(malformedCPUInfo), 0644))

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	info, ok := result.(*performance.CPUInfo)
	require.True(t, ok)

	// Should handle parsing errors gracefully
	assert.Equal(t, int32(2), info.LogicalCores)
	assert.Equal(t, "Intel", info.VendorID)
	assert.Equal(t, int32(64), info.CacheAlignment)
	assert.Contains(t, info.Flags, "fpu")
}

// Test cases from testdata files
func TestCPUInfoCollector_AMDEpyc7551(t *testing.T) {
	// AMD EPYC 7551 32-Core Processor
	const amdEpyc7551CPUInfo = `processor	: 0
vendor_id	: AuthenticAMD
cpu family	: 23
model		: 1
model name	: AMD EPYC 7551 32-Core Processor
stepping	: 2
microcode	: 0x8001250
cpu MHz		: 1996.232
cache size	: 512 KB
physical id	: 0
siblings	: 64
core id		: 0
cpu cores	: 32
apicid		: 0
initial apicid	: 0
fpu		: yes
fpu_exception	: yes
cpuid level	: 13
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ht syscall nx mmxext fxsr_opt pdpe1gb rdtscp lm constant_tsc rep_good nopl nonstop_tsc cpuid extd_apicid amd_dcm aperfmperf pni pclmulqdq monitor ssse3 fma cx16 sse4_1 sse4_2 movbe popcnt aes xsave avx f16c rdrand lahf_lm cmp_legacy svm extapic cr8_legacy abm sse4a misalignsse 3dnowprefetch osvw skinit wdt tce topoext perfctr_core perfctr_nb bpext perfctr_llc mwaitx cpb hw_pstate sme ssbd sev vmmcall fsgsbase bmi1 avx2 smep bmi2 rdseed adx smap clflushopt sha_ni xsaveopt xsavec xgetbv1 xsaves clzero irperf xsaveerptr arat npt lbrv svm_lock nrip_save tsc_scale vmcb_clean flushbyasid decodeassists pausefilter pfthreshold avic v_vmsave_vmload vgif overflow_recov succor smca
bugs		: sysret_ss_attrs null_seg spectre_v1 spectre_v2 spec_store_bypass
bogomips	: 3992.46
TLB size	: 2560 4K pages
clflush size	: 64
cache_alignment	: 64
address sizes	: 43 bits physical, 48 bits virtual
power management: ts ttp tm hwpstate cpb eff_freq_ro [13] [14]

processor	: 1
vendor_id	: AuthenticAMD
cpu family	: 23
model		: 1
model name	: AMD EPYC 7551 32-Core Processor
stepping	: 2
microcode	: 0x8001250
cpu MHz		: 1996.232
cache size	: 512 KB
physical id	: 0
siblings	: 64
core id		: 0
cpu cores	: 32
apicid		: 1
initial apicid	: 1
fpu		: yes
fpu_exception	: yes
cpuid level	: 13
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ht syscall nx mmxext fxsr_opt pdpe1gb rdtscp lm constant_tsc rep_good nopl nonstop_tsc cpuid extd_apicid amd_dcm aperfmperf pni pclmulqdq monitor ssse3 fma cx16 sse4_1 sse4_2 movbe popcnt aes xsave avx f16c rdrand lahf_lm cmp_legacy svm extapic cr8_legacy abm sse4a misalignsse 3dnowprefetch osvw skinit wdt tce topoext perfctr_core perfctr_nb bpext perfctr_llc mwaitx cpb hw_pstate sme ssbd sev vmmcall fsgsbase bmi1 avx2 smep bmi2 rdseed adx smap clflushopt sha_ni xsaveopt xsavec xgetbv1 xsaves clzero irperf xsaveerptr arat npt lbrv svm_lock nrip_save tsc_scale vmcb_clean flushbyasid decodeassists pausefilter pfthreshold avic v_vmsave_vmload vgif overflow_recov succor smca
bugs		: sysret_ss_attrs null_seg spectre_v1 spectre_v2 spec_store_bypass
bogomips	: 3992.46
TLB size	: 2560 4K pages
clflush size	: 64
cache_alignment	: 64
address sizes	: 43 bits physical, 48 bits virtual
power management: ts ttp tm hwpstate cpb eff_freq_ro [13] [14]
`

	collector, tmpDir := createTestCPUInfoCollector(t)
	cpuinfoPath := filepath.Join(tmpDir, "proc", "cpuinfo")
	require.NoError(t, os.WriteFile(cpuinfoPath, []byte(amdEpyc7551CPUInfo), 0644))

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	info, ok := result.(*performance.CPUInfo)
	require.True(t, ok)

	assert.Equal(t, "AuthenticAMD", info.VendorID)
	assert.Equal(t, "AMD EPYC 7551 32-Core Processor", info.ModelName)
	assert.Equal(t, int32(23), info.CPUFamily)
	assert.Equal(t, int32(1), info.Model)
	assert.Equal(t, int32(2), info.Stepping)
	assert.Equal(t, float64(3992.46), info.BogoMIPS)
	assert.Equal(t, int32(2), info.LogicalCores)  // Only 2 processors shown in snippet
	assert.Equal(t, int32(2), info.PhysicalCores) // VM fallback when all cores have same physical/core ID
	assert.Equal(t, "512 KB", info.CacheSize)
	assert.Equal(t, int32(64), info.CacheAlignment)
	assert.Contains(t, info.Flags, "fpu")
	assert.Contains(t, info.Flags, "avx2")
	assert.Contains(t, info.Flags, "sme")
}

func TestCPUInfoCollector_AMDEpyc7R13(t *testing.T) {
	// AMD EPYC 7R13 48-Core Processor (AWS)
	const amdEpyc7R13CPUInfo = `processor	: 0
vendor_id	: AuthenticAMD
cpu family	: 25
model		: 1
model name	: AMD EPYC 7R13 Processor
stepping	: 1
microcode	: 0xa0011d3
cpu MHz		: 3602.592
cache size	: 512 KB
physical id	: 0
siblings	: 2
core id		: 0
cpu cores	: 1
apicid		: 0
initial apicid	: 0
fpu		: yes
fpu_exception	: yes
cpuid level	: 16
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ht syscall nx mmxext fxsr_opt pdpe1gb rdtscp lm constant_tsc rep_good nopl nonstop_tsc cpuid extd_apicid aperfmperf tsc_known_freq pni pclmulqdq ssse3 fma cx16 pcid sse4_1 sse4_2 x2apic movbe popcnt aes xsave avx f16c rdrand hypervisor lahf_lm cmp_legacy cr8_legacy abm sse4a misalignsse 3dnowprefetch topoext invpcid_single ssbd ibrs ibpb stibp vmmcall fsgsbase bmi1 avx2 smep bmi2 erms invpcid rdseed adx smap clflushopt clwb sha_ni xsaveopt xsavec xgetbv1 xsaves clzero xsaveerptr rdpru wbnoinvd arat npt nrip_save rdpid
bugs		: sysret_ss_attrs null_seg spectre_v1 spectre_v2 spec_store_bypass
bogomips	: 5199.98
TLB size	: 2560 4K pages
clflush size	: 64
cache_alignment	: 64
address sizes	: 48 bits physical, 48 bits virtual
power management:

processor	: 1
vendor_id	: AuthenticAMD
cpu family	: 25
model		: 1
model name	: AMD EPYC 7R13 Processor
stepping	: 1
microcode	: 0xa0011d3
cpu MHz		: 3602.524
cache size	: 512 KB
physical id	: 0
siblings	: 2
core id		: 0
cpu cores	: 1
apicid		: 1
initial apicid	: 1
fpu		: yes
fpu_exception	: yes
cpuid level	: 16
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ht syscall nx mmxext fxsr_opt pdpe1gb rdtscp lm constant_tsc rep_good nopl nonstop_tsc cpuid extd_apicid aperfmperf tsc_known_freq pni pclmulqdq ssse3 fma cx16 pcid sse4_1 sse4_2 x2apic movbe popcnt aes xsave avx f16c rdrand hypervisor lahf_lm cmp_legacy cr8_legacy abm sse4a misalignsse 3dnowprefetch topoext invpcid_single ssbd ibrs ibpb stibp vmmcall fsgsbase bmi1 avx2 smep bmi2 erms invpcid rdseed adx smap clflushopt clwb sha_ni xsaveopt xsavec xgetbv1 xsaves clzero xsaveerptr rdpru wbnoinvd arat npt nrip_save rdpid
bugs		: sysret_ss_attrs null_seg spectre_v1 spectre_v2 spec_store_bypass
bogomips	: 5199.98
TLB size	: 2560 4K pages
clflush size	: 64
cache_alignment	: 64
address sizes	: 48 bits physical, 48 bits virtual
power management:
`

	collector, tmpDir := createTestCPUInfoCollector(t)
	cpuinfoPath := filepath.Join(tmpDir, "proc", "cpuinfo")
	require.NoError(t, os.WriteFile(cpuinfoPath, []byte(amdEpyc7R13CPUInfo), 0644))

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	info, ok := result.(*performance.CPUInfo)
	require.True(t, ok)

	assert.Equal(t, "AuthenticAMD", info.VendorID)
	assert.Equal(t, "AMD EPYC 7R13 Processor", info.ModelName)
	assert.Equal(t, int32(25), info.CPUFamily)
	assert.Equal(t, int32(1), info.Model)
	assert.Equal(t, int32(1), info.Stepping)
	assert.Equal(t, float64(5199.98), info.BogoMIPS)
	assert.Equal(t, int32(2), info.LogicalCores)
	assert.Equal(t, int32(2), info.PhysicalCores) // VM fallback: counts logical cores
	assert.Equal(t, "512 KB", info.CacheSize)
	assert.Equal(t, int32(64), info.CacheAlignment)
	assert.Contains(t, info.Flags, "fpu")
	assert.Contains(t, info.Flags, "avx2")
	assert.Contains(t, info.Flags, "hypervisor")
}

func TestCPUInfoCollector_ARM64CortexA53(t *testing.T) {
	// ARM64 Cortex-A53
	const arm64CortexA53CPUInfo = `processor	: 0
BogoMIPS	: 48.00
Features	: fp asimd evtstrm aes pmull sha1 sha2 crc32
CPU implementer	: 0x41
CPU architecture: 8
CPU variant	: 0x0
CPU part	: 0xd03
CPU revision	: 4

processor	: 1
BogoMIPS	: 48.00
Features	: fp asimd evtstrm aes pmull sha1 sha2 crc32
CPU implementer	: 0x41
CPU architecture: 8
CPU variant	: 0x0
CPU part	: 0xd03
CPU revision	: 4

processor	: 2
BogoMIPS	: 48.00
Features	: fp asimd evtstrm aes pmull sha1 sha2 crc32
CPU implementer	: 0x41
CPU architecture: 8
CPU variant	: 0x0
CPU part	: 0xd03
CPU revision	: 4

processor	: 3
BogoMIPS	: 48.00
Features	: fp asimd evtstrm aes pmull sha1 sha2 crc32
CPU implementer	: 0x41
CPU architecture: 8
CPU variant	: 0x0
CPU part	: 0xd03
CPU revision	: 4
`

	collector, tmpDir := createTestCPUInfoCollector(t)
	cpuinfoPath := filepath.Join(tmpDir, "proc", "cpuinfo")
	require.NoError(t, os.WriteFile(cpuinfoPath, []byte(arm64CortexA53CPUInfo), 0644))

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	info, ok := result.(*performance.CPUInfo)
	require.True(t, ok)

	assert.Equal(t, float64(48.00), info.BogoMIPS)
	assert.Equal(t, int32(4), info.LogicalCores)
	assert.Equal(t, int32(4), info.PhysicalCores) // No physical ID info, falls back to logical cores
	assert.Contains(t, info.Flags, "fp")
	assert.Contains(t, info.Flags, "aes")
	assert.Contains(t, info.Flags, "sha1")
	assert.Contains(t, info.Flags, "sha2")
	assert.Contains(t, info.Flags, "crc32")
}

func TestCPUInfoCollector_ARMAndroid(t *testing.T) {
	// ARM Android device
	const armAndroidCPUInfo = `Processor	: ARMv7 Processor rev 1 (v7l)
processor	: 0
BogoMIPS	: 1592.52

processor	: 1
BogoMIPS	: 2388.78

Features	: swp half thumb fastmult vfp edsp neon vfpv3 tls
CPU implementer	: 0x41
CPU architecture: 7
CPU variant	: 0x2
CPU part	: 0xc09
CPU revision	: 1

Hardware	: SMDK4210
Revision	: 000e
Serial		: 304d19f36a02309e
`

	collector, tmpDir := createTestCPUInfoCollector(t)
	cpuinfoPath := filepath.Join(tmpDir, "proc", "cpuinfo")
	require.NoError(t, os.WriteFile(cpuinfoPath, []byte(armAndroidCPUInfo), 0644))

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	info, ok := result.(*performance.CPUInfo)
	require.True(t, ok)

	assert.Equal(t, float64(1592.52), info.BogoMIPS) // First processor's BogoMIPS
	assert.Equal(t, int32(2), info.LogicalCores)
	assert.Equal(t, int32(2), info.PhysicalCores) // No physical ID info, falls back to logical cores
	assert.Contains(t, info.Flags, "neon")
	assert.Contains(t, info.Flags, "vfp")
	assert.Contains(t, info.Flags, "edsp")
}

func TestCPUInfoCollector_ARMCortexA7(t *testing.T) {
	// ARM Cortex-A7 (sun8i hardware)
	const armCortexA7CPUInfo = `Processor	: ARMv7 Processor rev 5 (v7l)
processor	: 0
BogoMIPS	: 2400.00

processor	: 1
BogoMIPS	: 2400.00

processor	: 2
BogoMIPS	: 2400.00

processor	: 3
BogoMIPS	: 2400.00

Features	: swp half thumb fastmult vfp edsp thumbee neon vfpv3 tls vfpv4 idiva idivt
CPU implementer	: 0x41
CPU architecture: 7
CPU variant	: 0x0
CPU part	: 0xc07
CPU revision	: 5

Hardware	: sun8i
Revision	: 0000
Serial		: 5400503583203c3c040e
`

	collector, tmpDir := createTestCPUInfoCollector(t)
	cpuinfoPath := filepath.Join(tmpDir, "proc", "cpuinfo")
	require.NoError(t, os.WriteFile(cpuinfoPath, []byte(armCortexA7CPUInfo), 0644))

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	info, ok := result.(*performance.CPUInfo)
	require.True(t, ok)

	assert.Equal(t, float64(2400.00), info.BogoMIPS)
	assert.Equal(t, int32(4), info.LogicalCores)
	assert.Equal(t, int32(4), info.PhysicalCores) // No physical ID info, falls back to logical cores
	assert.Contains(t, info.Flags, "neon")
	assert.Contains(t, info.Flags, "vfpv3")
	assert.Contains(t, info.Flags, "vfpv4")
	assert.Contains(t, info.Flags, "idiva")
	assert.Contains(t, info.Flags, "idivt")
}

func TestCPUInfoCollector_IntelCeleronG3900(t *testing.T) {
	// Intel Celeron G3900 @ 2.80GHz
	const intelCeleronG3900CPUInfo = `processor	: 0
vendor_id	: GenuineIntel
cpu family	: 6
model		: 94
model name	: Intel(R) Celeron(R) CPU G3900 @ 2.80GHz
stepping	: 3
microcode	: 0xea
cpu MHz		: 899.999
cache size	: 2048 KB
physical id	: 0
siblings	: 2
core id		: 0
cpu cores	: 2
apicid		: 0
initial apicid	: 0
fpu		: yes
fpu_exception	: yes
cpuid level	: 22
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr sse sse2 ss ht tm pbe syscall nx pdpe1gb rdtscp lm constant_tsc art arch_perfmon pebs bts rep_good nopl xtopology nonstop_tsc cpuid aperfmperf pni pclmulqdq dtes64 monitor ds_cpl vmx est tm2 ssse3 sdbg cx16 xtpr pdcm pcid sse4_1 sse4_2 x2apic movbe popcnt tsc_deadline_timer aes xsave rdrand lahf_lm abm 3dnowprefetch cpuid_fault epb invpcid_single pti ssbd ibrs ibpb stibp tpr_shadow vnmi flexpriority ept vpid fsgsbase tsc_adjust erms invpcid mpx rdseed adx smap clflushopt intel_pt xsaveopt xsavec xgetbv1 xsaves dtherm arat pln pts hwp hwp_notify hwp_act_window hwp_epp md_clear flush_l1d
bugs		: cpu_meltdown spectre_v1 spectre_v2 spec_store_bypass l1tf mds swapgs taa itlb_multihit srbds
bogomips	: 5615.85
clflush size	: 64
cache_alignment	: 64
address sizes	: 39 bits physical, 48 bits virtual
power management:

processor	: 1
vendor_id	: GenuineIntel
cpu family	: 6
model		: 94
model name	: Intel(R) Celeron(R) CPU G3900 @ 2.80GHz
stepping	: 3
microcode	: 0xea
cpu MHz		: 899.999
cache size	: 2048 KB
physical id	: 0
siblings	: 2
core id		: 1
cpu cores	: 2
apicid		: 2
initial apicid	: 2
fpu		: yes
fpu_exception	: yes
cpuid level	: 22
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr sse sse2 ss ht tm pbe syscall nx pdpe1gb rdtscp lm constant_tsc art arch_perfmon pebs bts rep_good nopl xtopology nonstop_tsc cpuid aperfmperf pni pclmulqdq dtes64 monitor ds_cpl vmx est tm2 ssse3 sdbg cx16 xtpr pdcm pcid sse4_1 sse4_2 x2apic movbe popcnt tsc_deadline_timer aes xsave rdrand lahf_lm abm 3dnowprefetch cpuid_fault epb invpcid_single pti ssbd ibrs ibpb stibp tpr_shadow vnmi flexpriority ept vpid fsgsbase tsc_adjust erms invpcid mpx rdseed adx smap clflushopt intel_pt xsaveopt xsavec xgetbv1 xsaves dtherm arat pln pts hwp hwp_notify hwp_act_window hwp_epp md_clear flush_l1d
bugs		: cpu_meltdown spectre_v1 spectre_v2 spec_store_bypass l1tf mds swapgs taa itlb_multihit srbds
bogomips	: 5615.85
clflush size	: 64
cache_alignment	: 64
address sizes	: 39 bits physical, 48 bits virtual
power management:
`

	collector, tmpDir := createTestCPUInfoCollector(t)
	cpuinfoPath := filepath.Join(tmpDir, "proc", "cpuinfo")
	require.NoError(t, os.WriteFile(cpuinfoPath, []byte(intelCeleronG3900CPUInfo), 0644))

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	info, ok := result.(*performance.CPUInfo)
	require.True(t, ok)

	assert.Equal(t, "GenuineIntel", info.VendorID)
	assert.Equal(t, "Intel(R) Celeron(R) CPU G3900 @ 2.80GHz", info.ModelName)
	assert.Equal(t, int32(6), info.CPUFamily)
	assert.Equal(t, int32(94), info.Model)
	assert.Equal(t, int32(3), info.Stepping)
	assert.Equal(t, float64(5615.85), info.BogoMIPS)
	assert.Equal(t, int32(2), info.LogicalCores)
	assert.Equal(t, int32(2), info.PhysicalCores)
	assert.Equal(t, "2048 KB", info.CacheSize)
	assert.Equal(t, int32(64), info.CacheAlignment)
	assert.Contains(t, info.Flags, "sse4_2")
	assert.Contains(t, info.Flags, "aes")
	assert.Contains(t, info.Flags, "vmx")
}

func TestCPUInfoCollector_IntelI74500U(t *testing.T) {
	// Intel Core i7-4500U CPU @ 1.80GHz (laptop processor)
	const intelI74500UCPUInfo = `processor	: 0
vendor_id	: GenuineIntel
cpu family	: 6
model		: 69
model name	: Intel(R) Core(TM) i7-4500U CPU @ 1.80GHz
stepping	: 1
microcode	: 0x17
cpu MHz		: 774.000
cache size	: 4096 KB
physical id	: 0
siblings	: 4
core id		: 0
cpu cores	: 2
apicid		: 0
initial apicid	: 0
fpu		: yes
fpu_exception	: yes
cpuid level	: 13
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr sse sse2 ss ht tm pbe syscall nx pdpe1gb rdtscp lm constant_tsc arch_perfmon pebs bts rep_good nopl xtopology nonstop_tsc aperfmperf eagerfpu pni pclmulqdq dtes64 monitor ds_cpl vmx est tm2 ssse3 fma cx16 xtpr pdcm pcid sse4_1 sse4_2 movbe popcnt tsc_deadline_timer aes xsave avx f16c rdrand lahf_lm abm ida arat epb xsaveopt pln pts dtherm tpr_shadow vnmi flexpriority ept vpid fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid
bogomips	: 3591.40
clflush size	: 64
cache_alignment	: 64
address sizes	: 39 bits physical, 48 bits virtual
power management:

processor	: 1
vendor_id	: GenuineIntel
cpu family	: 6
model		: 69
model name	: Intel(R) Core(TM) i7-4500U CPU @ 1.80GHz
stepping	: 1
microcode	: 0x17
cpu MHz		: 1600.000
cache size	: 4096 KB
physical id	: 0
siblings	: 4
core id		: 0
cpu cores	: 2
apicid		: 1
initial apicid	: 1
fpu		: yes
fpu_exception	: yes
cpuid level	: 13
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr sse sse2 ss ht tm pbe syscall nx pdpe1gb rdtscp lm constant_tsc arch_perfmon pebs bts rep_good nopl xtopology nonstop_tsc aperfmperf eagerfpu pni pclmulqdq dtes64 monitor ds_cpl vmx est tm2 ssse3 fma cx16 xtpr pdcm pcid sse4_1 sse4_2 movbe popcnt tsc_deadline_timer aes xsave avx f16c rdrand lahf_lm abm ida arat epb xsaveopt pln pts dtherm tpr_shadow vnmi flexpriority ept vpid fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid
bogomips	: 3591.40
clflush size	: 64
cache_alignment	: 64
address sizes	: 39 bits physical, 48 bits virtual
power management:

processor	: 2
vendor_id	: GenuineIntel
cpu family	: 6
model		: 69
model name	: Intel(R) Core(TM) i7-4500U CPU @ 1.80GHz
stepping	: 1
microcode	: 0x17
cpu MHz		: 1600.000
cache size	: 4096 KB
physical id	: 0
siblings	: 4
core id		: 1
cpu cores	: 2
apicid		: 2
initial apicid	: 2
fpu		: yes
fpu_exception	: yes
cpuid level	: 13
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr sse sse2 ss ht tm pbe syscall nx pdpe1gb rdtscp lm constant_tsc arch_perfmon pebs bts rep_good nopl xtopology nonstop_tsc aperfmperf eagerfpu pni pclmulqdq dtes64 monitor ds_cpl vmx est tm2 ssse3 fma cx16 xtpr pdcm pcid sse4_1 sse4_2 movbe popcnt tsc_deadline_timer aes xsave avx f16c rdrand lahf_lm abm ida arat epb xsaveopt pln pts dtherm tpr_shadow vnmi flexpriority ept vpid fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid
bogomips	: 3591.40
clflush size	: 64
cache_alignment	: 64
address sizes	: 39 bits physical, 48 bits virtual
power management:

processor	: 3
vendor_id	: GenuineIntel
cpu family	: 6
model		: 69
model name	: Intel(R) Core(TM) i7-4500U CPU @ 1.80GHz
stepping	: 1
microcode	: 0x17
cpu MHz		: 759.375
cache size	: 4096 KB
physical id	: 0
siblings	: 4
core id		: 1
cpu cores	: 2
apicid		: 3
initial apicid	: 3
fpu		: yes
fpu_exception	: yes
cpuid level	: 13
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr sse sse2 ss ht tm pbe syscall nx pdpe1gb rdtscp lm constant_tsc arch_perfmon pebs bts rep_good nopl xtopology nonstop_tsc aperfmperf eagerfpu pni pclmulqdq dtes64 monitor ds_cpl vmx est tm2 ssse3 fma cx16 xtpr pdcm pcid sse4_1 sse4_2 movbe popcnt tsc_deadline_timer aes xsave avx f16c rdrand lahf_lm abm ida arat epb xsaveopt pln pts dtherm tpr_shadow vnmi flexpriority ept vpid fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid
bogomips	: 3591.40
clflush size	: 64
cache_alignment	: 64
address sizes	: 39 bits physical, 48 bits virtual
power management:
`

	collector, tmpDir := createTestCPUInfoCollector(t)
	cpuinfoPath := filepath.Join(tmpDir, "proc", "cpuinfo")
	require.NoError(t, os.WriteFile(cpuinfoPath, []byte(intelI74500UCPUInfo), 0644))

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	info, ok := result.(*performance.CPUInfo)
	require.True(t, ok)

	assert.Equal(t, "GenuineIntel", info.VendorID)
	assert.Equal(t, "Intel(R) Core(TM) i7-4500U CPU @ 1.80GHz", info.ModelName)
	assert.Equal(t, int32(6), info.CPUFamily)
	assert.Equal(t, int32(69), info.Model)
	assert.Equal(t, int32(1), info.Stepping)
	assert.Equal(t, float64(3591.40), info.BogoMIPS)
	assert.Equal(t, int32(4), info.LogicalCores)  // 4 logical processors (hyperthreading)
	assert.Equal(t, int32(2), info.PhysicalCores) // 2 physical cores
	assert.Equal(t, "4096 KB", info.CacheSize)
	assert.Equal(t, int32(64), info.CacheAlignment)
	assert.Contains(t, info.Flags, "avx2")
	assert.Contains(t, info.Flags, "aes")
	assert.Contains(t, info.Flags, "vmx")
}

func TestCPUInfoCollector_IntelPentium4(t *testing.T) {
	// Intel Pentium 4 CPU 3.20GHz (legacy processor with HT)
	const intelPentium4CPUInfo = `processor	: 0
vendor_id	: GenuineIntel
cpu family	: 15
model		: 4
model name	: Intel(R) Pentium(R) 4 CPU 3.20GHz
stepping	: 1
cpu MHz		: 3200.000
cache size	: 1024 KB
physical id	: 0
siblings	: 2
core id		: 0
cpu cores	: 1
apicid		: 0
initial apicid	: 0
fdiv_bug	: no
hlt_bug		: no
f00f_bug	: no
coma_bug	: no
fpu		: yes
fpu_exception	: yes
cpuid level	: 5
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr sse sse2 ss ht tm pbe lm constant_tsc pebs bts pni dtes64 monitor ds_cpl cid cx16 xtpr
bogomips	: 6379.72
clflush size	: 64
power management:

processor	: 1
vendor_id	: GenuineIntel
cpu family	: 15
model		: 4
model name	: Intel(R) Pentium(R) 4 CPU 3.20GHz
stepping	: 1
cpu MHz		: 3200.000
cache size	: 1024 KB
physical id	: 0
siblings	: 2
core id		: 0
cpu cores	: 1
apicid		: 1
initial apicid	: 1
fdiv_bug	: no
hlt_bug		: no
f00f_bug	: no
coma_bug	: no
fpu		: yes
fpu_exception	: yes
cpuid level	: 5
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr sse sse2 ss ht tm pbe lm constant_tsc pebs bts pni dtes64 monitor ds_cpl cid cx16 xtpr
bogomips	: 6379.72
clflush size	: 64
power management:
`

	collector, tmpDir := createTestCPUInfoCollector(t)
	cpuinfoPath := filepath.Join(tmpDir, "proc", "cpuinfo")
	require.NoError(t, os.WriteFile(cpuinfoPath, []byte(intelPentium4CPUInfo), 0644))

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	info, ok := result.(*performance.CPUInfo)
	require.True(t, ok)

	assert.Equal(t, "GenuineIntel", info.VendorID)
	assert.Equal(t, "Intel(R) Pentium(R) 4 CPU 3.20GHz", info.ModelName)
	assert.Equal(t, int32(15), info.CPUFamily)
	assert.Equal(t, int32(4), info.Model)
	assert.Equal(t, int32(1), info.Stepping)
	assert.Equal(t, float64(6379.72), info.BogoMIPS)
	assert.Equal(t, int32(2), info.LogicalCores)  // 2 logical processors (hyperthreading)
	assert.Equal(t, int32(2), info.PhysicalCores) // VM fallback when all cores have same physical/core ID
	assert.Equal(t, "1024 KB", info.CacheSize)
	assert.Equal(t, int32(0), info.CacheAlignment) // cache_alignment not in processor block
	assert.Contains(t, info.Flags, "sse2")
	assert.Contains(t, info.Flags, "ht")
}

func TestCPUInfoCollector_IntelXeon8259CL(t *testing.T) {
	// Intel Xeon Platinum 8259CL CPU @ 2.50GHz (AWS EC2)
	const intelXeon8259CLCPUInfo = `processor	: 0
vendor_id	: GenuineIntel
cpu family	: 6
model		: 85
model name	: Intel(R) Xeon(R) Platinum 8259CL CPU @ 2.50GHz
stepping	: 7
microcode	: 0x5003901
cpu MHz		: 2499.998
cache size	: 36608 KB
physical id	: 0
siblings	: 2
core id		: 0
cpu cores	: 1
apicid		: 0
initial apicid	: 0
fpu		: yes
fpu_exception	: yes
cpuid level	: 13
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ss ht syscall nx pdpe1gb rdtscp lm constant_tsc rep_good nopl xtopology nonstop_tsc cpuid tsc_known_freq pni pclmulqdq ssse3 fma cx16 pcid sse4_1 sse4_2 x2apic movbe popcnt tsc_deadline_timer aes xsave avx f16c rdrand hypervisor lahf_lm abm 3dnowprefetch invpcid_single pti fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid mpx avx512f avx512dq rdseed adx smap clflushopt clwb avx512cd avx512bw avx512vl xsaveopt xsavec xgetbv1 xsaves ida arat pku ospke
bugs		: cpu_meltdown spectre_v1 spectre_v2 spec_store_bypass l1tf mds swapgs itlb_multihit mmio_stale_data retbleed gds bhi its
bogomips	: 4999.99
clflush size	: 64
cache_alignment	: 64
address sizes	: 46 bits physical, 48 bits virtual
power management:

processor	: 1
vendor_id	: GenuineIntel
cpu family	: 6
model		: 85
model name	: Intel(R) Xeon(R) Platinum 8259CL CPU @ 2.50GHz
stepping	: 7
microcode	: 0x5003901
cpu MHz		: 2499.998
cache size	: 36608 KB
physical id	: 0
siblings	: 2
core id		: 0
cpu cores	: 1
apicid		: 1
initial apicid	: 1
fpu		: yes
fpu_exception	: yes
cpuid level	: 13
wp		: yes
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ss ht syscall nx pdpe1gb rdtscp lm constant_tsc rep_good nopl xtopology nonstop_tsc cpuid tsc_known_freq pni pclmulqdq ssse3 fma cx16 pcid sse4_1 sse4_2 x2apic movbe popcnt tsc_deadline_timer aes xsave avx f16c rdrand hypervisor lahf_lm abm 3dnowprefetch invpcid_single pti fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid mpx avx512f avx512dq rdseed adx smap clflushopt clwb avx512cd avx512bw avx512vl xsaveopt xsavec xgetbv1 xsaves ida arat pku ospke
bugs		: cpu_meltdown spectre_v1 spectre_v2 spec_store_bypass l1tf mds swapgs itlb_multihit mmio_stale_data retbleed gds bhi its
bogomips	: 4999.99
clflush size	: 64
cache_alignment	: 64
address sizes	: 46 bits physical, 48 bits virtual
power management:
`

	collector, tmpDir := createTestCPUInfoCollector(t)
	cpuinfoPath := filepath.Join(tmpDir, "proc", "cpuinfo")
	require.NoError(t, os.WriteFile(cpuinfoPath, []byte(intelXeon8259CLCPUInfo), 0644))

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	info, ok := result.(*performance.CPUInfo)
	require.True(t, ok)

	assert.Equal(t, "GenuineIntel", info.VendorID)
	assert.Equal(t, "Intel(R) Xeon(R) Platinum 8259CL CPU @ 2.50GHz", info.ModelName)
	assert.Equal(t, int32(6), info.CPUFamily)
	assert.Equal(t, int32(85), info.Model)
	assert.Equal(t, int32(7), info.Stepping)
	assert.Equal(t, float64(4999.99), info.BogoMIPS)
	assert.Equal(t, int32(2), info.LogicalCores)
	assert.Equal(t, int32(2), info.PhysicalCores) // VM fallback: counts logical cores
	assert.Equal(t, "36608 KB", info.CacheSize)
	assert.Equal(t, int32(64), info.CacheAlignment)
	assert.Contains(t, info.Flags, "avx512f")
	assert.Contains(t, info.Flags, "avx512cd")
	assert.Contains(t, info.Flags, "avx512bw")
	assert.Contains(t, info.Flags, "hypervisor")
}

// For funsies
func TestCPUInfoCollector_PowerPC(t *testing.T) {
	// PowerPC PowerBook G4 (7410 processor)
	const powerPCCPUInfo = `processor : 0
cpu : 7410, altivec supported
temperature : 59-61 C (uncalibrated)
clock : 500MHz
revision : 17.3 (pvr 800c 1103)
bogomips : 996.14

machine : PowerBook3,2
motherboard : PowerBook3,2 MacRISC2 MacRISC Power Macintosh
L2 cache : 1024K unified
memory : 256MB
pmac-generation : NewWorld
`

	collector, tmpDir := createTestCPUInfoCollector(t)
	cpuinfoPath := filepath.Join(tmpDir, "proc", "cpuinfo")
	require.NoError(t, os.WriteFile(cpuinfoPath, []byte(powerPCCPUInfo), 0644))

	result, err := collector.Collect(context.Background())
	require.NoError(t, err)

	info, ok := result.(*performance.CPUInfo)
	require.True(t, ok)

	// PowerPC doesn't have vendor_id, model name, or numeric CPU identification
	assert.Equal(t, "", info.VendorID)
	assert.Equal(t, "", info.ModelName)
	assert.Equal(t, int32(0), info.CPUFamily)
	assert.Equal(t, int32(0), info.Model)
	assert.Equal(t, int32(0), info.Stepping)

	// PowerPC uses different field names
	assert.Equal(t, float64(996.14), info.BogoMIPS)
	assert.Equal(t, int32(1), info.LogicalCores)
	assert.Equal(t, int32(1), info.PhysicalCores) // Single processor

	// PowerPC doesn't expose CPU flags in cpuinfo
	assert.Empty(t, info.Flags)

	// PowerPC specific fields like "cpu", "clock", "temperature" are not parsed
	// by our generic collector, which is fine - we focus on common fields
}
