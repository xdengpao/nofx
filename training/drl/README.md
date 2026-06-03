# DRL PPO Training

This directory contains the Python training side for the NOFX DRL strategy.

## Install

```bash
cd training/drl
python3 -m venv .venv
. .venv/bin/activate
pip install -r requirements.txt
```

## Train

```bash
python scripts/train.py \
  --data-path ../../backtest_data/nofx_history.sqlite \
  --symbol ETHUSDT \
  --timeframe 4h \
  --start 2025-01-01 \
  --end 2026-01-01 \
  --output ../../models/drl/eth_ppo_v1.zip
```

## Evaluate

```bash
python scripts/evaluate.py \
  --data-path ../../backtest_data/nofx_history.sqlite \
  --model-path ../../models/drl/eth_ppo_v1.zip \
  --symbol ETHUSDT \
  --timeframe 4h
```

## Export ONNX

```bash
python scripts/export_model.py \
  --model-path ../../models/drl/eth_ppo_v1.zip \
  --output ../../models/drl/eth_ppo_v1.onnx \
  --observation-window 60
```

The exported model uses one input named `observation` with shape `[batch, observation_window*16+3]`
and one output named `action` in the `[-1, 1]` range.
