// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	opamppb "github.com/open-telemetry/opamp-go/protobufs"
	opampserver "github.com/open-telemetry/opamp-go/server"
	opamp "github.com/open-telemetry/opamp-go/server/types"
)

type SampleConfig struct {
	SequenceNum uint     `json:"sequenceNum"`
	Collectors  []string `json:"collectors"`
}

type Server struct {
	opampSrv opampserver.OpAMPServer
	registry *AgentRegistry
	started  bool
}

func NewServer(registry *AgentRegistry) *Server {
	return &Server{
		registry: registry,
	}
}

func (s *Server) Start(listenAddr string) error {
	callbacks := opamp.Callbacks{
		OnConnecting: func(request *http.Request) opamp.ConnectionResponse {
			slog.Debug("Agent connecting", "remoteAddr", request.RemoteAddr)
			return opamp.ConnectionResponse{
				Accept: true,
				ConnectionCallbacks: opamp.ConnectionCallbacks{
					OnMessage: s.onMessage,
					OnConnectionClose: func(conn opamp.Connection) {
						slog.Debug("Agent disconnected", "remoteAddr", request.RemoteAddr)
					},
				},
			}
		},
	}

	settings := opampserver.StartSettings{
		ListenEndpoint: listenAddr,
		Settings: opampserver.Settings{
			Callbacks: callbacks,
		},
	}

	opampSrv := opampserver.New(&Logger{logger: slog.Default()})
	if err := opampSrv.Start(settings); err != nil {
		return fmt.Errorf("failed to start OpAMP server: %w", err)
	}

	s.opampSrv = opampSrv
	slog.Info("OpAMP server started", "address", listenAddr)

	s.started = true
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	if !s.started {
		return fmt.Errorf("server not started")
	}
	if err := s.opampSrv.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop OpAMP server: %w", err)
	}
	return nil
}

func (s *Server) onMessage(ctx context.Context, conn opamp.Connection, msg *opamppb.AgentToServer) *opamppb.ServerToAgent {
	// Start building the response.
	response := &opamppb.ServerToAgent{}

	if len(msg.InstanceUid) != 16 {
		slog.ErrorContext(ctx, "Invalid length of msg.InstanceUid")
		return response
	}
	instanceId := InstanceId(msg.InstanceUid)

	agent := s.registry.FindOrCreateAgent(instanceId, conn)
	// Process the status report and continue building the response.
	agent.UpdateStatus(msg, response)
	// // Send new config after initial status update.
	if agent.SequenceNum() > 0 && agent.SequenceNum()%2 == 0 {
		config := s.createSampleConfig(agent.SequenceNum())
		changed := agent.SetCustomConfig(config)
		if changed {
			slog.Debug("Sending new config to agent", "instanceUid", instanceId, "sequenceNum", agent.SequenceNum())
			agent.SyncWithRemote(response)
		} else {
			slog.Debug("No config change for agent", "instanceUid", instanceId, "sequenceNum", agent.SequenceNum())
		}
	}
	return response
}

func (s *Server) createSampleConfig(seqNum uint) *opamppb.AgentConfigMap {
	// Create a sample configuration that the instrumentor agent can process
	config := SampleConfig{
		SequenceNum: seqNum,
		Collectors:  []string{"load"},
	}
	configJson, err := json.Marshal(config)
	if err != nil {
		// Should never happen
		panic(fmt.Sprintf("Failed to marshal sample config: %v", err))
	}

	return &opamppb.AgentConfigMap{
		ConfigMap: map[string]*opamppb.AgentConfigFile{
			"": {
				Body:        []byte(configJson),
				ContentType: "application/json",
			},
		},
	}
}

// Logger implements the OpAMP server logger interface that wraps slog.
type Logger struct {
	logger *slog.Logger
}

func (l *Logger) Debugf(ctx context.Context, format string, args ...any) {
	l.logger.DebugContext(ctx, fmt.Sprintf(format, args...))
}

func (l *Logger) Errorf(ctx context.Context, format string, args ...any) {
	l.logger.ErrorContext(ctx, fmt.Sprintf(format, args...))
}
