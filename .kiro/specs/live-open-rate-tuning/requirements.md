# Live Open Rate Tuning Requirements

## Background

161 `aster_deepseek` has been running in `programmatic` mode. The most recent 24h log review showed healthy services and continuous decision cycles, but no live opens. The dominant causes were conservative entry timing and quality gates:

- 1h structures were often kept as background because `direct_structure_open=false` and no fresh 15m trigger had passed.
- Several BTC/BNB/SOL candidates were close to passing but failed `min_remaining_net_rr=2.5` or `max_chase_ratio=0.35`.
- Stale, target-crossed, and invalid SL/TP candidates were correctly rejected and must remain protected.
- OI Top is intentionally out of scope for this change.

## Requirements

### R1 Preview Pilot Activation

WHEN a `preview_3x15m` signal passes existing open gate, SL/TP structure, missed-target, RR, and risk-budget checks, THE system SHALL be able to open a capped pilot position.

The pilot position SHALL use materially lower risk than a full confirmed 1h entry. The live tuning target is `risk_fraction=0.30` and `min_confidence=90`.

The system SHALL keep `preview_2x15m` as watchlist-only by default.

### R2 Symbol-Specific Entry Zone Thresholds

The entry zone policy SHALL support symbol-specific overrides for:

- `max_chase_ratio`;
- `min_remaining_net_rr`.

Overrides SHALL be normalized using the same symbol normalization as the programmatic symbol pool.

If no override exists for a symbol, the system SHALL preserve the global thresholds.

The live tuning target is:

- `BTCUSDT`, `ETHUSDT`, `BNBUSDT`: `max_chase_ratio=0.40`, `min_remaining_net_rr=2.20`;
- all other symbols: preserve global `max_chase_ratio=0.35`, `min_remaining_net_rr=2.50`.

### R3 Fresh 15m Trigger Utilization

The strategy SHALL continue to avoid direct 1h structure chasing by default.

The strategy SHALL use existing lower-timeframe trigger types, especially `preview_3x15m_pilot` and `pullback_retest_resume`, to reduce cycles that only wait for a new 1h close.

The change SHALL NOT bypass target-crossed, invalid-structure, stale-signal, open-gate, BTC environment, ADX/DI, position-limit, or risk-budget checks.

### R4 Open Rejection Daily Report

The replay tooling SHALL provide a daily open-rejection report that aggregates:

- rejected/open-blocked count by reason code;
- count by symbol;
- count by broad bucket;
- recent rejection examples;
- near-miss candidates where RR or chase threshold was close to passing.

The report SHALL read existing decision logs only and SHALL NOT change live trading state.

### R5 Out Of Scope

OI Top data source configuration SHALL NOT be changed in this spec.

