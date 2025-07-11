// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package collectors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/antimetal/agent/pkg/performance"
	"github.com/go-logr/logr"
)

const (
	// Default number of kernel messages to return
	defaultMessageLimit = 50

	// Buffer size for reading from /dev/kmsg
	kmsgBufferSize = 8192
)

// KernelCollector collects kernel messages from /dev/kmsg
// Reference: https://www.kernel.org/doc/Documentation/ABI/testing/dev-kmsg
type KernelCollector struct {
	performance.BaseCollector
	logger       logr.Logger
	kmsgPath     string
	messageLimit int
	procUtils    *procUtils
}

// KernelCollectorOption is a function for configuring KernelCollector
type KernelCollectorOption func(*KernelCollector)

// WithMessageLimit sets the maximum number of messages to return
func WithMessageLimit(limit int) KernelCollectorOption {
	return func(c *KernelCollector) {
		if limit > 0 {
			c.messageLimit = limit
		}
	}
}

func NewKernelCollector(logger logr.Logger, config performance.CollectionConfig, opts ...KernelCollectorOption) *KernelCollector {
	capabilities := performance.CollectorCapabilities{
		SupportsOneShot:    true,
		SupportsContinuous: false,
		RequiresRoot:       true, // /dev/kmsg typically requires CAP_SYSLOG or root
		RequiresEBPF:       false,
		MinKernelVersion:   "3.5.0", // /dev/kmsg was introduced in 3.5
	}

	collector := &KernelCollector{
		BaseCollector: performance.NewBaseCollector(
			performance.MetricTypeKernel,
			"Kernel Message Collector",
			logger,
			config,
			capabilities,
		),
		logger:       logger.WithName("kernel"),
		kmsgPath:     filepath.Join(config.HostDevPath, "kmsg"),
		messageLimit: defaultMessageLimit,
		procUtils:    newProcUtils(config.HostProcPath),
	}

	// Apply options
	for _, opt := range opts {
		opt(collector)
	}

	return collector
}

func (c *KernelCollector) Collect(ctx context.Context) (any, error) {
	messages, err := c.collectKernelMessages(ctx)
	if err != nil {
		// Handle permission errors gracefully
		if os.IsPermission(err) {
			c.logger.V(1).Info("Permission denied reading kernel messages", "path", c.kmsgPath)
			return []*performance.KernelMessage{}, nil
		}
		return nil, err
	}
	return messages, nil
}

// collectKernelMessages reads and parses kernel messages from /dev/kmsg
func (c *KernelCollector) collectKernelMessages(ctx context.Context) ([]*performance.KernelMessage, error) {
	// Open /dev/kmsg with O_RDONLY | O_NONBLOCK
	// O_NONBLOCK is critical to avoid blocking when no messages are available
	fd, err := syscall.Open(c.kmsgPath, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", c.kmsgPath, err)
	}
	defer syscall.Close(fd)

	// Note: We don't seek to the end because:
	// 1. Each reader of /dev/kmsg gets their own view of the ring buffer
	// 2. The first read will give us the oldest available message
	// 3. We limit the number of messages returned via messageLimit

	// Ring buffer to store raw message strings
	rawRingBuffer := make([]string, c.messageLimit)
	ringIndex := 0
	messageCount := 0

	buf := make([]byte, kmsgBufferSize)

	// Read all messages first, store raw strings
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Every read call to /dev/kmsg returns a single full message
		n, err := syscall.Read(fd, buf)
		if err != nil {
			// EAGAIN means no more messages available (expected)
			if err == syscall.EAGAIN {
				break
			}
			// EPIPE can occur if we miss messages due to buffer overrun
			if err == syscall.EPIPE {
				// XXX This might be useful signal to an investigator on its own
				c.logger.V(1).Info("Kernel ring buffer overrun, some messages lost")
				continue
			}
			return nil, fmt.Errorf("error reading kmsg: %w", err)
		}

		// Store raw message in ring buffer
		rawRingBuffer[ringIndex] = string(buf[:n])
		ringIndex = (ringIndex + 1) % c.messageLimit
		if messageCount < c.messageLimit {
			messageCount++
		}
	}

	// Now parse only the messages we're keeping
	messages, err := c.parseMessagesFromRing(rawRingBuffer, ringIndex, messageCount)
	if err == nil {
		c.logger.V(1).Info("Collected kernel messages", "count", len(messages), "limit", c.messageLimit)
	}
	return messages, err
}

// parseMessagesFromRing parses raw messages from the ring buffer in chronological order
func (c *KernelCollector) parseMessagesFromRing(rawRing []string, endIndex, count int) ([]*performance.KernelMessage, error) {
	if count == 0 {
		return []*performance.KernelMessage{}, nil
	}
	
	// Get boot time once for all messages
	bootTime, err := c.procUtils.GetBootTime()
	if err != nil {
		return nil, fmt.Errorf("failed to get boot time: %w", err)
	}
	
	// Allocate result slice - may be smaller if some messages fail to parse
	result := make([]*performance.KernelMessage, 0, count)
	
	// Calculate start position in ring buffer
	startIndex := endIndex - count
	if startIndex < 0 {
		startIndex += c.messageLimit
	}
	
	// Parse messages in chronological order
	for i := 0; i < count; i++ {
		rawMsg := rawRing[(startIndex+i)%c.messageLimit]
		msg, err := c.parseKmsgLine(rawMsg, bootTime)
		if err != nil {
			c.logger.V(2).Info("Failed to parse kmsg line", "error", err)
			continue // Skip unparseable messages
		}
		result = append(result, msg)
	}
	
	return result, nil
}

