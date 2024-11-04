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
	docker build --build-arg TARGETARCH=amd64 -t llmariner/inference-manager-server:latest -f build/server/Dockerfile .

.PHONY: build-docker-engine
build-docker-engine:
	docker build --build-arg TARGETARCH=amd64 -t llmariner/inference-manager-engine:latest -f build/engine/Dockerfile .

.PHONY: build-docker-triton-proxy
build-docker-triton-proxy:
	docker build --build-arg TARGETARCH=amd64 -t llmariner/inference-manager-triton-proxy:latest -f build/triton-proxy/Dockerfile .

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
