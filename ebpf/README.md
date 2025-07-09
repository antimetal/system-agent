# eBPF Programs

This directory contains eBPF programs used by the Antimetal Agent. These programs are licensed under GPL-2.0-only, as required for eBPF programs that use GPL-only kernel helper functions.

## Licensing

All files in this directory are licensed under GPL-2.0-only. This is separate from the rest of the Antimetal Agent codebase, which uses PolyForm licensing.

## Structure

- `src/` - eBPF C source files
- `include/` - eBPF header files
- `LICENSE` - GPL-2.0-only license text

## Building

eBPF programs are built separately from the main agent binary. See the main project Makefile for eBPF-specific build targets.