// parseKmsgLine parses a line from /dev/kmsg
//
// /dev/kmsg message format (from kernel Documentation/ABI/testing/dev-kmsg):
//
//	<priority>,<sequence>,<timestamp>,<flags>;<message>
//
// Where:
//   - priority: syslog priority (0-7) + facility (0-23) encoded as: (facility << 3) | severity
//   - sequence: 64-bit sequence number (increments with each message)
//   - timestamp: microseconds since boot (monotonic clock)
//   - flags: optional flags field (often '-' or empty)
//   - message: the actual kernel message text
//
// Examples:
//
//	"6,1234,5678901234,-;usb 1-1: new high-speed USB device number 2 using xhci_hcd"
//	"3,4567,8901234567,-;EXT4-fs (sda1): re-mounted. Opts: errors=remount-ro"
//	"0,100,50000000,-;kernel: Out of memory: Kill process 1234 (chrome) score 999"
//
// Priority encoding (from include/linux/kern_levels.h):
//
//	0 = KERN_EMERG   - system is unusable
//	1 = KERN_ALERT   - action must be taken immediately
//	2 = KERN_CRIT    - critical conditions
//	3 = KERN_ERR     - error conditions
//	4 = KERN_WARNING - warning conditions
//	5 = KERN_NOTICE  - normal but significant condition
//	6 = KERN_INFO    - informational
//	7 = KERN_DEBUG   - debug-level messages
func (c *KernelCollector) parseKmsgLine(line string, bootTime time.Time) (*performance.KernelMessage, error) {
	line = strings.TrimRight(line, "\n")
	parts := strings.SplitN(line, ";", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid kmsg format: missing semicolon")
	}
	header := parts[0]
	message := parts[1]

	headerFields := strings.Split(header, ",")
	if len(headerFields) < 3 {
		return nil, fmt.Errorf("invalid kmsg header: expected at least 3 fields, got %d", len(headerFields))
	}

	priority, err := strconv.ParseUint(headerFields[0], 10, 8)
	if err != nil {
		return nil, fmt.Errorf("failed to parse priority %q: %w", headerFields[0], err)
	}

	sequence, err := strconv.ParseUint(headerFields[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse sequence %q: %w", headerFields[1], err)
	}

	// Parse usSinceBoot (microseconds since boot)
	usSinceBoot, err := strconv.ParseInt(headerFields[2], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse timestamp %q: %w", headerFields[2], err)
	}

	// Convert timestamp to time.Time using cached boot time
	msgTime := bootTime.Add(time.Duration(usSinceBoot) * time.Microsecond)

	// Extract facility and severity from priority
	// Priority encoding: (facility << 3) | severity
	// So to decode: facility = priority >> 3, severity = priority & 7
	facility := uint8(priority >> 3)
	severity := uint8(priority & 7)

	// Parse subsystem and device from message if possible
	subsystem, device := parseMessageContent(message)

	return &performance.KernelMessage{
		Timestamp:   msgTime,
		Facility:    facility,
		Severity:    severity,
		SequenceNum: sequence,
		Message:     message,
		Subsystem:   subsystem,
		Device:      device,
	}, nil
}

// parseMessageContent extracts subsystem and device from kernel message text
// Patterns: [subsystem] message, subsystem: message, device subsystem: message
func parseMessageContent(message string) (subsystem, device string) {

	// Pattern 1: Try to extract subsystem from [subsystem] format
	if strings.HasPrefix(message, "[") {
		end := strings.Index(message, "]")
		if end > 1 {
			// Extract everything between [ and ]
			// e.g., "[drm:intel_dp_detect [i915]]" -> "drm:intel_dp_detect [i915]"
			subsystem = message[1:end]
			return subsystem, device
		}
	}

	// Pattern 2: Try to extract from "subsystem:" or "device subsystem:" format
	if idx := strings.Index(message, ":"); idx > 0 && idx < 50 {
		prefix := strings.TrimSpace(message[:idx])

		// Check if it looks like a subsystem name (no spaces, reasonable length)
		// This filters out timestamps and other non-subsystem prefixes
		if !strings.Contains(prefix, " ") && len(prefix) < 20 {
			subsystem = prefix

			// Pattern 3: Check if prefix contains "device subsystem" pattern
			// e.g., "usb 1-1" would split into device="usb" and subsystem="1-1"
			parts := strings.Fields(prefix)
			if len(parts) == 2 {
				device = parts[0]
				subsystem = parts[1]
			}
		}
	}

	return subsystem, device
}

