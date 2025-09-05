// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/antimetal/agent/internal/config"
	"github.com/antimetal/agent/internal/config/internal/mock"
	agentv1 "github.com/antimetal/agent/pkg/api/antimetal/agent/v1"
	agentsvcv1 "github.com/antimetal/agent/pkg/api/antimetal/service/agent/v1"
	typesv1 "github.com/antimetal/agent/pkg/api/antimetal/types/v1"
)

var hostStatsType = string((&agentv1.HostStatsCollectionConfig{}).ProtoReflect().Descriptor().FullName())

func createTestPbConfig(name, version, typeUrl string, data []byte) *typesv1.Object {
	return &typesv1.Object{
		Name:    name,
		Version: version,
		Type: &typesv1.TypeDescriptor{
			Type: typeUrl,
		},
		Data: data,
	}
}

func createMockAMSServer(t *testing.T, svc *mock.AgentManagementService) (*grpc.ClientConn, func()) {
	conn, cleanup := mock.NewGRPCServer(t, mock.GRPCService{
		Descriptor: &agentsvcv1.AgentManagementService_ServiceDesc,
		Impl:       svc,
	})
	return conn, cleanup
}

func TestAMSLoader_StreamRecreation(t *testing.T) {
	tests := []struct {
		name              string
		streamFailures    int
		expectedRetries   int
		finalSuccess      bool
		streamDuration    time.Duration
		maxStreamAge      time.Duration
		keepStreamAlive   bool
		testDuration      time.Duration
		expectedCreations int
	}{
		{
			name:              "server terminates stream",
			streamFailures:    0,
			expectedRetries:   2,
			finalSuccess:      true,
			streamDuration:    100 * time.Millisecond,
			maxStreamAge:      200 * time.Millisecond,
			keepStreamAlive:   false,
			testDuration:      300 * time.Millisecond,
			expectedCreations: 2,
		},
		{
			name:              "stream fails then succeeds",
			streamFailures:    1,
			expectedRetries:   1,
			finalSuccess:      true,
			streamDuration:    500 * time.Millisecond,
			maxStreamAge:      1000 * time.Millisecond,
			keepStreamAlive:   false,
			testDuration:      100 * time.Millisecond,
			expectedCreations: 2,
		},
		{
			name:              "multiple stream failures",
			streamFailures:    3,
			expectedRetries:   3,
			finalSuccess:      true,
			streamDuration:    500 * time.Millisecond,
			maxStreamAge:      1000 * time.Millisecond,
			keepStreamAlive:   false,
			testDuration:      200 * time.Millisecond,
			expectedCreations: 4,
		},
		{
			name:              "maxStreamAge reached",
			expectedRetries:   1,
			finalSuccess:      true,
			maxStreamAge:      100 * time.Millisecond,
			keepStreamAlive:   true,
			testDuration:      300 * time.Millisecond,
			expectedCreations: 2,
		},
		{
			name:              "stream failures with maxStreamAge",
			streamFailures:    1,
			expectedRetries:   2,
			finalSuccess:      true,
			maxStreamAge:      100 * time.Millisecond,
			keepStreamAlive:   true,
			testDuration:      200 * time.Millisecond,
			expectedCreations: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockService := mock.NewAgentManagementService(mock.AgentManagementServiceOptions{
				StreamFailures: tt.streamFailures,
				StreamDuration: tt.streamDuration,
				KeepAlive:      tt.keepStreamAlive,
			})
			conn, cleanup := createMockAMSServer(t, mockService)
			instance := &agentv1.Instance{Id: []byte("test-instance")}
			loader, err := config.NewAMSLoader(conn,
				config.WithInstance(instance),
				config.WithMaxStreamAge(tt.maxStreamAge))
			require.NoError(t, err)

			t.Cleanup(func() {
				loader.Close()
				cleanup()
			})

			time.Sleep(tt.testDuration)

			attempts := mockService.GetStreamAttempts()

			assert.GreaterOrEqual(t, attempts, tt.expectedRetries+1, "Should have made at least the expected number of stream retries")
			assert.GreaterOrEqual(t, attempts, tt.expectedCreations, "Should have created at least stream expected number of times")
		})
	}
}

