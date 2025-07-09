# This builds a vLLM docker image with additional audio libraries.
#
# The public vLLM images do not have these libraries as the libraries are
# LGPL (https://github.com/vllm-project/vllm/issues/8030)

FROM --platform=$BUILDPLATFORM vllm/vllm-openai:v0.9.2 as vllm
ARG TARGETARCH

RUN --mount=type=cache,target=/root/.cache/pip \
    pip install librosa soundfile

# Workaround for https://github.com/vllm-project/vllm/issues/19538
RUN pip install transformers==4.52.4

ENTRYPOINT ["python3", "-m", "vllm.entrypoints.openai.api_server"]
