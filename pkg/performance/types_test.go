// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance_test

import (
	"testing"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/stretchr/testify/assert"
)

func TestCollectionConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  performance.CollectionConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "all valid absolute paths",
			config: performance.CollectionConfig{
				HostProcPath: "/proc",
				HostSysPath:  "/sys",
				HostDevPath:  "/dev",
			},
			wantErr: false,
		},
		{
			name: "empty paths are valid",
			config: performance.CollectionConfig{
				HostProcPath: "",
				HostSysPath:  "",
				HostDevPath:  "",
			},
			wantErr: false,
		},
		{
			name: "invalid relative proc path",
			config: performance.CollectionConfig{
				HostProcPath: "proc",
				HostSysPath:  "/sys",
				HostDevPath:  "/dev",
			},
			wantErr: true,
			errMsg:  "HostProcPath must be an absolute path, got: \"proc\"",
		},
		{
			name: "invalid relative sys path",
			config: performance.CollectionConfig{
				HostProcPath: "/proc",
				HostSysPath:  "sys",
				HostDevPath:  "/dev",
			},
			wantErr: true,
			errMsg:  "HostSysPath must be an absolute path, got: \"sys\"",
		},
		{
			name: "invalid relative dev path",
			config: performance.CollectionConfig{
				HostProcPath: "/proc",
				HostSysPath:  "/sys",
				HostDevPath:  "dev",
			},
			wantErr: true,
			errMsg:  "HostDevPath must be an absolute path, got: \"dev\"",
		},
		{
			name: "mixed valid and invalid paths",
			config: performance.CollectionConfig{
				HostProcPath: "/proc",
				HostSysPath:  "sys",
				HostDevPath:  "/dev",
			},
			wantErr: true,
			errMsg:  "HostSysPath must be an absolute path, got: \"sys\"",
		},
		{
			name: "only proc path configured",
			config: performance.CollectionConfig{
				HostProcPath: "/proc",
			},
			wantErr: false,
		},
		{
			name: "only sys path configured",
			config: performance.CollectionConfig{
				HostSysPath: "/sys",
			},
			wantErr: false,
		},
		{
			name: "only dev path configured",
			config: performance.CollectionConfig{
				HostDevPath: "/dev",
			},
			wantErr: false,
		},
		{
			name: "paths with trailing slashes",
			config: performance.CollectionConfig{
				HostProcPath: "/proc/",
				HostSysPath:  "/sys/",
				HostDevPath:  "/dev/",
			},
			wantErr: false,
		},
		{
			name: "root path is valid",
			config: performance.CollectionConfig{
				HostProcPath: "/",
				HostSysPath:  "/",
				HostDevPath:  "/",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tt.errMsg, err.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
