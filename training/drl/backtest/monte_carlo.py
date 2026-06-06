from __future__ import annotations

import numpy as np


def monte_carlo_paths(
    prices: list[float] | np.ndarray,
    paths: int = 2000,
    steps: int = 100,
    seed: int | None = None,
) -> np.ndarray:
    base = np.asarray(prices, dtype=float)
    if base.size < 2:
        raise ValueError("at least two prices are required")
    returns = np.diff(np.log(np.maximum(base, 1e-12)))
    mu = float(np.mean(returns))
    sigma = float(np.std(returns))
    rng = np.random.default_rng(seed)
    out = np.zeros((paths, steps + 1), dtype=float)
    out[:, 0] = base[-1]
    for i in range(1, steps + 1):
        z = rng.standard_normal(paths)
        out[:, i] = out[:, i - 1] * np.exp((mu - sigma * sigma / 2.0) + sigma * z)
    return out


def monte_carlo_risk(path_values: np.ndarray, initial_value: float) -> dict[str, float]:
    terminal = np.asarray(path_values, dtype=float)[:, -1]
    returns = terminal / max(initial_value, 1e-12) - 1.0
    losses = -returns
    var95 = float(np.quantile(losses, 0.95))
    var99 = float(np.quantile(losses, 0.99))
    tail = losses[losses >= var95]
    return {
        "var_95": var95,
        "var_99": var99,
        "cvar": float(tail.mean()) if tail.size else 0.0,
        "loss_probability": float((returns < 0).mean()),
    }
