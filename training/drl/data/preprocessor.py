from __future__ import annotations

import numpy as np
import pandas as pd


def clean_klines(df: pd.DataFrame, timeframe: str = "4h") -> pd.DataFrame:
    if df is None or df.empty:
        raise ValueError("kline data is empty")
    out = df.copy()
    out.columns = [str(c).lower() for c in out.columns]
    required = ["open_time_ms", "close_time_ms", "open", "high", "low", "close", "volume"]
    missing = [c for c in required if c not in out.columns]
    if missing:
        raise ValueError(f"kline columns missing: {missing}")
    out = out.sort_values("open_time_ms").drop_duplicates("open_time_ms")
    for col in ["open", "high", "low", "close", "volume"]:
        out[col] = pd.to_numeric(out[col], errors="coerce")
    out[["open", "high", "low", "close"]] = out[["open", "high", "low", "close"]].ffill().bfill()
    out["volume"] = out["volume"].fillna(0.0).clip(lower=0.0)
    out = out.dropna(subset=["open", "high", "low", "close"])
    return _align_timeframe(out.reset_index(drop=True), timeframe)


def timeframe_to_pandas_freq(timeframe: str) -> str:
    tf = str(timeframe).lower().strip()
    mapping = {"3m": "3min", "15m": "15min", "1h": "1h", "4h": "4h"}
    if tf not in mapping:
        raise ValueError(f"unsupported timeframe: {timeframe}")
    return mapping[tf]


def _align_timeframe(df: pd.DataFrame, timeframe: str) -> pd.DataFrame:
    freq = timeframe_to_pandas_freq(timeframe)
    indexed = df.copy()
    indexed["timestamp"] = pd.to_datetime(indexed["open_time_ms"], unit="ms", utc=True)
    indexed = indexed.set_index("timestamp")
    full_index = pd.date_range(indexed.index.min(), indexed.index.max(), freq=freq, tz="UTC")
    indexed = indexed.reindex(full_index)
    for col in ["open", "high", "low", "close"]:
        indexed[col] = indexed[col].ffill().bfill()
    indexed["volume"] = indexed["volume"].fillna(0.0)
    indexed["open_time_ms"] = (indexed.index.view("int64") // 1_000_000).astype(np.int64)
    step_ms = int(pd.Timedelta(freq).total_seconds() * 1000)
    indexed["close_time_ms"] = indexed["open_time_ms"] + step_ms - 1
    return indexed.reset_index(drop=True)
