# Use a LoRA adapter in Ollama

Follow https://sarinsuriyakoon.medium.com/unsloth-lora-with-ollama-lightweight-solution-to-full-cycle-llm-development-edadb6d9e0f0


## Step 1. Convert to GGML

```bash
git clone git@github.com:ggerganov/llama.cpp.git
cd llama.cpp

python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt

lora_adapter_dir=/Users/kenji/dev/misc/ai-gpu/gemma-finetuned-openassistant
python convert-lora-to-ggml.py "${lora_adapter_dir}"
```

## Step 2. Create a Modelfile

```dockerifle
FROM google/gemma:2b
ADAPTER ./ggml-adapter-model.bin
```

## Step 3. Run Ollama

```bash
docker run --rm -it -p 11434:11434 -v ./:/models --entrypoint /bin/bash ollama/ollama:latest
# Run the following inside the container
ollama serve &
cd /models
ollama create example -f Modelfile
```

## Step 4. Send a test request

```bash
curl http://localhost:11434/api/generate -d '{
  "model": "example",
  "prompt":"Why is the sky blue?"
}'
```
