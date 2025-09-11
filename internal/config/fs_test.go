// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package config_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/antimetal/agent/internal/config"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	agentv1 "github.com/antimetal/agent/pkg/api/antimetal/agent/v1"
)

// createHostStatsJSON creates a JSON string for HostStatsCollectionConfig
func createHostStatsJSON(name, collector string) string {
	config := &agentv1.HostStatsCollectionConfig{
		Collector:       collector,
		IntervalSeconds: 30,
	}
	data, err := proto.Marshal(config)
	if err != nil {
		panic(err)
	}

	// Convert to base64 for JSON
	encodedData := base64.StdEncoding.EncodeToString(data)

	return `{"type":{"type":"` + string((&agentv1.HostStatsCollectionConfig{}).ProtoReflect().Descriptor().FullName()) + `"},"name":"` + name + `","data":"` + encodedData + `"}`
}

// createHostStatsYAML creates a YAML string for HostStatsCollectionConfig
func createHostStatsYAML(name, collector string) string {
	config := &agentv1.HostStatsCollectionConfig{
		Collector:       collector,
		IntervalSeconds: 30,
	}
	data, err := proto.Marshal(config)
	if err != nil {
		panic(err)
	}

	// Convert to base64 for YAML
	encodedData := base64.StdEncoding.EncodeToString(data)
	typeName := string((&agentv1.HostStatsCollectionConfig{}).ProtoReflect().Descriptor().FullName())

	return `type:
  type: ` + typeName + `
name: ` + name + `
data: "` + encodedData + `"`
}

func TestFSLoader_Watch(t *testing.T) {

	tests := []struct {
		name         string
		filename     string
		content      string
		expectObject bool
		expectName   string
		expectError  bool
	}{
		{
			name:         "valid JSON config",
			filename:     "config.json",
			content:      createHostStatsJSON("test-json-object", "cpu"),
			expectObject: true,
			expectName:   "test-json-object",
		},
		{
			name:         "valid YAML config",
			filename:     "config.yaml",
			content:      createHostStatsYAML("test-yaml-object", "cpu"),
			expectObject: true,
			expectName:   "test-yaml-object",
		},
		{
			name:         "valid YML config",
			filename:     "config.yml",
			content:      createHostStatsYAML("test-yml-object", "cpu"),
			expectObject: true,
			expectName:   "test-yml-object",
		},
		{
			name:         "case insensitive JSON",
			filename:     "Config.JSON",
			content:      createHostStatsJSON("test-object", "cpu"),
			expectObject: true,
			expectName:   "test-object",
		},
		{
			name:         "invalid JSON",
			filename:     "invalid.json",
			content:      `{"invalid": json}`,
			expectObject: false,
			expectError:  true,
		},
		{
			name:     "invalid YAML",
			filename: "invalid.yaml",
			content: `invalid: yaml
  - with: bad
    indentation`,
			expectObject: false,
			expectError:  true,
		},
		{
			name:         "empty JSON file",
			filename:     "empty.json",
			content:      "",
			expectObject: false,
			expectError:  true,
		},
		{
			name:         "empty YAML file",
			filename:     "empty.yaml",
			content:      "",
			expectObject: false,
			expectError:  true,
		},
		{
			name:         "non-config file",
			filename:     "test.txt",
			content:      "some text content",
			expectObject: false,
		},
		{
			name:         "XML file ignored",
			filename:     "config.xml",
			content:      "<config></config>",
			expectObject: false,
		},
		{
			name:         "no extension",
			filename:     "config",
			content:      "content",
			expectObject: false,
		},
		{
			name:         "just extension JSON",
			filename:     ".json",
			content:      createHostStatsJSON("test", "cpu"),
			expectObject: true,
			expectName:   "test",
		},
		{
			name:         "just extension YAML",
			filename:     ".yaml",
			content:      createHostStatsYAML("test", "cpu"),
			expectObject: true,
			expectName:   "test",
		},
		{
			name:         "subdirectory JSON config",
			filename:     "subdir/nested.json",
			content:      createHostStatsJSON("nested-object", "cpu"),
			expectObject: true,
			expectName:   "nested-object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger := testr.New(t)

			tempDir := t.TempDir()

			testFile := filepath.Join(tempDir, tt.filename)
			err := os.MkdirAll(filepath.Dir(testFile), 0755)
			require.NoError(t, err)

			err = os.WriteFile(testFile, []byte(tt.content), 0644)
			require.NoError(t, err)

			fl, err := config.NewFSLoader(tempDir, logger)
			require.NoError(t, err)
			defer fl.Close()

			instanceCh := fl.Watch(config.Options{})

			if tt.expectObject {
				select {
				case instance := <-instanceCh:
					if tt.expectName != "" {
						assert.Equal(t, tt.expectName, instance.Name)
					}
					// Verify the instance was parsed successfully
					assert.Equal(t, config.StatusOK, instance.Status)
				case <-time.After(2 * time.Second):
					t.Fatal("timeout waiting for instance")
				}
			} else {
				select {
				case instance := <-instanceCh:
					if tt.expectError {
						// For error cases, verify the status is Invalid
						assert.Equal(t, config.StatusInvalid, instance.Status)
					} else {
						// For non-config files, we shouldn't get any instance
						t.Fatalf("unexpected instance received: %+v", instance)
					}
				case <-time.After(500 * time.Millisecond):
					// Expected no update for non-config files
				}
			}
		})
	}
}

