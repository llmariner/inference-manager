# inference-manager

## TODO

- Implement the API endpoints (but still bypass to Ollama)
- Replace Ollama with its own code
- Be able to support multiple open source models
- Be able to support multiple models that are fine-tuned by users
- Support Autoscaling (with KEDA?)
- Support multi-GPU & multi-node inference (?)
- Explore optimizations
  - https://github.com/NVIDIA/TensorRT-LLM
  - https://github.com/vllm-project/vllm
  - https://github.com/predibase/lorax

Here are some other notes:

- Ollama internally uses [llama.cpp](https://github.com/ggerganov/llama.cpp). It provides a lightweight OpenAI API compatible HTTP server.
- [go-llama.cpp](https://github.com/go-skynet/go-llama.cpp) provides a Go binding.
- [LocalAI](https://github.com/mudler/LocalAI) is another OpenAI API compatible HTTP server (supported by Spectro Cloud).
- [kaito](https://github.com/Azure/kaito) internally uses `torchrun` or [`accelerate launch`](https://huggingface.co/docs/accelerate/en/index) to launch an inference workload.
  See [its Dockerfiles](https://github.com/Azure/kaito/tree/main/docker/presets) and [preset Python programs](https://github.com/Azure/kaito/tree/main/presets).
- [localllm](https://cloud.google.com/blog/products/application-development/new-localllm-lets-you-develop-gen-ai-apps-locally-without-gpus) from Google Cloud.


## Running Engine Locally

Run the following command:

```bash
make build-docker-engine
docker run \
  -v ./config:/config \
  -p 8080:8080 \
  -p 8081:8081 \
  llm-operator/inference-manager-engine \
  run \
  --config /config/config.yaml

```

`./config/config.yaml` has the following content:

```yaml
internalGrpcPort: 8081
ollamaPort: 8080
debug:
  standalone: true
```

Then hit the HTTP point and verify that Ollama responds.

```bash
curl http://localhost:11434/api/generate -d '{
  "model": "gemma:2b",
  "prompt":"Why is the sky blue?"
}'
```
