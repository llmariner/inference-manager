# inference-manager

The inference-manager manages inference runtimes (e.g., vLLM and Ollama) in containers, load models, and process requests.

## Set up Inference Server/Engine for development

Requirements:

- [Docker](https://docs.docker.com/engine/install/)
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [Helmfile](https://helmfile.readthedocs.io/en/latest/#installation)

Run the following command:

```console
make setup-llmariner setup-cluster helm-apply-inference
```

> [!TIP]
> - Run just only `make helm-apply-inference`, it will rebuild inference-manager container images and deploy them using the local helm chart.
> - You can configure parameters in [.values.yaml](hack/values.yaml).

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
