// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package config

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/go-logr/logr"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/antimetal/agent/internal/runtime"
	agentv1 "github.com/antimetal/agent/pkg/api/antimetal/agent/v1"
	agentsvcv1 "github.com/antimetal/agent/pkg/api/antimetal/service/agent/v1"
	typesv1 "github.com/antimetal/agent/pkg/api/antimetal/types/v1"
)

const (
	headerAuthorize         = "authorization"
	defaultMaxStreamAge     = 10 * time.Minute
	streamRestartBackoffMax = 30 * time.Second
)

type AMSLoader struct {
	apiKey       string
	client       agentsvcv1.AgentManagementServiceClient
	maxStreamAge time.Duration
	instance     *agentv1.Instance
	logger       logr.Logger

	// runtime fields
	wg         sync.WaitGroup
	subs       subscriptions
	cache      map[string]Instance
	cacheMu    sync.RWMutex
	cancel     context.CancelFunc
	lastSeqNum string
	closeOnce  sync.Once
}

type AMSLoaderOpts func(*AMSLoader)

func WithAMSLogger(logger logr.Logger) AMSLoaderOpts {
	return func(l *AMSLoader) {
		l.logger = logger
	}
}

func WithAMSAPIKey(apiKey string) AMSLoaderOpts {
	return func(l *AMSLoader) {
		l.apiKey = apiKey
	}
}

func WithMaxStreamAge(maxAge time.Duration) AMSLoaderOpts {
	return func(l *AMSLoader) {
		l.maxStreamAge = maxAge
	}
}

func WithInstance(instance *agentv1.Instance) AMSLoaderOpts {
	return func(l *AMSLoader) {
		l.instance = instance
	}
}

func NewAMSLoader(conn *grpc.ClientConn, opts ...AMSLoaderOpts) (*AMSLoader, error) {
	if conn == nil {
		return nil, fmt.Errorf("conn cannot be nil")
	}

	l := &AMSLoader{
		client:       agentsvcv1.NewAgentManagementServiceClient(conn),
		maxStreamAge: defaultMaxStreamAge,
		cache:        make(map[string]Instance),
	}

	for _, opt := range opts {
		opt(l)
	}

	if l.instance == nil {
		instance := runtime.GetInstance()
		if instance == nil {
			return nil, fmt.Errorf("could not retrieve runtime info. You can supply this explicitly using the WithInstance option")
		}
		l.instance = instance
	}

	l.logger = l.logger.WithName("config.loader.ams")

	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.manageStream(ctx)
	}()

	return l, nil
}

func (l *AMSLoader) ListConfigs(opts Options) (map[string][]Instance, error) {
	configs := make(map[string][]Instance)

	l.cacheMu.RLock()
	defer l.cacheMu.RUnlock()

	for _, instance := range l.cache {
		if !Matches(instance, opts.Filters) {
			continue
		}

		configType := instance.TypeUrl
		configs[configType] = append(configs[configType], instance)
	}

	return configs, nil
}

func (l *AMSLoader) GetConfig(configType, name string) (Instance, error) {
	cacheKey := l.getCacheKey(configType, name)

	l.cacheMu.RLock()
	instance, exists := l.cache[cacheKey]
	l.cacheMu.RUnlock()

	if !exists {
		return Instance{}, fmt.Errorf("config %s of type %s not found", name, configType)
	}
	return instance, nil
}

func (l *AMSLoader) Watch(opts Options) <-chan Instance {
	ch := l.subs.add(opts.Filters)
	// ch will be nil if closed
	if ch == nil {
		return ch
	}

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()

		configs, err := l.ListConfigs(opts)
		if err != nil {
			l.logger.Error(err, "failed to get current configs for watch")
			return
		}
		for _, instances := range configs {
			for _, instance := range instances {
				ch <- instance
			}
		}
	}()

	return ch
}

func (l *AMSLoader) Close() error {
	l.closeOnce.Do(func() {
		l.cancel()
		l.wg.Wait()
		l.subs.close()
	})
	return nil
}

func (l *AMSLoader) manageStream(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			l.runStream(ctx)
		}
	}
}

func (l *AMSLoader) runStream(ctx context.Context) {
	var stream agentsvcv1.AgentManagementService_WatchConfigClient
	streamCtx, streamCancel := context.WithTimeout(ctx, l.maxStreamAge)
	defer streamCancel()

	if l.apiKey != "" {
		streamCtx = metadata.NewOutgoingContext(streamCtx, metadata.Pairs(
			headerAuthorize, fmt.Sprintf("bearer %s", l.apiKey),
		))
	}

	// Continuously try to create a new stream
	var err error
	for {
		_, err = backoff.Retry(ctx, func() (bool, error) {
			var retryErr error
			stream, retryErr = l.client.WatchConfig(streamCtx)
			if retryErr != nil {
				l.logger.Error(retryErr, "failed to create stream, retrying...")
				return false, retryErr
			}
			return true, nil
		}, backoff.WithBackOff(backoff.NewExponentialBackOff()))
		if err == nil {
			break
		}

		// Return if the root context is done since that means we're shutting down.
		// This prevents runStream from hanging if we're in here when the context
		// is cancelled.
		if ctx.Err() != nil {
			return
		}
	}

	defer func() {
		if stream != nil {
			// CloseSend always returns a nil error so no need to check it.
			_ = stream.CloseSend()
		}
	}()

	if err := l.sendInitialRequest(stream); err != nil {
		l.logger.Error(err, "failed to send initial request")
		return
	}

	l.recvMessages(stream)
}

