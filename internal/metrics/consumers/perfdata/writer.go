// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package perfdata

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/go-logr/logr"

	"github.com/antimetal/agent/internal/metrics"
	"github.com/antimetal/agent/pkg/performance"
)

// WriterConfig holds configuration for the perf.data writer
type WriterConfig struct {
	FileMode   string // "file" or "pipe"
	BufferSize int
}

// Writer handles writing perf.data format
type Writer struct {
	writer io.Writer
	config WriterConfig
	logger logr.Logger

	// State
	headerWritten bool
}

// NewWriter creates a new perf.data writer
func NewWriter(w io.Writer, config WriterConfig, logger logr.Logger) *Writer {
	return &Writer{
		writer: bufio.NewWriterSize(w, config.BufferSize),
		config: config,
		logger: logger.WithName("perfdata-writer"),
	}
}

// WriteHeader writes the perf.data file header
func (w *Writer) WriteHeader() error {
	if w.headerWritten {
		return fmt.Errorf("header already written")
	}

	if w.config.FileMode == "pipe" {
		// Pipe mode: write simplified header
		if err := w.writePipeHeader(); err != nil {
			return err
		}
	} else {
		// File mode: write full header with sections
		if err := w.writeFileHeader(); err != nil {
			return err
		}
	}

	w.headerWritten = true
	return nil
}

// writePipeHeader writes a simplified header for pipe mode
func (w *Writer) writePipeHeader() error {
	// Pipe header format:
	// - 8 bytes: magic "PERFILE2"
	// - 8 bytes: size (always 16 for pipe mode)

	magic := []byte("PERFILE2")
	if err := binary.Write(w.writer, binary.LittleEndian, magic); err != nil {
		return fmt.Errorf("failed to write magic: %w", err)
	}

	size := uint64(16)
	if err := binary.Write(w.writer, binary.LittleEndian, size); err != nil {
		return fmt.Errorf("failed to write size: %w", err)
	}

	w.logger.V(1).Info("wrote pipe header")
	return nil
}

// writeFileHeader writes a full header for file mode
func (w *Writer) writeFileHeader() error {
	// TODO: Implement full file header with sections
	// For now, write minimal header

	magic := []byte("PERFILE2")
	if err := binary.Write(w.writer, binary.LittleEndian, magic); err != nil {
		return fmt.Errorf("failed to write magic: %w", err)
	}

	// Write placeholder header fields
	// TODO: Properly implement perf_header structure
	size := uint64(104) // Standard header size
	if err := binary.Write(w.writer, binary.LittleEndian, size); err != nil {
		return fmt.Errorf("failed to write size: %w", err)
	}

	// Write zeros for remaining header fields (placeholder)
	zeros := make([]byte, 96)
	if _, err := w.writer.Write(zeros); err != nil {
		return fmt.Errorf("failed to write header padding: %w", err)
	}

	w.logger.V(1).Info("wrote file header")
	return nil
}

// perf_event_header constants
const (
	PERF_RECORD_SAMPLE = 9

	PERF_SAMPLE_IP         = 1 << 0
	PERF_SAMPLE_TID        = 1 << 1
	PERF_SAMPLE_TIME       = 1 << 2
	PERF_SAMPLE_CPU        = 1 << 6
	PERF_SAMPLE_PERIOD     = 1 << 8
	PERF_SAMPLE_STACK_USER = 1 << 13
	PERF_SAMPLE_REGS_USER  = 1 << 12
	PERF_SAMPLE_IDENTIFIER = 1 << 16
)

// perfEventHeader represents the perf_event_header structure
type perfEventHeader struct {
	Type uint32
	Misc uint16
	Size uint16
}

// WriteProfile writes a profile event to the perf.data file
func (w *Writer) WriteProfile(event metrics.MetricEvent) (int64, error) {
	if !w.headerWritten {
		return 0, fmt.Errorf("header not written")
	}

	// Type assert to ProfileStats
	profileStats, ok := event.Data.(*performance.ProfileStats)
	if !ok {
		return 0, fmt.Errorf("expected *performance.ProfileStats, got %T", event.Data)
	}

	var totalBytes int64

	w.logger.V(2).Info("writing profile",
		"stacks", len(profileStats.Stacks),
		"event_name", profileStats.EventName)

	// Write each stack as a PERF_RECORD_SAMPLE
	for _, stack := range profileStats.Stacks {
		bytes, err := w.writeSample(stack, profileStats)
		if err != nil {
			return totalBytes, fmt.Errorf("failed to write sample: %w", err)
		}
		totalBytes += bytes
	}

	return totalBytes, nil
}

