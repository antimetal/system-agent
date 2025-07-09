SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

ROOT = $(shell git rev-parse --show-toplevel)
## Location to install dependencies to
LOCALBIN ?= $(ROOT)/bin
## Location to store build & release artifacts
DIST ?= $(ROOT)/dist

GO_OS ?= linux
GO_ARCH ?= $(shell go env GOARCH)
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
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: clean
clean: ## Removes build artifacts.
	rm -rf $(LOCALBIN)
	rm -rf $(DIST)
	rm -f $(TESTCOVERAGE_OUT)

##@ Development

.PHONY: generate
generate: manifests ## Generate all artifacts

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
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests fmt vet ## Run tests.
	go test ./... -v -coverprofile $(TESTCOVERAGE_OUT) -timeout 30s

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter & yamllint.
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes.
	$(GOLANGCI_LINT) run --fix

.PHONY: vendor
vendor:
	go mod vendor

##@ Build

build: goreleaser vendor manifests fmt vet ## Build agent binary for current GOOS and GOARCH.
	GOOS=$(GO_OS) $(GORELEASER) build --snapshot --clean --single-target

.PHONY: build-all
build-all: goreleaser manifests fmt vet ## Build agent binary for all platforms.
	$(GORELEASER) build --snapshot --clean

.PHONY: docker.builder
docker.builder:
	- docker buildx create --name antimetal-agent-builder 2> /dev/null || true
	docker buildx use antimetal-agent-builder

.PHONY: docker-build
docker-build: docker.builder build ## Build docker image for current GOOS and GOARCH.
	DOCKER_BUILDKIT=1 docker buildx build \
		--platform ${GO_OS}/${GO_ARCH} \
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
	$(KUBECTL) -n antimetal-system rollout restart deployment antimetal-agent >/dev/null 2>&1 || true

.PHONY: build-and-load-image
build-and-load-image: docker-build load-image ## Builds and loads Docker image into KIND cluster.

##@ Release

.PHONY: preview-release
preview-release: goreleaser lint ## Generate a release tarball.
	$(GORELEASER) release --clean --fail-fast --snapshot --skip publish

.PHONY: release
release: goreleaser lint ## Create a new release.
	$(GORELEASER) release --clean --fail-fast

##@ Dependencies

## Tool Binaries
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
KIND ?= $(LOCALBIN)/kind
KTF ?= $(LOCALBIN)/ktf
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
GORELEASER ?= $(LOCALBIN)/goreleaser
LICENSE_CHECK ?= tools/license_check/license_check.py

## Tool Versions
CONTROLLER_TOOLS_VERSION ?= v0.17.0
GOLANGCI_LINT_VERSION ?= v1.63.4
KIND_VERSION ?= v0.26.0
KTF_VERSION ?= v0.47.2
KUSTOMIZE_VERSION ?= v5.6.0
GORELEASER_VERSION ?= v2.10.2

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
tools: controller-gen envtest golangci-lint kind ktf kustomize goreleaser

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
