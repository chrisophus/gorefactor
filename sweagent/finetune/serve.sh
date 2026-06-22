#!/usr/bin/env bash
# Phase 2, step 5: serve the fine-tuned model via vLLM.
#
# Exposes an OpenAI-compatible API at localhost:8000.
# Point sweagent/config.yaml at it by setting:
#   agent:
#     model:
#       name: "openai/gorefactor-qwen-7b"
#       api_base: "http://localhost:8000/v1"
#
# Usage:
#   bash sweagent/finetune/serve.sh ./gorefactor-qwen-7b/merged
#
# Prerequisites:
#   pip install vllm
#   GPU with ≥16 GB VRAM for bfloat16 (7B model)

set -euo pipefail

MODEL_PATH="${1:-./gorefactor-qwen-7b/merged}"
HOST="${HOST:-0.0.0.0}"
PORT="${PORT:-8000}"
GPU_MEMORY_UTIL="${GPU_MEMORY_UTIL:-0.92}"    # leave 8% headroom
MAX_MODEL_LEN="${MAX_MODEL_LEN:-8192}"         # matches --max-seq-len in training
SERVED_MODEL_NAME="${SERVED_MODEL_NAME:-gorefactor-qwen-7b}"

if [ ! -d "$MODEL_PATH" ]; then
  echo "ERROR: model path not found: $MODEL_PATH"
  echo "Run: python sweagent/finetune/train_lora.py --merge-only --output ./gorefactor-qwen-7b"
  exit 1
fi

echo "Serving $MODEL_PATH on ${HOST}:${PORT} as '${SERVED_MODEL_NAME}'"
echo "API compatible with OpenAI client: http://localhost:${PORT}/v1"
echo ""

python -m vllm.entrypoints.openai.api_server \
  --model               "$MODEL_PATH" \
  --served-model-name   "$SERVED_MODEL_NAME" \
  --host                "$HOST" \
  --port                "$PORT" \
  --gpu-memory-utilization "$GPU_MEMORY_UTIL" \
  --max-model-len       "$MAX_MODEL_LEN" \
  --enable-auto-tool-choice \
  --tool-call-parser    hermes \
  --trust-remote-code
