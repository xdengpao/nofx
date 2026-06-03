from __future__ import annotations

from dataclasses import dataclass
from typing import Mapping, Optional

import numpy as np
import pandas as pd


FEATURE_PER_STEP = 16
ACCOUNT_FEATURES = 3


@dataclass(frozen=True)
class FeatureConfig:
    observation_window: int = 60
    ema_short_period: int = 12
    ema_long_period: int = 26
    rsi_period: int = 14
    atr_period: int = 14
    cci_period: int = 20
    bollinger_period: int = 20
    bollinger_std_dev: float = 2.0
    max_position_pct: float = 0.3


def observation_dim(observation_window: int) -> int:
    return int(observation_window) * FEATURE_PER_STEP + ACCOUNT_FEATURES


def build_observation(
    klines: pd.DataFrame,
    account: Optional[Mapping[str, float]] = None,
    position: Optional[Mapping[str, float | str]] = None,
    config: FeatureConfig | Mapping[str, float | int] | None = None,
) -> np.ndarray:
    cfg = _feature_config(config)
    df = _normalize_klines(klines)
    window = cfg.observation_window
    out = np.zeros(observation_dim(window), dtype=np.float32)
    if df.empty:
        _fill_account(out, account, position, cfg)
        return out

    if len(df) >= window:
        series = df.tail(window).copy()
        offset = 0
    else:
        series = df.copy()
        offset = window - len(series)

    ind = _indicator_frame(series, cfg)
    close_stats = _stats(series["close"].to_numpy(dtype=float))
    volume_stats = _stats(series["volume"].to_numpy(dtype=float))

    raw = np.zeros((window, FEATURE_PER_STEP), dtype=float)
    values = np.zeros((window, FEATURE_PER_STEP), dtype=np.float32)
    for local_idx, (_, row) in enumerate(series.iterrows()):
        target = offset + local_idx
        close = max(float(row["close"]), 1e-12)
        raw[target] = np.array(
            [
                row["open"],
                row["high"],
                row["low"],
                row["close"],
                row["volume"],
                ind["macd"].iloc[local_idx],
                ind["macd_signal"].iloc[local_idx],
                ind["macd_hist"].iloc[local_idx],
                ind["ema_short"].iloc[local_idx],
                ind["ema_long"].iloc[local_idx],
                ind["rsi"].iloc[local_idx],
                ind["atr"].iloc[local_idx],
                ind["cci"].iloc[local_idx],
                ind["bollinger_upper"].iloc[local_idx],
                ind["bollinger_middle"].iloc[local_idx],
                ind["bollinger_lower"].iloc[local_idx],
            ],
            dtype=float,
        )
        values[target] = np.array(
            [
                _zscore(row["open"], close_stats),
                _zscore(row["high"], close_stats),
                _zscore(row["low"], close_stats),
                _zscore(row["close"], close_stats),
                _zscore(row["volume"], volume_stats),
                ind["macd"].iloc[local_idx] / close,
                ind["macd_signal"].iloc[local_idx] / close,
                ind["macd_hist"].iloc[local_idx] / close,
                (ind["ema_short"].iloc[local_idx] - row["close"]) / close,
                (ind["ema_long"].iloc[local_idx] - row["close"]) / close,
                np.clip(ind["rsi"].iloc[local_idx] / 100.0, 0.0, 1.0),
                ind["atr"].iloc[local_idx] / close,
                np.clip(ind["cci"].iloc[local_idx] / 200.0, -1.0, 1.0),
                (ind["bollinger_upper"].iloc[local_idx] - row["close"]) / close,
                (ind["bollinger_middle"].iloc[local_idx] - row["close"]) / close,
                (ind["bollinger_lower"].iloc[local_idx] - row["close"]) / close,
            ],
            dtype=np.float32,
        )

    out[: window * FEATURE_PER_STEP] = values.reshape(-1)
    _fill_account(out, account, position, cfg)
    return np.nan_to_num(out, nan=0.0, posinf=0.0, neginf=0.0).astype(np.float32)


def _feature_config(config: FeatureConfig | Mapping[str, float | int] | None) -> FeatureConfig:
    if isinstance(config, FeatureConfig):
        return config
    if config is None:
        return FeatureConfig()
    return FeatureConfig(
        observation_window=int(config.get("observation_window", 60)),
        ema_short_period=int(config.get("ema_short_period", 12)),
        ema_long_period=int(config.get("ema_long_period", 26)),
        rsi_period=int(config.get("rsi_period", 14)),
        atr_period=int(config.get("atr_period", 14)),
        cci_period=int(config.get("cci_period", 20)),
        bollinger_period=int(config.get("bollinger_period", 20)),
        bollinger_std_dev=float(config.get("bollinger_std_dev", 2.0)),
        max_position_pct=float(config.get("max_position_pct", 0.3)),
    )


