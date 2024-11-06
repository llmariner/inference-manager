# vLLM requires GPU.
FROM --platform=$BUILDPLATFORM vllm/vllm-openai:v0.6.2 as vllm
ARG TARGETARCH

RUN --mount=type=cache,target=/root/.cache/pip \
    pip install librosa soundfile

ENTRYPOINT ["python3", "-m", "vllm.entrypoints.openai.api_server"]
