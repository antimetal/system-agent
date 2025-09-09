// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package debug

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"

	"github.com/antimetal/agent/internal/metrics"
)

const (
	consumerName = "debug"
)

// Consumer implements the metrics consumer interface for debug logging
type Consumer struct {
	config Config
	logger logr.Logger

	// Runtime state
	healthy   atomic.Bool
	lastError atomic.Pointer[error]

	// Metrics
	eventsProcessed atomic.Uint64
	errorsCount     atomic.Uint64
	startTime       time.Time

	// Statistics tracking
	eventsByType   map[string]*atomic.Uint64
	eventsBySource map[string]*atomic.Uint64
	statsMutex     sync.RWMutex
}

func NewConsumer(config Config, logger logr.Logger) (*Consumer, error) {
	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	consumer := &Consumer{
		config:         config,
		logger:         logger.WithName("debug-consumer"),
		startTime:      time.Now(),
		eventsByType:   make(map[string]*atomic.Uint64),
		eventsBySource: make(map[string]*atomic.Uint64),
	}

	consumer.healthy.Store(true)
	return consumer, nil
}

func (c *Consumer) Name() string {
	return consumerName
}

// HandleEvent processes a metric event by logging it immediately.
// This is non-blocking and returns immediately after logging.
func (c *Consumer) HandleEvent(event metrics.MetricEvent) error {
	if err := c.processEvent(event); err != nil {
		c.logger.Error(err, "Failed to process metrics event",
			"metric_type", event.MetricType,
			"source", event.Source)
		c.errorsCount.Add(1)
		c.lastError.Store(&err)
		return err
	} else {
		c.eventsProcessed.Add(1)
	}
	return nil
}

func (c *Consumer) Start(ctx context.Context) error {
	c.logger.Info("Starting Debug consumer",
		"log_level", c.config.LogLevel,
		"log_format", c.config.LogFormat,
		"include_data", c.config.IncludeEventData)

	return nil
}

func (c *Consumer) Health() metrics.ConsumerHealth {
	var lastErr error
	if errPtr := c.lastError.Load(); errPtr != nil {
		lastErr = *errPtr
	}

	return metrics.ConsumerHealth{
		Healthy:     c.healthy.Load(),
		LastError:   lastErr,
		EventsCount: c.eventsProcessed.Load(),
		ErrorsCount: c.errorsCount.Load(),
	}
}

func (c *Consumer) processEvent(event metrics.MetricEvent) error {
	// Check filters
	if !c.config.ShouldLogMetricType(string(event.MetricType)) {
		return nil
	}
	if !c.config.ShouldLogSource(event.Source) {
		return nil
	}

	// Update statistics
	c.updateStats(event)

	// Log the event based on configuration
	if c.config.LogFormat == "json" {
		return c.logEventJSON(event)
	}
	return c.logEventText(event)
}

func (c *Consumer) updateStats(event metrics.MetricEvent) {
	c.statsMutex.Lock()
	defer c.statsMutex.Unlock()

	// Track by type
	metricTypeKey := string(event.MetricType) // Convert once for map key
	if counter, exists := c.eventsByType[metricTypeKey]; exists {
		counter.Add(1)
	} else {
		counter := &atomic.Uint64{}
		counter.Store(1)
		c.eventsByType[metricTypeKey] = counter
	}

	// Track by source
	if counter, exists := c.eventsBySource[event.Source]; exists {
		counter.Add(1)
	} else {
		counter := &atomic.Uint64{}
		counter.Store(1)
		c.eventsBySource[event.Source] = counter
	}
}

// logEventJSON logs an event in JSON format
func (c *Consumer) logEventJSON(event metrics.MetricEvent) error {
	entry := LogEntry{
		Level:    "INFO",
		Consumer: consumerName,
		Message:  "Metrics event received",
		Event:    c.createEventSummary(event),
	}

	if c.config.IncludeTimestamp {
		entry.Timestamp = time.Now()
	}

	if c.config.IncludeEventData && c.config.LogLevel >= 2 {
		entry.Data = c.truncateData(event.Data)
	}

	// Log periodic stats
	if c.eventsProcessed.Load()%1000 == 0 {
		entry.Stats = c.getStatsSnapshot()
	}

	jsonBytes, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	c.logger.Info(string(jsonBytes))
	return nil
}

