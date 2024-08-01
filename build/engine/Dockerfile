FROM --platform=$BUILDPLATFORM golang:1.22 as builder
ARG TARGETARCH

WORKDIR /workspace
COPY . .

ENV GOCACHE=/root/gocache
RUN \
    --mount=type=cache,target=${GOCACHE} \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

RUN --mount=type=cache,target=${GOCACHE} \
    --mount=type=cache,id=inference-manager,sharing=locked,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} GO111MODULE=on make build-engine

FROM --platform=$BUILDPLATFORM ollama/ollama:0.3.1 as ollama
ARG TARGETARCH

# vLLM requires GPU.
FROM --platform=$BUILDPLATFORM vllm/vllm-openai:v0.5.3 as vllm
ARG TARGETARCH

WORKDIR /run

COPY --from=builder /workspace/bin/engine .

ENTRYPOINT ["./engine"]
