// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt


package collectors

import (
	"fmt"
	"sync/atomic"
)

// MinimalPool implements a fixed-size buffer pool with exactly 3 buffers.
// This provides minimal buffering for network sends while maintaining
// a strict memory budget of 3KB for buffer storage.
type MinimalPool struct {
	buffers chan []byte
	stats   PoolStats
}

// PoolStats tracks pool usage metrics
type PoolStats struct {
	Gets   uint64 // Total buffer requests
	Puts   uint64 // Total buffer returns
	Misses uint64 // Times pool was empty
	InUse  int32  // Current buffers in use
}

const (
	// PoolSize is the fixed number of buffers in the pool
	PoolSize = 3
	// BufferSize is the size of each buffer in bytes
	BufferSize = 1024
)

// NewMinimalPool creates a new buffer pool with exactly 3 pre-allocated buffers
func NewMinimalPool() *MinimalPool {
	pool := &MinimalPool{
		buffers: make(chan []byte, PoolSize),
	}

	// Pre-allocate exactly 3 buffers
	for i := 0; i < PoolSize; i++ {
		pool.buffers <- make([]byte, 0, BufferSize)
	}

	return pool
}

// Get retrieves a buffer from the pool.
// Returns nil if the pool is exhausted (all buffers in use).
// This is non-blocking and expected behavior under load.
func (p *MinimalPool) Get() []byte {
	select {
	case buf := <-p.buffers:
		atomic.AddInt32(&p.stats.InUse, 1)
		atomic.AddUint64(&p.stats.Gets, 1)
		return buf[:cap(buf)] // Return full capacity buffer
	default:
		// Pool exhausted - this is expected under load
		atomic.AddUint64(&p.stats.Misses, 1)
		return nil
	}
}

// Put returns a buffer to the pool.
// Panics if the buffer is not the expected size (development aid).
func (p *MinimalPool) Put(buf []byte) {
	if buf == nil {
		return // Ignore nil buffers
	}

	if cap(buf) != BufferSize {
		panic(fmt.Sprintf("invalid buffer size returned to pool: got %d, expected %d", cap(buf), BufferSize))
	}

	atomic.AddInt32(&p.stats.InUse, -1)
	atomic.AddUint64(&p.stats.Puts, 1)

	select {
	case p.buffers <- buf[:cap(buf)]: // Keep full capacity when returning
		// Buffer returned to pool
	default:
		// Should never happen with 3 buffer limit
		panic("pool overflow: more buffers returned than allocated")
	}
}

// GetStats returns a copy of the current pool statistics
func (p *MinimalPool) GetStats() PoolStats {
	return PoolStats{
		Gets:   atomic.LoadUint64(&p.stats.Gets),
		Puts:   atomic.LoadUint64(&p.stats.Puts),
		Misses: atomic.LoadUint64(&p.stats.Misses),
		InUse:  atomic.LoadInt32(&p.stats.InUse),
	}
}

// Available returns the number of buffers currently available in the pool
func (p *MinimalPool) Available() int {
	return len(p.buffers)
}

// MissRate returns the percentage of Get() calls that failed due to exhaustion
func (p *MinimalPool) MissRate() float64 {
	gets := atomic.LoadUint64(&p.stats.Gets)
	misses := atomic.LoadUint64(&p.stats.Misses)
	total := gets + misses
	if total == 0 {
		return 0
	}
	return float64(misses) / float64(total) * 100
}