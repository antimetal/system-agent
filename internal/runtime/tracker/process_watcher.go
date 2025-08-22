// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package tracker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
)

// ProcessWatcher monitors /proc filesystem for process lifecycle events
type ProcessWatcher struct {
	logger     logr.Logger
	procPath   string
	
	// Event management
	events      chan RuntimeEvent
	debounce    time.Duration
	lastEvents  map[string]time.Time
	eventsMu    sync.RWMutex
	
	// Process tracking
	knownPIDs   map[int32]bool
	pidsMu      sync.RWMutex
	
	// Filesystem watching
	watcher *fsnotify.Watcher
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewProcessWatcher creates a new process filesystem watcher
func NewProcessWatcher(logger logr.Logger, eventChan chan RuntimeEvent, debounce time.Duration) (*ProcessWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	return &ProcessWatcher{
		logger:     logger.WithName("process-watcher"),
		procPath:   "/proc",
		events:     eventChan,
		debounce:   debounce,
		lastEvents: make(map[string]time.Time),
		knownPIDs:  make(map[int32]bool),
		watcher:    watcher,
	}, nil
}

// Start begins watching the /proc filesystem for process events
func (pw *ProcessWatcher) Start(ctx context.Context) error {
	pw.logger.Info("Starting process watcher", "procPath", pw.procPath)

	ctx, cancel := context.WithCancel(ctx)
	pw.cancel = cancel

	// Check if /proc is accessible
	if _, err := os.Stat(pw.procPath); os.IsNotExist(err) {
		return fmt.Errorf("/proc filesystem not available")
	}

	// Initialize known PIDs
	if err := pw.initializeKnownPIDs(); err != nil {
		pw.logger.Error(err, "Failed to initialize known PIDs, starting with empty set")
	}

	// Set up filesystem watches
	if err := pw.setupWatches(); err != nil {
		return fmt.Errorf("failed to setup filesystem watches: %w", err)
	}

	// Start event processing
	pw.wg.Add(1)
	go pw.processEvents(ctx)

	// Start periodic PID scanning (fallback for missed events)
	pw.wg.Add(1)
	go pw.periodicPIDScan(ctx)

	return nil
}

// Stop stops the process watcher
func (pw *ProcessWatcher) Stop() error {
	pw.logger.Info("Stopping process watcher")
	
	if pw.cancel != nil {
		pw.cancel()
	}
	
	if pw.watcher != nil {
		pw.watcher.Close()
	}
	
	pw.wg.Wait()
	return nil
}

// initializeKnownPIDs scans /proc to build initial set of known PIDs
func (pw *ProcessWatcher) initializeKnownPIDs() error {
	entries, err := os.ReadDir(pw.procPath)
	if err != nil {
		return fmt.Errorf("failed to read /proc: %w", err)
	}

	pw.pidsMu.Lock()
	defer pw.pidsMu.Unlock()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		if pid, err := strconv.Atoi(entry.Name()); err == nil {
			pw.knownPIDs[int32(pid)] = true
		}
	}

	pw.logger.V(1).Info("Initialized known PIDs", "count", len(pw.knownPIDs))
	return nil
}

// setupWatches configures fsnotify to watch /proc for process events
func (pw *ProcessWatcher) setupWatches() error {
	// Watch /proc directory for new process directories
	if err := pw.watcher.Add(pw.procPath); err != nil {
		return fmt.Errorf("failed to watch /proc: %w", err)
	}

	pw.logger.V(1).Info("Process filesystem watches configured")
	return nil
}

// processEvents handles fsnotify events and converts them to runtime events
func (pw *ProcessWatcher) processEvents(ctx context.Context) {
	defer pw.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-pw.watcher.Events:
			if !ok {
				return
			}
			pw.handleFilesystemEvent(event)

		case err, ok := <-pw.watcher.Errors:
			if !ok {
				return
			}
			pw.logger.Error(err, "Process filesystem watcher error")
			// Send error event
			pw.sendEvent(RuntimeEvent{
				Type:      EventTypeError,
				Timestamp: time.Now(),
				Data: ErrorEvent{
					Error:   err,
					Context: "process filesystem watcher",
				},
			})
		}
	}
}

// handleFilesystemEvent processes individual fsnotify events
func (pw *ProcessWatcher) handleFilesystemEvent(event fsnotify.Event) {
	path := event.Name
	
	// Only interested in direct /proc entries (process directories)
	if filepath.Dir(path) != pw.procPath {
		return
	}

	filename := filepath.Base(path)
	
	// Check if this is a PID directory
	pid, err := strconv.Atoi(filename)
	if err != nil {
		return // Not a PID directory
	}

	pid32 := int32(pid)

	// Apply debouncing
	if !pw.shouldProcessEvent(path) {
		return
	}

	pw.logger.V(2).Info("Process filesystem event", "pid", pid32, "op", event.Op.String())

	// Handle different event types
	switch {
	case event.Op&fsnotify.Create != 0:
		pw.handleProcessCreated(pid32, path)
	case event.Op&fsnotify.Remove != 0:
		pw.handleProcessExited(pid32, path)
	}
}

