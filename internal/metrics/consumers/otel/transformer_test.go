// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package otel_test

import (
	"testing"

	"github.com/antimetal/agent/internal/metrics/consumers/otel"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() logr.Logger {
	return logr.Discard()
}

// Test Transformer through Consumer interface (Public API only)
// Note: We can't test internal transformer methods directly from _test package

func TestConsumer_WithTransformerIntegration(t *testing.T) {
	config := otel.Config{
		Endpoint:    "localhost:4317",
		Insecure:    true,
		ServiceName: "test-service",
	}

	consumer, err := otel.NewConsumer(config, testLogger())
	require.NoError(t, err)
	require.NotNil(t, consumer)

	// Test that consumer was created successfully with transformer
	assert.Equal(t, "opentelemetry", consumer.Name())

	// Test initial health
	health := consumer.Health()
	assert.True(t, health.Healthy)
	assert.Equal(t, uint64(0), health.EventsCount)
}

// Note: Testing Start/Stop requires a real OTLP collector or complex mocking.
// This is better suited for integration tests rather than unit tests.

// Note: Comprehensive transformer testing would require integration tests
// or making some methods public. For unit testing transformer logic specifically,
// consider keeping tests in the same package (otel) rather than otel_test
