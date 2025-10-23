// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package config

import (
	"fmt"
	"strconv"
	"strings"

	agentv1 "github.com/antimetal/agent/pkg/api/antimetal/agent/v1"
	typesv1 "github.com/antimetal/agent/pkg/api/antimetal/types/v1"
	"github.com/antimetal/agent/pkg/performance"
	"google.golang.org/protobuf/proto"
)

type configParser func(obj *typesv1.Object) (proto.Message, error)

var (
	configParsers = map[string]configParser{}

	hostStatsCollectionConfigName = string((&agentv1.HostStatsCollectionConfig{}).ProtoReflect().Descriptor().FullName())
	profileCollectionConfigName   = string((&agentv1.ProfileCollectionConfig{}).ProtoReflect().Descriptor().FullName())
)

func init() {
	configParsers[hostStatsCollectionConfigName] = parseHostStatsCollectionConfig
	configParsers[profileCollectionConfigName] = parseProfileCollectionConfig
}

// Parse a config object into an Instance.
func Parse(obj *typesv1.Object) (Instance, error) {
	if obj == nil {
		return Instance{Status: StatusInvalid}, fmt.Errorf("object is nil")
	}

	typeDesc := obj.GetType()
	if typeDesc == nil {
		return Instance{Status: StatusInvalid}, fmt.Errorf("object type is nil")
	}

	instance := Instance{
		TypeUrl: typeDesc.GetType(),
		Name:    obj.GetName(),
		Version: obj.GetVersion(),
	}

	if instance.TypeUrl == "" {
		instance.Status = StatusInvalid
		return instance, fmt.Errorf("object type.type is empty")
	}

	if instance.Name == "" {
		instance.Status = StatusInvalid
		return instance, fmt.Errorf("object name is empty")
	}

	data := obj.GetData()
	if data == nil {
		instance.Status = StatusInvalid
		return instance, fmt.Errorf("object data is empty")
	}

	parser, exists := configParsers[instance.TypeUrl]
	if !exists {
		instance.Status = StatusInvalid
		return instance, fmt.Errorf("unrecognized type: %s", typeDesc.GetType())
	}

	pbMsg, err := parser(obj)
	if err != nil {
		instance.Status = StatusInvalid
		return instance, fmt.Errorf("failed to parse config: %w", err)
	}

	instance.Object = pbMsg
	instance.Status = StatusOK

	return instance, nil
}

// CompareVersions compares two version strings.
// Returns:
//   - negative if current < prev
//   - zero if current == prev
//   - positive if current > prev
//   - positive if current is non-empty and prev is empty
//
// The return int is undefined if there is an error.
func CompareVersions(current, prev string) (int, error) {
	// Remove 'v' prefix if present
	current = strings.TrimPrefix(current, "v")
	prev = strings.TrimPrefix(prev, "v")

	currentNum, err := strconv.Atoi(current)
	if err != nil {
		return 0, fmt.Errorf("invalid version %s: %w", current, err)
	}

	if prev == "" {
		return 1, nil
	}

	prevNum, err := strconv.Atoi(prev)
	if err != nil {
		return 0, fmt.Errorf("invalid version %s: %w", prev, err)
	}

	if currentNum < 0 || prevNum < 0 {
		return 0, fmt.Errorf("version numbers cannot be negative")
	}

	if currentNum < prevNum {
		return -1, nil
	}
	if currentNum > prevNum {
		return 1, nil
	}
	return 0, nil
}

func parseHostStatsCollectionConfig(obj *typesv1.Object) (proto.Message, error) {
	config := &agentv1.HostStatsCollectionConfig{}
	if err := proto.Unmarshal(obj.GetData(), config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal: %w", err)
	}

	collectorName := config.GetCollector()
	if collectorName == "" {
		return config, fmt.Errorf("collector name is empty")
	}

	metricType := performance.MetricType(collectorName)
	available, reason := performance.GetCollectorStatus(metricType)
	if !available {
		return config, fmt.Errorf("collector %s is not available: %s", collectorName, reason)
	}

	return config, nil
}

func parseProfileCollectionConfig(obj *typesv1.Object) (proto.Message, error) {
	config := &agentv1.ProfileCollectionConfig{}
	if err := proto.Unmarshal(obj.GetData(), config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal: %w", err)
	}

	// Validate required field
	if config.GetEventName() == "" {
		return config, fmt.Errorf("event_name is required")
	}

	// Note: We don't check GetCollectorStatus here because capability checks
	// are runtime-dependent (pod security context). The profiler will fail
	// gracefully at startup if capabilities are missing.

	return config, nil
}
