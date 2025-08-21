// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tracker

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// DetectCapabilities analyzes the system to determine available tracking methods.
// This function probes filesystem permissions, fsnotify functionality, and other
// requirements for event-driven tracking.
func DetectCapabilities(cgroupPath string) TrackerCapabilities {
	caps := TrackerCapabilities{
		Limitations: make([]string, 0),
	}
	
	// Test fsnotify availability
	caps.FsnotifyAvailable = testFsnotifyAvailability()
	if !caps.FsnotifyAvailable {
		caps.Limitations = append(caps.Limitations, "fsnotify not available")
	}
	
	// Test /proc filesystem access
	caps.ProcfsAvailable = testProcfsAccess()
	if !caps.ProcfsAvailable {
		caps.Limitations = append(caps.Limitations, "/proc filesystem not accessible")
	}
	
	// Test cgroup filesystem access
	caps.CgroupfsAvailable = testCgroupfsAccess(cgroupPath)
	if !caps.CgroupfsAvailable {
		caps.Limitations = append(caps.Limitations, fmt.Sprintf("cgroup filesystem at %s not accessible", cgroupPath))
	}
	
	// Test ability to watch /proc for process events
	caps.CanWatchProc = caps.FsnotifyAvailable && caps.ProcfsAvailable && testProcWatch()
	if !caps.CanWatchProc && caps.FsnotifyAvailable && caps.ProcfsAvailable {
		caps.Limitations = append(caps.Limitations, "cannot watch /proc for process events")
	}
	
	// Test ability to watch cgroup directories
	caps.CanWatchCgroups = caps.FsnotifyAvailable && caps.CgroupfsAvailable && testCgroupWatch(cgroupPath)
	if !caps.CanWatchCgroups && caps.FsnotifyAvailable && caps.CgroupfsAvailable {
		caps.Limitations = append(caps.Limitations, "cannot watch cgroup directories for container events")
	}
	
	return caps
}

// CanUseEventDriven returns true if event-driven tracking is possible
func (c TrackerCapabilities) CanUseEventDriven() bool {
	return c.CanWatchProc || c.CanWatchCgroups
}

// CanUsePolling returns true if polling-based tracking is possible
func (c TrackerCapabilities) CanUsePolling() bool {
	return c.ProcfsAvailable && c.CgroupfsAvailable
}

// RecommendedMode returns the best tracking mode for the detected capabilities
func (c TrackerCapabilities) RecommendedMode() TrackerMode {
	if c.CanUseEventDriven() {
		return TrackerModeEventDriven
	}
	if c.CanUsePolling() {
		return TrackerModePolling
	}
	// If nothing works, return polling as fallback (will fail gracefully)
	return TrackerModePolling
}

// testFsnotifyAvailability tests if fsnotify can be created and used
func testFsnotifyAvailability() bool {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return false
	}
	defer watcher.Close()
	return true
}

// testProcfsAccess tests if /proc filesystem is accessible
func testProcfsAccess() bool {
	// Check if /proc exists and is readable
	if _, err := os.Stat("/proc"); err != nil {
		return false
	}
	
	// Try to read /proc/self/stat as a basic functionality test
	if _, err := os.ReadFile("/proc/self/stat"); err != nil {
		return false
	}
	
	return true
}

// testCgroupfsAccess tests if cgroup filesystem is accessible
func testCgroupfsAccess(cgroupPath string) bool {
	// Check if the cgroup path exists
	if _, err := os.Stat(cgroupPath); err != nil {
		return false
	}
	
	// Try to list directories in the cgroup path
	entries, err := os.ReadDir(cgroupPath)
	if err != nil {
		return false
	}
	
	// Must have at least some entries (controllers or unified hierarchy)
	return len(entries) > 0
}

// testProcWatch tests if we can actually watch /proc for events
func testProcWatch() bool {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return false
	}
	defer watcher.Close()
	
	// Try to watch /proc - some systems may not allow this
	err = watcher.Add("/proc")
	if err != nil {
		return false
	}
	
	return true
}

// testCgroupWatch tests if we can watch cgroup directories for events
func testCgroupWatch(cgroupPath string) bool {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return false
	}
	defer watcher.Close()
	
	// Try to watch the cgroup root
	err = watcher.Add(cgroupPath)
	if err != nil {
		return false
	}
	
	// Try to watch common cgroup subdirectories
	testPaths := []string{
		filepath.Join(cgroupPath, "systemd"),
		filepath.Join(cgroupPath, "system.slice"),
		filepath.Join(cgroupPath, "cpu"),
		filepath.Join(cgroupPath, "memory"),
	}
	
	watchableCount := 0
	for _, testPath := range testPaths {
		if _, err := os.Stat(testPath); err == nil {
			if err := watcher.Add(testPath); err == nil {
				watchableCount++
			}
		}
	}
	
	// We need at least one watchable cgroup directory
	return watchableCount > 0
}

// GetCapabilitySummary returns a human-readable summary of capabilities
func (c TrackerCapabilities) GetCapabilitySummary() string {
	summary := "Runtime Tracking Capabilities:\n"
	
	if c.CanUseEventDriven() {
		summary += "✅ Event-driven tracking: Available\n"
		if c.CanWatchProc {
			summary += "  ✅ Process events: Can watch /proc\n"
		} else {
			summary += "  ❌ Process events: Cannot watch /proc\n"
		}
		if c.CanWatchCgroups {
			summary += "  ✅ Container events: Can watch cgroups\n"
		} else {
			summary += "  ❌ Container events: Cannot watch cgroups\n"
		}
	} else {
		summary += "❌ Event-driven tracking: Not available\n"
	}
	
	if c.CanUsePolling() {
		summary += "✅ Polling tracking: Available\n"
	} else {
		summary += "❌ Polling tracking: Not available\n"
	}
	
	summary += fmt.Sprintf("🎯 Recommended mode: %s\n", c.RecommendedMode())
	
	if len(c.Limitations) > 0 {
		summary += "⚠️  Limitations:\n"
		for _, limitation := range c.Limitations {
			summary += fmt.Sprintf("  - %s\n", limitation)
		}
	}
	
	return summary
}