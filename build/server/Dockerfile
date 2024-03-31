FROM gcr.io/distroless/static:nonroot
ARG TARGETARCH

WORKDIR /run

COPY ./bin/${TARGETARCH}/server .

ENTRYPOINT ["./server"]
