# Copyright 2024 Nokia
# Licensed under the Apache License 2.0
# SPDX-License-Identifier: Apache-2.0

VERSION ?= latest
REGISTRY ?= europe-docker.pkg.dev/srlinux/eu.gcr.io
PROJECT ?= config-server
IMG ?= $(REGISTRY)/${PROJECT}:$(VERSION)

REPO = github.com/sdcio/config-server
USERID := 10000

## Tool Binaries
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
CONTROLLER_TOOLS_VERSION ?= v0.15.0
KFORM ?= $(LOCALBIN)/kform
KFORM_VERSION ?= v0.0.2

GOBIN := $(shell go env GOPATH)/bin

.PHONY: all
all: codegen fmt vet lint test tidy

.PHONY: docker
docker:
	GOOS=linux GOARCH=arm64 go build -o install/bin/apiserver
	docker build install --tag apiserver-caas:v0.0.0 --ssh default="$(SSH_AUTH_SOCK)"

.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	ssh-add ./keys/id_rsa 2>/dev/null; true
	docker build --build-arg USERID="$(USERID)" . -t ${IMG} --ssh default="$(SSH_AUTH_SOCK)"

.PHONY: docker-push
docker-push:  docker-build ## Push docker image with the manager.
	docker push ${IMG}

.PHONY: install
install: docker
	kustomize build install | kubectl apply -f -

.PHONY: reinstall
reinstall: docker
	kustomize build install | kubectl apply -f -
	kubectl delete pods -n config-system --all

.PHONY: apiserver-logs
apiserver-logs:
	kubectl logs -l apiserver=true --container apiserver -n config-system -f --tail 1000

.PHONY: codegen
codegen:
	(which apiserver-runtime-gen || go get sigs.k8s.io/apiserver-runtime/tools/apiserver-runtime-gen)
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
	(which golangci-lint || go get github.com/golangci/golangci-lint/cmd/golangci-lint)
	$(GOBIN)/golangci-lint run ./...

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

.PHONY: goreleaser-nightly
goreleaser-nightly:
	go install github.com/goreleaser/goreleaser/v2@latest
	goreleaser release --clean -f .goreleaser.nightlies.yml --skip=validate
