from __future__ import annotations

import pandas as pd

from .features import observation_dim, build_observation
from .trading_env import CryptoTradingEnv


def test_build_observation_dimension_and_padding():
    df = _klines(5)
    obs = build_observation(df, account={"total_equity": 1000, "available_balance": 800}, config={"observation_window": 10})
    assert obs.shape == (observation_dim(10),)
    assert obs.dtype.name == "float32"
    assert obs[: 5 * 16].sum() == 0
    assert obs[-1] == 0.8


def test_crypto_trading_env_reset_and_step():
    env = CryptoTradingEnv(_klines(30), {"observation_window": 10})
    obs, info = env.reset(seed=7)
    assert obs.shape == (observation_dim(10),)
    assert info["equity"] > 0
    next_obs, reward, terminated, truncated, next_info = env.step([0.5])
    assert next_obs.shape == obs.shape
    assert isinstance(reward, float)
    assert terminated in (True, False)
    assert truncated in (True, False)
    assert next_info["index"] == 11


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
