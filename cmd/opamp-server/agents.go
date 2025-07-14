// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package main

import (
	"bytes"
	"crypto/sha256"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	opamppb "github.com/open-telemetry/opamp-go/protobufs"
	"github.com/open-telemetry/opamp-go/protobufshelpers"
	opamp "github.com/open-telemetry/opamp-go/server/types"
)

type InstanceId uuid.UUID

// Agent represents a connected Agent.
type Agent struct {
	// Some fields in this struct are exported so that we can render them in the UI.

	// Agent's instance id. This is an immutable field.
	InstanceId InstanceId

	// Connection to the Agent.
	conn opamp.Connection

	// mutex for the fields that follow it.
	mux sync.RWMutex

	// Agent's current status.
	Status *opamppb.AgentToServer

	// The time when the agent has started. Valid only if Status.Health.Up==true
	StartedAt time.Time

	// Effective config reported by the Agent.
	EffectiveConfig string

	// Optional special remote config for this particular instance defined by
	// the user in the UI.
	CustomInstanceConfig string

	// Remote config that we will give to this Agent.
	remoteConfig *opamppb.AgentRemoteConfig

	// Sequence number of the last status report received from the Agent.
	sequenceNum uint
}

func NewAgent(
	instanceId InstanceId,
	conn opamp.Connection,
) *Agent {
	return &Agent{
		InstanceId: instanceId,
		conn:       conn,
	}
}

// CloneReadonly returns a copy of the Agent that is safe to read.
// Functions that modify the Agent should not be called on the cloned copy.
func (agent *Agent) CloneReadonly() *Agent {
	agent.mux.RLock()
	defer agent.mux.RUnlock()
	return &Agent{
		InstanceId:           agent.InstanceId,
		Status:               proto.Clone(agent.Status).(*opamppb.AgentToServer),
		EffectiveConfig:      agent.EffectiveConfig,
		CustomInstanceConfig: agent.CustomInstanceConfig,
		remoteConfig:         proto.Clone(agent.remoteConfig).(*opamppb.AgentRemoteConfig),
		StartedAt:            agent.StartedAt,
	}
}

// UpdateStatus updates the status of the Agent struct based on the newly received
// status report and sets appropriate fields in the response message to be sent
// to the Agent.
func (agent *Agent) UpdateStatus(
	statusMsg *opamppb.AgentToServer,
	response *opamppb.ServerToAgent,
) {
	agent.mux.Lock()

	agent.processStatusUpdate(statusMsg, response)

	agent.mux.Unlock()
}

// SetCustomConfig sets a custom config for this Agent.
// If the provided config is equal to the current remoteConfig of the Agent
// then the method will return false, meaning that the config was not changed,
// otherwise it will return true, meaning that the config was changed and
// the Agent should be notified about the change.
func (agent *Agent) SetCustomConfig(config *opamppb.AgentConfigMap) bool {
	agent.mux.Lock()
	defer agent.mux.Unlock()

	agent.CustomInstanceConfig = string(config.ConfigMap[""].Body)
	return agent.calcRemoteConfig()
}

func (agent *Agent) SequenceNum() uint {
	agent.mux.RLock()
	defer agent.mux.RUnlock()
	return agent.sequenceNum
}

func (agent *Agent) SyncWithRemote(response *opamppb.ServerToAgent) {
	if agent.remoteConfig != nil {
		response.RemoteConfig = agent.remoteConfig
	}
}

