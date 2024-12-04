#!/usr/bin/env bash

set -xeo pipefail

LLMA_PATH=${1:?LLMariner Path}
DEV_PATH=${LLMA_PATH}/provision/dev
HACK_PATH=$(realpath --relative-to=${DEV_PATH} hack)
RUNTIME=${2:?Runtime}
ARCH=$(uname -m)

cd ${DEV_PATH}
if [ "${RUNTIME}" = "ollama" ]; then
    helmfile apply \
         --skip-refresh \
         --skip-diff-on-install \
         --selector app=llmariner \
         --values ${HACK_PATH}/overwrite.yaml \
         --values ${HACK_PATH}/values.yaml
elif [ "${RUNTIME}" = "vllm" ]; then
    extra=()
    if [[ "$ARCH" == "arm"* || "$ARCH" == "aarch64" ]]; then
        extra=(--set inference-manager-engine.model.default.vllmExtraFlags={"--dtype=float16"})
    fi
    helmfile apply \
         --skip-refresh \
         --skip-diff-on-install \
         --selector app=llmariner \
         --values ${HACK_PATH}/overwrite.yaml \
         --values ${HACK_PATH}/values.yaml \
         --values ${HACK_PATH}/values.vllm.yaml ${extra[@]}
else
    echo "unsupported runtime"
    exit 1
fi
