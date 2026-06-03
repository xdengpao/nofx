from __future__ import annotations

import sqlite3
from pathlib import Path

import pandas as pd

from .loader import load_klines
from .preprocessor import clean_klines, timeframe_to_pandas_freq


def test_load_klines_from_historydb(tmp_path: Path):
    db_path = tmp_path / "history.sqlite"
    _write_historydb(db_path)
    df = load_klines(str(db_path), "BTCUSDT", "4h", start="2026-01-01", end="2026-01-03")
    assert len(df) == 3
    assert list(df["close"]) == [100.0, 101.0, 102.0]


def test_clean_klines_sorts_deduplicates_and_aligns():
    df = pd.DataFrame(
        [
            _row(2, 102.0),
            _row(0, 100.0),
            _row(0, 100.0),
            _row(1, None),
        ]
    )
    cleaned = clean_klines(df, "4h")
    assert list(cleaned["close"]) == [100.0, 100.0, 102.0]
    assert timeframe_to_pandas_freq("15m") == "15min"


def _write_historydb(path: Path) -> None:
    conn = sqlite3.connect(path)
    conn.execute(
        """
        CREATE TABLE klines (
            source TEXT NOT NULL,
            symbol TEXT NOT NULL,
            timeframe TEXT NOT NULL,
            open_time_ms INTEGER NOT NULL,
            close_time_ms INTEGER NOT NULL,
            open REAL NOT NULL,
            high REAL NOT NULL,
            low REAL NOT NULL,
            close REAL NOT NULL,
            volume REAL NOT NULL
        )
        """
    )
    base = int(pd.Timestamp("2026-01-01T00:00:00Z").timestamp() * 1000)
    for i in range(3):
        open_ms = base + i * 14_400_000
        conn.execute(
            "INSERT INTO klines VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            ("binance-futures", "BTCUSDT", "4h", open_ms, open_ms + 14_400_000 - 1, 99 + i, 105 + i, 95 + i, 100 + i, 10 + i),
        )
    conn.commit()
    conn.close()


def _row(i: int, close: float | None) -> dict[str, float | int | None]:
    open_ms = i * 14_400_000
    value = close if close is not None else None
    return {
        "open_time_ms": open_ms,
        "close_time_ms": open_ms + 14_400_000 - 1,
        "open": value,
        "high": value,
        "low": value,
        "close": value,
        "volume": 10,
    }
