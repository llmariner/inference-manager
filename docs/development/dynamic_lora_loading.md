# Dynamic LoRA Loading

Dynamic LoRA loading is a mechanism that loads LoRAs into a vLLM pod that serves their base models. This achieves
more efficient GPU utilization by avoiding a separate inference runtime pod per LoRA.

The code is located at the `engine/internal/runtime` package.

## LoRA Loading flow

Inference Manager Engine has the following three ways to load an LoRA into a vLLM pod.

### Model Activation

If a specified model is a fine-tuned model, the adapter is loaded into the base model.

If there is no runtime that serves a base model, a vLLM pod is first created (and then the adapter is loaded).

`Manager.PullModel()` is called.

### LoRA rebalancing

The rebalancer monitors the current LoRA loading status of running
vLLM pods and dynamically loads/unloads adapters based on request
traffic.

The current logic is naive, and the rebalancer simply attempts to load
an LoRA for base model `M` on every vLLM pod that serves `M`.

`Manager.loadLoRAAdapter()` is called.

### LoRA reconciliation

The reconciler monitors currently loaded LoRAs in each vLLM pod.

- If there is an LoRA loaded into a vLLM pod without corresponding a `runtime` struct, the reconciler creates a new `runtime` struct.
- When a pod is deleted, it updates the loRA loading status of `runtime` structs.

`Manager.processLoRAAdapterUpdate()` is called.
