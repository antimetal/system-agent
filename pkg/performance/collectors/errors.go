// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import "errors"

var (
	// ErrUnsupportedVersion indicates an unsupported format version
	ErrUnsupportedVersion = errors.New("unsupported format version")

	// ErrEventCountMismatch indicates event count doesn't match header
	ErrEventCountMismatch = errors.New("event count mismatch between header and batch")

	// ErrPoolExhausted indicates the buffer pool has no available buffers
	ErrPoolExhausted = errors.New("buffer pool exhausted")

	// ErrBufferTooSmall indicates the provided buffer is too small
	ErrBufferTooSmall = errors.New("buffer too small")

	// ErrInvalidMagic indicates invalid magic bytes in header
	ErrInvalidMagic = errors.New("invalid magic bytes")
)