#!/usr/bin/env bash

# test-cgroup-collectors.sh - Test cgroup CPU and memory collectors in KIND cluster
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
CLUSTER_NAME="${KIND_CLUSTER:-antimetal-agent-dev}"
NAMESPACE="antimetal-system"
TEST_NAMESPACE="default"
WORKLOAD_FILE="test/cgroup-test-workloads.yaml"

# Use local binaries if not in PATH
KIND="${KIND:-./bin/kind}"
KUBECTL="${KUBECTL:-kubectl}"
KUBECTL_CONTEXT="--context kind-${CLUSTER_NAME}"

# Helper functions
info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

header() {
    echo -e "\n${GREEN}========================================${NC}"
    echo -e "${GREEN}$1${NC}"
    echo -e "${GREEN}========================================${NC}\n"
}

# Check prerequisites
check_prerequisites() {
    header "Checking Prerequisites"
    
    # Check if KIND cluster exists
    if ! ${KIND} get clusters | grep -q "^${CLUSTER_NAME}$"; then
        error "KIND cluster '${CLUSTER_NAME}' not found. Run 'make cluster' first."
    fi
    success "KIND cluster '${CLUSTER_NAME}' exists"
    
    # Check if ${KUBECTL} context is set correctly
    if ! ${KUBECTL} config current-context | grep -q "kind-${CLUSTER_NAME}"; then
        warning "Setting ${KUBECTL} context to kind-${CLUSTER_NAME}"
        ${KUBECTL} ${KUBECTL_CONTEXT} config use-context "kind-${CLUSTER_NAME}"
    fi
    success "${KUBECTL} context set to kind-${CLUSTER_NAME}"
    
    # Check if agent is deployed
    if ! ${KUBECTL} get deployment -n ${NAMESPACE} agent &>/dev/null; then
        warning "Agent not deployed. Will deploy it now."
    fi
    success "Agent deployment found"
}

# Build and deploy agent with cgroup support
deploy_agent() {
    header "Building and Deploying Agent with Cgroup Support"
    
    info "Generating manifests..."
    make generate
    
    info "Building Docker image..."
    make docker-build
    
    info "Loading image into KIND cluster..."
    make load-image
    
    info "Deploying agent..."
    make deploy
    
    info "Waiting for agent to be ready..."
    ${KUBECTL} ${KUBECTL_CONTEXT} rollout status deployment/agent -n ${NAMESPACE} --timeout=60s
    
    success "Agent deployed successfully"
}

# Deploy test workloads
deploy_workloads() {
    header "Deploying Test Workloads"
    
    if [ ! -f "${WORKLOAD_FILE}" ]; then
        error "Workload file ${WORKLOAD_FILE} not found"
    fi
    
    info "Applying test workloads..."
    ${KUBECTL} ${KUBECTL_CONTEXT} apply -f "${WORKLOAD_FILE}"
    
    info "Waiting for pods to start..."
    sleep 10
    
    # Check pod status
    ${KUBECTL} ${KUBECTL_CONTEXT} get pods -l 'test' -o wide
    
    success "Test workloads deployed"
}

# Monitor cgroup collectors
monitor_collectors() {
    header "Monitoring Cgroup Collectors"
    
    info "Checking agent logs for cgroup detection..."
    echo "----------------------------------------"
    
    # Check for cgroup version detection
    info "Cgroup version detection:"
    ${KUBECTL} ${KUBECTL_CONTEXT} logs -n ${NAMESPACE} deployment/agent --tail=1000 | grep -i "cgroup version" || warning "No cgroup version detection found"
    
    echo ""
    info "Container discovery:"
    ${KUBECTL} ${KUBECTL_CONTEXT} logs -n ${NAMESPACE} deployment/agent --tail=1000 | grep -i "discovered.*container\|container.*discovered" || warning "No container discovery messages found"
    
    echo ""
    info "CPU throttling detection:"
    ${KUBECTL} ${KUBECTL_CONTEXT} logs -n ${NAMESPACE} deployment/agent --tail=1000 | grep -i "throttl" || warning "No throttling messages found yet"
    
    echo ""
    info "Memory statistics:"
    ${KUBECTL} ${KUBECTL_CONTEXT} logs -n ${NAMESPACE} deployment/agent --tail=1000 | grep -i "memory.*stats\|cgroup.*memory" || warning "No memory statistics found yet"
}

# Verify cgroup mounts in agent pod
verify_mounts() {
    header "Verifying Cgroup Mounts in Agent Pod"
    
    POD=$(${KUBECTL} get pods -n ${NAMESPACE} -l app.kubernetes.io/name=agent -o jsonpath='{.items[0].metadata.name}')
    
    info "Agent pod: ${POD}"
    
    info "Checking /host/cgroup mount..."
    ${KUBECTL} ${KUBECTL_CONTEXT} exec -n ${NAMESPACE} ${POD} -- ls -la /host/cgroup/ | head -10
    
    info "Checking cgroup version..."
    if ${KUBECTL} exec -n ${NAMESPACE} ${POD} -- test -f /host/cgroup/cgroup.controllers; then
        success "Cgroup v2 detected"
        ${KUBECTL} ${KUBECTL_CONTEXT} exec -n ${NAMESPACE} ${POD} -- cat /host/cgroup/cgroup.controllers
    elif ${KUBECTL} exec -n ${NAMESPACE} ${POD} -- test -d /host/cgroup/cpu; then
        success "Cgroup v1 detected"
        ${KUBECTL} ${KUBECTL_CONTEXT} exec -n ${NAMESPACE} ${POD} -- ls /host/cgroup/
    else
        error "Unable to detect cgroup version"
    fi
}

