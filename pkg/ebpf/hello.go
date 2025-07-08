//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target amd64,arm64 hello ../../ebpf/programs/hello.bpf.c

package ebpf

import (
    "context"
    "encoding/binary"
    "fmt"
    "log"
    "os"

    "github.com/cilium/ebpf"
    "github.com/cilium/ebpf/link"
    "github.com/cilium/ebpf/ringbuf"
    "github.com/cilium/ebpf/rlimit"
)

// Event represents the data structure from our eBPF program
type Event struct {
    PID  uint32
    Comm [16]byte
}

// String returns a string representation of the event
func (e *Event) String() string {
    // Convert null-terminated comm to string
    comm := ""
    for i, b := range e.Comm {
        if b == 0 {
            comm = string(e.Comm[:i])
            break
        }
    }
    if comm == "" {
        comm = string(e.Comm[:])
    }
    return fmt.Sprintf("PID: %d, Command: %s", e.PID, comm)
}

// HelloMonitor manages our simple eBPF program
type HelloMonitor struct {
    objs   helloObjects
    link   link.Link
    reader *ringbuf.Reader
}

// NewHelloMonitor creates a new hello monitor
func NewHelloMonitor() (*HelloMonitor, error) {
    // Check if we're running as root
    if os.Geteuid() != 0 {
        return nil, fmt.Errorf("hello monitor requires root privileges")
    }

    // Remove memory limit for eBPF
    if err := rlimit.RemoveMemlock(); err != nil {
        return nil, fmt.Errorf("failed to remove memlock limit: %w", err)
    }

    // Load eBPF objects
    objs := helloObjects{}
    if err := loadHelloObjects(&objs, nil); err != nil {
        return nil, fmt.Errorf("failed to load eBPF objects: %w", err)
    }

    monitor := &HelloMonitor{
        objs: objs,
    }

    // Create ring buffer reader
    reader, err := ringbuf.NewReader(objs.Events)
    if err != nil {
        objs.Close()
        return nil, fmt.Errorf("failed to create ring buffer reader: %w", err)
    }
    monitor.reader = reader

    return monitor, nil
}

// Attach attaches the eBPF program to the tracepoint
func (h *HelloMonitor) Attach() error {
    // Attach to syscalls/sys_enter_openat tracepoint
    l, err := link.Tracepoint("syscalls", "sys_enter_openat", h.objs.HelloWorld, nil)
    if err != nil {
        return fmt.Errorf("failed to attach tracepoint: %w", err)
    }
    h.link = l

    log.Println("eBPF program attached to syscalls/sys_enter_openat")
    return nil
}

// ReadEvents reads events from the ring buffer
func (h *HelloMonitor) ReadEvents(ctx context.Context) (<-chan *Event, <-chan error) {
    events := make(chan *Event, 100)
    errors := make(chan error, 10)

    go func() {
        defer close(events)
        defer close(errors)

        for {
            select {
            case <-ctx.Done():
                return
            default:
                record, err := h.reader.Read()
                if err != nil {
                    errors <- fmt.Errorf("failed to read from ring buffer: %w", err)
                    continue
                }

                event, err := h.parseEvent(record.RawSample)
                if err != nil {
                    errors <- fmt.Errorf("failed to parse event: %w", err)
                    continue
                }

                select {
                case events <- event:
                case <-ctx.Done():
                    return
                }
            }
        }
    }()

    return events, errors
}

// parseEvent parses a raw event from the ring buffer
func (h *HelloMonitor) parseEvent(raw []byte) (*Event, error) {
    if len(raw) < 20 { // 4 bytes for PID + 16 bytes for comm
        return nil, fmt.Errorf("event too small: %d bytes", len(raw))
    }

    event := &Event{}
    event.PID = binary.LittleEndian.Uint32(raw[0:4])
    copy(event.Comm[:], raw[4:20])

    return event, nil
}

// GetCounter returns the current counter value
func (h *HelloMonitor) GetCounter() (uint64, error) {
    var key uint32 = 0
    var value uint64

    if err := h.objs.Counter.Lookup(&key, &value); err != nil {
        if err == ebpf.ErrKeyNotExist {
            return 0, nil
        }
        return 0, fmt.Errorf("failed to lookup counter: %w", err)
    }

    return value, nil
}

// Close cleans up all resources
func (h *HelloMonitor) Close() error {
    if h.link != nil {
        if err := h.link.Close(); err != nil {
            log.Printf("Failed to close link: %v", err)
        }
    }

    if h.reader != nil {
        if err := h.reader.Close(); err != nil {
            log.Printf("Failed to close ring buffer reader: %v", err)
        }
    }

    if err := h.objs.Close(); err != nil {
        return fmt.Errorf("failed to close eBPF objects: %w", err)
    }

    return nil
}