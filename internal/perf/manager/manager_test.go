// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build integration

package manager_test

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"

	"github.com/antimetal/agent/internal/config"
	"github.com/antimetal/agent/internal/metrics"
	"github.com/antimetal/agent/internal/metrics/consumers/debug"
	"github.com/antimetal/agent/internal/perf/manager"
	agentv1 "github.com/antimetal/agent/pkg/api/antimetal/agent/v1"
)

func TestManager(t *testing.T) {
	t.Parallel()

	logger := testr.New(t)

	// Create temporary directory for test configs
	tempDir := t.TempDir()

	// Create debug consumer
	debugConfig := debug.DefaultConfig()
	debugConsumer, err := debug.NewConsumer(debugConfig, logger)
	require.NoError(t, err)

	// Create metrics router
	router := metrics.NewMetricsRouter(logger)
	err = router.RegisterConsumer(debugConsumer)
	require.NoError(t, err)

	// Create FSLoader
	fsLoader, err := config.NewFSLoader(tempDir, logger)
	require.NoError(t, err)
	t.Cleanup(func() { fsLoader.Close() })

	// Create manager
	mgr, err := manager.New(
		fsLoader,
		router,
		manager.WithLogger(logger),
	)
	require.NoError(t, err)

	// Start manager
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	managerDone := make(chan error, 1)
	go func() {
		managerDone <- mgr.Start(ctx)
	}()

	// Verify no collectors initially
	collectors := mgr.GetRunningCollectors()
	require.Empty(t, collectors)

	// Create initial config with version v1
	hostStatsConfig1 := &agentv1.HostStatsCollectionConfig{
		Collector:       "memory",
		IntervalSeconds: 1,
	}

	configContent1, err := createYAMLConfig("memory-collector", "v1", hostStatsConfig1)
	require.NoError(t, err)

	configFile := filepath.Join(tempDir, "memory-collector.yaml")
	err = os.WriteFile(configFile, configContent1, 0644)
	require.NoError(t, err)

	// Wait for config to be processed
	time.Sleep(1 * time.Second)

	// Verify collector is running with version v1
	collectors = mgr.GetRunningCollectors()
	require.Len(t, collectors, 1, "Expected 1 collector to be running, got: %v", collectors)
	require.Contains(t, collectors, "memory-collector")
	require.Equal(t, "v1", collectors["memory-collector"])

	// Verify debug consumer is receiving events from the collector
	time.Sleep(3 * time.Second)
	health := debugConsumer.Health()
	require.Greater(t, health.EventsCount, uint64(0), "Debug consumer should have received events from the collector")

	// Update config with same version (should be ignored)
	err = os.WriteFile(configFile, configContent1, 0644)
	require.NoError(t, err)

	// Wait a bit
	time.Sleep(1 * time.Second)

	// Should still be the same collector with same version
	collectors = mgr.GetRunningCollectors()
	require.Len(t, collectors, 1)
	require.Contains(t, collectors, "memory-collector")
	require.Equal(t, "v1", collectors["memory-collector"])

	// Update config with new version v2 (should restart collector)
	hostStatsConfig2 := &agentv1.HostStatsCollectionConfig{
		Collector:       "memory",
		IntervalSeconds: 15,
	}

	configContent2, err := createYAMLConfig("memory-collector", "v2", hostStatsConfig2)
	require.NoError(t, err)

	err = os.WriteFile(configFile, configContent2, 0644)
	require.NoError(t, err)

	// Wait for update to be processed
	time.Sleep(1 * time.Second)

	// Collector should still be running with new version v2
	collectors = mgr.GetRunningCollectors()
	require.Len(t, collectors, 1, "Expected 1 collector after update, got: %v", collectors)
	require.Contains(t, collectors, "memory-collector")
	require.Equal(t, "v2", collectors["memory-collector"])

	// Remove config file (should expire collector)
	err = os.Remove(configFile)
	require.NoError(t, err)

	// Wait for expiration
	time.Sleep(1 * time.Second)

	// Collector should be removed
	collectors = mgr.GetRunningCollectors()
	require.Empty(t, collectors)

	// Cancel and wait for manager to stop
	cancel()
	select {
	case err := <-managerDone:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("manager did not stop within timeout")
	}
}

// createYAMLConfig creates a YAML config file content with the given name, version and protobuf data
func createYAMLConfig(name string, version string, msg proto.Message) ([]byte, error) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}

	encodedData := base64.StdEncoding.EncodeToString(data)
	typeName := string(proto.MessageName(msg))

	configData := map[string]any{
		"type": map[string]string{
			"type": typeName,
		},
		"name":    name,
		"version": version,
		"data":    encodedData,
	}

	return yaml.Marshal(configData)
}
