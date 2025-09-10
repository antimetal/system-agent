// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package cpuutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCPUList(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected []int32
		wantErr  bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []int32{},
		},
		{
			name:     "single CPU",
			input:    "0",
			expected: []int32{0},
		},
		{
			name:     "multiple single CPUs",
			input:    "0,2,4",
			expected: []int32{0, 2, 4},
		},
		{
			name:     "simple range",
			input:    "0-3",
			expected: []int32{0, 1, 2, 3},
		},
		{
			name:     "multiple ranges",
			input:    "0-1,3-4",
			expected: []int32{0, 1, 3, 4},
		},
		{
			name:     "mixed singles and ranges",
			input:    "0,2-4,7",
			expected: []int32{0, 2, 3, 4, 7},
		},
		{
			name:     "with whitespace",
			input:    " 0 - 2 , 4 , 6 - 7 ",
			expected: []int32{0, 1, 2, 4, 6, 7},
		},
		{
			name:     "single CPU range (lenient parsing)",
			input:    "5-5",
			expected: []int32{5},
		},
		{
			name:     "large range",
			input:    "100-103",
			expected: []int32{100, 101, 102, 103},
		},
		{
			name:     "typical /sys/devices/system/cpu/online format",
			input:    "0-7\n",
			expected: []int32{0, 1, 2, 3, 4, 5, 6, 7},
		},
		{
			name:     "complex real-world example",
			input:    "0-3,8,10-11,15",
			expected: []int32{0, 1, 2, 3, 8, 10, 11, 15},
		},
		{
			name:    "invalid range format",
			input:   "0-2-4",
			wantErr: true,
		},
		{
			name:    "non-numeric",
			input:   "a,b,c",
			wantErr: true,
		},
		{
			name:    "invalid range start",
			input:   "a-3",
			wantErr: true,
		},
		{
			name:    "invalid range end",
			input:   "0-b",
			wantErr: true,
		},
		{
			name:    "reversed range",
			input:   "3-1",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ParseCPUList(tc.input)
			
			if tc.wantErr {
				assert.Error(t, err, "Expected error for input: %s", tc.input)
			} else {
				assert.NoError(t, err, "Unexpected error for input: %s", tc.input)
				assert.Equal(t, tc.expected, result, "CPU list mismatch for input: %s", tc.input)
			}
		})
	}
}

func TestFormatCPUList(t *testing.T) {
	testCases := []struct {
		name     string
		input    []int32
		expected string
	}{
		{
			name:     "empty slice",
			input:    []int32{},
			expected: "",
		},
		{
			name:     "single CPU",
			input:    []int32{0},
			expected: "0",
		},
		{
			name:     "two consecutive CPUs",
			input:    []int32{0, 1},
			expected: "0,1",
		},
		{
			name:     "range of three",
			input:    []int32{0, 1, 2},
			expected: "0-2",
		},
		{
			name:     "range of four",
			input:    []int32{0, 1, 2, 3},
			expected: "0-3",
		},
		{
			name:     "non-consecutive CPUs",
			input:    []int32{0, 2, 4},
			expected: "0,2,4",
		},
		{
			name:     "mixed ranges and singles",
			input:    []int32{0, 2, 3, 4, 7},
			expected: "0,2-4,7",
		},
		{
			name:     "unsorted input",
			input:    []int32{7, 2, 0, 4, 3},
			expected: "0,2-4,7",
		},
		{
			name:     "multiple ranges",
			input:    []int32{0, 1, 3, 4, 8, 9, 10},
			expected: "0,1,3,4,8-10",
		},
		{
			name:     "large consecutive range",
			input:    []int32{100, 101, 102, 103, 104, 105},
			expected: "100-105",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := FormatCPUList(tc.input)
			assert.Equal(t, tc.expected, result, "Formatted CPU list mismatch")
		})
	}
}

func TestRoundTrip(t *testing.T) {
	// Test that parse -> format -> parse produces the same result
	testCases := []string{
		"0",
		"0-3",
		"0,2-4,7",
		"0,1,3,4,8-11",
	}

	for _, original := range testCases {
		t.Run(original, func(t *testing.T) {
			// Parse the original
			parsed, err := ParseCPUList(original)
			assert.NoError(t, err)

			// Format it back
			formatted := FormatCPUList(parsed)

			// Parse again
			reparsed, err := ParseCPUList(formatted)
			assert.NoError(t, err)

			// Should be the same
			assert.Equal(t, parsed, reparsed, "Round trip failed for: %s", original)
		})
	}
}