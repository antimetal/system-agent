// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !linux

package graph

import (
	"crypto/md5"
	"fmt"
	"os"
)

// getMachineID returns a mock machine ID for non-Linux systems.
// This is primarily used for testing on macOS and other platforms.
// It generates a deterministic ID based on hostname for consistency in tests.
func getMachineID() string {
	hostname, err := os.Hostname()
	if err != nil {
		// Return a default mock ID if we can't get hostname
		return "mock-machine-id-123456789"
	}

	// Generate a deterministic hash based on hostname
	// This ensures consistent IDs across test runs on the same machine
	hash := md5.Sum([]byte("machine-" + hostname))
	return fmt.Sprintf("%x", hash)
}

// getSystemUUID returns a mock system UUID for non-Linux systems.
// This is primarily used for testing on macOS and other platforms.
func getSystemUUID() string {
	hostname, err := os.Hostname()
	if err != nil {
		// Return a default mock UUID if we can't get hostname
		return "mock-uuid-987654321"
	}

	// Generate a deterministic UUID-like string based on hostname
	hash := md5.Sum([]byte("system-" + hostname))
	// Format as a UUID-like string (8-4-4-4-12)
	hashStr := fmt.Sprintf("%x", hash)
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hashStr[0:8],
		hashStr[8:12],
		hashStr[12:16],
		hashStr[16:20],
		hashStr[20:32])
}