# Check specific container metrics
check_container_metrics() {
    header "Checking Container Metrics"
    
    POD=$(${KUBECTL} get pods -n ${NAMESPACE} -l app.kubernetes.io/name=agent -o jsonpath='{.items[0].metadata.name}')
    
    # Get container IDs for our test workloads
    info "Finding test container cgroups..."
    
    for pod in cpu-stress-test memory-stress-test bursty-workload; do
        if ${KUBECTL} get pod ${pod} &>/dev/null; then
            info "Checking ${pod}..."
            
            # Try to find the container in cgroups
            CONTAINER_ID=$(${KUBECTL} get pod ${pod} -o jsonpath='{.status.containerStatuses[0].containerID}' | cut -d'/' -f3 | cut -c1-12)
            
            if [ -n "${CONTAINER_ID}" ]; then
                info "  Container ID: ${CONTAINER_ID}"
                
                # Check if we can find it in cgroups (v1 or v2)
                ${KUBECTL} ${KUBECTL_CONTEXT} exec -n ${NAMESPACE} ${POD} -- sh -c "find /host/cgroup -name '*${CONTAINER_ID}*' -type d 2>/dev/null | head -5" || true
            fi
        fi
    done
}

# Stress test - generate load
stress_test() {
    header "Running Stress Test (30 seconds)"
    
    info "Current pod status:"
    ${KUBECTL} ${KUBECTL_CONTEXT} get pods -l 'test' --no-headers
    
    info "Waiting for workloads to generate metrics..."
    for i in {1..6}; do
        echo -n "."
        sleep 5
    done
    echo ""
    
    info "Checking agent logs for collector activity..."
    ${KUBECTL} ${KUBECTL_CONTEXT} logs -n ${NAMESPACE} deployment/agent --tail=100 | grep -i "cgroup\|throttl\|container" | tail -20 || true
}

# Enable verbose logging
enable_verbose_logging() {
    header "Enabling Verbose Logging"
    
    info "Setting verbosity level to 2..."
    ${KUBECTL} ${KUBECTL_CONTEXT} set env deployment/agent -n ${NAMESPACE} VERBOSITY=2
    
    info "Waiting for pod to restart..."
    ${KUBECTL} ${KUBECTL_CONTEXT} rollout status deployment/agent -n ${NAMESPACE} --timeout=60s
    
    success "Verbose logging enabled"
}

# Cleanup
cleanup() {
    header "Cleaning Up Test Resources"
    
    info "Deleting test workloads..."
    ${KUBECTL} ${KUBECTL_CONTEXT} delete -f "${WORKLOAD_FILE}" --ignore-not-found=true
    
    info "Resetting verbosity..."
    ${KUBECTL} ${KUBECTL_CONTEXT} set env deployment/agent -n ${NAMESPACE} VERBOSITY-
    
    success "Cleanup complete"
}

# Main test flow
main() {
    echo -e "${GREEN}═══════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}     Cgroup Collector Testing for System Agent${NC}"
    echo -e "${GREEN}═══════════════════════════════════════════════════════${NC}"
    
    # Parse arguments
    case "${1:-all}" in
        prereq)
            check_prerequisites
            ;;
        deploy)
            check_prerequisites
            deploy_agent
            ;;
        workloads)
            check_prerequisites
            deploy_workloads
            ;;
        monitor)
            check_prerequisites
            monitor_collectors
            ;;
        verify)
            check_prerequisites
            verify_mounts
            ;;
        metrics)
            check_prerequisites
            check_container_metrics
            ;;
        stress)
            check_prerequisites
            stress_test
            ;;
        verbose)
            check_prerequisites
            enable_verbose_logging
            ;;
        cleanup)
            cleanup
            ;;
        all)
            check_prerequisites
            deploy_agent
            deploy_workloads
            sleep 5
            verify_mounts
            sleep 5
            monitor_collectors
            check_container_metrics
            stress_test
            info "Run './scripts/test-cgroup-collectors.sh cleanup' when done"
            ;;
        *)
            echo "Usage: $0 [prereq|deploy|workloads|monitor|verify|metrics|stress|verbose|cleanup|all]"
            echo ""
            echo "Commands:"
            echo "  prereq    - Check prerequisites"
            echo "  deploy    - Build and deploy agent"
            echo "  workloads - Deploy test workloads"
            echo "  monitor   - Monitor collector logs"
            echo "  verify    - Verify cgroup mounts"
            echo "  metrics   - Check container metrics"
            echo "  stress    - Run stress test"
            echo "  verbose   - Enable verbose logging"
            echo "  cleanup   - Remove test resources"
            echo "  all       - Run all tests (default)"
            exit 1
            ;;
    esac
    
    echo -e "\n${GREEN}Test execution completed!${NC}"
}

# Run main function
main "$@"