SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

ROOT = $(shell git rev-parse --show-toplevel 2>/dev/null || pwd)
## Location to install dependencies to
LOCALBIN ?= $(ROOT)/bin
## Location to store build & release artifacts
DIST ?= $(ROOT)/dist

# System environment
OS ?= $(shell uname -s)

# Golang environment
GO_OS ?= linux
GOARCH ?= $(shell go env GOARCH)
GO_TOOLCHAIN ?= $(shell grep -oE "^toolchain go[[:digit:]]*\.[[:digit:]]*\.+[[:digit:]]*" go.mod | cut -d ' ' -f2)

# Image URL to use all building/pushing image targets
IMG ?= antimetal/agent:dev
BUILD_ARGS ?= --load

# KIND_CLUSTER defines the name to use when creating KIND clusters.
KIND_CLUSTER ?= antimetal-agent-dev

# Test coverage output file
TESTCOVERAGE_OUT ?= cover.out

.PHONY: all
all: build

# Sometimes we have a file-target that we want Make to always try to
# re-generate. We could mark it as .PHONY, but that tells Make that
# the target isn't a real file, which has a several implications for Make,
# most of which we don't want.  Instead, we can have them "depend" on a .PHONY
# target named "FORCE", so that they are always considered out-of-date by Make,
# but without being .PHONY themselves.
.PHONY: FORCE

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php
.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-].+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: clean
clean: clean-ebpf ## Removes build artifacts.
	rm -rf $(LOCALBIN)
	rm -rf $(DIST)
	rm -f $(TESTCOVERAGE_OUT)

##@ Development

.PHONY: install-hooks
install-hooks: ## Install shared git hooks for development workflow.
	@./.githooks/install.sh

.PHONY: generate
generate: ## Generate all artifacts
generate: manifests generate-ebpf-types generate-ebpf-bindings proto

.PHONY: ebpf-typegen
ebpf-typegen: ## Build the ebpf-typegen tool
	@echo "Building ebpf-typegen tool..."
	@mkdir -p $(LOCALBIN)
	@go build -o $(LOCALBIN)/ebpf-typegen $(ROOT)/tools/ebpf-typegen/main.go

.PHONY: generate-ebpf-types
generate-ebpf-types: ebpf-typegen ## Generate Go types from eBPF header files
	@echo "Generating Go types from eBPF headers..."
	@$(ROOT)/scripts/generate-ebpf-types.sh

.PHONY: generate-ebpf-bindings
generate-ebpf-bindings: ## Generate Go bindings from eBPF C code
	@echo "Generating Go bindings from eBPF C code..."
	go generate $(ROOT)/pkg/ebpf/...

.PHONY: gen-check
gen-check: generate ## Check if generated files are up to date.
	@trap "echo 'ERROR: Some files need to be updated, please run make generate and commit any changed files'" ERR && \
		git diff --exit-code > /dev/null

.PHONY: license-check
license-check: ## Check that source code files have the correct license header.
	@$(LICENSE_CHECK)

.PHONY: gen-license-headers
gen-license-headers: ## Generate license headers for source code files.
	@$(LICENSE_CHECK) --write

.PHONY: manifests
manifests: controller-gen ## Generate K8s objects in config/ directory.
	$(CONTROLLER_GEN) rbac:roleName=antimetal-agent-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: fmt
fmt: fmt.go ## Run all formatters

.PHONY: fmt.go
fmt.go: ## Run go fmt
	@echo "Running Go fmt..."
	@go fmt ./...

.PHONY: fmt.clang
fmt.clang: ## Run clang format
	@echo "Running clang-format on C/C++ files..."
	@command -v clang-format >/dev/null 2>&1 || { echo "Error: clang-format is required but not installed. Please install clang-format."; exit 1; }
	@find . -name "*.c" -o -name "*.h" | grep -E "(ebpf|bpf)" | while read -r file; do \
		echo "Formatting $$file"; \
		clang-format -i -style=file "$$file"; \
	done

.PHONY: vet
vet: generate ## Run go vet against code.
	go vet ./...

.PHONY: test
test: test-unit test-integration ## Run all tests (unit and integration).

.PHONY: test-unit
test-unit: generate ## Run unit tests only.
	go test ./... -v -timeout 30s

.PHONY: test-integration
test-integration: generate manifests build-ebpf ## Run integration tests.
	EBPF_BUILD_DIR=$(EBPF_BUILD_DIR) ANTIMETAL_BPF_PATH=$(EBPF_BUILD_DIR) go test -tags integration ./... -v -timeout 60s

.PHONY: lint
<<<<<<< HEAD
lint: golangci-lint generate ## Run golangci-lint linter & yamllint.
=======
lint: golangci-lint generate ## Run golangci-lint linter
>>>>>>> 819d4f1 (ci: refactor integration testing to support multi-binary architecture)
	$(GOLANGCI_LINT) run --timeout 10m

