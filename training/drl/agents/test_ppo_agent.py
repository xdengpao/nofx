from __future__ import annotations

from pathlib import Path

import numpy as np
import pandas as pd
import pytest

from .ppo_agent import RollingWindowTrainer

try:
    from ..env.trading_env import TradingEnvConfig
except ImportError:
    from env.trading_env import TradingEnvConfig


def test_rolling_trainer_expands_validation_window_for_observation_context(tmp_path: Path):
    trained_lengths: list[int] = []

    class FakeModel:
        def predict(self, obs, deterministic=True):
            return np.array([0.0], dtype=np.float32), None

    class FakeAgent:
        env_config = TradingEnvConfig(observation_window=60)

        def train(self, klines, total_timesteps, model_path, progress_path=None):
            trained_lengths.append(len(klines))
            return FakeModel()

    trainer = RollingWindowTrainer(
        lambda: FakeAgent(),
        train_window=90,
        val_window=14,
        step=7,
        total_timesteps=1000,
    )

    results = trainer.train_rolling(_klines(165), str(tmp_path))

    assert trained_lengths == [90]
    assert results[0]["train_rows"] == 90
    assert results[0]["validation_rows"] == 75


def test_rolling_trainer_reports_required_rows_for_short_data(tmp_path: Path):
    trainer = RollingWindowTrainer(
        lambda: _FakeAgent(observation_window=60),
        train_window=90,
        val_window=14,
        total_timesteps=1000,
    )

    with pytest.raises(ValueError, match="至少需要165根"):
        trainer.train_rolling(_klines(120), str(tmp_path))


class _FakeAgent:
    def __init__(self, observation_window: int):
        self.env_config = TradingEnvConfig(observation_window=observation_window)


def _klines(n: int) -> pd.DataFrame:
    rows = []
    base = pd.Timestamp("2026-01-01T00:00:00Z")
    for i in range(n):
        close = 1000 + i * 2
        ts = base + pd.Timedelta(hours=4 * i)
        rows.append(
            {
                "open_time_ms": int(ts.timestamp() * 1000),
                "close_time_ms": int((ts + pd.Timedelta(hours=4)).timestamp() * 1000) - 1,
                "open": close - 1,
                "high": close + 5,
                "low": close - 5,
                "close": close,
                "volume": 100 + i,
            }
        )
    return pd.DataFrame(rows)
