//go:build tools

package tools

import (
	_ "github.com/cilium/ebpf/cmd/bpf2go"
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
)