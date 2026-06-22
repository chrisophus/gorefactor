#!/usr/bin/env python3
"""
Phase 2, step 4: fine-tune Qwen2.5-Coder-7B-Instruct on gorefactor trajectories.

Uses TRL's SFTTrainer with LoRA so the full model weights are frozen and only
~0.5% of parameters are trained. A single A100 40GB (or two 3090s) is enough.

Usage:
  python sweagent/finetune/train_lora.py \
    --data  sweagent/finetune/training_data.jsonl \
    --output ./gorefactor-qwen-7b

  # Smaller GPU (12 GB): add quantization
  python sweagent/finetune/train_lora.py \
    --data sweagent/finetune/training_data.jsonl \
    --output ./gorefactor-qwen-7b \
    --load-in-4bit

  # Resume from checkpoint:
  python sweagent/finetune/train_lora.py \
    --data sweagent/finetune/training_data.jsonl \
    --output ./gorefactor-qwen-7b \
    --resume-from ./gorefactor-qwen-7b/checkpoint-100

After training, the adapter is saved to --output. Merge it with the base:
  python sweagent/finetune/train_lora.py --merge-only --output ./gorefactor-qwen-7b

Then serve with vLLM (see serve.sh).
"""

import argparse
import json
import os
import sys
from pathlib import Path


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="LoRA fine-tune Qwen2.5-Coder-7B on gorefactor traces")
    p.add_argument("--data",    help="Training JSONL from convert_to_sft.py")
    p.add_argument("--val",     help="Validation JSONL (optional, enables eval during training)")
    p.add_argument("--output",  required=True, help="Where to save the LoRA adapter (and merged model)")
    p.add_argument("--base-model", default="Qwen/Qwen2.5-Coder-7B-Instruct",
                   help="HuggingFace model ID for the base model")
    p.add_argument("--lora-r",  type=int, default=16,   help="LoRA rank (higher = more capacity)")
    p.add_argument("--lora-alpha", type=int, default=32, help="LoRA alpha (scaling factor)")
    p.add_argument("--lora-dropout", type=float, default=0.05)
    p.add_argument("--epochs",  type=int,   default=3,    help="Training epochs")
    p.add_argument("--lr",      type=float, default=2e-4, help="Learning rate")
    p.add_argument("--batch",   type=int,   default=1,    help="Per-device batch size")
    p.add_argument("--grad-accum", type=int, default=8,   help="Gradient accumulation steps")
    p.add_argument("--max-seq-len", type=int, default=8192, help="Max token length per sample")
    p.add_argument("--load-in-4bit", action="store_true",
                   help="Load base model in 4-bit (QLoRA) to reduce VRAM")
    p.add_argument("--resume-from", help="Resume training from a checkpoint directory")
    p.add_argument("--merge-only", action="store_true",
                   help="Skip training; merge existing LoRA adapter into base model and exit")
    return p.parse_args()


def load_dataset(jsonl_path: str):
    """Load JSONL into a HuggingFace Dataset."""
    from datasets import Dataset
    records = [json.loads(l) for l in Path(jsonl_path).read_text().splitlines() if l.strip()]
    return Dataset.from_list(records)


def build_model_and_tokenizer(args: argparse.Namespace):
    """Load (and optionally quantize) the base model + tokenizer."""
    import torch
    from transformers import AutoModelForCausalLM, AutoTokenizer, BitsAndBytesConfig

    tokenizer = AutoTokenizer.from_pretrained(
        args.base_model,
        trust_remote_code=True,
        padding_side="right",
    )
    if tokenizer.pad_token is None:
        tokenizer.pad_token = tokenizer.eos_token

    quant_config = None
    if args.load_in_4bit:
        quant_config = BitsAndBytesConfig(
            load_in_4bit=True,
            bnb_4bit_compute_dtype=torch.bfloat16,
            bnb_4bit_quant_type="nf4",
            bnb_4bit_use_double_quant=True,
        )

    model = AutoModelForCausalLM.from_pretrained(
        args.base_model,
        quantization_config=quant_config,
        device_map="auto",
        trust_remote_code=True,
        torch_dtype=torch.bfloat16 if not args.load_in_4bit else None,
        attn_implementation="flash_attention_2",  # remove if FA2 not installed
    )
    model.config.use_cache = False  # required for gradient checkpointing
    return model, tokenizer


def apply_lora(model, args: argparse.Namespace):
    """Wrap the model with LoRA adapters via PEFT."""
    from peft import LoraConfig, TaskType, get_peft_model, prepare_model_for_kbit_training

    if args.load_in_4bit:
        model = prepare_model_for_kbit_training(model)

    lora_cfg = LoraConfig(
        r=args.lora_r,
        lora_alpha=args.lora_alpha,
        lora_dropout=args.lora_dropout,
        task_type=TaskType.CAUSAL_LM,
        # Target the attention and MLP projection layers (Qwen architecture)
        target_modules=["q_proj", "k_proj", "v_proj", "o_proj", "gate_proj", "up_proj", "down_proj"],
        bias="none",
    )
    model = get_peft_model(model, lora_cfg)
    model.print_trainable_parameters()
    return model


