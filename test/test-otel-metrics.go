// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/antimetal/agent/pkg/metrics"
	"github.com/antimetal/agent/pkg/observability/otel"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
)

func main() {
	// Setup logger
	zapLog, err := zap.NewDevelopment()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	logger := zapr.NewLogger(zapLog)

	// Setup OpenTelemetry consumer
	otelConfig := otel.Config{
		Enabled:     true,
		Endpoint:    "localhost:4317",
		Insecure:    true,
		Compression: "gzip",
		Timeout:     30 * time.Second,
		ServiceName: "test-agent",
		GlobalTags:  []string{"env:test"},
	}

	consumer, err := otel.NewConsumerFromConfig(otelConfig, logger)
	if err != nil {
		log.Fatalf("Failed to create OpenTelemetry consumer: %v", err)
	}

	if consumer == nil {
		log.Fatal("OpenTelemetry consumer is nil - check if enabled")
	}

	// Setup metrics router
	routerConfig := metrics.DefaultRouterConfig()
	router := metrics.NewRouter(routerConfig, logger)

	// Register consumer
	if err := router.RegisterConsumer(&OtelConsumerAdapter{consumer: consumer}); err != nil {
		log.Fatalf("Failed to register OpenTelemetry consumer: %v", err)
	}

	// Start the metrics pipeline in a goroutine (it blocks)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := router.Start(ctx); err != nil {
			log.Printf("Router exited with error: %v", err)
		}
	}()

	logger.Info("OpenTelemetry metrics test started - sending sample metrics")

	// Create sample metrics events
	go func() {
		for i := 0; i < 10; i++ {
			// CPU metrics
			cpuEvent := metrics.MetricEvent{
				Timestamp:   time.Now(),
				Source:      "test-collector",
				NodeName:    "test-node",
				ClusterName: "test-cluster",
				MetricType:  metrics.MetricTypeCPU,
				EventType:   metrics.EventTypeSnapshot,
				Data: otel.CPUStats{
					CPUIndex: 0,
					User:     uint64(1000 + i*100),
					Nice:     10,
					System:   uint64(500 + i*50),
					Idle:     uint64(8000 - i*50),
					IOWait:   50,
				},
				Tags: map[string]string{
					"cpu": "0",
				},
			}

			// Memory metrics
			memoryEvent := metrics.MetricEvent{
				Timestamp:   time.Now(),
				Source:      "test-collector",
				NodeName:    "test-node",
				ClusterName: "test-cluster",
				MetricType:  metrics.MetricTypeMemory,
				EventType:   metrics.EventTypeSnapshot,
				Data: otel.MemoryStats{
					MemTotal:     8589934592,                      // 8GB
					MemFree:      uint64(2147483648 - i*10485760), // 2GB - decreasing
					MemAvailable: uint64(3221225472 - i*5242880),  // 3GB - decreasing
					Buffers:      134217728,
					Cached:       536870912,
				},
				Tags: map[string]string{
					"host": "test-host",
				},
			}

			if err := router.Publish(cpuEvent); err != nil {
				logger.Error(err, "Failed to publish CPU event")
			}
			if err := router.Publish(memoryEvent); err != nil {
				logger.Error(err, "Failed to publish memory event")
			}

			logger.Info(fmt.Sprintf("Sent metrics batch %d", i+1))
			time.Sleep(2 * time.Second)
		}
	}()

	// Wait for interrupt
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop

	logger.Info("Shutting down...")

	// Cancel context to stop the router
	cancel()

	logger.Info("OpenTelemetry metrics test completed")
}

// OtelConsumerAdapter adapts the OpenTelemetry consumer to the metrics router interface
type OtelConsumerAdapter struct {
	consumer *otel.Consumer
}

func (a *OtelConsumerAdapter) Name() string {
	return a.consumer.Name()
}

func (a *OtelConsumerAdapter) Start(events <-chan metrics.MetricEvent) error {
	// Convert the channel from metrics.MetricEvent to otel.MetricEvent
	otelEvents := make(chan otel.MetricEvent, 1000)

	go func() {
		defer close(otelEvents)
		for event := range events {
			otelEvent := otel.MetricEvent{
				Timestamp:   event.Timestamp,
				Source:      event.Source,
				NodeName:    event.NodeName,
				ClusterName: event.ClusterName,
				MetricType:  string(event.MetricType),
				EventType:   string(event.EventType),
				Data:        event.Data,
				Tags:        event.Tags,
			}
			otelEvents <- otelEvent
		}
	}()

	return a.consumer.Start(otelEvents)
}

func (a *OtelConsumerAdapter) Stop() error {
	return a.consumer.Stop()
}

func (a *OtelConsumerAdapter) Health() metrics.ConsumerHealth {
	health := a.consumer.Health()
	return metrics.ConsumerHealth{
		Healthy:     health.Healthy,
		LastError:   health.LastError,
		EventsCount: health.EventsCount,
		ErrorsCount: health.ErrorsCount,
	}
}
