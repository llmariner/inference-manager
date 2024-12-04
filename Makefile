SERVER_IMAGE ?= llmariner/inference-manager-server
ENGINE_IMAGE ?= llmariner/inference-manager-engine
TAG ?= latest

.PHONY: default
default: test

include common.mk

.PHONY: test
test: go-test-all

.PHONY: lint
lint: go-lint-all helm-lint git-clean-check

.PHONY: generate
generate: buf-generate-all typescript-compile

.PHONY: build-server
build-server:
	go build -o ./bin/server ./server/cmd/

.PHONY: build-engine
build-engine:
	go build -o ./bin/engine ./engine/cmd/

.PHONY: build-triton-proxy
build-triton-proxy:
	go build -o ./bin/triton-proxy ./triton-proxy/cmd/

.PHONY: build-docker-server
build-docker-server:
	docker build --build-arg TARGETARCH=amd64 -t $(SERVER_IMAGE):$(TAG) -f build/server/Dockerfile .

.PHONY: build-docker-engine
build-docker-engine:
	docker build --build-arg TARGETARCH=amd64 -t $(ENGINE_IMAGE):$(TAG) -f build/engine/Dockerfile .

.PHONY: build-docker-triton-proxy
build-docker-triton-proxy:
	docker build --build-arg TARGETARCH=amd64 -t llmariner/inference-manager-triton-proxy:latest -f build/triton-proxy/Dockerfile .

.PHONY: build-docker-vllm
build-docker-vllm:
	docker build --build-arg TARGETARCH=amd64 -t llmariner/vllm-openai:0.6.2 -f build/vllm/Dockerfile .

.PHONY: check-helm-tool
check-helm-tool:
	@command -v helm-tool >/dev/null 2>&1 || $(MAKE) install-helm-tool

.PHONY: install-helm-tool
install-helm-tool:
	go install github.com/cert-manager/helm-tool@latest

.PHONY: generate-chart-schema
generate-chart-schema: generate-chart-schema-server generate-chart-schema-engine

.PHONY: generate-chart-schema-server
generate-chart-schema-server: check-helm-tool
	@cd ./deployments/server && helm-tool schema > values.schema.json

.PHONY: generate-chart-schema-engine
generate-chart-schema-engine: check-helm-tool
	@cd ./deployments/engine && helm-tool schema > values.schema.json

.PHONY: helm-lint
helm-lint: helm-lint-server helm-lint-engine

.PHONY: helm-lint-server
helm-lint-server: generate-chart-schema-server
	cd ./deployments/server && helm-tool lint
	helm lint ./deployments/server

.PHONY: helm-lint-engine
helm-lint-engine: generate-chart-schema-engine
	cd ./deployments/engine && helm-tool lint
	helm lint ./deployments/engine

KIND_CLUSTER ?= kind
LLMA_REPO ?= https://github.com/llmariner/llmariner.git
CLONE_PATH ?= work
RUNTIME ?= ollama

.PHONY: setup-all
setup-all: setup-llmariner setup-cluster helm-apply-inference

.PHONY: setup-llmariner
setup-llmariner: update-llmariner configure-llma-chart

.PHONY: update-llmariner
update-llmariner:
	@if [ -d $(CLONE_PATH) ]; then \
		cd $(CLONE_PATH) && \
		git checkout -- deployments/llmariner/Chart.yaml && \
		git pull; \
	else \
		git clone $(LLMA_REPO) $(CLONE_PATH); \
	fi

.PHONY: configure-llma-chart
configure-llma-chart:
	hack/overwrite-llma-chart-for-test.sh $(CLONE_PATH)
	-rm $(CLONE_PATH)/deployments/llmariner/Chart.lock

.PHONY: setup-cluster
setup-cluster: create-kind-cluster helm-apply-deps

.PHONY: create-kind-cluster
create-kind-cluster:
	@if ! kind get clusters | grep -q $(KIND_CLUSTER); then \
		kind create cluster --name $(KIND_CLUSTER) --config hack/kind-config.yaml; \
	else \
		echo "Cluster '$(KIND_CLUSTER)' already exists"; \
	fi

.PHONY: load-docker-image-all
load-docker-image-all: load-docker-image-server load-docker-image-engine

.PHONY: load-docker-image-server
load-docker-image-server: build-docker-server
	kind load docker-image $(SERVER_IMAGE):$(TAG) --name $(KIND_CLUSTER)

.PHONY: load-docker-images-engine
load-docker-image-engine: build-docker-engine
	kind load docker-image $(ENGINE_IMAGE):$(TAG) --name $(KIND_CLUSTER)

.PHONY: helm-apply-deps
helm-apply-deps:
	hack/helm-apply-deps.sh $(CLONE_PATH)

.PHONY: helm-apply-inference
helm-apply-inference: load-docker-image-all
	hack/helm-apply-inference.sh $(CLONE_PATH) $(RUNTIME)

.PHONY: helm-reapply-inference-server
helm-reapply-inference-server: load-docker-image-server
	hack/helm-apply-inference.sh $(CLONE_PATH) $(RUNTIME)
	kubectl rollout restart deployment -n llmariner inference-manager-server

.PHONY: helm-reapply-inference-engine
helm-reapply-inference-engine: load-docker-image-engine
	hack/helm-apply-inference.sh $(CLONE_PATH) $(RUNTIME)
	kubectl rollout restart deployment -n llmariner inference-manager-engine