def merge_and_save(args: argparse.Namespace) -> None:
    """Merge LoRA adapter into base model and save the full model."""
    import torch
    from peft import PeftModel
    from transformers import AutoModelForCausalLM, AutoTokenizer

    output = Path(args.output)
    merged_path = output / "merged"
    print(f"Merging adapter from {output} into {args.base_model}...")

    tokenizer = AutoTokenizer.from_pretrained(args.base_model, trust_remote_code=True)
    base = AutoModelForCausalLM.from_pretrained(
        args.base_model,
        device_map="cpu",
        trust_remote_code=True,
        torch_dtype=torch.bfloat16,
    )
    model = PeftModel.from_pretrained(base, str(output))
    model = model.merge_and_unload()

    merged_path.mkdir(parents=True, exist_ok=True)
    model.save_pretrained(str(merged_path))
    tokenizer.save_pretrained(str(merged_path))
    print(f"Merged model saved to {merged_path}")
    print(f"\nServe with:")
    print(f"  bash sweagent/finetune/serve.sh {merged_path}")


def format_for_training(example: dict, tokenizer) -> dict:
    """
    Apply the model's chat template to a messages+tools record.
    Returns {"input_ids": [...], "attention_mask": [...], "labels": [...]}.
    The labels mask out all non-assistant tokens so we only train on
    the model's own outputs (standard for instruction fine-tuning).
    """
    import torch

    messages = example["messages"]
    tools    = example.get("tools")

    # apply_chat_template handles system/user/assistant/tool roles
    # and embeds tool schemas when tools= is provided.
    text = tokenizer.apply_chat_template(
        messages,
        tools=tools,
        tokenize=False,
        add_generation_prompt=False,
    )
    tokens = tokenizer(
        text,
        return_tensors="pt",
        truncation=True,
        max_length=8192,
    )
    input_ids = tokens["input_ids"][0]

    # Build labels: -100 for non-assistant tokens (masked in cross-entropy)
    labels = input_ids.clone()
    # Find assistant token boundaries by re-tokenising each message boundary.
    # Simple heuristic: mask everything before the first <|im_start|>assistant token.
    # TRL's DataCollatorForCompletionOnlyLM is a cleaner alternative.
    assistant_token = tokenizer.encode("<|im_start|>assistant", add_special_tokens=False)
    if assistant_token:
        asst_id = assistant_token[-1]
        in_asst = False
        for i, tok in enumerate(input_ids.tolist()):
            if tok == asst_id:
                in_asst = True
            elif in_asst and tok == tokenizer.encode("<|im_end|>", add_special_tokens=False)[0]:
                in_asst = False
            if not in_asst:
                labels[i] = -100

    return {
        "input_ids":      input_ids,
        "attention_mask": tokens["attention_mask"][0],
        "labels":         labels,
    }


def train(args: argparse.Namespace) -> None:
    import torch
    from trl import SFTConfig, SFTTrainer

    if not args.data:
        print("ERROR: --data required for training", file=sys.stderr)
        sys.exit(1)

    print(f"Loading dataset from {args.data}...")
    train_ds = load_dataset(args.data)
    eval_ds  = load_dataset(args.val) if args.val else None
    print(f"  train: {len(train_ds)} records" + (f"  val: {len(eval_ds)}" if eval_ds else ""))

    print(f"Loading model {args.base_model}...")
    model, tokenizer = build_model_and_tokenizer(args)
    model = apply_lora(model, args)

    output_dir = Path(args.output)
    output_dir.mkdir(parents=True, exist_ok=True)

    sft_config = SFTConfig(
        output_dir=str(output_dir),
        num_train_epochs=args.epochs,
        per_device_train_batch_size=args.batch,
        gradient_accumulation_steps=args.grad_accum,
        learning_rate=args.lr,
        lr_scheduler_type="cosine",
        warmup_ratio=0.05,
        bf16=True,
        tf32=True,
        gradient_checkpointing=True,
        gradient_checkpointing_kwargs={"use_reentrant": False},
        max_seq_length=args.max_seq_len,
        logging_steps=5,
        save_steps=50,
        save_total_limit=3,
        eval_strategy="steps" if eval_ds else "no",
        eval_steps=50 if eval_ds else None,
        load_best_model_at_end=bool(eval_ds),
        report_to="none",            # swap for "wandb" if you want tracking
        resume_from_checkpoint=args.resume_from,
    )

    trainer = SFTTrainer(
        model=model,
        tokenizer=tokenizer,
        train_dataset=train_ds,
        eval_dataset=eval_ds,
        args=sft_config,
        # SFTTrainer handles apply_chat_template when messages_column is set
        # and the dataset contains "messages" + "tools" keys.
        # If your TRL version doesn't auto-detect, pass formatting_func instead.
    )

    print("Starting training...")
    trainer.train(resume_from_checkpoint=args.resume_from)

    print(f"Saving LoRA adapter to {output_dir}...")
    trainer.save_model(str(output_dir))
    tokenizer.save_pretrained(str(output_dir))

    print(f"\nTraining complete. LoRA adapter saved to {output_dir}")
    print(f"\nMerge the adapter into the base model:")
    print(f"  python sweagent/finetune/train_lora.py --merge-only --output {output_dir}")


def main() -> None:
    args = parse_args()
    if args.merge_only:
        merge_and_save(args)
    else:
        train(args)


if __name__ == "__main__":
    main()