// writeSample writes a single PERF_RECORD_SAMPLE
func (w *Writer) writeSample(stack performance.ProfileStack, stats *performance.ProfileStats) (int64, error) {
	// Calculate sample size
	headerSize := uint16(8) // type(4) + misc(2) + size(2)
	dataSize := uint16(0)

	// PERF_SAMPLE_IDENTIFIER
	dataSize += 8 // uint64

	// PERF_SAMPLE_IP
	ip := uint64(0)
	if len(stack.UserStack) > 0 {
		ip = stack.UserStack[0]
	}
	dataSize += 8 // uint64

	// PERF_SAMPLE_TID
	dataSize += 8 // pid + tid (2 x uint32)

	// PERF_SAMPLE_TIME
	dataSize += 8 // uint64 timestamp

	// PERF_SAMPLE_CPU
	dataSize += 8 // cpu + res (uint32 + uint32)

	// PERF_SAMPLE_PERIOD
	dataSize += 8 // uint64

	// PERF_SAMPLE_STACK_USER
	userStackSize := uint64(len(stack.UserStack) * 8)
	dataSize += 8                     // size (uint64)
	dataSize += uint16(userStackSize) // actual stack data
	dataSize += 8                     // dyn_size (uint64)

	totalSize := headerSize + dataSize

	// Write header
	header := perfEventHeader{
		Type: PERF_RECORD_SAMPLE,
		Misc: 0, // PERF_RECORD_MISC_USER
		Size: totalSize,
	}

	if err := binary.Write(w.writer, binary.LittleEndian, header.Type); err != nil {
		return 0, err
	}
	if err := binary.Write(w.writer, binary.LittleEndian, header.Misc); err != nil {
		return 0, err
	}
	if err := binary.Write(w.writer, binary.LittleEndian, header.Size); err != nil {
		return 0, err
	}

	// PERF_SAMPLE_IDENTIFIER (sample ID for deduplication)
	sampleID := uint64(stack.PID)<<32 | uint64(stack.TID)
	if err := binary.Write(w.writer, binary.LittleEndian, sampleID); err != nil {
		return 0, err
	}

	// PERF_SAMPLE_IP
	if err := binary.Write(w.writer, binary.LittleEndian, ip); err != nil {
		return 0, err
	}

	// PERF_SAMPLE_TID
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(stack.PID)); err != nil {
		return 0, err
	}
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(stack.TID)); err != nil {
		return 0, err
	}

	// PERF_SAMPLE_TIME
	timestamp := uint64(stats.CollectionTime.UnixNano())
	if err := binary.Write(w.writer, binary.LittleEndian, timestamp); err != nil {
		return 0, err
	}

	// PERF_SAMPLE_CPU
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(stack.CPU)); err != nil {
		return 0, err
	}
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(0)); err != nil { // reserved
		return 0, err
	}

	// PERF_SAMPLE_PERIOD
	period := stats.SamplePeriod
	if err := binary.Write(w.writer, binary.LittleEndian, period); err != nil {
		return 0, err
	}

	// PERF_SAMPLE_STACK_USER
	// Write size
	if err := binary.Write(w.writer, binary.LittleEndian, userStackSize); err != nil {
		return 0, err
	}

	// Write stack data
	for _, addr := range stack.UserStack {
		if err := binary.Write(w.writer, binary.LittleEndian, addr); err != nil {
			return 0, err
		}
	}

	// Write dynamic size (actual size of valid data)
	if err := binary.Write(w.writer, binary.LittleEndian, userStackSize); err != nil {
		return 0, err
	}

	return int64(totalSize), nil
}

// Flush flushes any buffered data
func (w *Writer) Flush() error {
	if bw, ok := w.writer.(*bufio.Writer); ok {
		return bw.Flush()
	}
	return nil
}

// Close closes the writer
func (w *Writer) Close() error {
	if err := w.Flush(); err != nil {
		return err
	}
	return nil
}
