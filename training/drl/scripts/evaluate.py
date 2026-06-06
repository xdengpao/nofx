#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from agents.ppo_agent import PPOAgent
from backtest.metrics import calculate_metrics, directional_accuracy
from data.loader import load_klines
from data.preprocessor import clean_klines
from env.trading_env import CryptoTradingEnv, TradingEnvConfig


def main() -> int:
    parser = argparse.ArgumentParser(description="Evaluate DRL PPO model")
    parser.add_argument("--data-path", required=True)
    parser.add_argument("--model-path", required=True)
    parser.add_argument("--symbol", required=True)
    parser.add_argument("--source", default="binance-futures")
    parser.add_argument("--timeframe", default="4h")
    parser.add_argument("--start")
    parser.add_argument("--end")
    parser.add_argument("--observation-window", type=int, default=60)
    args = parser.parse_args()

    df = clean_klines(load_klines(args.data_path, args.symbol, args.timeframe, args.source, args.start, args.end), args.timeframe)
    env_config = TradingEnvConfig(observation_window=args.observation_window)
    env = CryptoTradingEnv(df, env_config)
    model = PPOAgent(env_config).load(args.model_path)
    obs, info = env.reset(seed=42)
    predictions: list[float] = []
    equity = [float(info["equity"])]
    terminated = truncated = False
    while not (terminated or truncated):
        action, _ = model.predict(obs, deterministic=True)
        predictions.append(float(np.asarray(action).reshape(-1)[0]))
        obs, _, terminated, truncated, info = env.step(action)
        equity.append(float(info["equity"]))
    metrics = calculate_metrics(equity)
    metrics["directional_accuracy"] = directional_accuracy(predictions, df["close"].to_numpy(), env_config.action_threshold)
    print(json.dumps(metrics, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