func TestAMSLoader_ListConfigs(t *testing.T) {
	hostStatsConfig1 := &agentv1.HostStatsCollectionConfig{Collector: "cpu", IntervalSeconds: 30}
	config1Data, err := proto.Marshal(hostStatsConfig1)
	require.NoError(t, err)

	hostStatsConfig2 := &agentv1.HostStatsCollectionConfig{Collector: "memory", IntervalSeconds: 60}
	config2Data, err := proto.Marshal(hostStatsConfig2)
	require.NoError(t, err)

	tests := []struct {
		name                string
		configsToCache      []*typesv1.Object
		filters             config.Filters
		expectedTypes       []string
		expectedCounts      map[string]int
		expectedConfigNames map[string][]string
	}{
		{
			name: "list all configs with no filters",
			configsToCache: []*typesv1.Object{
				createTestPbConfig("config1", "1", hostStatsType, config1Data),
				createTestPbConfig("config2", "1", hostStatsType, config2Data),
			},
			filters:        config.Filters{},
			expectedTypes:  []string{hostStatsType},
			expectedCounts: map[string]int{hostStatsType: 2},
			expectedConfigNames: map[string][]string{
				hostStatsType: {"config1", "config2"},
			},
		},
		{
			name: "filter by specific type",
			configsToCache: []*typesv1.Object{
				createTestPbConfig("config1", "1", hostStatsType, config1Data),
				createTestPbConfig("config2", "1", "other.Type", config2Data),
			},
			filters: config.Filters{
				Types: []string{hostStatsType},
			},
			expectedTypes:  []string{hostStatsType},
			expectedCounts: map[string]int{hostStatsType: 1},
			expectedConfigNames: map[string][]string{
				hostStatsType: {"config1"},
			},
		},
		{
			name: "filter by status - only valid configs",
			configsToCache: []*typesv1.Object{
				createTestPbConfig("valid-config", "1", hostStatsType, config1Data),
				createTestPbConfig("invalid-config", "1", hostStatsType, []byte("invalid-data")),
			},
			filters: config.Filters{
				Status: config.StatusOK,
			},
			expectedTypes:  []string{hostStatsType},
			expectedCounts: map[string]int{hostStatsType: 1},
			expectedConfigNames: map[string][]string{
				hostStatsType: {"valid-config"},
			},
		},
		{
			name: "filter by status - include both valid and invalid",
			configsToCache: []*typesv1.Object{
				createTestPbConfig("valid-config", "1", hostStatsType, config1Data),
				createTestPbConfig("invalid-config", "1", hostStatsType, []byte("invalid-data")),
			},
			filters: config.Filters{
				Status: config.StatusOK | config.StatusInvalid,
			},
			expectedTypes:  []string{hostStatsType},
			expectedCounts: map[string]int{hostStatsType: 2},
			expectedConfigNames: map[string][]string{
				hostStatsType: {"valid-config", "invalid-config"},
			},
		},
		{
			name: "combined type and status filters",
			configsToCache: []*typesv1.Object{
				createTestPbConfig("config1", "1", hostStatsType, config1Data),
				createTestPbConfig("config2", "1", "other.Type", config2Data),
				createTestPbConfig("config3", "1", hostStatsType, []byte("invalid-data")),
			},
			filters: config.Filters{
				Types:  []string{hostStatsType},
				Status: config.StatusOK,
			},
			expectedTypes:  []string{hostStatsType},
			expectedCounts: map[string]int{hostStatsType: 1},
			expectedConfigNames: map[string][]string{
				hostStatsType: {"config1"},
			},
		},
		{
			name:                "empty cache",
			configsToCache:      []*typesv1.Object{},
			filters:             config.Filters{},
			expectedTypes:       []string{},
			expectedCounts:      map[string]int{},
			expectedConfigNames: map[string][]string{},
		},
		{
			name: "no matching configs after filtering",
			configsToCache: []*typesv1.Object{
				createTestPbConfig("config1", "1", "other.Type", config1Data),
			},
			filters: config.Filters{
				Types: []string{hostStatsType},
			},
			expectedTypes:       []string{},
			expectedCounts:      map[string]int{},
			expectedConfigNames: map[string][]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockService := mock.NewAgentManagementService(mock.AgentManagementServiceOptions{
				StreamFailures: 0,
				StreamDuration: 0,
				KeepAlive:      true,
			})

			conn, cleanup := createMockAMSServer(t, mockService)
			instance := &agentv1.Instance{Id: []byte("test-instance")}

			loader, err := config.NewAMSLoader(conn, config.WithInstance(instance))
			require.NoError(t, err)

			t.Cleanup(func() {
				loader.Close()
				cleanup()
			})

			if len(tt.configsToCache) > 0 {
				mockService.SendUpdates(tt.configsToCache)
				// Wait for configs to be cached
				time.Sleep(200 * time.Millisecond)
			}

			configs, err := loader.ListConfigs(config.Options{Filters: tt.filters})
			require.NoError(t, err)

			assert.Len(t, configs, len(tt.expectedTypes))
			for _, expectedType := range tt.expectedTypes {
				assert.Contains(t, configs, expectedType)
				assert.Len(t, configs[expectedType], tt.expectedCounts[expectedType])

				if expectedNames, exists := tt.expectedConfigNames[expectedType]; exists {
					actualNames := make([]string, len(configs[expectedType]))
					for i, cfg := range configs[expectedType] {
						actualNames[i] = cfg.Name
					}
					for _, expectedName := range expectedNames {
						assert.Contains(t, actualNames, expectedName)
					}
				}
			}
		})
	}
}

