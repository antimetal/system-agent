// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package runtime

import (
	"os"
	"path/filepath"
	"strconv"

	agentv1 "github.com/antimetal/agent/pkg/api/antimetal/agent/v1"
	runtimev1 "github.com/antimetal/agent/pkg/api/antimetal/runtime/v1"
	"github.com/antimetal/agent/pkg/containers"
	"github.com/antimetal/agent/pkg/kernel"
	"github.com/antimetal/agent/pkg/performance"
	"github.com/google/uuid"
)

var (
	instance           *agentv1.Instance
	excludedCollectors = map[performance.MetricType]bool{
		performance.MetricTypeCPUInfo:     true,
		performance.MetricTypeMemoryInfo:  true,
		performance.MetricTypeDiskInfo:    true,
		performance.MetricTypeNetworkInfo: true,
		performance.MetricTypeProcess:     true,
	}
)

func init() {
	createInstance()
}

// GetInstance returns the Instance proto object containing
// runtime information.
func GetInstance() *agentv1.Instance {
	return instance
}

func createInstance() {
	id, err := uuid.NewV7()
	if err != nil {
		// something would be terribly wrong if this happened
		panic(err)
	}
	instanceID, err := id.MarshalBinary()
	if err != nil {
		panic(err)
	}

	instance = &agentv1.Instance{
		Id:                  instanceID,
		Build:               getBuildInfo(),
		SupportedCollectors: getSupportedCollectors(),
	}

	linuxRuntime, err := getLinuxRuntime()
	// If we get an error, assume that we are not running on Linux.
	if err == nil {
		instance.LinuxRuntime = linuxRuntime
	}
}

func getBuildInfo() *agentv1.Build {
	major, err := strconv.ParseUint(buildMajor, 10, 32)
	if err != nil {
		major = 0
	}

	minor, err := strconv.ParseUint(buildMinor, 10, 32)
	if err != nil {
		minor = 0
	}

	patch, err := strconv.ParseUint(buildPatch, 10, 32)
	if err != nil {
		patch = 0
	}

	return &agentv1.Build{
		Version: &agentv1.SemanticVersion{
			Major: uint32(major),
			Minor: uint32(minor),
			Patch: uint32(patch),
		},
		Revision: buildRev,
	}
}

func getLinuxRuntime() (*runtimev1.Linux, error) {
	version, err := kernel.GetCurrentVersion()
	if err != nil {
		return nil, err
	}

	cgroupInfo, err := getCgroupInfo()
	if err != nil {
		return nil, err
	}

	return &runtimev1.Linux{
		KernelVersion: version.Raw,
		CgroupInfo:    cgroupInfo,
	}, nil
}

func getCgroupInfo() (*runtimev1.CgroupInfo, error) {
	hostSysPath := os.Getenv("HOST_SYS")
	if hostSysPath != "" {
		hostSysPath = "/sys"
	}
	cgroupPath := filepath.Join(hostSysPath, "fs", "cgroup")
	cgroupVer, err := containers.NewDiscovery(cgroupPath).DetectCgroupVersion()
	if err != nil {
		return nil, err
	}

	var cgroupVersion runtimev1.CgroupVersion
	if cgroupVer == 2 {
		cgroupVersion = runtimev1.CgroupVersion_CGROUP_VERSION_V2
	} else {
		cgroupVersion = runtimev1.CgroupVersion_CGROUP_VERSION_V1
	}

	var cgroupDriver runtimev1.CgroupDriver
	systemSlice := filepath.Join(cgroupPath, "system.slice")
	if info, err := os.Stat(systemSlice); err == nil && info.IsDir() {
		cgroupDriver = runtimev1.CgroupDriver_CGROUP_DRIVER_SYSTEMD
	} else {
		cgroupDriver = runtimev1.CgroupDriver_CGROUP_DRIVER_CGROUPFS
	}

	cgroupInfo := &runtimev1.CgroupInfo{
		Version: cgroupVersion,
		Driver:  cgroupDriver,
	}
	return cgroupInfo, nil
}

func getSupportedCollectors() []string {
	availableTypes := performance.GetAvailableCollectors()
	collectors := make([]string, 0)

	for _, metricType := range availableTypes {
		if excludedCollectors[metricType] {
			continue
		}
		collectors = append(collectors, string(metricType))
	}

	return collectors
}
