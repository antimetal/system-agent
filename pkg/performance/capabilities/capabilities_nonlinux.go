// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !linux

package capabilities

// HasAllCapabilities on non-Linux systems always returns true
// since capabilities are a Linux-specific concept
func HasAllCapabilities(required []Capability) (bool, []Capability, error) {
	// On non-Linux systems, we assume all capabilities are available
	return true, nil, nil
}