def _normalize_klines(klines: pd.DataFrame) -> pd.DataFrame:
    if klines is None:
        return pd.DataFrame(columns=["open", "high", "low", "close", "volume"])
    df = klines.copy()
    df.columns = [str(c).lower() for c in df.columns]
    required = ["open", "high", "low", "close", "volume"]
    missing = [c for c in required if c not in df.columns]
    if missing:
        raise ValueError(f"kline columns missing: {missing}")
    if "close_time_ms" in df.columns:
        df = df.sort_values("close_time_ms")
    elif "timestamp" in df.columns:
        df = df.sort_values("timestamp")
    return df.drop_duplicates().reset_index(drop=True)


def _indicator_frame(df: pd.DataFrame, cfg: FeatureConfig) -> pd.DataFrame:
    close = df["close"].astype(float)
    high = df["high"].astype(float)
    low = df["low"].astype(float)
    typical = (high + low + close) / 3.0
    ema_short = close.ewm(span=cfg.ema_short_period, adjust=False).mean()
    ema_long = close.ewm(span=cfg.ema_long_period, adjust=False).mean()
    macd = ema_short - ema_long
    macd_signal = macd.ewm(span=9, adjust=False).mean()
    macd_hist = macd - macd_signal
    atr = _atr(df, cfg.atr_period)
    middle = close.rolling(cfg.bollinger_period, min_periods=1).mean()
    std = close.rolling(cfg.bollinger_period, min_periods=1).std(ddof=0).fillna(0.0)
    cci_ma = typical.rolling(cfg.cci_period, min_periods=1).mean()
    cci_dev = (typical - cci_ma).abs().rolling(cfg.cci_period, min_periods=1).mean()
    cci = (typical - cci_ma) / (0.015 * cci_dev.replace(0, np.nan))
    return pd.DataFrame(
        {
            "ema_short": ema_short,
            "ema_long": ema_long,
            "macd": macd,
            "macd_signal": macd_signal,
            "macd_hist": macd_hist,
            "rsi": _rsi(close, cfg.rsi_period),
            "atr": atr,
            "cci": cci.fillna(0.0),
            "bollinger_upper": middle + cfg.bollinger_std_dev * std,
            "bollinger_middle": middle,
            "bollinger_lower": middle - cfg.bollinger_std_dev * std,
        }
    ).fillna(0.0)


def _rsi(close: pd.Series, period: int) -> pd.Series:
    delta = close.diff().fillna(0.0)
    gain = delta.clip(lower=0.0).ewm(alpha=1.0 / period, adjust=False).mean()
    loss = (-delta.clip(upper=0.0)).ewm(alpha=1.0 / period, adjust=False).mean()
    rs = gain / loss.replace(0, np.nan)
    return (100.0 - 100.0 / (1.0 + rs)).fillna(50.0)


def _atr(df: pd.DataFrame, period: int) -> pd.Series:
    high = df["high"].astype(float)
    low = df["low"].astype(float)
    close = df["close"].astype(float)
    prev_close = close.shift(1).fillna(close)
    tr = pd.concat([(high - low).abs(), (high - prev_close).abs(), (low - prev_close).abs()], axis=1).max(axis=1)
    return tr.ewm(alpha=1.0 / period, adjust=False).mean().fillna(0.0)


def _stats(values: np.ndarray) -> tuple[float, float]:
    if values.size == 0:
        return 0.0, 1.0
    mean = float(np.mean(values))
    std = float(np.std(values))
    return mean, max(std, 1e-8)


def _zscore(value: float, stats: tuple[float, float]) -> float:
    mean, std = stats
    return float(np.clip((float(value) - mean) / std, -10.0, 10.0))


def _fill_account(
    out: np.ndarray,
    account: Optional[Mapping[str, float]],
    position: Optional[Mapping[str, float | str]],
    cfg: FeatureConfig,
) -> None:
    account = account or {}
    position = position or {}
    total_equity = max(float(account.get("total_equity", 0.0)), 1e-12)
    available = float(account.get("available_balance", total_equity))
    margin_used = float(account.get("margin_used", 0.0))
    pnl = float(position.get("unrealized_pnl", account.get("total_pnl", 0.0)) or 0.0)
    side = str(position.get("side", "")).lower()
    direction = 1.0 if side == "long" else -1.0 if side == "short" else 0.0
    position_pct = margin_used / total_equity
    if "margin_used" in position:
        position_pct = float(position.get("margin_used", 0.0) or 0.0) / total_equity
    out[-3:] = np.array(
        [
            np.clip(direction * position_pct / max(cfg.max_position_pct, 1e-8), -1.0, 1.0),
            np.clip(pnl / total_equity, -1.0, 1.0),
            np.clip(available / total_equity, 0.0, 1.0),
        ],
        dtype=np.float32,
    )
