from __future__ import annotations

import sqlite3
from dataclasses import dataclass
from pathlib import Path
from typing import Optional

import pandas as pd


@dataclass(frozen=True)
class HistoryDBLoader:
    db_path: str
    source: str = "binance-futures"

    def load_klines(
        self,
        symbol: str,
        timeframe: str,
        start: Optional[str] = None,
        end: Optional[str] = None,
    ) -> pd.DataFrame:
        return load_klines(self.db_path, symbol, timeframe, self.source, start, end)


def load_klines(
    db_path: str,
    symbol: str,
    timeframe: str,
    source: str = "binance-futures",
    start: Optional[str] = None,
    end: Optional[str] = None,
) -> pd.DataFrame:
    path = Path(db_path)
    if not path.exists():
        raise FileNotFoundError(f"historydb not found: {db_path}")
    params: list[object] = [source.lower(), symbol.upper(), timeframe.lower()]
    where = ["source = ?", "symbol = ?", "timeframe = ?"]
    if start:
        where.append("close_time_ms >= ?")
        params.append(_to_millis(start))
    if end:
        where.append("close_time_ms < ?")
        params.append(_to_millis(end))
    query = f"""
        SELECT open_time_ms, close_time_ms, open, high, low, close, volume
        FROM klines
        WHERE {' AND '.join(where)}
        ORDER BY open_time_ms ASC
    """
    with sqlite3.connect(path) as conn:
        df = pd.read_sql_query(query, conn, params=params)
    for col in ["open", "high", "low", "close", "volume"]:
        df[col] = pd.to_numeric(df[col], errors="coerce")
    return df


def _to_millis(value: str) -> int:
    return int(pd.Timestamp(value, tz="UTC").timestamp() * 1000)
