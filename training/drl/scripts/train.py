#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from agents.ppo_agent import PPOAgent, RollingWindowTrainer
from agents.export import export_policy_to_onnx
from data.loader import load_klines
from data.preprocessor import clean_klines
from env.features import observation_dim
from env.trading_env import TradingEnvConfig


def main() -> int:
    parser = argparse.ArgumentParser(description="Train DRL PPO strategy model")
    parser.add_argument("--data-path", required=True)
    parser.add_argument("--symbol", required=True)
    parser.add_argument("--source", default="binance-futures")
    parser.add_argument("--timeframe", default="4h")
    parser.add_argument("--start")
    parser.add_argument("--end")
    parser.add_argument("--output", required=True)
    parser.add_argument("--total-timesteps", type=int, default=100_000)
    parser.add_argument("--n-steps", type=int)
    parser.add_argument("--batch-size", type=int)
    parser.add_argument("--n-epochs", type=int)
    parser.add_argument("--observation-window", type=int, default=60)
    parser.add_argument("--initial-balance", type=float, default=10_000.0)
    parser.add_argument("--taker-fee", type=float, default=0.0005)
    parser.add_argument("--maker-fee", type=float, default=0.0002)
    parser.add_argument("--slippage", type=float, default=0.0003)
    parser.add_argument("--rolling", action="store_true")
    args = parser.parse_args()

    df = load_klines(args.data_path, args.symbol, args.timeframe, args.source, args.start, args.end)
    df = clean_klines(df, args.timeframe)
    env_config = TradingEnvConfig(
        observation_window=args.observation_window,
        initial_balance=args.initial_balance,
        taker_fee=args.taker_fee,
        maker_fee=args.maker_fee,
        slippage=args.slippage,
    )
    hyperparams = {}
    if args.n_steps:
        hyperparams["n_steps"] = args.n_steps
    if args.batch_size:
        hyperparams["batch_size"] = args.batch_size
    if args.n_epochs:
        hyperparams["n_epochs"] = args.n_epochs
    agent = PPOAgent(env_config=env_config, hyperparams=hyperparams or None)
    output = Path(args.output)
    if args.rolling:
        trainer = RollingWindowTrainer(lambda: PPOAgent(env_config=env_config, hyperparams=hyperparams or None), total_timesteps=args.total_timesteps)
        result = trainer.train_rolling(df, str(output.parent))
        output.write_text(json.dumps(result, indent=2), encoding="utf-8")
    else:
        model_output = output
        if output.suffix.lower() == ".onnx":
            model_output = output.with_suffix(".zip")
        agent.train(df, args.total_timesteps, str(model_output))
        if output.suffix.lower() == ".onnx":
            export_policy_to_onnx(str(model_output), str(output), observation_dim(args.observation_window))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
