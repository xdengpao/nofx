from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable

import numpy as np
import pandas as pd

try:
    from ..env.trading_env import CryptoTradingEnv, TradingEnvConfig
    from ..backtest.metrics import directional_accuracy
except ImportError:
    from env.trading_env import CryptoTradingEnv, TradingEnvConfig
    from backtest.metrics import directional_accuracy


PPO_HYPERPARAMS: dict[str, Any] = {
    "learning_rate": 3e-4,
    "n_steps": 2048,
    "batch_size": 64,
    "n_epochs": 10,
    "gamma": 0.99,
    "gae_lambda": 0.95,
    "clip_range": 0.2,
    "ent_coef": 0.01,
    "vf_coef": 0.5,
    "max_grad_norm": 0.5,
    "policy_kwargs": {"net_arch": [256, 256]},
}


@dataclass
class PPOAgent:
    env_config: TradingEnvConfig
    hyperparams: dict[str, Any] | None = None
    policy: str = "MlpPolicy"

    def train(self, klines: pd.DataFrame, total_timesteps: int, model_path: str):
        try:
            from stable_baselines3 import PPO
        except ImportError as exc:
            raise RuntimeError("stable-baselines3未安装，无法执行PPO训练") from exc
        env = CryptoTradingEnv(klines, self.env_config)
        params = dict(PPO_HYPERPARAMS)
        if self.hyperparams:
            params.update(self.hyperparams)
        model = PPO(self.policy, env, verbose=1, **params)
        model.learn(total_timesteps=total_timesteps)
        Path(model_path).parent.mkdir(parents=True, exist_ok=True)
        model.save(model_path)
        return model

    def load(self, model_path: str):
        try:
            from stable_baselines3 import PPO
        except ImportError as exc:
            raise RuntimeError("stable-baselines3未安装，无法加载PPO模型") from exc
        return PPO.load(model_path)


@dataclass
class RollingWindowTrainer:
    agent_factory: Callable[[], PPOAgent]
    min_da: float = 0.55
    train_window: int = 90
    val_window: int = 14
    step: int = 7
    total_timesteps: int = 100_000

    def train_rolling(self, data: pd.DataFrame, output_dir: str) -> list[dict[str, Any]]:
        if len(data) <= self.train_window + self.val_window:
            raise ValueError("滚动训练数据不足")
        Path(output_dir).mkdir(parents=True, exist_ok=True)
        results: list[dict[str, Any]] = []
        for start in range(0, len(data) - self.train_window - self.val_window + 1, self.step):
            train_data = data.iloc[start : start + self.train_window].reset_index(drop=True)
            val_data = data.iloc[start + self.train_window : start + self.train_window + self.val_window].reset_index(drop=True)
            model_path = str(Path(output_dir) / f"ppo_window_{start}.zip")
            agent = self.agent_factory()
            model = agent.train(train_data, self.total_timesteps, model_path)
            metrics = self.validate(model, val_data, agent.env_config)
            accepted = metrics["directional_accuracy"] >= self.min_da
            results.append({"start": start, "model_path": model_path, "accepted": accepted, **metrics})
        return results

    def validate(self, model: Any, val_data: pd.DataFrame, env_config: TradingEnvConfig) -> dict[str, float]:
        env = CryptoTradingEnv(val_data, env_config)
        obs, _ = env.reset(seed=42)
        predictions: list[float] = []
        closes = val_data["close"].astype(float).to_numpy()
        terminated = truncated = False
        while not (terminated or truncated):
            action, _ = model.predict(obs, deterministic=True)
            raw = float(np.asarray(action).reshape(-1)[0])
            predictions.append(raw)
            obs, _, terminated, truncated, _ = env.step(action)
        return {"directional_accuracy": directional_accuracy(predictions, closes, env_config.action_threshold)}
