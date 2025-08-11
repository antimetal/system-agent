// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !integration

package kernel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Version
		wantErr bool
	}{
		{
			name:  "standard version",
			input: "5.15.0",
			want: &Version{
				Major: 5,
				Minor: 15,
				Patch: 0,
				Raw:   "5.15.0",
			},
		},
		{
			name:  "version with suffix",
			input: "5.15.0-generic",
			want: &Version{
				Major: 5,
				Minor: 15,
				Patch: 0,
				Raw:   "5.15.0-generic",
			},
		},
		{
			name:  "version without patch",
			input: "5.15",
			want: &Version{
				Major: 5,
				Minor: 15,
				Patch: 0,
				Raw:   "5.15",
			},
		},
		{
			name:  "older kernel version",
			input: "4.18.0-348.el8.x86_64",
			want: &Version{
				Major: 4,
				Minor: 18,
				Patch: 0,
				Raw:   "4.18.0-348.el8.x86_64",
			},
		},
		{
			name:  "kernel 6.x",
			input: "6.2.0-26-generic",
			want: &Version{
				Major: 6,
				Minor: 2,
				Patch: 0,
				Raw:   "6.2.0-26-generic",
			},
		},
		{
			name:  "patch with extra info",
			input: "5.10.0rc1",
			want: &Version{
				Major: 5,
				Minor: 10,
				Patch: 0, // Can't parse "0rc1" as int
				Raw:   "5.10.0rc1",
			},
		},
		{
			name:    "invalid format - single number",
			input:   "5",
			wantErr: true,
		},
		{
			name:    "invalid format - non-numeric major",
			input:   "v5.15.0",
			wantErr: true,
		},
		{
			name:    "invalid format - non-numeric minor",
			input:   "5.x.0",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVersion(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestVersion_IsAtLeast(t *testing.T) {
	tests := []struct {
		name    string
		version *Version
		major   int
		minor   int
		want    bool
	}{
		{
			name:    "exact match",
			version: &Version{Major: 5, Minor: 8},
			major:   5,
			minor:   8,
			want:    true,
		},
		{
			name:    "newer major version",
			version: &Version{Major: 6, Minor: 0},
			major:   5,
			minor:   8,
			want:    true,
		},
		{
			name:    "newer minor version",
			version: &Version{Major: 5, Minor: 10},
			major:   5,
			minor:   8,
			want:    true,
		},
		{
			name:    "older major version",
			version: &Version{Major: 4, Minor: 18},
			major:   5,
			minor:   8,
			want:    false,
		},
		{
			name:    "same major, older minor",
			version: &Version{Major: 5, Minor: 4},
			major:   5,
			minor:   8,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.version.IsAtLeast(tt.major, tt.minor)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestVersion_Compare(t *testing.T) {
	tests := []struct {
		name string
		v1   *Version
		v2   *Version
		want int
	}{
		{
			name: "equal versions",
			v1:   &Version{Major: 5, Minor: 8, Patch: 0},
			v2:   &Version{Major: 5, Minor: 8, Patch: 0},
			want: 0,
		},
		{
			name: "v1 newer major",
			v1:   &Version{Major: 6, Minor: 0, Patch: 0},
			v2:   &Version{Major: 5, Minor: 8, Patch: 0},
			want: 1,
		},
		{
			name: "v1 older major",
			v1:   &Version{Major: 4, Minor: 18, Patch: 0},
			v2:   &Version{Major: 5, Minor: 8, Patch: 0},
			want: -1,
		},
		{
			name: "same major, v1 newer minor",
			v1:   &Version{Major: 5, Minor: 10, Patch: 0},
			v2:   &Version{Major: 5, Minor: 8, Patch: 0},
			want: 1,
		},
		{
			name: "same major, v1 older minor",
			v1:   &Version{Major: 5, Minor: 4, Patch: 0},
			v2:   &Version{Major: 5, Minor: 8, Patch: 0},
			want: -1,
		},
		{
			name: "same major/minor, v1 newer patch",
			v1:   &Version{Major: 5, Minor: 8, Patch: 10},
			v2:   &Version{Major: 5, Minor: 8, Patch: 5},
			want: 1,
		},
		{
			name: "same major/minor, v1 older patch",
			v1:   &Version{Major: 5, Minor: 8, Patch: 3},
			v2:   &Version{Major: 5, Minor: 8, Patch: 5},
			want: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.v1.Compare(tt.v2)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestVersion_String(t *testing.T) {
	tests := []struct {
		name    string
		version *Version
		want    string
	}{
		{
			name:    "standard version",
			version: &Version{Major: 5, Minor: 15, Patch: 0},
			want:    "5.15.0",
		},
		{
			name:    "single digit versions",
			version: &Version{Major: 4, Minor: 4, Patch: 0},
			want:    "4.4.0",
		},
		{
			name:    "with patch",
			version: &Version{Major: 5, Minor: 10, Patch: 123},
			want:    "5.10.123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.version.String()
			assert.Equal(t, tt.want, got)
		})
	}
}