func TestAMSLoader_GetConfig(t *testing.T) {
	hostStatsConfig := &agentv1.HostStatsCollectionConfig{Collector: "cpu", IntervalSeconds: 30}
	configData, err := proto.Marshal(hostStatsConfig)
	require.NoError(t, err)

	tests := []struct {
		name           string
		configsToCache []*typesv1.Object
		requestType    string
		requestName    string
		expectError    bool
		expectedName   string
		expectedType   string
	}{
		{
			name: "get existing config by type and name",
			configsToCache: []*typesv1.Object{
				createTestPbConfig("test-config", "1", hostStatsType, configData),
			},
			requestType:  hostStatsType,
			requestName:  "test-config",
			expectError:  false,
			expectedName: "test-config",
			expectedType: hostStatsType,
		},
		{
			name: "get config with multiple configs cached",
			configsToCache: []*typesv1.Object{
				createTestPbConfig("config1", "1", hostStatsType, configData),
				createTestPbConfig("config2", "1", hostStatsType, configData),
			},
			requestType:  hostStatsType,
			requestName:  "config2",
			expectError:  false,
			expectedName: "config2",
			expectedType: hostStatsType,
		},
		{
			name: "config not found - wrong name",
			configsToCache: []*typesv1.Object{
				createTestPbConfig("test-config", "1", hostStatsType, configData),
			},
			requestType: hostStatsType,
			requestName: "nonexistent-config",
			expectError: true,
		},
		{
			name: "config not found - wrong type",
			configsToCache: []*typesv1.Object{
				createTestPbConfig("test-config", "1", hostStatsType, configData),
			},
			requestType: "nonexistent.Type",
			requestName: "test-config",
			expectError: true,
		},
		{
			name:           "config not found - empty cache",
			configsToCache: []*typesv1.Object{},
			requestType:    hostStatsType,
			requestName:    "test-config",
			expectError:    true,
		},
		{
			name: "get invalid config by type and name",
			configsToCache: []*typesv1.Object{
				createTestPbConfig("invalid-config", "1", hostStatsType, []byte("invalid-data")),
			},
			requestType:  hostStatsType,
			requestName:  "invalid-config",
			expectError:  false,
			expectedName: "invalid-config",
			expectedType: hostStatsType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockService := mock.NewAgentManagementService(mock.AgentManagementServiceOptions{
				StreamFailures: 0,
				StreamDuration: 0,
				KeepAlive:      true,
			})

			conn, cleanup := createMockAMSServer(t, mockService)
			instance := &agentv1.Instance{Id: []byte("test-instance")}

			loader, err := config.NewAMSLoader(conn, config.WithInstance(instance))
			require.NoError(t, err)

			t.Cleanup(func() {
				loader.Close()
				cleanup()
			})

			if len(tt.configsToCache) > 0 {
				mockService.SendUpdates(tt.configsToCache)
				// Wait for configs to be cached
				time.Sleep(200 * time.Millisecond)
			}

			config, err := loader.GetConfig(tt.requestType, tt.requestName)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedName, config.Name)
			assert.Equal(t, tt.expectedType, config.TypeUrl)
		})
	}
}

