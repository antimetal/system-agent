// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package performance

// MetricsReceiverAdapter wraps any object that has an Accept method compatible
// with the Receiver interface but can't directly implement it due to import cycles.
// This is specifically designed for the MetricsRouter in internal/metrics.
type MetricsReceiverAdapter struct {
	router interface {
		Accept(data any) error
		Name() string
	}
}

// NewMetricsReceiverAdapter creates a new adapter for the metrics router
func NewMetricsReceiverAdapter(router interface {
	Accept(data any) error
	Name() string
}) Receiver {
	return &MetricsReceiverAdapter{router: router}
}

// Accept implements the Receiver interface
func (a *MetricsReceiverAdapter) Accept(data any) error {
	// Simply forward to the underlying router
	return a.router.Accept(data)
}

// Name implements the Receiver interface
func (a *MetricsReceiverAdapter) Name() string {
	return a.router.Name()
}
