// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

// Package proc provides utilities for reading system information from the /proc filesystem.
//
// This package offers access to common system parameters that are typically read
// once and reused throughout the program lifecycle. All functions support optional
// /proc paths for testing or containerized environments.
//
// Example usage:
//
//	// Get system boot time
//	bootTime, err := proc.BootTime()
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Get USER_HZ for converting kernel ticks to time
//	userHZ, err := proc.UserHZ()
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Get system page size
//	pageSize, err := proc.PageSize()
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Use custom /proc path (useful in containers)
//	bootTime, err := proc.BootTime("/host/proc")
package proc
