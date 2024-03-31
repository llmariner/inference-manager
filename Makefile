.PHONY: default
default: test

include common.mk

.PHONY: test
test: go-test-all

.PHONY: lint
lint: go-lint-all git-clean-check

.PHONY: generate
generate: buf-generate-all

.PHONY: build-server
build-server:
	@GOOS=linux GOARCH=amd64 go build -o ./bin/amd64/server ./server/cmd/
	@GOOS=linux GOARCH=arm64 go build -o ./bin/arm64/server ./server/cmd/
