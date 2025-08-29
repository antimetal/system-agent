// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCPUList(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  []int
		wantError bool
		errorMsg  string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []int{},
		},
		{
			name:     "whitespace only",
			input:    "   \n\t  ",
			expected: []int{},
		},
		{
			name:     "single CPU",
			input:    "5",
			expected: []int{5},
		},
		{
			name:     "single CPU with whitespace",
			input:    "  5  ",
			expected: []int{5},
		},
		{
			name:     "multiple individual CPUs",
			input:    "0,2,4,6",
			expected: []int{0, 2, 4, 6},
		},
		{
			name:     "multiple CPUs with spaces",
			input:    "0, 2, 4, 6",
			expected: []int{0, 2, 4, 6},
		},
		{
			name:     "simple range",
			input:    "0-3",
			expected: []int{0, 1, 2, 3},
		},
		{
			name:     "range with spaces",
			input:    " 0 - 3 ",
			expected: []int{0, 1, 2, 3},
		},
		{
			name:     "larger range",
			input:    "10-15",
			expected: []int{10, 11, 12, 13, 14, 15},
		},
		{
			name:     "single CPU range (start equals end)",
			input:    "5-5",
			expected: []int{5},
		},
		{
			name:     "mixed format with ranges and individual CPUs",
			input:    "0-3,6,8-10",
			expected: []int{0, 1, 2, 3, 6, 8, 9, 10},
		},
		{
			name:     "complex mixed format",
			input:    "0-3,8,10-11,15",
			expected: []int{0, 1, 2, 3, 8, 10, 11, 15},
		},
		{
			name:     "non-sequential individual CPUs",
			input:    "7,3,15,0,9",
			expected: []int{7, 3, 15, 0, 9},
		},
		{
			name:     "multiple ranges",
			input:    "0-2,4-6,8-10",
			expected: []int{0, 1, 2, 4, 5, 6, 8, 9, 10},
		},
		{
			name:     "trailing comma",
			input:    "0,1,2,",
			expected: []int{0, 1, 2},
		},
		{
			name:     "leading comma",
			input:    ",0,1,2",
			expected: []int{0, 1, 2},
		},
		{
			name:     "multiple commas",
			input:    "0,,1,,,2",
			expected: []int{0, 1, 2},
		},
		{
			name:     "real NUMA node example",
			input:    "0-7,16-23",
			expected: []int{0, 1, 2, 3, 4, 5, 6, 7, 16, 17, 18, 19, 20, 21, 22, 23},
		},
		{
			name:     "real container cpuset example",
			input:    "1-4,6",
			expected: []int{1, 2, 3, 4, 6},
		},
		// Error cases
		{
			name:      "invalid number",
			input:     "abc",
			wantError: true,
			errorMsg:  "invalid CPU ID",
		},
		{
			name:      "invalid range start",
			input:     "abc-5",
			wantError: true,
			errorMsg:  "invalid start CPU ID",
		},
		{
			name:      "invalid range end",
			input:     "0-xyz",
			wantError: true,
			errorMsg:  "invalid end CPU ID",
		},
		{
			name:      "invalid range format with multiple dashes",
			input:     "0-3-5",
			wantError: true,
			errorMsg:  "invalid range format",
		},
		{
			name:      "mixed valid and invalid",
			input:     "0-3,invalid,5",
			wantError: true,
			errorMsg:  "invalid CPU ID",
		},
		{
			name:      "negative CPU number",
			input:     "-1",
			wantError: true,
			errorMsg:  "invalid start CPU ID", // Interpreted as range with empty start
		},
		{
			name:      "negative range",
			input:     "-5--1",
			wantError: true,
			errorMsg:  "invalid range format", // Too many dashes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseCPUList(tt.input)

			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)

				// Check error type
				_, ok := err.(*ErrInvalidCPURange)
				assert.True(t, ok, "Expected error to be of type *ErrInvalidCPURange")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestParseCPUList_LargeCPUNumbers(t *testing.T) {
	// Test with large CPU numbers that might exist on high-core count systems
	tests := []struct {
		name     string
		input    string
		expected []int
	}{
		{
			name:     "high core count system",
			input:    "0-127",
			expected: generateRange(0, 127),
		},
		{
			name:     "very high CPU numbers",
			input:    "240-255",
			expected: generateRange(240, 255),
		},
		{
			name:     "mixed high and low",
			input:    "0,128,255",
			expected: []int{0, 128, 255},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseCPUList(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestErrInvalidCPURange_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *ErrInvalidCPURange
		expected string
	}{
		{
			name: "basic error",
			err: &ErrInvalidCPURange{
				Input:  "abc",
				Reason: "invalid CPU ID",
			},
			expected: "invalid CPU range 'abc': invalid CPU ID",
		},
		{
			name: "range format error",
			err: &ErrInvalidCPURange{
				Input:  "0-3-5",
				Reason: "invalid range format",
			},
			expected: "invalid CPU range '0-3-5': invalid range format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

// Benchmark ParseCPUList performance
func BenchmarkParseCPUList(b *testing.B) {
	benchmarks := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"single", "5"},
		{"range", "0-15"},
		{"mixed_small", "0-3,6,8-10"},
		{"mixed_large", "0-31,48-63,96,128-191"},
		{"high_core_count", "0-255"},
		{"complex_numa", "0-7,16-23,32-39,48-55"},
		{"many_individual", "0,2,4,6,8,10,12,14,16,18,20,22,24,26,28,30"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _ = ParseCPUList(bm.input)
			}
		})
	}
}

// BenchmarkParseCPUListParallel benchmarks concurrent parsing
func BenchmarkParseCPUListParallel(b *testing.B) {
	input := "0-31,48-63,96,128-191" // Complex NUMA topology

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = ParseCPUList(input)
		}
	})
}

// Helper function to generate a range of integers
func generateRange(start, end int) []int {
	result := make([]int, end-start+1)
	for i := range result {
		result[i] = start + i
	}
	return result
}
