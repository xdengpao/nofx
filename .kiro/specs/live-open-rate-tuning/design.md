# Live Open Rate Tuning Design

## Config

Add optional `symbol_overrides` under `programmatic_strategy.entry_timing.entry_zone`:

```json
{
  "entry_timing": {
    "direct_structure_open": false,
    "require_fresh_trigger": true,
    "trigger_timeframe": "15m",
    "allowed_trigger_types": [
      "preview_2x15m_watchlist",
      "preview_3x15m_pilot",
      "pullback_retest_resume"
    ],
    "entry_zone": {
      "max_chase_ratio": 0.35,
      "min_remaining_net_rr": 2.5,
      "symbol_overrides": {
        "BTCUSDT": { "max_chase_ratio": 0.40, "min_remaining_net_rr": 2.20 },
        "ETHUSDT": { "max_chase_ratio": 0.40, "min_remaining_net_rr": 2.20 },
        "BNBUSDT": { "max_chase_ratio": 0.40, "min_remaining_net_rr": 2.20 }
      }
    },
    "pilot": {
      "enabled": true,
      "risk_fraction": 0.30,
      "min_confidence": 90
    }
  },
  "preview_signals": {
    "allow_pilot_open": true,
    "pilot_risk_fraction": 0.30,
    "pilot_min_confidence": 90
  }
}
```

`symbol_overrides` are also mapped through normalized profile, runtime policy, and `config_hash`.

## Entry Threshold Evaluation

Add a small helper in the Chanlun engine to resolve the effective entry zone for a symbol:

1. Start from global `EntryZone`.
2. Apply `symbol_overrides[Normalize(symbol)]` values when set.
3. Use the effective `max_chase_ratio` in entry window and pullback detection.
4. Use the effective `min_remaining_net_rr` in both entry window checks and the later signal freshness guard, so one stricter global check does not re-reject a symbol override.

The default path for symbols without overrides remains unchanged.

## Preview Pilot

Existing preview pilot execution remains the implementation path. Config changes only enable it explicitly:

- `preview_3x15m` may produce a pilot open only after all existing checks pass.
- `preview_2x15m` remains watchlist-only.
- Direct 1h structure open remains disabled.

## Rejection Daily Report

Extend `logger/replay.go` with a read-only `BuildOpenRejectionDailyReport(records, maxNearMisses)` helper.

The helper reads:

- structured `DecisionAction` open rejections;
- strategy diagnostic messages that explain blocked programmatic signals.

It classifies common reasons such as:

- `entry_chase_ratio_too_high`;
- `remaining_net_rr_too_low`;
- `structure_background_only`;
- `invalid_stop_take_profit_structure`;
- `target_already_crossed`;
- `signal_expired`.

Near-miss extraction parses RR and chase diagnostics to calculate `gap = abs(threshold - value)` and sorts smallest gaps first.

Expose the report from `cmd/replay` behind `-open-rejection-daily`.

## Safety

Do not relax:

- stale/expired signal rejection;
- target-crossed guard;
- invalid SL/TP shape rejection;
- open gate and risk-budget checks;
- OI Top configuration.

