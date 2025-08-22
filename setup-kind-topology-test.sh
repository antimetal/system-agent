#!/bin/bash
# Copyright Antimetal, Inc. All rights reserved.
#
# Use of this source code is governed by a source available license that can be found in the
# LICENSE file or at:
# https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

set -euo pipefail

echo "=== Setting up KIND Cluster for Container-Process-Hardware Topology Testing ==="
echo ""

# Check prerequisites
check_prerequisites() {
    echo "🔍 Checking prerequisites..."
    
    # Check for KIND
    if ! command -v kind &> /dev/null; then
        echo "❌ KIND is not installed. Please install KIND first:"
        echo "   https://kind.sigs.k8s.io/docs/user/quick-start/#installation"
        exit 1
    fi
    echo "✅ KIND found: $(kind version)"
    
    # Check for kubectl
    if ! command -v kubectl &> /dev/null; then
        echo "❌ kubectl is not installed. Please install kubectl first:"
        echo "   https://kubernetes.io/docs/tasks/tools/install-kubectl/"
        exit 1
    fi
    echo "✅ kubectl found: $(kubectl version --client -o yaml | grep gitVersion | cut -d':' -f2 | tr -d ' ')"
    
    # Check for Docker
    if ! command -v docker &> /dev/null; then
        echo "❌ Docker is not installed or not accessible"
        exit 1
    fi
    echo "✅ Docker found: $(docker version --format '{{.Server.Version}}' 2>/dev/null || echo 'accessible')"
    
    echo ""
}

# Create KIND cluster
create_cluster() {
    echo "🏗️  Creating KIND cluster for topology testing..."
    
    # Check if cluster already exists
    if kind get clusters | grep -q "antimetal-topology-test"; then
        echo "⚠️  Cluster 'antimetal-topology-test' already exists"
        read -p "Do you want to delete and recreate it? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            echo "🗑️  Deleting existing cluster..."
            kind delete cluster --name antimetal-topology-test
        else
            echo "Using existing cluster"
            return 0
        fi
    fi
    
    # Create cluster with configuration
    echo "Creating cluster with custom configuration..."
    if kind create cluster --config kind-topology-test.yaml; then
        echo "✅ KIND cluster created successfully"
    else
        echo "❌ Failed to create KIND cluster"
        exit 1
    fi
    
    echo ""
}

# Deploy test workloads
deploy_test_workloads() {
    echo "🚀 Deploying test workloads for container discovery..."
    
    # Create test namespace
    kubectl create namespace antimetal-test --dry-run=client -o yaml | kubectl apply -f -
    
    # Deploy various workload types for testing
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-cpu-workload
  namespace: antimetal-test
spec:
  replicas: 2
  selector:
    matchLabels:
      app: test-cpu-workload
  template:
    metadata:
      labels:
        app: test-cpu-workload
    spec:
      containers:
      - name: cpu-worker
        image: alpine:latest
        command: ["/bin/sh"]
        args: ["-c", "while true; do echo 'CPU work'; sleep 10; done"]
        resources:
          requests:
            cpu: 100m
            memory: 64Mi
          limits:
            cpu: 200m
            memory: 128Mi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-memory-workload
  namespace: antimetal-test
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test-memory-workload
  template:
    metadata:
      labels:
        app: test-memory-workload
    spec:
      containers:
      - name: memory-worker
        image: alpine:latest
        command: ["/bin/sh"]
        args: ["-c", "while true; do echo 'Memory work'; sleep 15; done"]
        resources:
          requests:
            cpu: 50m
            memory: 128Mi
          limits:
            cpu: 100m
            memory: 256Mi
---
apiVersion: v1
kind: Pod
metadata:
  name: test-privileged-pod
  namespace: antimetal-test
spec:
  containers:
  - name: privileged-container
    image: alpine:latest
    command: ["/bin/sh"]
    args: ["-c", "while true; do echo 'Privileged work'; sleep 20; done"]
    securityContext:
      privileged: true
    volumeMounts:
    - name: host-proc
      mountPath: /host/proc
      readOnly: true
    - name: host-sys
      mountPath: /host/sys
      readOnly: true
  volumes:
  - name: host-proc
    hostPath:
      path: /proc
  - name: host-sys
    hostPath:
      path: /sys
EOF
    
    echo "✅ Test workloads deployed"
    echo ""
}

# Wait for workloads to be ready
wait_for_workloads() {
    echo "⏳ Waiting for test workloads to be ready..."
    
    # Wait for deployments
    kubectl wait --for=condition=available --timeout=120s deployment/test-cpu-workload -n antimetal-test
    kubectl wait --for=condition=available --timeout=120s deployment/test-memory-workload -n antimetal-test
    
    # Wait for pod
    kubectl wait --for=condition=ready --timeout=120s pod/test-privileged-pod -n antimetal-test
    
    echo "✅ All workloads are ready"
    echo ""
}

