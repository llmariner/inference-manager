# Development Guide

This document describes the inference-manager specific development guide. Please refere the common [Development Guide](https://github.com/llmariner/llmariner/blob/main/DEVELOPMENT.md) for the versioning, style guide, protocol buffer, and testing.

## Deploy the inference-manager in development locally

### Requirements

- [Python 3](https://www.python.org/)
- [Docker](https://docs.docker.com/engine/install/)
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [Helmfile](https://helmfile.readthedocs.io/en/latest/#installation)


### Minimal configuration of the inference-manager

By running the `make setup-all` command, then create a kind cluster, configure the helm chart for the code under development locally, build docker images, and deploy them to the cluster.

```console
make setup-all
```

> [!NOTE]
> If you are getting a 403 forbidden error, please try `docker logout public.ecr.aws`. Please see [AWS document](https://docs.aws.amazon.com/AmazonECR/latest/public/public-troubleshooting.html) for more details.

Once the `make` command is successfully executed, you can confirm the inference and model manager are deployed in the `llmariner` namespace.

```console
> kubectl get deploy,job -A
NAMESPACE            NAME                                       READY   UP-TO-DATE   AVAILABLE   AGE
kong                 deployment.apps/kong                       1/1     1            1           10m
kube-system          deployment.apps/coredns                    2/2     2            2           10m
llmariner            deployment.apps/inference-manager-engine   1/1     1            1           5m13s
llmariner            deployment.apps/inference-manager-server   1/1     1            1           5m13s
llmariner            deployment.apps/model-manager-server       1/1     1            1           5m13s
local-path-storage   deployment.apps/local-path-provisioner     1/1     1            1           10m
minio                deployment.apps/minio                      1/1     1            1           10m

NAMESPACE   NAME                             STATUS    COMPLETIONS   DURATION   AGE
llmariner   job.batch/model-manager-loader   Running   1/1           5m13s      5m13s
```

### Try out model and inference APIs

In this minimal configuration, the authentication is disabled. You can simply call the API directly.

with curl:

```console
curl -X GET http://localhost:8080/v1/models
curl -X POST http://localhost:8080/v1/chat/completions -d '{
  "model": "google-gemma-2b-it-q4_0",
  "messages": [{"role": "user", "content": "hello"}]
}'
```

with llma:

```console
export LLMARINER_API_KEY=dummy
llma models list
llma chat completions create \
    --model google-gemma-2b-it-q4_0 \
    --role system \
    --completion 'hi'
```

## Adjust Deployment Settings

### Re-deploy Changes

When you have already set up the cluster and want to deploy the changes,
simply run `make helm-reapply-inference-server` or `make helm-reapply-inference-engine`. This will rebuild inference-manager container images, deploy them using the local helm chart, and restart containers.

> [!TIP]
> - You can configure parameters in [.values.yaml](hack/values.yaml).

### Deploy additional dependent components

In the default development settings, only Kong and Minio are deployed as dependent components. If you want to install additional components, specify the component name in the `EXTRA_DEPS` variable and run `make helm-apply-deps`.

For example:

```console
make helm-apply-deps EXTRA_DEPS=prometheus,keda
```

### Run vLLM on ARM MacOS

To run vLLM on ARM CPU (macOS), you'll need to build an image.

```console
git clone https://github.com/vllm-project/vllm.git
cd vllm
docker build -f Dockerfile.arm -t vllm-cpu-env --shm-size=4g .
kind load docker-image vllm-cpu-env:latest
```

Then, run `make` with the `RUNTIME` option.

```console
make setup-all RUNTIME=vllm
```

> [!NOTE]
> See [vLLM - ARM installation](https://docs.vllm.ai/en/latest/getting_started/arm-installation.html) for details.
