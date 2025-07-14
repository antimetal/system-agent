// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package instrumentor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	opampclient "github.com/open-telemetry/opamp-go/client"
	opamp "github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/protobufs"
	opamppb "github.com/open-telemetry/opamp-go/protobufs"
)

type Agent struct {
	mu sync.RWMutex

	instanceId  uuid.UUID
	url         string
	logger      logr.Logger
	opampClient opampclient.OpAMPClient

	status          Status
	effectiveConfig []byte
}

func NewAgent(ctx context.Context, id uuid.UUID, logger logr.Logger, url string) *Agent {
	agent := &Agent{
		instanceId: id,
		logger:     logger,
		url:        url,
	}
	return agent
}

func (a *Agent) Start(ctx context.Context) error {
	a.opampClient = opampclient.NewHTTP(&opampLogger{
		logger: a.logger,
	})

	hostname, err := os.Hostname()
	if err != nil {
		return err
	}

	err = a.opampClient.SetAgentDescription(&opamppb.AgentDescription{
		IdentifyingAttributes: []*opamppb.KeyValue{
			{
				Key: "service.name",
				Value: &opamppb.AnyValue{
					Value: &opamppb.AnyValue_StringValue{
						StringValue: "antimetal-agent",
					},
				},
			},
		},
		NonIdentifyingAttributes: []*opamppb.KeyValue{
			{
				Key: "os.type",
				Value: &opamppb.AnyValue{
					Value: &opamppb.AnyValue_StringValue{
						StringValue: runtime.GOOS,
					},
				},
			},
			{
				Key: "host.arch",
				Value: &opamppb.AnyValue{
					Value: &opamppb.AnyValue_StringValue{
						StringValue: runtime.GOARCH,
					},
				},
			},
			{
				Key: "host.name",
				Value: &opamppb.AnyValue{
					Value: &opamppb.AnyValue_StringValue{
						StringValue: hostname,
					},
				},
			},
		},
	})
	if err != nil {
		return err
	}

	settings := opamp.StartSettings{
		InstanceUid:    opamp.InstanceUid(a.instanceId),
		OpAMPServerURL: a.url,
		Capabilities: opamppb.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig |
			opamppb.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig |
			opamppb.AgentCapabilities_AgentCapabilities_ReportsEffectiveConfig |
			opamppb.AgentCapabilities_AgentCapabilities_ReportsOwnMetrics,
		Callbacks: opamp.Callbacks{
			OnConnect: func(_ context.Context) {
				a.logger.V(1).Info("connected to OpAMP server", "url", a.url)
			},
			OnConnectFailed: func(_ context.Context, err error) {
				a.logger.Error(err, "failed to connect to OpAMP server", "url", a.url)
			},
			OnError: func(_ context.Context, err *protobufs.ServerErrorResponse) {
				a.logger.Error(nil, "received error from OpAMP server", "url", a.url, "error", err.String())
			},
			OnMessage: a.onMessage,
			SaveRemoteConfigStatus: func(_ context.Context, status *opamppb.RemoteConfigStatus) {
				a.setStatus(status)
			},
		},
	}

	if err := a.opampClient.Start(ctx, settings); err != nil {
		a.logger.Error(err, "failed to start opamp client")
		return err
	}

	<-ctx.Done()
	a.logger.Info("shutting down instrumentor agent")
	if err := a.opampClient.Stop(context.Background()); err != nil {
		a.logger.Error(err, "failed to stop opamp client")
		return err
	}
	return nil
}

func (a *Agent) GetEffectiveConfig() []byte {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.effectiveConfig
}

func (a *Agent) Status() Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

func (a *Agent) onMessage(ctx context.Context, msg *opamp.MessageData) {
	var configChanged bool
	var err error
	if msg.RemoteConfig != nil {
		a.logger.V(1).Info("received new config from server")
		configChanged, err = a.updateConfig(msg.RemoteConfig)
		if err != nil {
			a.opampClient.SetRemoteConfigStatus(&opamppb.RemoteConfigStatus{
				LastRemoteConfigHash: msg.RemoteConfig.ConfigHash,
				Status:               opamppb.RemoteConfigStatuses_RemoteConfigStatuses_FAILED,
				ErrorMessage:         err.Error(),
			})
		} else {
			a.opampClient.SetRemoteConfigStatus(&opamppb.RemoteConfigStatus{
				LastRemoteConfigHash: msg.RemoteConfig.ConfigHash,
				Status:               opamppb.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
			})
		}
	}

	//TODO: Handle other msg fields as needed

	if configChanged {
		if err := a.opampClient.UpdateEffectiveConfig(ctx); err != nil {
			a.logger.Error(err, "failed to send updated effective config to server")
		}
	}
}

func (a *Agent) updateConfig(remoteConfig *opamppb.AgentRemoteConfig) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// TODO: Placeholder for actual config update logic/
	// Handle merges, validations, config changes, etc.
	a.effectiveConfig = nil
	if remoteConfig != nil {
		for _, file := range remoteConfig.GetConfig().GetConfigMap() {
			if file.GetBody() != nil {
				a.effectiveConfig = append(a.effectiveConfig, file.GetBody()...)
			}
		}
	}
	a.logger.V(1).Info("updated effective config", "config", json.RawMessage(a.effectiveConfig))
	return true, nil
}

func (a *Agent) setStatus(status *opamppb.RemoteConfigStatus) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch status.Status {
	case opamppb.RemoteConfigStatuses_RemoteConfigStatuses_UNSET:
		a.status = StatusUnset
	case opamppb.RemoteConfigStatuses_RemoteConfigStatuses_APPLYING:
		a.status = StatusApplying
	case opamppb.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED:
		a.status = StatusApplied
	case opamppb.RemoteConfigStatuses_RemoteConfigStatuses_FAILED:
		a.status = StatusFailed
	default:
		a.logger.Error(nil, "unknown status received", "status", status.Status)
	}
}

// opampLogger wrap logr.Logger to implement
// github.com/open-telemetry/opamp-go/client/types.Logger
type opampLogger struct {
	logger logr.Logger
}

func (l *opampLogger) Debugf(_ context.Context, format string, args ...any) {
	l.logger.V(1).Info(fmt.Sprintf(format, args...))
}

func (l *opampLogger) Errorf(_ context.Context, format string, args ...any) {
	l.logger.Error(nil, fmt.Sprintf(format, args...))
}
