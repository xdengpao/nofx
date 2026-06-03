from __future__ import annotations


def compute_reward(
    old_value: float,
    new_value: float,
    action: float,
    previous_position: float,
    peak_value: float,
    drawdown_penalty_weight: float = 0.5,
    switch_penalty: float = 0.001,
) -> float:
    if old_value <= 0:
        return 0.0
    base_reward = (new_value - old_value) / old_value
    direction_changed = previous_position != 0 and action != 0 and (previous_position > 0) != (action > 0)
    penalty = switch_penalty if direction_changed else 0.0
    drawdown = 0.0
    if peak_value > 0 and new_value < peak_value:
        drawdown = (peak_value - new_value) / peak_value
    if drawdown > 0.05:
        penalty += drawdown * drawdown_penalty_weight
    return float(base_reward - penalty)
