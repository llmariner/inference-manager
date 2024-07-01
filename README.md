# inference-manager

## Running with Docker Compose

Run the following command:

```bash
docker-compose build
docker-compose up
```

You can hit `inference-manager-server` at port 8080.

```bash
curl --request POST http://localhost:8080/v1/chat/completions -d '{
  "model": "google-gemma-2b-it-q4",
  "messages": [{"role": "user", "content": "hello"}]
}'
```

## Running Engine Locally

Run the following command:

```bash
make build-docker-engine
docker run \
  -v ./configs/engine:/config \
  -p 8080:8080 \
  -p 8081:8081 \
  llm-operator/inference-manager-engine \
  run \
  --config /config/config.yaml
```

Then hit the HTTP point and verify that Ollama responds.

```bash
curl http://localhost:8080/api/generate -d '{
  "model": "gemma:2b",
  "prompt":"Why is the sky blue?"
}'
```


If you want to load modelds from your local filesystem, you can add mount the volume.

```bash
docker run \
  -v ./configs/engine:/config \
  -p 8080:8080 \
  -p 8081:8081 \
  -v ./models:/models \
  llm-operator/inference-manager-engine \
  run \
  --config /config/config.yaml
```

Then import the models to Ollama.

```bash
docker exec -it <contaiener ID> bash

export OLLAMA_HOST=0.0.0.0:8080
ollama create <model-name> -f <modelfile>
```

Here are example modelfiles:

```
FROM /models/gemma-2b-it.gguf
TEMPLATE """<start_of_turn>user
{{ if .System }}{{ .System }} {{ end }}{{ .Prompt }}<end_of_turn>
<start_of_turn>model
{{ .Response }}<end_of_turn>
"""
PARAMETER repeat_penalty 1
PARAMETER stop "<start_of_turn>"
PARAMETER stop "<end_of_turn>"
```

```
FROM gemma-2b-it
ADAPTER /models/ggml-adapter-model.bin
```
