#!/usr/bin/env bash

set -xeo pipefail

LLMA_PATH=${1:?LLMariner Path}
BASE_PATH=$(pwd)

pip install pyyaml
python hack/overwrite-llma-chart-for-test.py \
       ${LLMA_PATH}/deployments/llmariner/Chart.yaml \
       $(realpath --relative-to=${LLMA_PATH}/deployments/llmariner deployments)
