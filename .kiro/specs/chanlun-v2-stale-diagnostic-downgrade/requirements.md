# Requirements: Chanlun V2 Stale Diagnostic Downgrade

## Background

2026-05-24 线上 `aster_chanlun_v2` 重启后候选池已恢复为加密合约，但 `BNBUSDT`、`HYPEUSDT` 仍在每个 3 分钟周期重复识别旧的 1h `buy2`。现有 V2 freshness suppression 已能把首次过期后的重复 `OpenRejection` 静默，但这些旧信号仍先进入开仓候选链路和普通信号诊断，导致日志里的 `signal_count_24h` 继续按候选币数量增长，并容易维持 `runaway_rejection_loop` 的噪音。

本次只优化过期 stale signal 的处理路径，其余开仓框架、1h 主结构、新鲜度阈值、open gate、position sizing 和执行保护保持不变。

## Glossary

- Stale diagnostic downgrade: 在信号转成可执行开仓候选之前，识别硬过期的 V2 信号，并把它降级为诊断信息。
- Hard expired signal: `age_candles > max_lifetime_candles` 的 V2 信号，对应 `freshness_gate.signal_expired`。
- Active signal count: 本周期真正进入后续开仓验证链路的 V2 开仓信号数量。

## Requirements

### 1. Early Stale Downgrade

**User story:** As an operator, I want hard-expired Chanlun V2 signals to be downgraded before they become open candidates, so that stale signals do not look like fresh trading opportunities.

1. WHEN a V2 trade signal is open-like and already hard expired, THEN the engine SHALL NOT append it to the executable decision candidate list.
2. WHEN such a signal is downgraded, THEN the engine SHALL keep a clear diagnostic message containing symbol, signal type, reason code, age candles, trade timeframe, and max lifetime.
3. WHEN a V2 signal is fresh or soft-aged, THEN the engine SHALL keep the existing conversion, freshness guard, validation, and sizing flow.

### 2. Observable Signal Counts

**User story:** As an operator, I want `signal_count_24h` to reflect active signals rather than the number of watched symbols, so that stale loops do not inflate frequency diagnostics.

1. WHEN V2 emits strategy diagnostics, THEN it SHALL include an explicit `signal_count` equal to the number of non-downgraded open signal candidates.
2. WHEN `signal_count` is explicitly present and equals `0`, THEN rolling frequency state SHALL count `0` instead of falling back to `candidate_details`.
3. WHEN `signal_count` is absent for older strategies/logs, THEN existing fallback behavior MAY remain for compatibility.

### 3. Marker Visibility

**User story:** As a chart user, I still want to see that the strategy detected an expired structure, but I do not want it treated as a new executable signal.

1. WHEN a hard-expired signal is downgraded, THEN the existing V2 signal marker SHOULD be marked terminal/rejected with freshness metadata.
2. WHEN the same downgraded signal appears in later cycles, THEN marker preservation SHALL keep the terminal marker visible.
3. The downgrade SHALL NOT add another `OpenRejection` action for the stale signal.

### 4. Trading Safety

**User story:** As a trader, I want less noise without weakening risk controls.

1. The change SHALL NOT make stale signals executable.
2. The change SHALL NOT bypass validation for fresh or soft-aged signals.
3. Tests SHALL NOT place real orders or require live exchange side effects.
