from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Optional

import numpy as np
import pandas as pd

from .features import FeatureConfig, build_observation, observation_dim
from .rewards import compute_reward

try:
    import gymnasium as gym
    from gymnasium import spaces
except ImportError:
    class _FallbackEnv:
        metadata: dict[str, Any] = {}

        def reset(self, seed: Optional[int] = None, options: Optional[dict[str, Any]] = None):
            if seed is not None:
                np.random.seed(seed)

    class _Box:
        def __init__(self, low: float, high: float, shape: tuple[int, ...], dtype: Any):
            self.low = low
            self.high = high
            self.shape = shape
            self.dtype = dtype

        def sample(self) -> np.ndarray:
            low = -1.0 if not np.isfinite(self.low) else self.low
            high = 1.0 if not np.isfinite(self.high) else self.high
            return np.random.uniform(low, high, size=self.shape).astype(self.dtype)

    class _FallbackGym:
        Env = _FallbackEnv

    class _FallbackSpaces:
        Box = _Box

    gym = _FallbackGym()
    spaces = _FallbackSpaces()


@dataclass
class TradingEnvConfig:
    observation_window: int = 60
    initial_balance: float = 10_000.0
    taker_fee: float = 0.0005
    maker_fee: float = 0.0002
    slippage: float = 0.0003
    max_position_pct: float = 0.3
    max_drawdown: float = 0.20
    action_threshold: float = 0.1
    random_reset: bool = False


class CryptoTradingEnv(gym.Env):
    metadata = {"render_modes": []}

    def __init__(self, klines: pd.DataFrame, config: TradingEnvConfig | dict[str, Any] | None = None):
        self.config = _env_config(config)
        self.klines = _normalize_frame(klines)
        if len(self.klines) < self.config.observation_window + 2:
            raise ValueError("历史K线数量不足以创建DRL训练环境")
        dim = observation_dim(self.config.observation_window)
        self.observation_space = spaces.Box(low=-np.inf, high=np.inf, shape=(dim,), dtype=np.float32)
        self.action_space = spaces.Box(low=-1.0, high=1.0, shape=(1,), dtype=np.float32)
        self._rng = np.random.default_rng()
        self._reset_state()

    def reset(self, seed: Optional[int] = None, options: Optional[dict[str, Any]] = None):
        super().reset(seed=seed)
        if seed is not None:
            self._rng = np.random.default_rng(seed)
        self._reset_state()
        if self.config.random_reset:
            max_start = max(self.config.observation_window, len(self.klines) - 2)
            self.index = int(self._rng.integers(self.config.observation_window, max_start))
        return self._observation(), self._info()

    def step(self, action):
        raw_action = float(np.asarray(action, dtype=np.float32).reshape(-1)[0])
        raw_action = float(np.clip(raw_action, -1.0, 1.0))
        price = float(self.klines.iloc[self.index]["close"])
        old_value = self._account_value(price)
        previous_position = self.position_qty
        self._rebalance(raw_action, price)
        self.index += 1
        new_price = float(self.klines.iloc[self.index]["close"])
        new_value = self._account_value(new_price)
        self.peak_value = max(self.peak_value, new_value)
        reward = compute_reward(old_value, new_value, raw_action, previous_position, self.peak_value)
        drawdown = 0.0 if self.peak_value <= 0 else (self.peak_value - new_value) / self.peak_value
        terminated = drawdown >= self.config.max_drawdown
        truncated = self.index >= len(self.klines) - 1
        return self._observation(), reward, terminated, truncated, self._info()

    def render(self):
        return self._info()

    def _reset_state(self) -> None:
        self.index = self.config.observation_window
        self.cash = float(self.config.initial_balance)
        self.position_qty = 0.0
        self.entry_price = 0.0
        self.peak_value = float(self.config.initial_balance)
        self.last_fee = 0.0

    def _rebalance(self, action: float, price: float) -> None:
        value = self._account_value(price)
        if abs(action) <= self.config.action_threshold:
            target_qty = 0.0
        else:
            target_notional = action * self.config.max_position_pct * value
            exec_price = price * (1.0 + self.config.slippage * (1.0 if action > self.position_qty else -1.0))
            target_qty = target_notional / max(exec_price, 1e-12)
        delta_qty = target_qty - self.position_qty
        turnover = abs(delta_qty) * price
        fee = turnover * self.config.taker_fee
        self.cash -= delta_qty * price + fee
        self.position_qty = target_qty
        self.last_fee = fee
        if abs(self.position_qty) > 0:
            self.entry_price = price
        else:
            self.entry_price = 0.0

    def _account_value(self, price: float) -> float:
        return float(self.cash + self.position_qty * price)

    def _observation(self) -> np.ndarray:
        window_df = self.klines.iloc[: self.index + 1]
        price = float(self.klines.iloc[self.index]["close"])
        value = self._account_value(price)
        position_notional = abs(self.position_qty) * price
        position = None
        if abs(self.position_qty) > 0:
            position = {
                "side": "long" if self.position_qty > 0 else "short",
                "margin_used": position_notional,
                "unrealized_pnl": self.position_qty * (price - self.entry_price),
            }
        return build_observation(
            window_df,
            account={"total_equity": value, "available_balance": max(self.cash, 0.0), "margin_used": position_notional},
            position=position,
            config=FeatureConfig(
                observation_window=self.config.observation_window,
                max_position_pct=self.config.max_position_pct,
            ),
        )

    def _info(self) -> dict[str, float | int]:
        price = float(self.klines.iloc[self.index]["close"])
        value = self._account_value(price)
        return {
            "index": self.index,
            "price": price,
            "equity": value,
            "cash": self.cash,
            "position_qty": self.position_qty,
            "last_fee": self.last_fee,
        }


def _env_config(config: TradingEnvConfig | dict[str, Any] | None) -> TradingEnvConfig:
    if isinstance(config, TradingEnvConfig):
        return config
    if config is None:
        return TradingEnvConfig()
    return TradingEnvConfig(**{k: v for k, v in config.items() if hasattr(TradingEnvConfig, k)})


def _normalize_frame(klines: pd.DataFrame) -> pd.DataFrame:
    df = klines.copy()
    df.columns = [str(c).lower() for c in df.columns]
    if "close_time_ms" in df.columns:
        df = df.sort_values("close_time_ms")
    return df.reset_index(drop=True)
