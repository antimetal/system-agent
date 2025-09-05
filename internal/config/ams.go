// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package config

import (
	"container/heap"
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
	headerAuthorize     = "authorization"
	defaultMaxStreamAge = 10 * time.Minute
)

type cacheEntry struct {
	Instance   Instance
	ttlHeapIdx int
}

type AMSLoader struct {
	apiKey       string
	client       agentsvcv1.AgentManagementServiceClient
	maxStreamAge time.Duration
	instance     *agentv1.Instance
	logger       logr.Logger

	// runtime fields
	wg         sync.WaitGroup
	subs       subscriptions
	cache      map[string]*cacheEntry
	cacheMu    sync.RWMutex
	ttlHeap    ttlHeap
	ttlHeapMu  sync.Mutex
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
		cache:        make(map[string]*cacheEntry),
	}
	heap.Init(&l.ttlHeap)

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

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.manageConfigLifecyle(ctx)
	}()

	return l, nil
}

func (l *AMSLoader) ListConfigs(opts Options) (map[string][]Instance, error) {
	configs := make(map[string][]Instance)

	l.cacheMu.RLock()
	defer l.cacheMu.RUnlock()

	for _, entry := range l.cache {
		if !Matches(entry.Instance, opts.Filters) {
			continue
		}

		configType := entry.Instance.TypeUrl
		configs[configType] = append(configs[configType], entry.Instance)
	}

	return configs, nil
}

func (l *AMSLoader) GetConfig(configType, name string) (Instance, error) {
	cacheKey := l.getCacheKey(configType, name)

	l.cacheMu.RLock()
	entry, exists := l.cache[cacheKey]
	l.cacheMu.RUnlock()

	if !exists {
		return Instance{}, fmt.Errorf("config %s of type %s not found", name, configType)
	}
	return entry.Instance, nil
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

func (l *AMSLoader) manageConfigLifecyle(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.ttlHeapMu.Lock()
			if l.ttlHeap.Len() == 0 {
				l.ttlHeapMu.Unlock()
				continue
			}

			now := time.Now()
			if !now.After(l.ttlHeap[0].ttl) {
				l.ttlHeapMu.Unlock()
				continue
			}

			// TTL expired, pop from heap
			node := heap.Pop(&l.ttlHeap).(*ttlEntry)
			l.ttlHeapMu.Unlock()

			l.cacheMu.Lock()
			if entry, exists := l.cache[node.cacheKey]; exists {
				// copy instance value so that cached entry can be garbage collected
				instance := entry.Instance
				instance.Expired = true
				delete(l.cache, node.cacheKey)
				l.subs.send(instance)
			}
			l.cacheMu.Unlock()
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
	for _, entry := range l.cache {
		obj := instanceToPbObject(entry.Instance)
		if entry.Instance.Status == StatusOK && obj != nil {
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

	prevEntry, configExists := l.cache[cacheKey]
	var prevInstance Instance
	if configExists {
		prevInstance = prevEntry.Instance
	}

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

	// Update the cache only if it's a new config or the new config is valid
	if parseErr != nil && prevInstance.Object != nil {
		return instance, parseErr
	}

	if configExists {
		prevEntry.Instance = instance
	} else {
		l.cache[cacheKey] = &cacheEntry{
			Instance:   instance,
			ttlHeapIdx: -1,
		}
	}

	var ttlDuration time.Duration
	if obj.GetTtl() != nil {
		ttlDuration = obj.GetTtl().AsDuration()
	}

	// Don't add to ttlIdx if new config object ttl is not set
	if ttlDuration <= 0 {
		if configExists {
			l.removeCfgFromTTLHeap(cacheKey)
		}
		return instance, parseErr
	}

	expiryTime := time.Now().Add(ttlDuration)

	if !configExists {
		l.addCfgToTTLHeap(cacheKey, expiryTime)
		return instance, parseErr
	}

	l.updateCfgInTTLHeap(cacheKey, expiryTime)

	return instance, parseErr
}

func (l *AMSLoader) getCacheKey(configType, name string) string {
	return configType + ":" + name
}

func (l *AMSLoader) addCfgToTTLHeap(name string, ttl time.Time) {
	l.ttlHeapMu.Lock()
	defer l.ttlHeapMu.Unlock()

	node := &ttlEntry{
		cacheKey: name,
		ttl:      ttl,
	}

	heap.Push(&l.ttlHeap, node)

	if entry, exists := l.cache[name]; exists {
		entry.ttlHeapIdx = node.index
	}
}

func (l *AMSLoader) updateCfgInTTLHeap(name string, ttl time.Time) {
	l.ttlHeapMu.Lock()
	defer l.ttlHeapMu.Unlock()

	if entry, exists := l.cache[name]; exists && entry.ttlHeapIdx >= 0 {
		l.ttlHeap[entry.ttlHeapIdx].ttl = ttl
		heap.Fix(&l.ttlHeap, entry.ttlHeapIdx)
	}
}

func (l *AMSLoader) removeCfgFromTTLHeap(name string) {
	l.ttlHeapMu.Lock()
	defer l.ttlHeapMu.Unlock()

	if entry, exists := l.cache[name]; exists && entry.ttlHeapIdx >= 0 {
		heap.Remove(&l.ttlHeap, entry.ttlHeapIdx)
		entry.ttlHeapIdx = -1
	}
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

type ttlEntry struct {
	cacheKey string
	ttl      time.Time
	index    int
}

type ttlHeap []*ttlEntry

func (h *ttlHeap) Len() int { return len(*h) }

func (h *ttlHeap) Less(i, j int) bool {
	return (*h)[i].ttl.Before((*h)[j].ttl)
}

func (h *ttlHeap) Swap(i, j int) {
	(*h)[i], (*h)[j] = (*h)[j], (*h)[i]
	(*h)[i].index = i
	(*h)[j].index = j
}

func (h *ttlHeap) Push(x any) {
	n := len(*h)
	node := x.(*ttlEntry)
	node.index = n
	*h = append(*h, node)
}

func (h *ttlHeap) Pop() any {
	old := *h
	n := len(old)
	node := old[n-1]
	old[n-1] = nil
	node.index = -1
	*h = old[0 : n-1]
	return node
}
