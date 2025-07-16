// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package procutils

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProcUtils provides common utilities for parsing /proc files
type ProcUtils struct {
	procPath string

	// Cached boot time - this never changes during system runtime
	bootTime     time.Time
	bootTimeOnce sync.Once
	bootTimeErr  error

	// Cached USER_HZ value - this never changes during system runtime
	userHZ     int64
	userHZOnce sync.Once
	userHZErr  error

	// Cached page size - this never changes during system runtime
	pageSize     int64
	pageSizeOnce sync.Once
	pageSizeErr  error
}

// New creates a new ProcUtils instance
func New(procPath string) *ProcUtils {
	return &ProcUtils{
		procPath: procPath,
	}
}

// GetBootTime returns the system boot time from /proc/stat
// The result is cached after the first successful read
func (p *ProcUtils) GetBootTime() (time.Time, error) {
	p.bootTimeOnce.Do(func() {
		p.bootTime, p.bootTimeErr = p.readBootTime()
	})
	return p.bootTime, p.bootTimeErr
}

// readBootTime reads the boot time from /proc/stat
// Format: btime <seconds_since_epoch>
func (p *ProcUtils) readBootTime() (time.Time, error) {
	statPath := filepath.Join(p.procPath, "stat")
	data, err := os.ReadFile(statPath)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to read %s: %w", statPath, err)
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "btime ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				btime, err := strconv.ParseInt(parts[1], 10, 64)
				if err != nil {
					return time.Time{}, fmt.Errorf("failed to parse btime: %w", err)
				}
				return time.Unix(btime, 0), nil
			}
		}
	}

	return time.Time{}, fmt.Errorf("btime not found in %s", statPath)
}

// GetUserHZ returns the USER_HZ value (clock ticks per second)
// The result is cached after the first successful read
func (p *ProcUtils) GetUserHZ() (int64, error) {
	p.userHZOnce.Do(func() {
		p.userHZ, p.userHZErr = p.readUserHZ()
	})
	return p.userHZ, p.userHZErr
}

// readUserHZ reads the USER_HZ value from /proc/self/auxv
//
// The auxiliary vector contains system information passed from the kernel to
// user processes. AT_CLKTCK (value 17) contains the USER_HZ value.
//
// Format: The auxv file contains pairs of 8-byte values:
// - First 8 bytes: key (AT_* constant)
// - Next 8 bytes: value
//
// Reference: https://man7.org/linux/man-pages/man3/getauxval.3.html
func (p *ProcUtils) readUserHZ() (int64, error) {
	const AT_CLKTCK = 17 // Frequency of times() from <asm/auxvec.h>

	auxvPath := filepath.Join(p.procPath, "self", "auxv")
	data, err := os.ReadFile(auxvPath)
	if err != nil {
		// Fallback to standard value if auxv is not available
		return 100, nil
	}

	// Parse auxv entries (8-byte key + 8-byte value pairs)
	for i := 0; i <= len(data)-16; i += 16 {
		key := binary.LittleEndian.Uint64(data[i : i+8])
		val := binary.LittleEndian.Uint64(data[i+8 : i+16])

		if key == AT_CLKTCK {
			return int64(val), nil
		}

		if key == 0 { // AT_NULL marks end of auxv
			break
		}
	}

	// If we can't find it in auxv, return the standard value
	// USER_HZ is typically 100 on most Linux systems
	return 100, nil
}

// GetPageSize returns the system page size in bytes
// The result is cached after the first successful read
func (p *ProcUtils) GetPageSize() (int64, error) {
	p.pageSizeOnce.Do(func() {
		p.pageSize, p.pageSizeErr = p.readPageSize()
	})
	return p.pageSize, p.pageSizeErr
}

// readPageSize reads the page size from /proc/self/auxv
//
// AT_PAGESZ (value 6) contains the system page size.
// This is typically 4096 bytes on x86_64 systems.
func (p *ProcUtils) readPageSize() (int64, error) {
	const AT_PAGESZ = 6 // System page size from <asm/auxvec.h>

	auxvPath := filepath.Join(p.procPath, "self", "auxv")
	data, err := os.ReadFile(auxvPath)
	if err != nil {
		// Fallback to standard value if auxv is not available
		return 4096, nil
	}

	// Parse auxv entries (8-byte key + 8-byte value pairs)
	for i := 0; i <= len(data)-16; i += 16 {
		key := binary.LittleEndian.Uint64(data[i : i+8])
		val := binary.LittleEndian.Uint64(data[i+8 : i+16])

		if key == AT_PAGESZ {
			return int64(val), nil
		}

		if key == 0 { // AT_NULL marks end of auxv
			break
		}
	}

	// If we can't find it in auxv, return the standard value
	// Page size is typically 4096 on most systems
	return 4096, nil
}