func TestAMSLoader_Watch(t *testing.T) {
	hostStatsConfig1 := &agentv1.HostStatsCollectionConfig{Collector: "cpu", IntervalSeconds: 30}
	config1Data, err := proto.Marshal(hostStatsConfig1)
	require.NoError(t, err)

	hostStatsConfig2 := &agentv1.HostStatsCollectionConfig{Collector: "memory", IntervalSeconds: 30}
	config2Data, err := proto.Marshal(hostStatsConfig2)
	require.NoError(t, err)

	hostStatsConfig1Updated := &agentv1.HostStatsCollectionConfig{Collector: "cpu", IntervalSeconds: 10}
	config1UpdatedData, err := proto.Marshal(hostStatsConfig1Updated)
	require.NoError(t, err)

	tests := []struct {
		name           string
		updateSequence [][]*typesv1.Object
		filters        config.Filters
		expectedCount  int
		expectConfigs  []string
	}{
		{
			name: "watch with no filters returns all configs",
			updateSequence: [][]*typesv1.Object{
				{
					createTestPbConfig("config1", "1", hostStatsType, config1Data),
					createTestPbConfig("config2", "1", hostStatsType, config2Data),
				},
			},
			filters:       config.Filters{},
			expectedCount: 2,
			expectConfigs: []string{"config1", "config2"},
		},
		{
			name: "watch with type filter returns matching configs",
			updateSequence: [][]*typesv1.Object{
				{
					createTestPbConfig("config1", "1", hostStatsType, config1Data),
					createTestPbConfig("config2", "1", "other.Type", config2Data),
				},
			},
			filters: config.Filters{
				Types: []string{hostStatsType},
			},
			expectedCount: 1,
			expectConfigs: []string{"config1"},
		},
		{
			name: "watch with status bitmask filter returns matching configs",
			updateSequence: [][]*typesv1.Object{
				{
					createTestPbConfig("config1", "1", hostStatsType, config1Data),
					createTestPbConfig("config2", "1", hostStatsType, []byte("invalid-data")),
				},
			},
			filters: config.Filters{
				Status: config.StatusOK,
			},
			expectedCount: 1,
			expectConfigs: []string{"config1"},
		},
		{
			name: "watch with combined status bitmask filter",
			updateSequence: [][]*typesv1.Object{
				{
					createTestPbConfig("config1", "1", hostStatsType, config1Data),
					createTestPbConfig("config2", "1", hostStatsType, []byte("invalid-data")),
				},
			},
			filters: config.Filters{
				Status: config.StatusOK | config.StatusInvalid,
			},
			expectedCount: 2,
			expectConfigs: []string{"config1", "config2"},
		},
		{
			name: "watch with multiple type filters",
			updateSequence: [][]*typesv1.Object{
				{
					createTestPbConfig("config1", "1", hostStatsType, config1Data),
					createTestPbConfig("config2", "1", "other.Type", config2Data),
					createTestPbConfig("config3", "1", "third.Type", config1Data),
				},
			},
			filters: config.Filters{
				Types:  []string{hostStatsType, "third.Type"},
				Status: config.StatusOK | config.StatusInvalid,
			},
			expectedCount: 2,
			expectConfigs: []string{"config1", "config3"},
		},
		{
			name:           "watch with empty cache returns no configs",
			updateSequence: [][]*typesv1.Object{},
			filters:        config.Filters{},
			expectedCount:  0,
			expectConfigs:  []string{},
		},
		{
			name: "watch receives config update",
			updateSequence: [][]*typesv1.Object{
				{createTestPbConfig("config1", "1", hostStatsType, config1Data)},
				{createTestPbConfig("config1", "2", hostStatsType, config1UpdatedData)},
			},
			filters:       config.Filters{},
			expectedCount: 2,
			expectConfigs: []string{"config1", "config1"},
		},
		{
			name: "watch receives new config via update",
			updateSequence: [][]*typesv1.Object{
				{createTestPbConfig("config1", "1", hostStatsType, config1Data)},
				{createTestPbConfig("config2", "1", hostStatsType, config2Data)},
			},
			filters:       config.Filters{},
			expectedCount: 2,
			expectConfigs: []string{"config1", "config2"},
		},
		{
			name: "watch receives multiple configs in single update",
			updateSequence: [][]*typesv1.Object{
				{createTestPbConfig("config1", "1", hostStatsType, config1Data)},
				{
					createTestPbConfig("config1", "2", hostStatsType, config1UpdatedData),
					createTestPbConfig("config2", "1", hostStatsType, config2Data),
				},
			},
			filters:       config.Filters{},
			expectedCount: 3,
			expectConfigs: []string{"config1", "config1", "config2"},
		},
		{
			name: "watch receives multiple sequential updates",
			updateSequence: [][]*typesv1.Object{
				{createTestPbConfig("config1", "1", hostStatsType, config1Data)},
				{createTestPbConfig("config2", "1", hostStatsType, config2Data)},
				{createTestPbConfig("config1", "2", hostStatsType, config1UpdatedData)},
			},
			filters:       config.Filters{},
			expectedCount: 3,
			expectConfigs: []string{"config1", "config2", "config1"},
		},
		{
			name: "watch receives updates with no initial configs",
			updateSequence: [][]*typesv1.Object{
				{createTestPbConfig("config1", "1", hostStatsType, config1Data)},
				{createTestPbConfig("config2", "1", hostStatsType, config2Data)},
			},
			filters:       config.Filters{},
			expectedCount: 2,
			expectConfigs: []string{"config1", "config2"},
		},
		{
			name: "watch receives invalid config update",
			updateSequence: [][]*typesv1.Object{
				{createTestPbConfig("config1", "1", hostStatsType, config1Data)},
				{createTestPbConfig("config1", "2", hostStatsType, []byte("invalid-data"))},
			},
			filters:       config.Filters{Status: config.StatusOK | config.StatusInvalid},
			expectedCount: 2,
			expectConfigs: []string{"config1", "config1"},
		},
		{
			name: "watch with type filter receives matching updates",
			updateSequence: [][]*typesv1.Object{
				{createTestPbConfig("config1", "1", hostStatsType, config1Data)},
				{
					createTestPbConfig("config1", "2", hostStatsType, config1UpdatedData),
					createTestPbConfig("config2", "1", "other.Type", config2Data),
				},
			},
			filters: config.Filters{
				Types: []string{hostStatsType},
			},
			expectedCount: 2,
			expectConfigs: []string{"config1", "config1"},
		},
		{
			name: "invalid version",
			updateSequence: [][]*typesv1.Object{
				{createTestPbConfig("config1", "2", hostStatsType, config1Data)},
				{createTestPbConfig("config1", "1", hostStatsType, config1UpdatedData)},
			},
			filters:       config.Filters{Status: config.StatusInvalid},
			expectedCount: 1,
			expectConfigs: []string{"config1"},
		},
		{
			name: "same version",
			updateSequence: [][]*typesv1.Object{
				{createTestPbConfig("config1", "1", hostStatsType, config1Data)},
				{createTestPbConfig("config1", "1", hostStatsType, config1UpdatedData)},
			},
			filters:       config.Filters{Status: config.StatusOK},
			expectedCount: 2,
			expectConfigs: []string{"config1", "config1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockService := mock.NewAgentManagementService(mock.AgentManagementServiceOptions{
				StreamFailures: 0,
				StreamDuration: 0,
				KeepAlive:      true,
			})

			conn, cleanup := createMockAMSServer(t, mockService)
			instance := &agentv1.Instance{Id: []byte("test-instance")}

			loader, err := config.NewAMSLoader(conn, config.WithInstance(instance))
			require.NoError(t, err)

			t.Cleanup(func() {
				loader.Close()
				cleanup()
			})

			watchCh := loader.Watch(config.Options{Filters: tt.filters})

			for i, updateBatch := range tt.updateSequence {
				if i > 0 {
					// Wait a bit before sending the next update
					time.Sleep(100 * time.Millisecond)
				}
				mockService.SendUpdates(updateBatch)
			}

			var receivedNames []string
			timeout := time.After(1 * time.Second)

		collectLoop:
			for {
				select {
				case cfg := <-watchCh:
					receivedNames = append(receivedNames, cfg.Name)
				case <-timeout:
					break collectLoop
				}
			}

			assert.GreaterOrEqual(t, len(receivedNames), tt.expectedCount, "Should receive at least expected number of configs")

			// For at-least-once delivery, verify we got the expected configs (allowing duplicates)
			expectedMap := make(map[string]int)
			for _, name := range tt.expectConfigs {
				expectedMap[name]++
			}

			receivedMap := make(map[string]int)
			for _, name := range receivedNames {
				receivedMap[name]++
			}

			// Verify we received at least the expected number of each config
			for expectedName, expectedCount := range expectedMap {
				assert.GreaterOrEqual(t, receivedMap[expectedName], expectedCount,
					"Should receive at least %d instances of config %s", expectedCount, expectedName)
			}
		})
	}
}