func (agent *Agent) updateAgentDescription(newStatus *opamppb.AgentToServer) (agentDescrChanged bool) {
	prevStatus := agent.Status

	if agent.Status == nil {
		// First time this Agent reports a status, remember it.
		agent.Status = newStatus
		agentDescrChanged = true
	} else {
		// Not a new Agent. Update the Status.
		agent.Status.SequenceNum = newStatus.SequenceNum

		// Check what's changed in the AgentDescription.
		if newStatus.AgentDescription != nil {
			// If the AgentDescription field is set it means the Agent tells us
			// something is changed in the field since the last status report
			// (or this is the first report).
			// Make full comparison of previous and new descriptions to see if it
			// really is different.
			if prevStatus != nil && proto.Equal(prevStatus.AgentDescription, newStatus.AgentDescription) {
				// Agent description didn't change.
				agentDescrChanged = false
			} else {
				// Yes, the description is different, update it.
				agent.Status.AgentDescription = newStatus.AgentDescription
				agentDescrChanged = true
			}
		} else {
			// AgentDescription field is not set, which means description didn't change.
			agentDescrChanged = false
		}

		// Update remote config status if it is included and is different from what we have.
		if newStatus.RemoteConfigStatus != nil &&
			!proto.Equal(agent.Status.RemoteConfigStatus, newStatus.RemoteConfigStatus) {
			agent.Status.RemoteConfigStatus = newStatus.RemoteConfigStatus
		}
	}
	return agentDescrChanged
}

func (agent *Agent) updateHealth(newStatus *opamppb.AgentToServer) {
	if newStatus.Health == nil {
		return
	}

	agent.Status.Health = newStatus.Health

	if agent.Status != nil && agent.Status.Health != nil && agent.Status.Health.Healthy {
		agent.StartedAt = time.Unix(0, int64(agent.Status.Health.StartTimeUnixNano)).UTC()
	}
}

func (agent *Agent) updateRemoteConfigStatus(newStatus *opamppb.AgentToServer) {
	// Update remote config status if it is included and is different from what we have.
	if newStatus.RemoteConfigStatus != nil {
		agent.Status.RemoteConfigStatus = newStatus.RemoteConfigStatus
	}
}

func (agent *Agent) updateStatusField(newStatus *opamppb.AgentToServer) (agentDescrChanged bool) {
	if agent.Status == nil {
		// First time this Agent reports a status, remember it.
		agent.Status = newStatus
		agentDescrChanged = true
	}

	agentDescrChanged = agent.updateAgentDescription(newStatus) || agentDescrChanged
	agent.updateRemoteConfigStatus(newStatus)
	agent.updateHealth(newStatus)

	return agentDescrChanged
}

func (agent *Agent) updateEffectiveConfig(newStatus *opamppb.AgentToServer) {
	// Update effective config if provided.
	if newStatus.EffectiveConfig != nil {
		if newStatus.EffectiveConfig.ConfigMap != nil {
			agent.Status.EffectiveConfig = newStatus.EffectiveConfig

			// Convert to string for displaying purposes.
			agent.EffectiveConfig = ""
			for _, cfg := range newStatus.EffectiveConfig.ConfigMap.ConfigMap {
				// TODO: we just concatenate parts of effective config as a single
				// blob to show in the UI. A proper approach is to keep the effective
				// config as a set and show the set in the UI.
				agent.EffectiveConfig = agent.EffectiveConfig + string(cfg.Body)
			}
		}
	}
}

func (agent *Agent) hasCapability(capability opamppb.AgentCapabilities) bool {
	return agent.Status.Capabilities&uint64(capability) != 0
}

