FROM --platform=$BUILDPLATFORM golang:1.23 AS builder
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

FROM --platform=$BUILDPLATFORM gcr.io/distroless/static:nonroot
ARG TARGETARCH

WORKDIR /run

COPY --from=builder /workspace/bin/engine .

ENTRYPOINT ["./engine"]
