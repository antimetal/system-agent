// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !linux

package host

// GetMachineID returns the machine ID or an empty string if not available.
// This function provides a platform-agnostic interface that doesn't return errors.
func GetMachineID() string {
	return MachineID()
}

// GetSystemUUID returns the system UUID or an empty string if not available.
// This function provides a platform-agnostic interface that doesn't return errors.
func GetSystemUUID() string {
	return SystemUUID()
}
