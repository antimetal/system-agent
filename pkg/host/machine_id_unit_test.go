// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !integration

package host

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetMachineID_AlwaysReturnsValue(t *testing.T) {
	// Even in unit tests (which may run on Mac), the function should return something
	id := GetMachineID()
	assert.NotEmpty(t, id, "Machine ID should never be empty")
	
	// It should return either a real ID or "unknown" as last resort
	// On Mac, it will likely return the hostname
	t.Logf("Got machine ID in unit test: %s", id)
}

func TestGetMachineID_ConsistencyUnit(t *testing.T) {
	// Ensure multiple calls return the same value even in unit tests
	id1 := GetMachineID()
	id2 := GetMachineID()
	
	assert.Equal(t, id1, id2, "Machine ID should be consistent across calls")
}

func TestGetSystemUUID_DoesNotPanic(t *testing.T) {
	// Should not panic even if files don't exist (e.g., on Mac)
	assert.NotPanics(t, func() {
		uuid := GetSystemUUID()
		// May be empty on non-Linux systems
		t.Logf("System UUID in unit test: %s (may be empty)", uuid)
	})
}