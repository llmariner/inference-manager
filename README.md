# inference-manager

The inference-manager manages inference runtimes (e.g., vLLM and Ollama) in containers, load models, and process requests.

## Inferece Request flow.

Please see [inference_request_flow.md](docs/development/inference_request_flow.md).

## Set up Inference Server/Engine for development

Requirements:

- [Docker](https://docs.docker.com/engine/install/)
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [Helmfile](https://helmfile.readthedocs.io/en/latest/#installation)

Run the following command:

```console
make setup-all
```

> [!TIP]
> - Run just only `make helm-reapply-inference-server` or `make helm-reapply-inference-engine`, it will rebuild inference-manager container images, deploy them using the local helm chart, and restart containers.
> - You can configure parameters in [.values.yaml](hack/values.yaml).

### Run vLLM on ARM macOS

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

### Try out inference APIs

with curl:

```console
curl --request POST http://localhost:8080/v1/chat/completions -d '{
  "model": "google-gemma-2b-it-q4_0",
  "messages": [{"role": "user", "content": "hello"}]
}'
```

with llma:

```console
export LLMARINER_API_KEY=dummy
llma chat completions create \
    --model google-gemma-2b-it-q4_0 \
    --role system \
    --completion 'hi'
```
