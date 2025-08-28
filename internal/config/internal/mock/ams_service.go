// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package mock

import (
	"errors"
	"fmt"
	"sync"
	"time"

	agentsvcv1 "github.com/antimetal/agent/pkg/api/antimetal/service/agent/v1"
	typesv1 "github.com/antimetal/agent/pkg/api/antimetal/types/v1"
)

type AgentManagementServiceOptions struct {
	StreamFailures int
	StreamDuration time.Duration
	KeepAlive      bool
}

// NewAgentManagementService creates a new mock AMS service with the given options
func NewAgentManagementService(opts AgentManagementServiceOptions) *AgentManagementService {
	svc := &AgentManagementService{
		streamFailures: opts.StreamFailures,
		streamDuration: opts.StreamDuration,
		keepAlive:      opts.KeepAlive,
		updateCh:       make(chan []*typesv1.Object, 10),
	}
	return svc
}

// AgentManagementService mock
type AgentManagementService struct {
	agentsvcv1.UnimplementedAgentManagementServiceServer

	mu                    sync.Mutex
	streamFailures        int
	currentAttempt        int
	streamDuration        time.Duration
	keepAlive             bool
	updateCh              chan []*typesv1.Object
	seqNum                int
	initialConfigRequests [][]string
}

// SendUpdates sends config updates through the mock service
func (m *AgentManagementService) SendUpdates(configs []*typesv1.Object) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.updateCh != nil {
		m.updateCh <- configs
	}
}

// GetStreamAttempts get the current number of WatchConfig stream creations
func (m *AgentManagementService) GetStreamAttempts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentAttempt
}

// GetInitialConfigRequests returns a copy of all the initial configs sent
// for each INITIAL request received.
func (m *AgentManagementService) GetInitialConfigRequests() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	initialRequests := make([][]string, len(m.initialConfigRequests))
	copy(initialRequests, m.initialConfigRequests)
	return initialRequests
}

// WatchConfig RPC
func (m *AgentManagementService) WatchConfig(stream agentsvcv1.AgentManagementService_WatchConfigServer) error {
	m.mu.Lock()
	m.currentAttempt++
	m.mu.Unlock()

	req, err := stream.Recv()
	if err != nil {
		return err
	}

	if req.Type == agentsvcv1.WatchConfigRequestType_WATCH_CONFIG_REQUEST_TYPE_INITIAL {
		m.mu.Lock()
		var configNames []string
		for _, config := range req.InitialConfigs {
			configNames = append(configNames, config.GetName())
		}
		m.initialConfigRequests = append(m.initialConfigRequests, configNames)
		m.mu.Unlock()
	}

	if m.currentAttempt <= m.streamFailures {
		return errors.New("simulated stream failure")
	}

	if !m.keepAlive {
		// Simulate stream running for specified duration then ending
		time.Sleep(m.streamDuration)
		return nil
	}

	for {
		m.mu.Lock()
		updateCh := m.updateCh
		m.mu.Unlock()

		if updateCh != nil {
			select {
			case <-stream.Context().Done():
				return stream.Context().Err()
			case updates := <-updateCh:
				if updates != nil {
					m.mu.Lock()
					m.seqNum++
					seqNum := fmt.Sprintf("%d", m.seqNum)
					m.mu.Unlock()

					resp := &agentsvcv1.WatchConfigResponse{
						SeqNum:  seqNum,
						Configs: updates,
					}

					if err := stream.Send(resp); err != nil {
						return err
					}

					_, err = stream.Recv()
					if err != nil {
						return err
					}
				}
			}
		} else {
			select {
			case <-stream.Context().Done():
				return stream.Context().Err()
			default:
				// No update channel available, just wait a bit
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
}