func (agent *Agent) processStatusUpdate(
	newStatus *opamppb.AgentToServer,
	response *opamppb.ServerToAgent,
) {
	// We don't have any status for this Agent, or we lost the previous status update from the Agent, so our
	// current status is not up-to-date.
	lostPreviousUpdate := (agent.Status == nil) || (agent.Status != nil && agent.Status.SequenceNum+1 != newStatus.SequenceNum)

	agentDescrChanged := agent.updateStatusField(newStatus)

	// Check if any fields were omitted in the status report.
	effectiveConfigOmitted := newStatus.EffectiveConfig == nil &&
		agent.hasCapability(opamppb.AgentCapabilities_AgentCapabilities_ReportsEffectiveConfig)

	packageStatusesOmitted := newStatus.PackageStatuses == nil &&
		agent.hasCapability(opamppb.AgentCapabilities_AgentCapabilities_ReportsPackageStatuses)

	remoteConfigStatusOmitted := newStatus.RemoteConfigStatus == nil &&
		agent.hasCapability(opamppb.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig)

	healthOmitted := newStatus.Health == nil &&
		agent.hasCapability(opamppb.AgentCapabilities_AgentCapabilities_ReportsHealth)

	// True if the status was not fully reported.
	statusIsCompressed := effectiveConfigOmitted || packageStatusesOmitted || remoteConfigStatusOmitted || healthOmitted

	if statusIsCompressed && lostPreviousUpdate {
		// The status message is not fully set in the message that we received, but we lost the previous
		// status update. Request full status update from the agent.
		response.Flags |= uint64(opamppb.ServerToAgentFlags_ServerToAgentFlags_ReportFullState)
	}

	configChanged := false
	if agentDescrChanged {
		// Agent description is changed.

		// We need to recalculate the config.
		configChanged = agent.calcRemoteConfig()
	}

	// If remote config is changed and different from what the Agent has then
	// send the new remote config to the Agent.
	if configChanged ||
		(agent.Status.RemoteConfigStatus != nil &&
			bytes.Compare(agent.Status.RemoteConfigStatus.LastRemoteConfigHash, agent.remoteConfig.ConfigHash) != 0) {
		// The new status resulted in a change in the config of the Agent or the Agent
		// does not have this config (hash is different). Send the new config the Agent.
		response.RemoteConfig = agent.remoteConfig
	}

	agent.updateEffectiveConfig(newStatus)
	agent.sequenceNum = agent.sequenceNum + 1
}

// calcRemoteConfig calculates the remote config for this Agent. It returns true if
// the calculated new config is different from the existing config stored in
// Agent.remoteConfig.
func (agent *Agent) calcRemoteConfig() bool {
	hash := sha256.New()

	cfg := opamppb.AgentRemoteConfig{
		Config: &opamppb.AgentConfigMap{
			ConfigMap: map[string]*opamppb.AgentConfigFile{},
		},
	}

	// Add the custom config for this particular Agent instance. Use empty
	// string as the config file name.
	cfg.Config.ConfigMap[""] = &opamppb.AgentConfigFile{
		Body: []byte(agent.CustomInstanceConfig),
	}

	// Calculate the hash.
	for k, v := range cfg.Config.ConfigMap {
		hash.Write([]byte(k))
		hash.Write(v.Body)
		hash.Write([]byte(v.ContentType))
	}

	cfg.ConfigHash = hash.Sum(nil)

	configChanged := !isEqualRemoteConfig(agent.remoteConfig, &cfg)

	agent.remoteConfig = &cfg

	return configChanged
}

var DefaultAgentRegistry = &AgentRegistry{
	agentsById:  make(map[InstanceId]*Agent),
	connections: make(map[opamp.Connection]map[InstanceId]bool),
}

type AgentRegistry struct {
	mux         sync.RWMutex
	agentsById  map[InstanceId]*Agent
	connections map[opamp.Connection]map[InstanceId]bool
}

// RemoveConnection removes the connection all Agent instances associated with the
// connection.
func (r *AgentRegistry) RemoveConnection(conn opamp.Connection) {
	r.mux.Lock()
	defer r.mux.Unlock()

	for instanceId := range r.connections[conn] {
		delete(r.agentsById, instanceId)
	}
	delete(r.connections, conn)
}

// SetCustomConfigForAgent sets a custom config for the Agent with the given
// instance ID.
//
// If the provided config is equal to the current remoteConfig of
// the Agent, then the method will return false, meaning that the config was not
// changed.
//
// If the provided config is not equal to the current remoteConfig of the Agent,
// then the method will return true, meaning that the config was changed and
// the Agent should be notified about the change.
//
// If the Agent with the given instance ID is not found, the method will return
// false.
func (r *AgentRegistry) SetCustomConfigForAgent(
	agentId InstanceId,
	config *opamppb.AgentConfigMap,
) (configChanged bool) {
	agent := r.FindAgent(agentId)
	if agent != nil {
		configChanged = agent.SetCustomConfig(config)
	}
	return
}

