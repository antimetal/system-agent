// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

// Package testutil provides common test utilities and helpers.
package testutil

import (
	"runtime"
	"testing"
)

// RequireLinux skips the test if not running on Linux.
func RequireLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Test requires Linux")
	}
}

// RequireLinuxFilesystem skips the test if Linux filesystems are not available.
func RequireLinuxFilesystem(t *testing.T) {
	t.Helper()
	RequireLinux(t)
	// Additional filesystem checks could be added here if needed
}