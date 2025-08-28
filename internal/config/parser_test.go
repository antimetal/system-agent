// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	"github.com/antimetal/agent/internal/config"
	agentv1 "github.com/antimetal/agent/pkg/api/antimetal/agent/v1"
	typesv1 "github.com/antimetal/agent/pkg/api/antimetal/types/v1"
)

func createTestObject(name, objectType, version string) *typesv1.Object {
	return &typesv1.Object{
		Name:    name,
		Version: version,
		Type: &typesv1.TypeDescriptor{
			Type: objectType,
		},
		Data: []byte("test-data"),
	}
}

func createTestObjectWithoutData(name, objectType, version string) *typesv1.Object {
	return &typesv1.Object{
		Name:    name,
		Version: version,
		Type: &typesv1.TypeDescriptor{
			Type: objectType,
		},
	}
}

func createHostStatsCollectionConfigObject(name, version string, collectorName string) *typesv1.Object {
	configData := &agentv1.HostStatsCollectionConfig{
		Collector:       collectorName,
		IntervalSeconds: 30,
	}

	data, err := proto.Marshal(configData)
	if err != nil {
		panic(err)
	}

	return &typesv1.Object{
		Name:    name,
		Version: version,
		Type: &typesv1.TypeDescriptor{
			Type: string((&agentv1.HostStatsCollectionConfig{}).ProtoReflect().Descriptor().FullName()),
		},
		Data: data,
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name           string
		obj            *typesv1.Object
		expectedStatus config.Status
		expectError    bool
	}{
		// Basic parsing validation tests
		{
			name:           "nil object",
			obj:            nil,
			expectedStatus: config.StatusInvalid,
			expectError:    true,
		},
		{
			name:           "empty object name",
			obj:            createTestObject("", "test.Type", "1"),
			expectedStatus: config.StatusInvalid,
			expectError:    true,
		},
		{
			name: "nil object type",
			obj: &typesv1.Object{
				Name:    "test-obj",
				Version: "1",
				Type:    nil,
				Data:    []byte("test-data"),
			},
			expectedStatus: config.StatusInvalid,
			expectError:    true,
		},
		{
			name: "empty object type.type",
			obj: &typesv1.Object{
				Name:    "test-obj",
				Version: "1",
				Type: &typesv1.TypeDescriptor{
					Type: "",
					Kind: "test.Kind",
				},
				Data: []byte("test-data"),
			},
			expectedStatus: config.StatusInvalid,
			expectError:    true,
		},
		{
			name:           "empty object data",
			obj:            createTestObjectWithoutData("test-obj", "test.Type", "1"),
			expectedStatus: config.StatusInvalid,
			expectError:    true,
		},
		{
			name:           "unrecognized type",
			obj:            createTestObject("test-obj", "unrecognized.Type", "1"),
			expectedStatus: config.StatusInvalid,
			expectError:    true,
		},
		// HostStatsCollectionConfig specific tests
		{
			name:           "valid config with registered test collector",
			obj:            createHostStatsCollectionConfigObject("test-config", "1", "cpu"),
			expectedStatus: config.StatusOK,
			expectError:    false,
		},
		{
			name:           "unavailable collector",
			obj:            createHostStatsCollectionConfigObject("unavailable", "1", "unavailable"),
			expectedStatus: config.StatusInvalid,
			expectError:    true,
		},
		{
			name:           "empty collector name",
			obj:            createHostStatsCollectionConfigObject("empty-collector", "1", ""),
			expectedStatus: config.StatusInvalid,
			expectError:    true,
		},
		{
			name: "invalid protobuf data",
			obj: &typesv1.Object{
				Name:    "invalid-data-config",
				Version: "1",
				Type: &typesv1.TypeDescriptor{
					Type: string((&agentv1.HostStatsCollectionConfig{}).ProtoReflect().Descriptor().FullName()),
				},
				Data: []byte("invalid-protobuf-data"),
			},
			expectedStatus: config.StatusInvalid,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			instance, err := config.Parse(tt.obj)

			assert.Equal(t, tt.expectedStatus, instance.Status)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, instance.Object)
			assert.Equal(t, tt.obj.GetName(), instance.Name)
			assert.Equal(t, tt.obj.GetVersion(), instance.Version)
			assert.Equal(t, tt.obj.GetType().GetType(), instance.TypeUrl)
		})
	}
}
