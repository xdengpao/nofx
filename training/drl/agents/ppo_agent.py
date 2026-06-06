from __future__ import annotations

import json
import time
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

    def train(self, klines: pd.DataFrame, total_timesteps: int, model_path: str, progress_path: str | None = None):
        try:
            from stable_baselines3 import PPO
            from stable_baselines3.common.callbacks import BaseCallback
        except ImportError as exc:
            raise RuntimeError("stable-baselines3未安装，无法执行PPO训练") from exc
        env = CryptoTradingEnv(klines, self.env_config)
        params = dict(PPO_HYPERPARAMS)
        if self.hyperparams:
            params.update(self.hyperparams)
        model = PPO(self.policy, env, verbose=1, **params)
        callback = make_progress_callback(progress_path, total_timesteps, BaseCallback) if progress_path else None
        model.learn(total_timesteps=total_timesteps, callback=callback)
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

    def train_rolling(self, data: pd.DataFrame, output_dir: str, progress_path: str | None = None) -> list[dict[str, Any]]:
        first_agent = self.agent_factory()
        train_window, val_window, step = self._effective_windows(first_agent.env_config)
        required_rows = train_window + val_window
        if len(data) < required_rows:
            raise ValueError(
                "滚动训练数据不足: "
                f"当前{len(data)}根K线，至少需要{required_rows}根 "
                f"(train_window={train_window}, val_window={val_window}, "
                f"observation_window={first_agent.env_config.observation_window})"
            )
        Path(output_dir).mkdir(parents=True, exist_ok=True)
        results: list[dict[str, Any]] = []
        for idx, start in enumerate(range(0, len(data) - train_window - val_window + 1, step)):
            train_data = data.iloc[start : start + train_window].reset_index(drop=True)
            val_data = data.iloc[start + train_window : start + train_window + val_window].reset_index(drop=True)
            model_path = str(Path(output_dir) / f"ppo_window_{start}.zip")
            agent = first_agent if idx == 0 else self.agent_factory()
            model = agent.train(train_data, self.total_timesteps, model_path, progress_path=progress_path)
            metrics = self.validate(model, val_data, agent.env_config)
            accepted = metrics["directional_accuracy"] >= self.min_da
            results.append({
                "start": start,
                "model_path": model_path,
                "accepted": accepted,
                "train_rows": len(train_data),
                "validation_rows": len(val_data),
                **metrics,
            })
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

    def _effective_windows(self, env_config: TradingEnvConfig) -> tuple[int, int, int]:
        observation_window = max(1, int(env_config.observation_window))
        min_env_rows = observation_window + 2
        train_window = max(1, int(self.train_window), min_env_rows)
        configured_val_window = max(1, int(self.val_window))
        if configured_val_window <= observation_window + 1:
            val_window = observation_window + configured_val_window + 1
        else:
            val_window = configured_val_window
        step = max(1, int(self.step))
        return train_window, val_window, step


def make_progress_callback(progress_path: str, total_timesteps: int, base_callback_cls: type):
    class ProgressJSONLCallback(base_callback_cls):
        def __init__(self, path: str, total: int):
            super().__init__()
            self.path = Path(path)
            self.total = int(total)
            self.interval = max(1, self.total // 100)
            self.started_at = time.time()

        def _on_step(self) -> bool:
            if self.num_timesteps % self.interval == 0 or self.num_timesteps >= self.total:
                self.path.parent.mkdir(parents=True, exist_ok=True)
                payload = {
                    "timesteps": int(self.num_timesteps),
                    "total_timesteps": self.total,
                    "elapsed_seconds": time.time() - self.started_at,
                    "timestamp": pd.Timestamp.utcnow().isoformat(),
                }
                with self.path.open("a", encoding="utf-8") as f:
                    f.write(json.dumps(payload, ensure_ascii=False) + "\n")
            return True

    return ProgressJSONLCallback(progress_path, total_timesteps)
