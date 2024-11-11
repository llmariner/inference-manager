#!/usr/bin/env bash

set -xeo pipefail

LLMA_PATH=${1:?LLMariner Path}

cd ${LLMA_PATH}/provision/dev
helmfile apply \
         --skip-diff-on-install \
         --selector tier!=monitoring,app!=llmariner,app!=milvus