func (l *AMSLoader) sendInitialRequest(stream agentsvcv1.AgentManagementService_WatchConfigClient) error {
	l.cacheMu.RLock()
	var initialConfigs []*typesv1.Object
	for _, instance := range l.cache {
		obj := instanceToPbObject(instance)
		if instance.Status == StatusOK && obj != nil {
			initialConfigs = append(initialConfigs, obj)
		}
	}
	l.cacheMu.RUnlock()

	req := &agentsvcv1.WatchConfigRequest{
		Instance:       l.instance,
		Type:           agentsvcv1.WatchConfigRequestType_WATCH_CONFIG_REQUEST_TYPE_INITIAL,
		InitialConfigs: initialConfigs,
	}

	l.logger.V(1).Info("sending initial config request", "instance_id", l.instance.Id, "num_initial_configs", len(initialConfigs))
	return stream.Send(req)
}

func (l *AMSLoader) recvMessages(stream agentsvcv1.AgentManagementService_WatchConfigClient) {
	for {
		resp, err := stream.Recv()
		if err != nil {
			l.logger.V(1).Info("stream ended", "reason", err)
			return
		}

		l.logger.V(1).Info("received config response", "seq_num", resp.SeqNum, "num_configs", len(resp.Configs))

		l.lastSeqNum = resp.SeqNum

		var errorDetails []proto.Message
		var errorMessage string

		for _, config := range resp.Configs {
			instance, err := l.processConfig(config)

			l.subs.send(instance)

			if err != nil {
				configError := &agentv1.ConfigError{
					ConfigRef: &typesv1.ObjectRef{
						Type: config.GetType(),
						Name: config.GetName(),
					},
					Reason: err.Error(),
				}
				errorDetails = append(errorDetails, configError)
				if errorMessage == "" {
					errorMessage = err.Error()
				}
			}
		}

		err = l.ack(stream, errorMessage, errorDetails)
		if err != nil {
			l.logger.V(1).Info("stream ended", "reason", err)
			return
		}
	}
}

func (l *AMSLoader) ack(
	stream agentsvcv1.AgentManagementService_WatchConfigClient,
	message string,
	errorDetails []proto.Message,
) error {
	req := &agentsvcv1.WatchConfigRequest{
		Instance:       l.instance,
		ResponseSeqNum: l.lastSeqNum,
	}

	if len(errorDetails) > 0 {
		req.Type = agentsvcv1.WatchConfigRequestType_WATCH_CONFIG_REQUEST_TYPE_NACK
		l.logger.V(1).Info("sending NACK", "seq_num", l.lastSeqNum)

		details := make([]*anypb.Any, 0, len(errorDetails))
		for _, e := range errorDetails {
			anyDetail, err := anypb.New(e)
			if err != nil {
				l.logger.Error(err, "failed to marshal status detail")
				continue
			}
			details = append(details, anyDetail)
		}

		errorStatus := &status.Status{
			Code:    int32(codes.InvalidArgument),
			Message: message,
			Details: details,
		}
		req.ErrorDetail = errorStatus
	} else {
		req.Type = agentsvcv1.WatchConfigRequestType_WATCH_CONFIG_REQUEST_TYPE_ACK
		l.logger.V(1).Info("sending ACK", "seq_num", l.lastSeqNum)
	}

	return stream.Send(req)
}

func (l *AMSLoader) processConfig(obj *typesv1.Object) (Instance, error) {
	instance, parseErr := Parse(obj)

	cacheKey := l.getCacheKey(instance.TypeUrl, instance.Name)
	l.cacheMu.Lock()
	defer l.cacheMu.Unlock()
	prevInstance := l.cache[cacheKey]

	compVer, err := CompareVersions(instance.Version, prevInstance.Version)
	if err != nil {
		instance.Status = StatusInvalid
		return instance, fmt.Errorf("could not parse version %s", instance.Version)
	}
	if compVer < 0 {
		instance.Status = StatusInvalid
		return instance, fmt.Errorf("version %s is less than the currently stored version %s",
			instance.Version, prevInstance.Version,
		)
	}
	if parseErr == nil || prevInstance.Object == nil {
		l.cache[cacheKey] = instance
	}

	return instance, parseErr
}

func (l *AMSLoader) getCacheKey(configType, name string) string {
	return configType + ":" + name
}

func instanceToPbObject(instance Instance) *typesv1.Object {
	if instance.Object == nil {
		return nil
	}

	data, err := proto.Marshal(instance.Object)
	if err != nil {
		return nil
	}

	return &typesv1.Object{
		Name:    instance.Name,
		Version: instance.Version,
		Type: &typesv1.TypeDescriptor{
			Type: instance.TypeUrl,
		},
		Data: data,
	}
}