// logEventText logs an event in human-readable text format
func (c *Consumer) logEventText(event metrics.MetricEvent) error {
	var parts []string

	// Basic event info (level 0+)
	parts = append(parts, fmt.Sprintf("Event: %s", event.MetricType))

	if c.config.LogLevel >= 1 {
		// Add detailed info (level 1+)
		if event.Source != "" {
			parts = append(parts, fmt.Sprintf("Source: %s", event.Source))
		}
		if event.NodeName != "" {
			parts = append(parts, fmt.Sprintf("Node: %s", event.NodeName))
		}
		if event.ClusterName != "" {
			parts = append(parts, fmt.Sprintf("Cluster: %s", event.ClusterName))
		}
		if string(event.EventType) != "" {
			parts = append(parts, fmt.Sprintf("Type: %s", string(event.EventType)))
		}
	}

	if c.config.LogLevel >= 2 {
		// Add data info (level 2+)
		if event.Data != nil {
			dataType := reflect.TypeOf(event.Data).String()
			parts = append(parts, fmt.Sprintf("DataType: %s", dataType))

			if c.config.IncludeEventData {
				dataStr := c.formatDataForText(event.Data)
				parts = append(parts, fmt.Sprintf("Data: %s", dataStr))
			}
		}
	}

	message := strings.Join(parts, " | ")

	// Add timestamp if requested
	if c.config.IncludeTimestamp {
		timestamp := time.Now().Format("2006-01-02 15:04:05.000")
		message = fmt.Sprintf("[%s] %s", timestamp, message)
	}

	c.logger.Info(message)

	// Log periodic stats
	if c.eventsProcessed.Load()%1000 == 0 {
		c.logStatsText()
	}

	return nil
}

// createEventSummary creates a summary of the event for JSON logging
func (c *Consumer) createEventSummary(event metrics.MetricEvent) *MetricEventSummary {
	summary := &MetricEventSummary{
		MetricType:  string(event.MetricType), // Explicit conversion needed for struct field
		EventType:   string(event.EventType),
		Source:      event.Source,
		NodeName:    event.NodeName,
		ClusterName: event.ClusterName,
	}

	if event.Data != nil {
		summary.DataType = reflect.TypeOf(event.Data).String()
		if dataBytes, err := json.Marshal(event.Data); err == nil {
			summary.DataSize = len(dataBytes)
		}
	}

	return summary
}

// truncateData truncates data payload if it exceeds MaxDataLength
func (c *Consumer) truncateData(data interface{}) interface{} {
	if c.config.MaxDataLength == 0 {
		return data
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Sprintf("Error marshaling data: %v", err)
	}

	if len(dataBytes) <= c.config.MaxDataLength {
		return data
	}

	// Try to truncate at a JSON boundary for cleaner output
	truncated := dataBytes[:c.config.MaxDataLength]
	// Find the last complete field before truncation point
	lastComma := -1
	lastBrace := -1
	for i := len(truncated) - 1; i >= 0; i-- {
		if truncated[i] == ',' {
			lastComma = i
			break
		}
		if truncated[i] == '{' {
			lastBrace = i
			break
		}
	}
	
	// Truncate at a clean boundary if possible
	if lastComma > 0 {
		truncated = truncated[:lastComma]
	} else if lastBrace > 0 {
		truncated = truncated[:lastBrace+1]
	}
	
	return fmt.Sprintf("%s... (truncated from %d bytes)", string(truncated), len(dataBytes))
}

// formatDataForText formats data for text logging
func (c *Consumer) formatDataForText(data interface{}) string {
	// Use JSON marshaling for prettier output with proper field names
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		// Fall back to fmt.Sprintf if JSON marshaling fails
		dataStr := fmt.Sprintf("%+v", data)
		if c.config.MaxDataLength > 0 && len(dataStr) > c.config.MaxDataLength {
			return fmt.Sprintf("%s... (truncated from %d chars)",
				dataStr[:c.config.MaxDataLength], len(dataStr))
		}
		return dataStr
	}

	// Convert to string for display
	dataStr := string(jsonBytes)
	
	if c.config.MaxDataLength > 0 && len(dataStr) > c.config.MaxDataLength {
		return fmt.Sprintf("%s... (truncated from %d chars)",
			dataStr[:c.config.MaxDataLength], len(dataStr))
	}

	return dataStr
}

// getStatsSnapshot returns current statistics for JSON logging
func (c *Consumer) getStatsSnapshot() *ConsumerStats {
	return c.getStats()
}

// getStats returns current consumer statistics
func (c *Consumer) getStats() *ConsumerStats {
	c.statsMutex.RLock()
	defer c.statsMutex.RUnlock()

	eventsByType := make(map[string]uint64)
	for t, counter := range c.eventsByType {
		eventsByType[t] = counter.Load()
	}

	eventsBySource := make(map[string]uint64)
	for s, counter := range c.eventsBySource {
		eventsBySource[s] = counter.Load()
	}

	return &ConsumerStats{
		EventsProcessed: c.eventsProcessed.Load(),
		ErrorsCount:     c.errorsCount.Load(),
		Uptime:          time.Since(c.startTime),
		EventsByType:    eventsByType,
		EventsBySource:  eventsBySource,
	}
}

// logStatsText logs statistics in text format
func (c *Consumer) logStatsText() {
	stats := c.getStats()
	c.logger.Info("Debug consumer stats",
		"events_processed", stats.EventsProcessed,
		"errors", stats.ErrorsCount,
		"uptime", stats.Uptime,
		"types", len(stats.EventsByType),
		"sources", len(stats.EventsBySource))
}

// Compile-time check that Consumer implements ConsumerInterface
var _ metrics.Consumer = (*Consumer)(nil)
