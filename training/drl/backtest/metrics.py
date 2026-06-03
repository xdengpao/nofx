from __future__ import annotations

import numpy as np
import pandas as pd


def calculate_metrics(equity: list[float] | np.ndarray, trades: list[dict] | None = None, periods_per_year: int = 365) -> dict[str, float]:
    values = np.asarray(equity, dtype=float)
    if values.size < 2:
        return {"annual_return": 0.0, "sharpe": 0.0, "sortino": 0.0, "max_drawdown": 0.0, "win_rate": 0.0}
    returns = np.diff(values) / np.maximum(values[:-1], 1e-12)
    total_return = values[-1] / max(values[0], 1e-12) - 1.0
    annual_return = (1.0 + total_return) ** (periods_per_year / max(len(returns), 1)) - 1.0
    sharpe = _ratio(returns.mean(), returns.std(ddof=0)) * np.sqrt(periods_per_year)
    downside = returns[returns < 0]
    sortino = _ratio(returns.mean(), downside.std(ddof=0) if downside.size else 0.0) * np.sqrt(periods_per_year)
    running_peak = np.maximum.accumulate(values)
    max_drawdown = float(np.max((running_peak - values) / np.maximum(running_peak, 1e-12)))
    win_rate = _win_rate(trades or [])
    return {
        "annual_return": float(annual_return),
        "sharpe": float(sharpe),
        "sortino": float(sortino),
        "max_drawdown": max_drawdown,
        "win_rate": win_rate,
    }


def directional_accuracy(predictions: list[float] | np.ndarray, closes: list[float] | np.ndarray, threshold: float = 0.1) -> float:
    preds = np.asarray(predictions, dtype=float)
    prices = np.asarray(closes, dtype=float)
    n = min(preds.size, max(prices.size - 1, 0))
    if n <= 0:
        return 0.0
    predicted = np.where(preds[:n] > threshold, 1, np.where(preds[:n] < -threshold, -1, 0))
    actual = np.sign(np.diff(prices[: n + 1]))
    valid = actual != 0
    if not valid.any():
        return 0.0
    return float((predicted[valid] == actual[valid]).mean())


def _ratio(mean: float, std: float) -> float:
    if std <= 1e-12:
        return 0.0
    return float(mean / std)


def _win_rate(trades: list[dict]) -> float:
    if not trades:
        return 0.0
    wins = 0
    total = 0
    for trade in trades:
        pnl = float(trade.get("realized_pnl", trade.get("pnl", 0.0)))
        total += 1
        if pnl > 0:
            wins += 1
    return wins / total if total else 0.0