func (r *AgentRegistry) ForEachAgent(fn func(agent *Agent)) {
	r.mux.RLock()
	defer r.mux.RUnlock()

	for _, agent := range r.agentsById {
		fn(agent)
	}
}

func (r *AgentRegistry) FindAgent(agentId InstanceId) *Agent {
	r.mux.RLock()
	defer r.mux.RUnlock()
	return r.agentsById[agentId]
}

func (r *AgentRegistry) FindOrCreateAgent(agentId InstanceId, conn opamp.Connection) *Agent {
	r.mux.Lock()
	defer r.mux.Unlock()

	// Ensure the Agent is in the agentsById map.
	agent := r.agentsById[agentId]
	if agent == nil {
		agent = NewAgent(agentId, conn)
		r.agentsById[agentId] = agent

		// Ensure the Agent's instance id is associated with the connection.
		if r.connections[conn] == nil {
			r.connections[conn] = map[InstanceId]bool{}
		}
		r.connections[conn][agentId] = true
	}

	return agent
}

func (r *AgentRegistry) GetAgentReadonlyClone(agentId InstanceId) *Agent {
	agent := r.FindAgent(agentId)
	if agent == nil {
		return nil
	}

	// Return a clone to allow safe access after returning.
	return agent.CloneReadonly()
}

func (r *AgentRegistry) GetAllAgentsReadonlyClone() map[InstanceId]*Agent {
	r.mux.RLock()

	// Clone the map first
	m := map[InstanceId]*Agent{}
	for id, agent := range r.agentsById {
		m[id] = agent
	}
	r.mux.RUnlock()

	// Clone agents in the map
	for id, agent := range m {
		// Return a clone to allow safe access after returning.
		m[id] = agent.CloneReadonly()
	}
	return m
}

func isEqualRemoteConfig(c1, c2 *opamppb.AgentRemoteConfig) bool {
	if c1 == c2 {
		return true
	}
	if c1 == nil || c2 == nil {
		return false
	}
	return isEqualConfigSet(c1.Config, c2.Config)
}

func isEqualConfigSet(c1, c2 *opamppb.AgentConfigMap) bool {
	if c1 == c2 {
		return true
	}
	if c1 == nil || c2 == nil {
		return false
	}
	if len(c1.ConfigMap) != len(c2.ConfigMap) {
		return false
	}
	for k, v1 := range c1.ConfigMap {
		v2, ok := c2.ConfigMap[k]
		if !ok {
			return false
		}
		if !isEqualConfigFile(v1, v2) {
			return false
		}
	}
	return true
}

func isEqualConfigFile(f1, f2 *opamppb.AgentConfigFile) bool {
	if f1 == f2 {
		return true
	}
	if f1 == nil || f2 == nil {
		return false
	}
	return bytes.Compare(f1.Body, f2.Body) == 0 && f1.ContentType == f2.ContentType
}
func isEqualAgentDescr(d1, d2 *opamppb.AgentDescription) bool {
	if d1 == d2 {
		return true
	}
	if d1 == nil || d2 == nil {
		return false
	}
	return isEqualAttrs(d1.IdentifyingAttributes, d2.IdentifyingAttributes) &&
		isEqualAttrs(d1.NonIdentifyingAttributes, d2.NonIdentifyingAttributes)
}

func isEqualAttrs(attrs1, attrs2 []*opamppb.KeyValue) bool {
	if len(attrs1) != len(attrs2) {
		return false
	}
	for i, a1 := range attrs1 {
		a2 := attrs2[i]
		if !protobufshelpers.IsEqualKeyValue(a1, a2) {
			return false
		}
	}
	return true
}
