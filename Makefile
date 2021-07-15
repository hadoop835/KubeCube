# Copyright 2021 The KubeCube Authors. All rights reserved.
# Use of this source code is governed by a Apache license
# that can be found in the LICENSE file.

IMG ?= hub.c.163.com/kubecube/cube:v0.0.1
MULTI_ARCH ?= false
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=false"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

GOFILES=$(shell find . -name "*.go" -type f -not -path "./vendor/*")

all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Tools

manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	@gofmt -s -w ${GOFILES}

fmt-check:
	@diff=`gofmt -s -l ${GOFILES}`; \
	if [ -n "$${diff}" ]; then \
		echo "Please run 'make fmt' and commit the result:"; \
		echo "$${diff}"; \
	fi;

swag-gen:
	swag init --parseDependency --parseInternal --parseDepth 5 -g ./pkg/apiserver/apiserver.go

vendor:
	go mod vendor

vet: ## Run go vet against code.
	go vet ./...

ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test: manifests generate fmt vet ## Run tests.
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.7.0/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); go test ${go list ./... | grep -v /test/e2e} -coverprofile cover.out

##@ Run

run-cube:
	bash hack/run_cube.sh

run-warden:
	bash hack/run_warden.sh

##@ Build

build-cube: #generate fmt vet
ifeq ($(MULTI_ARCH),true)
	CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build -mod=vendor -a -o cube cmd/cube/main.go
else
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -mod=vendor -a -o cube cmd/cube/main.go
endif

build-warden:
ifeq ($(MULTI_ARCH),true)
	CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build -a -o warden cmd/warden/main.go
else
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o warden cmd/warden/main.go
endif

docker-build-cube: vendor #test ## Build docker image with the manager.
	docker build -f ./build/cube/Dockerfile -t ${IMG} .

docker-build-cube-multi-arch: vendor #test
	MULTI_ARCH=true
	docker buildx build -f ./build/cube/Dockerfile -t ${IMG} --platform=linux/arm,linux/arm64,linux/amd64 . --push

docker-build-warden: vendor #test
	docker build -f ./build/warden/Dockerfile -t ${IMG} .

docker-build-warden-multi-arch: vendor #test
	MULTI_ARCH=true
	docker buildx build -f ./build/warden/Dockerfile -t ${IMG} --platform=linux/arm,linux/arm64,linux/amd64 . --push

docker-build-warden-init:
	docker build -f ./build/warden/init.Dockerfile -t ${IMG} .

docker-build-warden-init-multi-arch:
	docker buildx build -f ./build/warden/init.Dockerfile -t ${IMG} --platform=linux/arm,linux/arm64,linux/amd64 . --push

##@ Deployment

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1)

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef