# Programmatic Confidence And Execution State Fix Requirements

## Background

161 线上 `aster_deepseek` 使用 `decision_mode=programmatic`。交易日志显示程序化开仓候选长期以 `confidence=75` 进入 open gate，并被 `75 < 82/85/88` 拒绝。排查确认主信号层在 `buildSignal` 中固定写入 75，且候选在风控验证前就写入 `executed_signals`，导致拒绝信号后续显示为已处理。

## Requirements

### R1 Dynamic Programmatic Confidence

WHEN a programmatic main signal is generated, THE system SHALL calculate confidence from signal quality and market context instead of using a fixed value.

The calculation SHALL include at least:
- signal type quality;
- ADX strength;
- DI direction alignment with intended side;
- market state alignment or counter-trend penalty;
- short-term price momentum;
- MACD divergence and MA kiss diagnostics when available.

The signal diagnostics SHALL include the factors used to calculate confidence.

### R2 Open Gate Compatibility

WHEN a programmatic open/add decision is derived from a signal, THE decision SHALL use the calculated signal confidence.

High-quality aligned trend signals MAY pass the current open gate minimum confidence thresholds. Weak, ranging, counter-DI, or counter-trend signals SHALL remain below stricter thresholds or be blocked by existing gates.

### R3 Execution State Accuracy

WHEN a programmatic signal is only detected or rejected by validation, THE system SHALL NOT write it to `executed_signals`.

WHEN a programmatic open/add action succeeds, THE system SHALL write its signal id to `executed_signals` so future cycles dedupe the already executed signal.

WHEN execution fails or is skipped, THE signal SHALL remain eligible for future evaluation.

### R4 Observability

Decision logs and strategy signal APIs SHALL continue to expose marker status (`detected`, `rejected`, `failed`, `executed`) and the rejection reason. The executed signal state SHALL reflect actual executed open/add actions, not validation attempts.
