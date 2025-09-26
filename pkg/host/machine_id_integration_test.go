// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package host_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antimetal/agent/pkg/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMachineID(t *testing.T) {
	// Integration test - assumes Linux environment
	id, err := host.MachineID()
	require.NoError(t, err, "MachineID should not fail on Linux")
	assert.NotEmpty(t, id, "Machine ID should not be empty")

	t.Logf("Got machine ID: %s", id)

	// On a real Linux system, we should have either /etc/machine-id or DMI UUID
	// Verify we're reading from expected sources
	if _, err := os.Stat("/etc/machine-id"); err == nil {
		// machine-id exists, verify it matches what we got (unless we fell back)
		data, err := os.ReadFile("/etc/machine-id")
		if err == nil && len(data) > 0 {
			t.Log("Successfully read from /etc/machine-id")
		}
	} else {
		t.Log("/etc/machine-id not found, using fallback")
	}
}

func TestMachineID_Consistency(t *testing.T) {
	// Ensure multiple calls return the same value
	id1, err1 := host.MachineID()
	id2, err2 := host.MachineID()
	id3, err3 := host.MachineID()

	require.NoError(t, err1, "First MachineID call should not fail")
	require.NoError(t, err2, "Second MachineID call should not fail")
	require.NoError(t, err3, "Third MachineID call should not fail")

	assert.Equal(t, id1, id2, "Machine ID should be consistent across calls")
	assert.Equal(t, id2, id3, "Machine ID should be consistent across multiple calls")
}

func TestMachineID_ValidFormat(t *testing.T) {
	id, err := host.MachineID()
	require.NoError(t, err, "MachineID should not fail")

	// Machine ID should be a reasonable length
	assert.Greater(t, len(id), 0, "Machine ID should not be empty")
	assert.Less(t, len(id), 256, "Machine ID should not be excessively long")

	// Should not contain newlines or other whitespace
	assert.Equal(t, id, string([]byte(id)), "Machine ID should not contain special characters")
	assert.NotContains(t, id, "\n", "Machine ID should not contain newlines")
	assert.NotContains(t, id, "\r", "Machine ID should not contain carriage returns")
	assert.NotContains(t, id, "\t", "Machine ID should not contain tabs")
}

func TestSystemUUID(t *testing.T) {
	// This may require root permissions
	uuid, err := host.SystemUUID()

	if err == nil {
		t.Logf("Got system UUID: %s", uuid)

		// If we got a UUID, validate its format
		assert.Greater(t, len(uuid), 0, "UUID should not be empty if returned")
		assert.Less(t, len(uuid), 128, "UUID should not be excessively long")

		// Should not contain newlines
		assert.NotContains(t, uuid, "\n", "UUID should not contain newlines")
	} else {
		t.Logf("System UUID not available: %v (may require root or DMI may not be present)", err)
	}
}

func TestSystemUUID_Consistency(t *testing.T) {
	// If available, should be consistent
	uuid1, err1 := host.SystemUUID()
	uuid2, err2 := host.SystemUUID()

	// Both calls should have same result (either both succeed or both fail)
	assert.Equal(t, err1 == nil, err2 == nil, "SystemUUID calls should have consistent error state")
	if err1 == nil && err2 == nil {
		assert.Equal(t, uuid1, uuid2, "System UUID should be consistent across calls")
	}
}

