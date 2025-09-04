// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package store

import "github.com/go-logr/logr"

// badgerLogger wraps a logr.Logger to implement the badger.Logger interface
type badgerLogger struct {
	log logr.Logger
}

// newBadgerLogger creates a new badger logger that wraps a logr.Logger
func newBadgerLogger(log logr.Logger) *badgerLogger {
	return &badgerLogger{log: log}
}

// Errorf logs an ERROR level message
func (l *badgerLogger) Errorf(format string, args ...any) {
	l.log.Error(nil, format, args...)
}

// Warningf logs a WARNING level message
func (l *badgerLogger) Warningf(format string, args ...any) {
	// Map warning to info since logr doesn't have a warning level
	l.log.Info(format, args...)
}

// Infof logs an INFO level message
func (l *badgerLogger) Infof(format string, args ...any) {
	l.log.Info(format, args...)
}

// Debugf logs a DEBUG level message
func (l *badgerLogger) Debugf(format string, args ...any) {
	// Use V(1) for debug level logging
	l.log.V(1).Info(format, args...)
}
