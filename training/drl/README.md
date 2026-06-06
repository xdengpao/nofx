# DRL-PPO Training

本目录包含 NOFX DRL-PPO 策略的 Python 训练、评估和 ONNX 导出脚本。训练产物和历史数据应写入统一 Storage Root，不直接写源码目录下的 `data/`、`models/`、`logs/`。

## Storage Root

- 原生运行默认 Storage Root 为 `/Volumes/light2/nofx`，目录必须已存在且可写。
- 可在 `config.json` 中设置 `storage.root`，或用 `NOFX_STORAGE_ROOT=/path/to/nofx` 覆盖。
- Docker Compose 默认把项目本地 `./runtime` 挂载到容器 `/app/runtime`，不会默认挂载外置盘。
- 如需让 Compose 使用外置盘，显式设置：

```bash
NOFX_RUNTIME_HOST_PATH=/Volumes/light2/nofx docker compose up -d --build
```

历史库默认位于 `<Storage Root>/backtest_data/nofx_history.sqlite`，模型位于 `<Storage Root>/models/drl/`，训练 job/log 位于 `<Storage Root>/logs/drl_ppo_train/jobs/`。

## Training API

训练 API 默认关闭。原生运行并确认 Python 依赖可用后，可以在 `config.json` 中启用：

```json
{
  "drl_ppo_train": {
    "enabled": true,
    "max_concurrency": 1,
    "python_bin": "python3"
  }
}
```

也可以用环境变量覆盖：

```bash
NOFX_DRL_TRAIN_API_ENABLED=true NOFX_STORAGE_ROOT=/Volumes/light2/nofx go run main.go
```

Docker 后端镜像使用 Debian/Python slim 运行时并内置 CPU 训练依赖。默认仍保持 `NOFX_DRL_TRAIN_API_ENABLED=false`，需要本机执行训练时显式开启，并共享 `/app/runtime` 作为 Storage Root。镜像构建会先从 PyTorch CPU 索引安装 `torch`，避免在本机 Docker 环境拉取 CUDA 组件。

## Install

```bash
cd training/drl
python3 -m venv .venv
. .venv/bin/activate
pip install -r requirements.txt
```

## Short Native Train

```bash
export NOFX_STORAGE_ROOT=/Volumes/light2/nofx
python scripts/train.py \
  --data-path "$NOFX_STORAGE_ROOT/backtest_data/nofx_history.sqlite" \
  --symbol BTCUSDT \
  --source binance-futures \
  --timeframe 1h \
  --start 2026-01-01 \
  --end 2026-02-01 \
  --total-timesteps 1000 \
  --observation-window 60 \
  --output "$NOFX_STORAGE_ROOT/models/drl/btc_ppo_smoke/model.onnx" \
  --progress-path "$NOFX_STORAGE_ROOT/logs/drl_ppo_train/btc_ppo_smoke_progress.jsonl"
```

## Evaluate

```bash
export NOFX_STORAGE_ROOT=/Volumes/light2/nofx
python scripts/evaluate.py \
  --data-path "$NOFX_STORAGE_ROOT/backtest_data/nofx_history.sqlite" \
  --model-path "$NOFX_STORAGE_ROOT/models/drl/btc_ppo_smoke/model.zip" \
  --symbol BTCUSDT \
  --source binance-futures \
  --timeframe 1h
```

## Export ONNX

```bash
export NOFX_STORAGE_ROOT=/Volumes/light2/nofx
python scripts/export_model.py \
  --model-path "$NOFX_STORAGE_ROOT/models/drl/btc_ppo_smoke/model.zip" \
  --output "$NOFX_STORAGE_ROOT/models/drl/btc_ppo_smoke/model.onnx" \
  --observation-window 60
```

The exported model uses one input named `observation` with shape `[batch, observation_window*16+3]`
and one output named `action` in the `[-1, 1]` range.

## UI Workflow

启用训练 API 后，前端会出现 `DRL-PPO` 页面入口。推荐顺序：

1. 检查 Storage 状态。
2. 选择 `source/symbol/timeframe/from/to` 并检查历史覆盖。
3. 覆盖不足时先补数据，再刷新 coverage。
4. 提交训练 job，查看 stdout/stderr 增量日志。
5. 对模型执行评估，只生成 staging config，不自动修改 `config.json`，不重启 trader。
