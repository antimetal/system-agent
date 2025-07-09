# eBPF Hello World Test Program

This is a simple test program that demonstrates the eBPF hello world example by tracing `openat()` system calls.

## Prerequisites

- Linux kernel 5.8+ (for ring buffer support)
- Root privileges (required for eBPF)
- Go 1.23+

## Building

First, generate the eBPF bindings:

```bash
# From the project root
./scripts/generate-ebpf.sh

# Or using Docker
./scripts/generate-ebpf-docker.sh
```

Then build the test program:

```bash
go build -o ebpf-hello ./cmd/ebpf-hello
```

## Running

The program must be run as root:

```bash
sudo ./ebpf-hello
```

You should see output like:

```
eBPF Hello World - Tracing openat() system calls
Press Ctrl+C to exit

[PID: 1234, UID: 1000] opened: /etc/passwd
[PID: 5678, UID: 0] opened: /var/log/syslog
...
```

The program will display every file that is opened on the system while it's running.

## Testing

To generate some events, you can:

1. Open another terminal and run commands that open files:
   ```bash
   cat /etc/hosts
   ls /tmp
   find /etc -name "*.conf" | head -5
   ```

2. The test program should show these file accesses in real-time.

## Troubleshooting

If you get an error about:
- "operation not permitted" - Make sure you're running as root
- "failed to load eBPF objects" - Ensure the eBPF bindings are generated
- "unknown field" - Make sure your kernel supports ring buffers (5.8+)