// handleProcessCreated handles process creation events
func (pw *ProcessWatcher) handleProcessCreated(pid int32, path string) {
	// Check if we already know about this PID
	pw.pidsMu.Lock()
	_, known := pw.knownPIDs[pid]
	if !known {
		pw.knownPIDs[pid] = true
	}
	pw.pidsMu.Unlock()

	if known {
		return // Already known, ignore
	}

	// Try to read process information
	procInfo, err := pw.readProcessInfo(pid)
	if err != nil {
		pw.logger.V(2).Info("Failed to read process info", "pid", pid, "error", err)
		return
	}

	pw.sendEvent(RuntimeEvent{
		Type:      EventTypeProcessCreated,
		Timestamp: time.Now(),
		Data:      procInfo,
	})
}

// handleProcessExited handles process exit events
func (pw *ProcessWatcher) handleProcessExited(pid int32, path string) {
	// Remove from known PIDs
	pw.pidsMu.Lock()
	_, known := pw.knownPIDs[pid]
	if known {
		delete(pw.knownPIDs, pid)
	}
	pw.pidsMu.Unlock()

	if !known {
		return // Not tracked, ignore
	}

	pw.sendEvent(RuntimeEvent{
		Type:      EventTypeProcessExited,
		Timestamp: time.Now(),
		Data: ProcessEvent{
			PID:    pid,
			Action: "exited",
			Path:   path,
		},
	})
}

// readProcessInfo reads basic process information from /proc/PID/
func (pw *ProcessWatcher) readProcessInfo(pid int32) (ProcessEvent, error) {
	procDir := filepath.Join(pw.procPath, strconv.Itoa(int(pid)))
	
	// Read command line
	var command string
	if cmdlineBytes, err := os.ReadFile(filepath.Join(procDir, "cmdline")); err == nil {
		// cmdline is null-separated, take first part as command
		cmdlineParts := strings.Split(string(cmdlineBytes), "\x00")
		if len(cmdlineParts) > 0 && cmdlineParts[0] != "" {
			command = cmdlineParts[0]
		}
	}

	// Read stat for PPID
	var ppid int32
	if statBytes, err := os.ReadFile(filepath.Join(procDir, "stat")); err == nil {
		statFields := strings.Fields(string(statBytes))
		if len(statFields) >= 4 {
			if ppidInt, err := strconv.Atoi(statFields[3]); err == nil {
				ppid = int32(ppidInt)
			}
		}
	}

	return ProcessEvent{
		PID:    pid,
		PPID:   ppid,
		Action: "created",
		Path:   command,
	}, nil
}

// periodicPIDScan performs periodic scans to catch missed events
func (pw *ProcessWatcher) periodicPIDScan(ctx context.Context) {
	defer pw.wg.Done()

	// Scan every 5 seconds as fallback
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pw.scanForChanges()
		}
	}
}

// scanForChanges performs a full scan to detect missed process events
func (pw *ProcessWatcher) scanForChanges() {
	entries, err := os.ReadDir(pw.procPath)
	if err != nil {
		pw.logger.V(2).Info("Failed to scan /proc", "error", err)
		return
	}

	currentPIDs := make(map[int32]bool)
	
	// Build set of current PIDs
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		if pid, err := strconv.Atoi(entry.Name()); err == nil {
			currentPIDs[int32(pid)] = true
		}
	}

	pw.pidsMu.Lock()
	defer pw.pidsMu.Unlock()

	// Find new PIDs (created)
	for pid := range currentPIDs {
		if !pw.knownPIDs[pid] {
			pw.knownPIDs[pid] = true
			// Handle as created (but don't debounce periodic scans)
			go func(p int32) {
				if procInfo, err := pw.readProcessInfo(p); err == nil {
					pw.sendEvent(RuntimeEvent{
						Type:      EventTypeProcessCreated,
						Timestamp: time.Now(),
						Data:      procInfo,
					})
				}
			}(pid)
		}
	}

	// Find exited PIDs (removed)
	for pid := range pw.knownPIDs {
		if !currentPIDs[pid] {
			delete(pw.knownPIDs, pid)
			pw.sendEvent(RuntimeEvent{
				Type:      EventTypeProcessExited,
				Timestamp: time.Now(),
				Data: ProcessEvent{
					PID:    pid,
					Action: "exited",
					Path:   fmt.Sprintf("/proc/%d", pid),
				},
			})
		}
	}
}

// shouldProcessEvent implements debouncing for filesystem events
func (pw *ProcessWatcher) shouldProcessEvent(path string) bool {
	pw.eventsMu.Lock()
	defer pw.eventsMu.Unlock()

	now := time.Now()
	lastEventTime, exists := pw.lastEvents[path]
	
	if exists && now.Sub(lastEventTime) < pw.debounce {
		return false // Too soon, skip this event
	}

	pw.lastEvents[path] = now
	return true
}

// sendEvent sends a runtime event through the channel (non-blocking)
func (pw *ProcessWatcher) sendEvent(event RuntimeEvent) {
	select {
	case pw.events <- event:
		pw.logger.V(1).Info("Sent process event", "type", event.Type)
	default:
		pw.logger.V(1).Info("Event channel full, dropping process event", "type", event.Type)
	}
}