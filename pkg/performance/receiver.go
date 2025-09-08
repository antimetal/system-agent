// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

// Receiver accepts output from collectors and processes it accordingly.
// Different receiver implementations handle different types of collector data.
type Receiver interface {
	// Accept processes data from a collector.
	// The data parameter contains the actual collected data (e.g., *CPUInfo, *MemoryStats).
	Accept(data any) error

	// Name returns the receiver's name for logging and identification.
	Name() string
}