func TestFSLoader_ListConfigs(t *testing.T) {
	t.Parallel()

	logger := testr.New(t)

	tempDir := t.TempDir()

	config1 := createHostStatsJSON("config1", "cpu")
	err := os.WriteFile(filepath.Join(tempDir, "config1.json"), []byte(config1), 0644)
	require.NoError(t, err)

	config2 := createHostStatsJSON("config2", "memory")
	err = os.WriteFile(filepath.Join(tempDir, "config2.json"), []byte(config2), 0644)
	require.NoError(t, err)

	fl, err := config.NewFSLoader(tempDir, logger)
	require.NoError(t, err)
	defer fl.Close()

	configs, err := fl.ListConfigs(config.Options{})
	require.NoError(t, err)

	hostStatsTypeName := string((&agentv1.HostStatsCollectionConfig{}).ProtoReflect().Descriptor().FullName())
	assert.Len(t, configs, 1)
	assert.Contains(t, configs, hostStatsTypeName)
	assert.Len(t, configs[hostStatsTypeName], 2)

	configs, err = fl.ListConfigs(config.Options{
		Filters: config.Filters{
			Types: []string{hostStatsTypeName},
		},
	})
	require.NoError(t, err)

	assert.Len(t, configs, 1)
	assert.Contains(t, configs, hostStatsTypeName)
}

func TestFSLoader_GetConfig(t *testing.T) {
	t.Parallel()

	logger := testr.New(t)

	tempDir := t.TempDir()

	config1 := createHostStatsJSON("config1", "cpu")
	err := os.WriteFile(filepath.Join(tempDir, "config1.json"), []byte(config1), 0644)
	require.NoError(t, err)

	fl, err := config.NewFSLoader(tempDir, logger)
	require.NoError(t, err)
	defer fl.Close()

	hostStatsTypeName := string((&agentv1.HostStatsCollectionConfig{}).ProtoReflect().Descriptor().FullName())
	instance, err := fl.GetConfig(hostStatsTypeName, "config1")
	require.NoError(t, err)
	assert.Equal(t, "config1", instance.Name)
	assert.Equal(t, hostStatsTypeName, instance.TypeUrl)

	_, err = fl.GetConfig(hostStatsTypeName, "nonexistent")
	assert.Error(t, err)

	_, err = fl.GetConfig("nonexistent.Type", "config1")
	assert.Error(t, err)
}

func TestFSLoader_FileChangeSubscription(t *testing.T) {
	tests := []struct {
		name          string
		filters       config.Filters
		initialConfig string
		updatedConfig string
		expectUpdate  bool
		expectStatus  config.Status
		expectError   bool
	}{
		{
			name:          "valid config update",
			initialConfig: createHostStatsJSON("config1", "cpu"),
			updatedConfig: createHostStatsJSON("config1", "cpu"),
			expectUpdate:  true,
			expectStatus:  config.StatusOK,
		},
		{
			name:          "invalid JSON",
			filters:       config.Filters{Status: config.StatusOK | config.StatusInvalid},
			initialConfig: createHostStatsJSON("config1", "cpu"),
			updatedConfig: `{"invalid": json}`,
			expectUpdate:  true,
			expectStatus:  config.StatusInvalid,
			expectError:   true,
		},
		{
			name:          "empty file",
			filters:       config.Filters{Status: config.StatusOK | config.StatusInvalid},
			initialConfig: createHostStatsJSON("config1", "cpu"),
			updatedConfig: ``,
			expectUpdate:  true,
			expectStatus:  config.StatusInvalid,
			expectError:   true,
		},
		{
			name:          "missing type",
			filters:       config.Filters{Status: config.StatusOK | config.StatusInvalid},
			initialConfig: createHostStatsJSON("config1", "cpu"),
			updatedConfig: `{"name":"config1","version":"2"}`,
			expectUpdate:  true,
			expectStatus:  config.StatusInvalid,
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger := testr.New(t)

			tempDir := t.TempDir()
			configFile := filepath.Join(tempDir, "config1.json")

			err := os.WriteFile(configFile, []byte(tt.initialConfig), 0644)
			require.NoError(t, err)

			fl, err := config.NewFSLoader(tempDir, logger)
			require.NoError(t, err)
			defer fl.Close()

			instanceCh := fl.Watch(config.Options{Filters: tt.filters})

			// Drain initial config from subscription (loaded at startup)
			select {
			case <-instanceCh:
			case <-time.After(1 * time.Second):
				t.Fatal("timeout waiting for initial config")
			}

			err = os.WriteFile(configFile, []byte(tt.updatedConfig), 0644)
			require.NoError(t, err)

			if tt.expectUpdate {
				// Should receive updated config via subscription
				select {
				case instance := <-instanceCh:
					assert.Equal(t, tt.expectStatus, instance.Status)
					if tt.expectStatus == config.StatusOK {
						assert.Equal(t, "config1", instance.Name)
						assert.NotNil(t, instance.Object)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("timeout waiting for config update via subscription")
				}
			} else {
				select {
				case instance := <-instanceCh:
					t.Fatalf("unexpected instance received: %+v", instance)
				case <-time.After(500 * time.Millisecond):
					// Expected no update
				}
			}
		})
	}
}