# Deploy antimetal agent for testing
deploy_antimetal_agent() {
    echo "🤖 Deploying Antimetal Agent for topology testing..."
    
    # Create ConfigMap with test configuration
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: antimetal-agent-config
  namespace: antimetal-test
data:
  config.yaml: |
    # Antimetal Agent test configuration
    runtime:
      update_interval: 10s
      cgroup_path: /host/sys/fs/cgroup
    hardware:
      update_interval: 30s
    performance:
      host_proc_path: /host/proc
      host_sys_path: /host/sys
      host_dev_path: /host/dev
EOF
    
    # Deploy agent as DaemonSet for testing
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: antimetal-agent
  namespace: antimetal-test
spec:
  selector:
    matchLabels:
      app: antimetal-agent
  template:
    metadata:
      labels:
        app: antimetal-agent
    spec:
      hostNetwork: true
      hostPID: true
      containers:
      - name: agent
        image: alpine:latest
        command: ["/bin/sh"]
        args: ["-c", "echo 'Antimetal Agent placeholder - copy actual binary here'; while true; do sleep 60; done"]
        securityContext:
          privileged: true
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        volumeMounts:
        - name: host-proc
          mountPath: /host/proc
          readOnly: true
        - name: host-sys
          mountPath: /host/sys
          readOnly: true
        - name: host-dev
          mountPath: /host/dev
          readOnly: true
        - name: host-cgroup
          mountPath: /host/sys/fs/cgroup
          readOnly: true
        - name: host-docker
          mountPath: /host/var/lib/docker
          readOnly: true
        - name: host-containerd
          mountPath: /host/run/containerd
          readOnly: true
        - name: config
          mountPath: /etc/antimetal
      volumes:
      - name: host-proc
        hostPath:
          path: /proc
      - name: host-sys
        hostPath:
          path: /sys
      - name: host-dev
        hostPath:
          path: /dev
      - name: host-cgroup
        hostPath:
          path: /sys/fs/cgroup
      - name: host-docker
        hostPath:
          path: /var/lib/docker
      - name: host-containerd
        hostPath:
          path: /run/containerd
      - name: config
        configMap:
          name: antimetal-agent-config
      tolerations:
      - operator: Exists
        effect: NoSchedule
EOF
    
    echo "✅ Antimetal Agent DaemonSet deployed (placeholder)"
    echo ""
}

# Show cluster information
show_cluster_info() {
    echo "📊 KIND Cluster Information:"
    echo "============================"
    echo ""
    
    # Cluster info
    echo "🏗️  Cluster Nodes:"
    kubectl get nodes -o wide
    echo ""
    
    # Test workloads
    echo "🚀 Test Workloads:"
    kubectl get all -n antimetal-test
    echo ""
    
    # Container runtime info
    echo "🐳 Container Runtime Information:"
    kubectl get nodes -o jsonpath='{.items[*].status.nodeInfo.containerRuntimeVersion}' | tr ' ' '\n' | sort | uniq
    echo ""
    
    # Cgroup information
    echo "📁 Cgroup Information (from control-plane node):"
    if kubectl exec -n kube-system deployment/coredns -- test -f /host/sys/fs/cgroup/cgroup.controllers 2>/dev/null; then
        echo "   ✅ Cgroup v2 detected"
    elif kubectl exec -n kube-system deployment/coredns -- test -d /host/sys/fs/cgroup/memory 2>/dev/null; then
        echo "   ✅ Cgroup v1 detected"
    else
        echo "   ⚠️  Cgroup version unclear"
    fi
    echo ""
}

# Provide testing instructions
show_testing_instructions() {
    echo "🧪 Testing Instructions:"
    echo "========================"
    echo ""
    echo "1. Copy your test binaries to a cluster node:"
    echo "   kubectl cp /path/to/topology-test-amd64 antimetal-test/antimetal-agent-<pod-id>:/tmp/"
    echo ""
    echo "2. Execute topology tests in the agent pod:"
    echo "   kubectl exec -it -n antimetal-test daemonset/antimetal-agent -- /tmp/topology-test-amd64"
    echo ""
    echo "3. Check container discovery:"
    echo "   kubectl exec -it -n antimetal-test daemonset/antimetal-agent -- ls -la /host/var/lib/docker/containers/"
    echo "   kubectl exec -it -n antimetal-test daemonset/antimetal-agent -- ls -la /host/run/containerd/"
    echo ""
    echo "4. Validate cgroup access:"
    echo "   kubectl exec -it -n antimetal-test daemonset/antimetal-agent -- ls -la /host/sys/fs/cgroup/"
    echo ""
    echo "5. Monitor test workloads:"
    echo "   kubectl top pods -n antimetal-test"
    echo "   kubectl logs -f -n antimetal-test deployment/test-cpu-workload"
    echo ""
    echo "6. Clean up when done:"
    echo "   kind delete cluster --name antimetal-topology-test"
    echo ""
}

# Main execution
main() {
    check_prerequisites
    create_cluster
    
    # Wait a moment for cluster to be fully ready
    echo "⏳ Waiting for cluster to be fully ready..."
    sleep 10
    
    deploy_test_workloads
    wait_for_workloads
    deploy_antimetal_agent
    
    show_cluster_info
    show_testing_instructions
    
    echo "✅ KIND cluster setup completed successfully!"
    echo "Cluster name: antimetal-topology-test"
    echo "Namespace: antimetal-test"
}

# Run main function if script is executed directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi