#!/usr/bin/env bash

set -xeo pipefail

LLMA_PATH=${1:?LLMariner Path}

extra_flags=()
if [ -n "$2" ]; then
    IFS=',' read -r -a EXTRA_APPS <<< "$2"
    for app in "${EXTRA_APPS[@]}"; do
        extra_flags+=("-l app=$app")
    done
fi

cd ${LLMA_PATH}/provision/dev
helmfile apply \
         --skip-diff-on-install \
         -f *helmfile.yaml.gotmpl \
         -l app=postgres -l app=minio -l app=kong ${extra_flags[@]}
