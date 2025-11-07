// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

// Package environment provides utilities for extracting configuration from environment variables
package environment

import (
	"os"

	"k8s.io/apimachinery/pkg/util/validation"
)

// GetNodeName returns the node name from NODE_NAME environment variable,
// falling back to hostname if not set.
func GetNodeName() (string, error) {
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return "", err
		}
		nodeName = hostname
	}
	return nodeName, nil
}

// GetClusterName returns the cluster name from CLUSTER_NAME environment variable.
// Returns empty string if not set.
func GetClusterName() string {
	return os.Getenv("CLUSTER_NAME")
}

// PodMetadata contains Kubernetes pod metadata from downward API
type PodMetadata struct {
	Name      string // Pod name
	Namespace string // Pod namespace
	UID       string // Pod UID
}

// GetPodMetadata returns pod metadata from environment variables set by Kubernetes downward API.
// Returns nil if POD_NAME is not set or if metadata fails validation.
func GetPodMetadata() *PodMetadata {
	podName := os.Getenv("POD_NAME")
	if podName == "" {
		return nil
	}

	// Validate pod name follows Kubernetes DNS-1123 label format
	if errs := validation.IsDNS1123Label(podName); len(errs) > 0 {
		return nil
	}

	namespace := os.Getenv("POD_NAMESPACE")
	// Validate namespace if provided
	if namespace != "" {
		if errs := validation.IsDNS1123Label(namespace); len(errs) > 0 {
			return nil
		}
	}

	return &PodMetadata{
		Name:      podName,
		Namespace: namespace,
		UID:       os.Getenv("POD_UID"),
	}
}

// HostPaths contains the host filesystem paths for containerized environments
type HostPaths struct {
	Proc string // Path to /proc (e.g., /host/proc in containers)
	Sys  string // Path to /sys (e.g., /host/sys in containers)
	Dev  string // Path to /dev (e.g., /host/dev in containers)
	Etc  string // Path to /etc (e.g., /host/etc in containers)
	Var  string // Path to /var (e.g., /host/var in containers)
}

// GetHostPaths returns the host filesystem paths from environment variables,
// with defaults if not set.
func GetHostPaths() HostPaths {
	paths := HostPaths{
		Proc: "/proc",
		Sys:  "/sys",
		Dev:  "/dev",
		Etc:  "/etc",
		Var:  "/var",
	}

	if procPath := os.Getenv("HOST_PROC"); procPath != "" {
		paths.Proc = procPath
	}
	if sysPath := os.Getenv("HOST_SYS"); sysPath != "" {
		paths.Sys = sysPath
	}
	if devPath := os.Getenv("HOST_DEV"); devPath != "" {
		paths.Dev = devPath
	}
	if etcPath := os.Getenv("HOST_ETC"); etcPath != "" {
		paths.Etc = etcPath
	}
	if varPath := os.Getenv("HOST_VAR"); varPath != "" {
		paths.Var = varPath
	}

	return paths
}
