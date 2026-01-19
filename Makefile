# Copyright 2024 Nokia
# Licensed under the Apache License 2.0
# SPDX-License-Identifier: Apache-2.0

VERSION ?= latest
REGISTRY ?= europe-docker.pkg.dev/srlinux/eu.gcr.io
IMG_SERVER ?= $(REGISTRY)/sdc-apiserver:$(VERSION)
IMG_CONTROLLER ?= $(REGISTRY)/sdc-controller:$(VERSION)

REPO = github.com/sdcio/config-server
USERID := 10000

## Tool Binaries
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
CONTROLLER_TOOLS_VERSION ?= v0.15.0
KFORM ?= $(LOCALBIN)/kform
KFORM_VERSION ?= v0.0.2
GOLANGCILINT_VERSION ?= v1.64.8
GOLANGCILINT ?= $(LOCALBIN)/golangci-lint

GOBIN := $(shell go env GOPATH)/bin

.PHONY: all
all: fmt vet lint test tidy

.PHONY: docker
docker:
	GOOS=linux GOARCH=arm64 go build -o install/bin/apiserver
	docker build install --tag apiserver-caas:v0.0.0 --ssh default="$(SSH_AUTH_SOCK)"

.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	ssh-add ./keys/id_rsa 2>/dev/null; true
	docker build --ssh default="$(SSH_AUTH_SOCK)" --build-arg USERID="$(USERID)" \
		-f DockerfileAPIServer -t ${IMG_SERVER} .
	docker build --ssh default="$(SSH_AUTH_SOCK)" --build-arg USERID="$(USERID)" \
		-f DockerfileController -t ${IMG_CONTROLLER} .

.PHONY: docker-push
docker-push:  docker-build ## Push docker image with the manager.
	docker push ${IMG_SERVER}
	docker push ${IMG_CONTROLLER}

.PHONY: install
install: artifacts
	kubectl apply -f artifacts/out

.PHONY: reinstall
reinstall: docker-push artifacts
	kubectl apply -f artifacts/out

.PHONY: codegen
codegen:
	go generate

.PHONY: genclients
genclients:
	go run ./tools/apiserver-runtime-gen \
		-g deepcopy-gen \
		-g client-gen \
		-g informer-gen \
		-g lister-gen \
		-g openapi-gen \
		-g defaulter-gen \
		-g conversion-gen \
		--module $(REPO) \

.PHONY: genproto
genproto:
	go run ./tools/apiserver-runtime-gen \
		-g go-to-protobuf \
		--module $(REPO) \

.PHONY: generate
generate: controller-gen 
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./apis/inv/..."

.PHONY: manifests
manifests: controller-gen crds artifacts ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./apis/inv/..." output:crd:artifacts:config=artifacts

.PHONY: crds
crds: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	mkdir -p crds
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./apis/..." output:crd:artifacts:config=crds

.PHONY: artifacts
artifacts: kform
	mkdir -p artifacts/out
	$(KFORM) apply artifacts -i artifacts/in/configmap-input-vars.yaml -o artifacts/out/artifacts.yaml

.PHONY: fix
fix:
	go fix ./...

.PHONY: fmt
fmt:
	test -z $(go fmt ./tools/...)

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: lint
lint:
	$(GOLANGCILINT) run ./...

.PHONY: test
test:
	go test -cover ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: local-run
local-run:
	apiserver-boot run local --run=etcd,apiserver

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: kform
kform: $(KFORM) ## Download kform locally if necessary.
$(KFORM): $(LOCALBIN)
	test -s $(LOCALBIN)/kform || GOBIN=$(LOCALBIN) go install github.com/kform-dev/kform/cmd/kform@$(KFORM_VERSION)

.PHONY: golangci-lint
golangci-lint: $(GOLANGCILINT) ## Download kform locally if necessary.
$(GOLANGCILINT): $(LOCALBIN)
	test -s $(LOCALBIN)/golangci-lint || GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCILINT_VERSION)

.PHONY: goreleaser-nightly
goreleaser-nightly:
	go install github.com/goreleaser/goreleaser/v2@latest
	goreleaser release --clean -f .goreleaser.nightlies.yml --skip=validate
