# eBPF Programs

This directory contains eBPF programs that run in Linux kernel space.

## Licensing

All eBPF programs in this directory are dual-licensed under:
- GNU General Public License v2 (GPL-2.0)
- BSD 2-Clause License

This dual licensing is **required** because:
1. eBPF programs must be GPL-compatible to load into the Linux kernel
2. The BSD option allows permissive use in other contexts

## Important Note

These eBPF programs are licensed **separately** from the main project.
The userspace code that loads these programs is under the PolyForm Shield License.

## Structure

```
ebpf/
├── LICENSE           # Full license text
├── README.md        # This file
├── headers/         # Shared headers
└── programs/        # eBPF programs (*.bpf.c)
```

## Development

When adding new eBPF programs:
1. Include the SPDX identifier: `// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)`
2. Add copyright header: `// Copyright (c) 2024 Antimetal`
3. Set license in code: `char LICENSE[] SEC("license") = "Dual BSD/GPL";`