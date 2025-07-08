// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package intake

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v5"
	"k8s.io/client-go/util/workqueue"

	"github.com/antimetal/agent/pkg/resource"
	resourcev1 "github.com/antimetal/apis/gengo/resource/v1"
	intakev1 "github.com/antimetal/apis/gengo/service/resource/v1"
	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	workerName          = "intake-worker"
	headerAuthorize     = "authorization"
	defaultDeltaTTL     = 5 * time.Minute
	heartbeatInterval   = 1 * time.Minute
	defaultMaxBatchSize = 100         // Default maximum number of deltas in a batch
	defaultFlushPeriod  = time.Second // Default flush period
)

type deltasBatch struct {
	deltas []*intakev1.Delta
	id     uint64
}

var deltaVersion string

func init() {
	b := make([]byte, 4)
	_, err := rand.Read(b)
	if err != nil {
		// This should never happen because rand.Read should never return an error
		panic(fmt.Sprintf("failed to generate random delta version: %v", err))
	}
	deltaVersion = hex.EncodeToString(b)
}

var batchCounter uint64

func newDeltasBatch(deltas []*intakev1.Delta) *deltasBatch {
	return &deltasBatch{
		deltas: deltas,
		id:     atomic.AddUint64(&batchCounter, 1),
	}
}

type worker struct {
	apiKey string
	client intakev1.IntakeServiceClient
	store  resource.Store
	logger logr.Logger
	queue  workqueue.TypedRateLimitingInterface[*deltasBatch]
	batch  *deltasBatch
	mu     sync.Mutex

	// configurable options
	maxBatchSize int
	flushPeriod  time.Duration

	// runtime fields
	stream intakev1.IntakeService_DeltaClient
}

type WorkerOpts func(*worker)

func WithGRPCConn(conn *grpc.ClientConn) WorkerOpts {
	return func(w *worker) {
		w.client = intakev1.NewIntakeServiceClient(conn)
	}
}

func WithLogger(logger logr.Logger) WorkerOpts {
	return func(w *worker) {
		w.logger = logger
	}
}

func WithAPIKey(apiKey string) WorkerOpts {
	return func(w *worker) {
		w.apiKey = apiKey
	}
}

func WithMaxBatchSize(size int) WorkerOpts {
	return func(w *worker) {
		w.maxBatchSize = size
	}
}

func WithFlushPeriod(period time.Duration) WorkerOpts {
	return func(w *worker) {
		w.flushPeriod = period
	}
}

func NewWorker(store resource.Store, opts ...WorkerOpts) (*worker, error) {
	if store == nil {
		return nil, fmt.Errorf("store can't be nil")
	}

	ratelimiter := workqueue.DefaultTypedControllerRateLimiter[*deltasBatch]()
	queue := workqueue.NewTypedRateLimitingQueueWithConfig(ratelimiter,
		workqueue.TypedRateLimitingQueueConfig[*deltasBatch]{
			Name: workerName,
		},
	)

	batch := newDeltasBatch([]*intakev1.Delta{})

	w := &worker{
		store:        store,
		queue:        queue,
		batch:        batch,
		maxBatchSize: defaultMaxBatchSize,
		flushPeriod:  defaultFlushPeriod,
	}
	for _, opt := range opts {
		opt(w)
	}

	if w.client == nil {
		return nil, fmt.Errorf("can't create client")
	}
	return w, nil
}

func (w *worker) flushBatch() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.batch.deltas) == 0 {
		return
	}

	w.queue.AddRateLimited(w.batch)
	w.batch = newDeltasBatch([]*intakev1.Delta{})
}

func (w *worker) Start(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.streamer(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		w.heartbeatWorker(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		w.batchFlusher(ctx)
	}()

	for event := range w.store.Subscribe(nil) {
		for _, obj := range event.Objs {
			obj.Ttl = durationpb.New(defaultDeltaTTL)
			obj.DeltaVersion = deltaVersion
		}

		delta := &intakev1.Delta{
			Op:      eventTypeToOp(event.Type),
			Objects: event.Objs,
		}

		w.mu.Lock()
		w.batch.deltas = append(w.batch.deltas, delta)
		shouldFlush := len(w.batch.deltas) >= w.maxBatchSize
		w.mu.Unlock()

		if shouldFlush {
			w.flushBatch()
		}
	}

	w.logger.Info("shutting down intake worker")
	w.flushBatch()
	w.queue.ShutDownWithDrain()
	wg.Wait()
	return nil
}

func (w *worker) batchFlusher(ctx context.Context) {
	ticker := time.NewTicker(w.flushPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.flushBatch()
		}
	}
}

func (w *worker) streamer(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			if w.stream != nil {
				if _, err := w.stream.CloseAndRecv(); err != nil {
					w.logger.Error(err, "error closing intake stream")
				}
			}
			return
		default:
			w.sendDelta(ctx)
		}
	}
}

func (w *worker) heartbeatWorker(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.queue.AddRateLimited(newDeltasBatch([]*intakev1.Delta{{
				Op: intakev1.DeltaOperation_DELTA_OPERATION_HEARTBEAT,
				Objects: []*resourcev1.Object{
					{
						DeltaVersion: deltaVersion,
						Ttl:          durationpb.New(defaultDeltaTTL),
					},
				},
			}}))
		}
	}
}

func (w *worker) sendDelta(ctx context.Context) {
	batch, shutdown := w.queue.Get()
	if shutdown {
		return
	}
	defer w.queue.Done(batch)

	if w.stream == nil {
		for {
			_, err := backoff.Retry(ctx, func() (bool, error) {
				streamCtx := metadata.NewOutgoingContext(
					context.Background(), metadata.Pairs(headerAuthorize, fmt.Sprintf("bearer %s", w.apiKey)),
				)
				stream, err := w.client.Delta(streamCtx)
				if err != nil {
					w.logger.Error(err, "failed to create intake stream, retrying...")
					return false, err
				}
				w.stream = stream
				return true, nil
			}, backoff.WithBackOff(backoff.NewExponentialBackOff()))

			if err == nil {
				break
			}

			// Return if the context is canceled since that means we're shutting down.
			if ctx.Err() == context.Canceled {
				return
			}
		}
	}

	w.logger.V(1).Info("sending deltas", "numDeltas", len(batch.deltas), "version", deltaVersion)
	err := w.stream.Send(&intakev1.DeltaRequest{Deltas: batch.deltas})
	if err != nil {
		_, err = w.stream.CloseAndRecv()
		if err != nil {
			code := status.Code(err)
			if code == codes.Unavailable || code == codes.Canceled {
				// Resetting stream due server max connection age
				w.logger.V(1).Info("resetting intake stream")
			} else {
				w.logger.Error(err, "failed to send to intake stream")
			}
		}
		w.stream = nil
		if !w.queue.ShuttingDown() {
			w.queue.AddRateLimited(batch)
		}
		return
	}
	w.queue.Forget(batch)
}

func eventTypeToOp(e resource.EventType) intakev1.DeltaOperation {
	switch e {
	case resource.EventTypeAdd:
		return intakev1.DeltaOperation_DELTA_OPERATION_CREATE
	case resource.EventTypeUpdate:
		return intakev1.DeltaOperation_DELTA_OPERATION_UPDATE
	case resource.EventTypeDelete:
		return intakev1.DeltaOperation_DELTA_OPERATION_DELETE
	default:
		return intakev1.DeltaOperation_DELTA_OPERATION_CREATE
	}
}
