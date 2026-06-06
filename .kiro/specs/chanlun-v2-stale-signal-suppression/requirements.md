# Requirements: Chanlun V2 Stale Signal Suppression

## Background

2026-05-23 线上 `aster_chanlun_v2` 连续多个 3 分钟周期重复产出同一批 1h 缠论 V2 开仓信号，并被信号新鲜度门控拒绝：

- `CLUSDT` `buy2`：信号年龄 8 根 1h，超过硬上限 2 根。
- `BNBUSDT` `buy2`：信号年龄 19 根 1h，超过硬上限 2 根。

这些信号的 `signal_id` 和 `evaluation_close_time` 没有变化，说明策略在同一根已闭合 1h K 线内重复分析到了同一个已经终态过期的信号。当前 freshness gate 会每个扫描周期重新返回 `open_rejected`，导致前端周期记录和执行日志持续刷屏，且容易被误读为新的开仓机会。

## Glossary

- Terminal freshness rejection: `freshness_gate.signal_expired`、`freshness_gate.target_crossed`、`freshness_gate.rr_invalid` 这类无需在同一信号生命周期内重复尝试的终态拒绝。
- Signal lifecycle: 同一个 `signal_id` 表示同一个缠论 V2 交易信号生命周期。
- Evaluation candle: `evaluation_close_time` / `decision_close_time` 对应本次策略评估使用的已闭合交易级别 K 线。
- Suppressed repeat: 已被终态 freshness gate 拒绝的同一 `signal_id` 在后续周期再次出现时，不再作为可执行开仓候选写入 `open_rejected`。

## Requirements

### 1. Terminal Freshness Suppression

**User story:** As an operator, I want an expired Chanlun V2 signal to be rejected once and then stay quiet while the same signal persists, so that repeated cycles do not look like new trading opportunities.

1. WHEN a Chanlun V2 open-like decision is rejected by `freshness_gate.signal_expired`, `freshness_gate.target_crossed`, or `freshness_gate.rr_invalid`, THEN the engine SHALL mark that `trader_id + symbol + signal_id` lifecycle as terminally freshness-rejected.
2. WHEN the same terminally rejected `signal_id` appears again in later cycles, THEN the engine SHALL suppress it before validation and SHALL NOT return another `OpenRejection` for that same lifecycle.
3. WHEN a different `signal_id` appears for the same symbol/action, THEN the engine SHALL evaluate it normally and SHALL NOT inherit the old suppression.
4. IF the strategy is restarted, THEN in-memory suppression MAY reset; the first rejection after restart is acceptable.

### 2. Same Evaluation Candle Dedupe

**User story:** As an operator, I want a repeated stale rejection on the same evaluation candle to be logged once at most, so that short scan intervals do not duplicate identical terminal outcomes.

1. WHEN the freshness gate creates a terminal rejection, THEN the engine SHALL dedupe repeated records by `trader_id + symbol + signal_id + reason_code + evaluation_close_time`.
2. WHEN the same dedupe key appears again, THEN the engine SHALL suppress the duplicate `OpenRejection`.
3. WHEN the same `signal_id` later reaches a different evaluation close time but is already lifecycle-terminal, THEN lifecycle suppression SHALL still keep it quiet.

### 3. Marker and Strategy Check Visibility

**User story:** As a user checking the strategy chart, I want the rejected marker to remain visible at the decision/evaluation candle even after later cycles suppress repeats.

1. WHEN the first terminal freshness rejection occurs, THEN the V2 signal marker SHALL move to rejected status with freshness metadata, including signal close time, evaluation close time, reason code, age candles, and stale reason.
2. WHEN later cycles suppress the same stale signal, THEN marker preservation SHALL keep the rejected lifecycle marker visible and SHALL NOT replace it with a ready marker.
3. WHEN strategy signal APIs return marker summaries, THEN suppressed repeats MAY be counted diagnostically but SHALL NOT remove the original rejected marker.

### 4. Decision Logs and Wording

**User story:** As an operator, I want log wording to distinguish freshness rejection from open gate/risk rejection, so that I can understand why no order was attempted.

1. WHEN an `OpenRejection` has freshness gate metadata or a `freshness_gate.*` reason code, THEN `trader.AutoTrader` SHALL write the execution log using “信号新鲜度拒绝” wording instead of “开仓门控拒绝”.
2. WHEN an `OpenRejection` is produced by validation/open gate, THEN existing “开仓门控拒绝” wording MAY remain.
3. WHEN repeats are suppressed, THEN strategy diagnostics SHOULD include a concise suppressed message/count, but execution logs SHALL NOT add another failed action line for the same stale lifecycle.

### 5. Trading Safety

**User story:** As a trader, I want this optimization to reduce noise without weakening protection.

1. The change SHALL NOT convert rejected stale signals into executable decisions.
2. The change SHALL NOT bypass `ValidateStrategyDecisions`, open gate, position sizing, minimum order checks, stop-loss/take-profit checks, or final decision limits for new valid signals.
3. The change SHALL NOT place real orders in tests.
