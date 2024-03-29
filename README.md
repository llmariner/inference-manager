# inference-server

Inference Server.

Currently we just run [Ollama](https://ollama.com/) by following [LLM Starter Pack](https://github.com/cncf/llm-starter-pack).

The following commands build and run a Docker container.

```bash
docker build -t inference-server:latest -f build/inference-server/Dockerfile .
docker run -p 11434:11434 inference-server:latest
```

You can then send an HTTP request to verify:

```bash
curl http://localhost:11434/api/generate -d '{
  "model": "gemma:2b",
  "prompt":"Why is the sky blue?"
}'
```

# TODO

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
