#!/usr/bin/env bash

set -xeo pipefail

LLMA_PATH=${1:?LLMariner Path}
DEV_PATH=${LLMA_PATH}/provision/dev
HACK_PATH=$(realpath --relative-to=${DEV_PATH} hack)

cd ${DEV_PATH}
helmfile apply \
         --skip-refresh \
         --skip-diff-on-install \
         --selector app=llmariner \
         --values ${HACK_PATH}/overwrite.yaml \
         --values ${HACK_PATH}/values.yaml