.PHONY: lint-fix
lint-fix: golangci-lint generate ## Run golangci-lint linter and perform fixes.
	$(GOLANGCI_LINT) run --fix --timeout 10m

##@ eBPF

# eBPF build configuration
EBPF_DIR ?= $(ROOT)/ebpf
EBPF_SRC_DIR ?= $(EBPF_DIR)/src
EBPF_BUILD_DIR ?= $(EBPF_DIR)/build
EBPF_INCLUDES ?= -I$(EBPF_DIR)/include -I/usr/include

# Find all eBPF source files
EBPF_SOURCES := $(wildcard $(EBPF_SRC_DIR)/*.bpf.c)
EBPF_OBJECTS := $(patsubst $(EBPF_SRC_DIR)/%.bpf.c,$(EBPF_BUILD_DIR)/%.bpf.o,$(EBPF_SOURCES))

# Clang flags for eBPF compilation
CLANG ?= clang
CLANG_FLAGS := -O2 -g -Wall -Werror -target bpf \
	-D__TARGET_ARCH_$(GOARCH) \
	-D__BPF_TRACING__ \
	$(EBPF_INCLUDES)

# Define eBPF builder based on platform
ifneq ($(shell uname -s),Linux)
$(info ebpf-builder=docker)
EBPF_BUILDER=docker
else
$(info ebpf-builder=native)
EBPF_BUILDER=native
endif

.PHONY: build-ebpf
build-ebpf: build-ebpf-$(EBPF_BUILDER) ## Build all eBPF programs

.PHONY: build-ebpf-native
build-ebpf-native: generate-vmlinux $(EBPF_BUILD_DIR) $(EBPF_OBJECTS) ## Build eBPF programs natively (Linux only)

.PHONY: generate-vmlinux
generate-vmlinux: ## Generate vmlinux.h for eBPF compilation
	@echo "Checking/generating vmlinux.h..."
	@$(EBPF_DIR)/scripts/generate_vmlinux.sh

$(EBPF_BUILD_DIR):
	@mkdir -p $(EBPF_BUILD_DIR)

$(EBPF_BUILD_DIR)/%.bpf.o: $(EBPF_SRC_DIR)/%.bpf.c
	@echo "Building eBPF program: $<"
	$(CLANG) $(CLANG_FLAGS) -c $< -o $@

.PHONY: build-ebpf-docker
build-ebpf-docker: ## Build eBPF programs using Docker (for consistent build environment)
	@echo "Building eBPF programs in Docker..."
	@if ! docker images antimetal/ebpf-builder --format "{{.Repository}}" | grep -q "antimetal/ebpf-builder"; then \
		echo "Docker image not found, building antimetal/ebpf-builder..."; \
		docker build -t antimetal/ebpf-builder -f $(EBPF_DIR)/Dockerfile.builder $(EBPF_DIR); \
	else \
		echo "Using existing antimetal/ebpf-builder image"; \
	fi
	@docker run --rm -v $(ROOT):/workspace -w /workspace antimetal/ebpf-builder make build-ebpf-native

.PHONY: verify-ebpf
verify-ebpf: build-ebpf ## Load & verify all ebpf programs
ifneq ($(OS),Linux)
	@echo "Verify target is only available on Linux"
	@exit 1
else
	@command -v bpftool >/dev/null 2>&1 || { echo "bpftool not found. For some reason it was not included in your kernel build so you probably need to build it from source: https://github.com/libbpf/bpftool"; exit 1; }
	@echo "Verifying eBPF programs"
	@for prog in $(EBPF_OBJECTS); do \
		echo "Verifying $$prog"; \
		prog_name=`basename $$prog`; \
		sudo bpftool prog load $$prog "/sys/fs/bpf/$${prog_name%%.*}_test" || exit 1; \
		sudo rm "/sys/fs/bpf/$${prog_name%%.*}_test"; \
	done
	@echo "All programs verified successfully"
endif

.PHONY: build-ebpf-builder
build-ebpf-builder: ## Force rebuild the eBPF builder Docker image
	@echo "Building antimetal/ebpf-builder Docker image..."
	@docker build -t antimetal/ebpf-builder -f $(EBPF_DIR)/Dockerfile.builder $(EBPF_DIR)

.PHONY: clean-ebpf
clean-ebpf: ## Clean eBPF build artifacts
	rm -rf $(EBPF_BUILD_DIR)
	rm -f $(EBPF_DIR)/include/vmlinux.h

##@ Build

build: ## Build agent binary for current GOOS and GOARCH.
build: goreleaser manifests generate build-ebpf
	GOOS=$(GO_OS) $(GORELEASER) build --snapshot --clean --single-target

.PHONY: build-all
build-all: goreleaser manifests build-ebpf ## Build agent binary for all platforms.
	$(GORELEASER) build --snapshot --clean

.PHONY: docker.builder
docker.builder:
	- docker buildx create --name antimetal-agent-builder 2> /dev/null || true
	docker buildx use antimetal-agent-builder

.PHONY: docker-build
docker-build: docker.builder build ## Build docker image for current GOOS and GOARCH.
	DOCKER_BUILDKIT=1 docker buildx build \
		--platform ${GO_OS}/${GOARCH} \
		-t ${IMG} \
		-f $(ROOT)/Dockerfile \
		${BUILD_ARGS} \
		$(DIST)

.PHONY: docker-build-all
docker-build-all: docker.builder build-all ## Build docker image for all platforms.
	DOCKER_BUILDKIT=1 docker buildx build \
	--platform linux/amd64,linux/arm64 \
	-t ${IMG} \
	-f $(ROOT)/Dockerfile \
	${BUILD_ARGS} \
	$(DIST)

.PHONY: docker-push
docker-push: ## Push docker image.
	DOCKER_BUILDKIT=1 docker push ${IMG}

.PHONY: docker-build-and-push
docker-build-and-push: docker-build-all docker-push ## Build and push docker image.

##@ Protobuf

.PHONY: proto
proto: buf ## Generate protobuf files.
	$(BUF) generate

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: deploy
deploy: manifests kustomize ## Deploy agent to the K8s cluster specified in the current context in ~/.kube/config.
	@mkdir -p $(ROOT)/tmp && cp -r $(ROOT)/config $(ROOT)/tmp
	@cd $(ROOT)/tmp/config/default && \
		$(KUSTOMIZE) edit set image agent=$(IMG) && \
		kubectl apply -k .
	@rm -r $(ROOT)/tmp

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in the current context in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	@mkdir -p $(ROOT)/tmp && cp -r $(ROOT)/config $(ROOT)/tmp
	@cd $(ROOT)/tmp/config/default && \
		$(KUSTOMIZE) edit set image amagent=$(IMG) && \
		kubectl delete --ignore-not-found -k .
	@rm -r $(ROOT)/tmp

.PHONY: preview-deploy
preview-deploy: manifests kustomize ## Generate a consolidated YAML for deployment.
	@mkdir -p $(ROOT)/tmp && cp -r $(ROOT)/config $(ROOT)/tmp
	@cd $(ROOT)/tmp/config/default && $(KUSTOMIZE) edit set image agent=$(IMG)
	$(KUSTOMIZE) build $(ROOT)/tmp/config/default
	@rm -r $(ROOT)/tmp

.PHONY: cluster
cluster: ktf kustomize ## Build a KIND cluster which can be used for testing and development.
	PATH="$(LOCALBIN):${PATH}" $(KTF) env create --name $(KIND_CLUSTER) --addon metallb

.PHONY: delete-cluster
destroy-cluster: ktf ## Delete the KIND cluster.
	PATH="$(LOCALBIN):${PATH}" $(KTF) env delete --name $(KIND_CLUSTER)

.PHONY: load.image
load-image: kind ## Loads Docker image into KIND cluster and restarts agent for new image if it exists.
	$(KIND) load docker-image $(IMG) --name $(KIND_CLUSTER)
	$(KUBECTL) -n antimetal-system rollout restart deployment agent >/dev/null 2>&1 || true

.PHONY: build-and-load-image
build-and-load-image: docker-build load-image ## Builds and loads Docker image into KIND cluster.

##@ GitHub Actions Testing

.PHONY: test-actions
test-actions: ## Test GitHub Actions locally with act (requires act to be installed)
	@command -v act >/dev/null 2>&1 || { echo "act is not installed. See installation instructions at: https://github.com/nektos/act#installation"; exit 1; }
	@echo "Available workflows and jobs:"
	@act -l

.PHONY: test-claude-review
test-claude-review: ## Test Claude code review workflow locally
	@command -v act >/dev/null 2>&1 || { echo "act is not installed. See installation instructions at: https://github.com/nektos/act#installation"; exit 1; }
	@if [ ! -f .secrets ]; then \
		echo "Creating .secrets file template..."; \
		echo "ANTHROPIC_API_KEY=your-api-key-here" > .secrets; \
		echo "Please update .secrets with your actual API key"; \
		exit 1; \
	fi
	act pull_request -j claude-review --secret-file .secrets

.PHONY: test-linear-sync
test-linear-sync: ## Test Linear sync workflow locally
	@command -v act >/dev/null 2>&1 || { echo "act is not installed. See installation instructions at: https://github.com/nektos/act#installation"; exit 1; }
	@if [ ! -f .secrets ]; then \
		echo "Creating .secrets file template..."; \
		echo "LINEAR_API_TOKEN=your-token-here" > .secrets; \
		echo "Please update .secrets with your actual API token"; \
		exit 1; \
	fi
	@if [ ! -d test-events ]; then \
		echo "Creating test-events directory with examples..."; \
		mkdir -p test-events; \
		echo '{"action":"opened","issue":{"number":1,"title":"Test Issue","body":"Test body","html_url":"https://github.com/antimetal/system-agent/issues/1"}}' > test-events/issue-opened.json; \
	fi
	@if [ ! -f .vars ]; then \
		echo "LINEAR_TEAM_ID=your-team-id-here" > .vars; \
		echo "Please update .vars with your actual Linear team ID"; \
		exit 1; \
	fi
	act issues -j create-linear-issue --secret-file .secrets --var-file .vars --eventpath test-events/issue-opened.json

.PHONY: test-actions-dry
test-actions-dry: ## Dry run of GitHub Actions (show what would be executed)
	@command -v act >/dev/null 2>&1 || { echo "act is not installed. See installation instructions at: https://github.com/nektos/act#installation"; exit 1; }
	act -n -l

##@ Release

.PHONY: preview-release
preview-release: goreleaser lint ## Generate a release tarball.
	$(GORELEASER) release --clean --fail-fast --snapshot --skip publish

.PHONY: release
release: goreleaser lint ## Create a new release.
	$(GORELEASER) release --clean --fail-fast

##@ Dependencies

## Tool Binaries
BUF ?= $(LOCALBIN)/buf
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
GORELEASER ?= $(LOCALBIN)/goreleaser
KIND ?= $(LOCALBIN)/kind
KTF ?= $(LOCALBIN)/ktf
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
LICENSE_CHECK ?= tools/license_check/license_check.py

## Tool Versions
BUF_VERSION ?= v1.55.1
CONTROLLER_TOOLS_VERSION ?= v0.17.0
GOLANGCI_LINT_VERSION ?= v1.63.4
GORELEASER_VERSION ?= v2.10.2
KIND_VERSION ?= v0.26.0
KTF_VERSION ?= v0.47.2
KUSTOMIZE_VERSION ?= v5.6.0

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of installed binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@set -e; { \
	binary=$(1)@$(3) ;\
	if [ ! -f $${binary} ]; then \
		package=$(2)@$(3) ;\
		echo "Downloading $${package}" ;\
		GOBIN=$$(dirname $(1)) GOTOOLCHAIN=$(GO_TOOLCHAIN)+auto go install $${package} ;\
		mv $(1) $(1)@$(3) ;\
	fi ;\
}
endef

.PHONY: tools
tools: ## Download all tool dependencies if neccessary.
tools: controller-gen golangci-lint kind ktf kustomize goreleaser buf

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(CONTROLLER_GEN)@$(CONTROLLER_TOOLS_VERSION) FORCE
	@ln -sf $< $@
$(CONTROLLER_GEN)@$(CONTROLLER_TOOLS_VERSION):
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(GOLANGCI_LINT)@$(GOLANGCI_LINT_VERSION) FORCE
	@ln -sf $< $@
$(GOLANGCI_LINT)@$(GOLANGCI_LINT_VERSION):
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint,${GOLANGCI_LINT_VERSION})

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary.
$(KIND): $(KIND)@$(KIND_VERSION) FORCE
	@ln -sf $< $@
$(KIND)@$(KIND_VERSION):
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind,$(KIND_VERSION))

.PHONY: ktf
ktf: $(KTF) kind ## Download kubernetes-testing-framework locally if necessary.
$(KTF): $(KTF)@$(KTF_VERSION) FORCE
	@ln -sf $< $@
$(KTF)@$(KTF_VERSION):
	$(call go-install-tool,$(KTF),github.com/kong/kubernetes-testing-framework/cmd/ktf,$(KTF_VERSION))

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(KUSTOMIZE)@$(KUSTOMIZE_VERSION) FORCE
	@ln -sf $< $@
$(KUSTOMIZE)@$(KUSTOMIZE_VERSION):
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: goreleaser
goreleaser: $(GORELEASER) ## Download goreleaser locally if necessary.
$(GORELEASER): $(GORELEASER)@$(GORELEASER_VERSION) FORCE
	@ln -sf $< $@
$(GORELEASER)@$(GORELEASER_VERSION):
	$(call go-install-tool,$(GORELEASER),github.com/goreleaser/goreleaser/v2,$(GORELEASER_VERSION))

.PHONY: buf
buf: $(BUF) ## Download buf locally if necessary.
$(BUF): $(BUF)@$(BUF_VERSION) FORCE
	@ln -sf $< $@
$(BUF)@$(BUF_VERSION):
	$(call go-install-tool,$(BUF),github.com/bufbuild/buf/cmd/buf,$(BUF_VERSION))