func TestAMSLoader_InitialConfigsOnStreamRecreation(t *testing.T) {
	hostStatsConfig1 := &agentv1.HostStatsCollectionConfig{Collector: "cpu", IntervalSeconds: 30}
	config1Data, err := proto.Marshal(hostStatsConfig1)
	require.NoError(t, err)

	hostStatsConfig2 := &agentv1.HostStatsCollectionConfig{Collector: "memory", IntervalSeconds: 30}
	config2Data, err := proto.Marshal(hostStatsConfig2)
	require.NoError(t, err)

	tests := []struct {
		name                       string
		initialConfigs             []*typesv1.Object
		updateConfigs              []*typesv1.Object
		maxStreamAge               time.Duration
		testDuration               time.Duration
		minExpectedStreamCreations int
		expectedInitialConfigs     [][]string
	}{
		{
			name: "initial configs sent on stream recreation",
			initialConfigs: []*typesv1.Object{
				createTestPbConfig("config1", "1", hostStatsType, config1Data),
				createTestPbConfig("config2", "1", hostStatsType, config2Data),
			},
			maxStreamAge:               300 * time.Millisecond,
			testDuration:               350 * time.Millisecond,
			minExpectedStreamCreations: 2,
			expectedInitialConfigs:     [][]string{{}, {"config1", "config2"}},
		},
		{
			name: "single config sent",
			initialConfigs: []*typesv1.Object{
				createTestPbConfig("config1", "1", hostStatsType, config1Data),
			},
			maxStreamAge:               250 * time.Millisecond,
			testDuration:               550 * time.Millisecond,
			minExpectedStreamCreations: 3,
			expectedInitialConfigs:     [][]string{{}, {"config1"}, {"config1"}},
		},
		{
			name:                       "no initial configs",
			initialConfigs:             []*typesv1.Object{},
			maxStreamAge:               400 * time.Millisecond,
			testDuration:               500 * time.Millisecond,
			minExpectedStreamCreations: 2,
			expectedInitialConfigs:     [][]string{{}, {}},
		},
		{
			name: "receive new config after stream recreation",
			initialConfigs: []*typesv1.Object{
				createTestPbConfig("config1", "1", hostStatsType, config1Data),
			},
			updateConfigs: []*typesv1.Object{
				createTestPbConfig("config2", "1", hostStatsType, config2Data),
			},
			maxStreamAge:               700 * time.Millisecond,
			testDuration:               750 * time.Millisecond,
			minExpectedStreamCreations: 2,
			expectedInitialConfigs:     [][]string{{}, {"config1", "config2"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockService := mock.NewAgentManagementService(mock.AgentManagementServiceOptions{
				StreamFailures: 0,
				StreamDuration: 0,
				KeepAlive:      true,
			})

			conn, cleanup := createMockAMSServer(t, mockService)
			instance := &agentv1.Instance{Id: []byte("test-instance")}

			loader, err := config.NewAMSLoader(conn,
				config.WithInstance(instance),
				config.WithMaxStreamAge(tt.maxStreamAge))
			require.NoError(t, err)

			t.Cleanup(func() {
				loader.Close()
				cleanup()
			})

			if len(tt.initialConfigs) > 0 {
				mockService.SendUpdates(tt.initialConfigs)
				// Wait for initial configs to be processed
				time.Sleep(200 * time.Millisecond)
			}

			// Send update configs
			if len(tt.updateConfigs) > 0 {
				mockService.SendUpdates(tt.updateConfigs)
				require.Eventually(t, func() bool {
					_, err := loader.GetConfig(hostStatsType, "config2")
					return err == nil
				}, 500*time.Millisecond, 50*time.Millisecond, "config2 should be cached after update")
			}

			// Let the loader run to trigger remaining stream recreations
			time.Sleep(tt.testDuration)

			attempts := mockService.GetStreamAttempts()
			assert.GreaterOrEqual(t, attempts, tt.minExpectedStreamCreations,
				"Should have created at least the minimum expected number of streams due to maxStreamAge")

			initialRequests := mockService.GetInitialConfigRequests()
			assert.GreaterOrEqual(t, len(initialRequests), tt.minExpectedStreamCreations,
				"Should have received initial config requests for at least the minimum expected number of stream creations")

			for i, expectedConfigs := range tt.expectedInitialConfigs {
				if i < len(initialRequests) {
					assert.ElementsMatch(t, expectedConfigs, initialRequests[i],
						"Initial configs for stream %d should match expected", i+1)
				}
			}

			// Verify configs are still accessible after stream recreations
			expectedFinalCount := len(tt.initialConfigs) + len(tt.updateConfigs)

			if expectedFinalCount > 0 {
				configs, err := loader.ListConfigs(config.Options{})
				require.NoError(t, err)

				totalConfigs := 0
				for _, instances := range configs {
					totalConfigs += len(instances)
				}
				assert.Equal(t, expectedFinalCount, totalConfigs,
					"All configs should remain accessible after stream recreations")
			}
		})
	}
}
