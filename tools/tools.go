//go:build tools
// +build tools

package tools

import (
	// eBPF code generation
	_ "github.com/cilium/ebpf/cmd/bpf2go"
)