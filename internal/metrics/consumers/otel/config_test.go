// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package otel

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	// Verify reduced default queue size
	assert.Equal(t, 1000, config.MaxQueueSize, "MaxQueueSize should be reduced to 1000")
	assert.Equal(t, 500, config.ExportBatchSize, "ExportBatchSize should remain 500")
	assert.Equal(t, 10*time.Second, config.BatchTimeout, "BatchTimeout should be 10s")
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config with reduced queue size",
			config: Config{
				Endpoint:        "localhost:4317",
				MaxQueueSize:    1000,
				ExportBatchSize: 500,
				BatchTimeout:    10 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "queue size too large",
			config: Config{
				Endpoint:        "localhost:4317",
				MaxQueueSize:    MaxSafeQueueSize + 1,
				ExportBatchSize: 500,
			},
			wantErr: true,
		},
		{
			name: "batch size too large",
			config: Config{
				Endpoint:        "localhost:4317",
				MaxQueueSize:    1000,
				ExportBatchSize: MaxSafeBatchSize + 1,
			},
			wantErr: true,
		},
		{
			name: "zero queue size gets default",
			config: Config{
				Endpoint:     "localhost:4317",
				MaxQueueSize: 0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.config.MaxQueueSize == 0 {
					// Verify it was set to the new default
					assert.Equal(t, 1000, tt.config.MaxQueueSize)
				}
			}
		})
	}
}
