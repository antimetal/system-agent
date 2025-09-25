// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package types

import (
	"time"

	"github.com/antimetal/agent/pkg/performance"
)

// Snapshot represents a complete hardware snapshot at a point in time
// This is a simplified version focused on hardware topology rather than performance metrics
type Snapshot struct {
	Timestamp   time.Time
	NodeName    string
	ClusterName string

	// Hardware configuration data
	CPUInfo     *performance.CPUInfo
	MemoryInfo  *performance.MemoryInfo
	DiskInfo    []*performance.DiskInfo
	NetworkInfo []*performance.NetworkInfo

	// Collection metadata
	CollectorRun performance.CollectorRunInfo
}