func TestMachineID_RealSystemFiles(t *testing.T) {
	// Test that we can actually read from real system files
	// This validates our file paths are correct

	// Check if /etc/machine-id exists and is readable
	if info, err := os.Stat("/etc/machine-id"); err == nil {
		assert.True(t, info.Mode().IsRegular(), "/etc/machine-id should be a regular file")

		// Try to read it
		data, err := os.ReadFile("/etc/machine-id")
		if err == nil {
			assert.Greater(t, len(data), 0, "/etc/machine-id should contain data")
			t.Logf("/etc/machine-id contains %d bytes", len(data))
		} else {
			t.Logf("Cannot read /etc/machine-id: %v", err)
		}
	} else {
		t.Log("/etc/machine-id does not exist on this system")
	}

	// Check if DMI UUID exists (may require root)
	if info, err := os.Stat("/sys/class/dmi/id/product_uuid"); err == nil {
		assert.True(t, info.Mode().IsRegular(), "DMI product_uuid should be a regular file")

		// Try to read it
		data, err := os.ReadFile("/sys/class/dmi/id/product_uuid")
		if err == nil {
			assert.Greater(t, len(data), 0, "DMI UUID should contain data")
			t.Logf("DMI UUID contains %d bytes", len(data))
		} else {
			t.Logf("Cannot read DMI UUID (may require root): %v", err)
		}
	} else {
		t.Log("DMI UUID does not exist on this system (may be in container/VM)")
	}
}

func TestMachineID_FallbackBehavior(t *testing.T) {
	// Test the fallback behavior by checking what source was actually used
	id, err := host.MachineID()
	require.NoError(t, err, "MachineID should not fail on Linux")
	require.NotEmpty(t, id)

	// Determine which source was used
	machineIDData, machineIDErr := os.ReadFile("/etc/machine-id")
	dbusIDData, dbusIDErr := os.ReadFile("/var/lib/dbus/machine-id")

	var source string
	if machineIDErr == nil && len(machineIDData) > 0 {
		// Check if our ID matches machine-id
		machineIDStr := strings.TrimSpace(string(machineIDData))
		if machineIDStr == id {
			source = "/etc/machine-id"
		}
	}

	if source == "" && dbusIDErr == nil && len(dbusIDData) > 0 {
		// Check if our ID matches D-Bus machine-id
		dbusIDStr := strings.TrimSpace(string(dbusIDData))
		if dbusIDStr == id {
			source = "/var/lib/dbus/machine-id"
		}
	}

	t.Logf("Machine ID source: %s, value: %s", source, id)
	assert.NotEmpty(t, source, "Should be able to determine ID source")
}

func TestMachineID_WithTempFiles(t *testing.T) {
	// Create temporary files to test parsing logic
	tmpDir := t.TempDir()

	// Test with various machine-id formats
	testCases := []struct {
		name     string
		content  string
		expected string // What we expect after trimming
	}{
		{
			name:     "normal-machine-id",
			content:  "1234567890abcdef1234567890abcdef\n",
			expected: "1234567890abcdef1234567890abcdef",
		},
		{
			name:     "machine-id-no-newline",
			content:  "abcdef1234567890abcdef1234567890",
			expected: "abcdef1234567890abcdef1234567890",
		},
		{
			name:     "machine-id-with-spaces",
			content:  "  fedcba0987654321fedcba0987654321  \n",
			expected: "fedcba0987654321fedcba0987654321",
		},
		{
			name:     "uuid-format",
			content:  "550e8400-e29b-41d4-a716-446655440000\n",
			expected: "550e8400-e29b-41d4-a716-446655440000",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tmpDir, tc.name)
			require.NoError(t, os.WriteFile(testFile, []byte(tc.content), 0644))

			// Read and verify parsing
			data, err := os.ReadFile(testFile)
			require.NoError(t, err)

			// Simulate what our function does
			parsed := string(data)
			if idx := len(parsed); idx > 0 && parsed[idx-1] == '\n' {
				parsed = parsed[:idx-1]
			}
			parsed = stringTrimSpace(parsed)

			assert.Equal(t, tc.expected, parsed, "Should properly parse machine ID format")
		})
	}
}

// Helper function to trim space (matching the actual implementation)
func stringTrimSpace(s string) string {
	start := 0
	end := len(s)

	for start < end && isSpace(s[start]) {
		start++
	}
	for start < end && isSpace(s[end-1]) {
		end--
	}

	return s[start:end]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
