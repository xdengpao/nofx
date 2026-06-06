# 开仓频率优化 Design

## Overview

本设计把“提高开仓数量”拆成三类可控变化：

1. **实盘默认变化**：`balanced` 档使用 12 分钟新机会分析间隔、10 个 prompt 候选、自动缩仓执行。
2. **只观测变化**：高 ADX 放宽、RR 阈值放宽、rolling gate 宽松化只做 report-only，不改变默认实盘决策。
3. **显式激进变化**：`active` 档使用 9 分钟分析间隔，并启用每日新增开仓上限和 24 小时自动回退。

当前策略最近样本 Profit Factor 约 0.20，说明直接放宽所有门槛会放大亏损速度。因此设计原则是：先提高机会搜索频率和可执行性，再用 report-only 量化软风控放宽的收益/风险，最后由人工显式开启 `active`。

## Design Principles

- **兼容优先**：旧 `config.json` 没有 `trading_frequency` 配置块时，行为保持当前 15 分钟、8 候选、原有 gate。
- **配置集中**：开仓频率相关参数由一个 `trading_frequency` 配置块派生，避免多处手改。
- **实盘与模拟分离**：report-only 结果只进入日志/API/replay，不参与执行排序和下单。
- **硬风控不降级**：最大持仓、单笔风险、总风险、回撤硬停、保护单失败阻断继续优先。
- **小账户可执行性**：AI 仓位超过风险上限但缩仓后仍可执行时，自动缩仓，而不是直接拒绝。

## Architecture

```mermaid
flowchart TD
    Config[config.json trading_frequency] --> Normalize[config.ApplyDefaults/NormalizeTradingFrequency]
    Normalize --> PoolCfg[pool.DynamicCandidatePoolConfig prompt limit]
    Normalize --> Manager[manager.AddTrader]
    Manager --> AutoCfg[trader.AutoTraderConfig.FrequencyPolicy]
    AutoCfg --> Context[decision.Context.FrequencyPolicy]

    Context --> ShouldCallAI[shouldCallAIForNewOpportunities]
    Context --> OpenGate[EvaluateOpenGate]
    Context --> Validate[validateOpenDecision]
    Context --> FinalLimits[enforceFinalDecisionLimits]

    OpenGate --> LiveDecision[Live allow/penalize/block]
    OpenGate --> ReportOnly[Report-only simulations]
    Validate --> AutoSizing[Auto shrink when risk oversized]
    FinalLimits --> DailyCap[Daily open cap for active mode]

    LiveDecision --> Execute[AutoTrader execution]
    ReportOnly --> Logs[Decision logs / RiskState]
    Logs --> Replay[cmd/replay frequency diagnostics]
    Logs --> API[/api/status and /api/performance]
```

## Configuration Design

### `config.TradingFrequencyConfig`

Add a new optional top-level config block:

```go
type TradingFrequencyConfig struct {
    Mode                    string  `json:"mode,omitempty"` // safe, balanced, active
    AnalysisIntervalMinutes int     `json:"analysis_interval_minutes,omitempty"`
    PromptCandidateLimit    int     `json:"prompt_candidate_limit,omitempty"`
    DailyOpenLimit          int     `json:"daily_open_limit,omitempty"`
    RollbackWindowHours     int     `json:"rollback_window_hours,omitempty"`
    RollbackMinProfitFactor float64 `json:"rollback_min_profit_factor,omitempty"`
    RollbackMaxDrawdownPct  float64 `json:"rollback_max_drawdown_pct,omitempty"`
    ReportOnly              struct {
        HighADX     bool `json:"high_adx,omitempty"`
        RRThreshold bool `json:"rr_threshold,omitempty"`
        RollingGate bool `json:"rolling_gate,omitempty"`
    } `json:"report_only,omitempty"`
}
```

`Config` should use a pointer:

```go
TradingFrequency *TradingFrequencyConfig `json:"trading_frequency,omitempty"`
```

This lets the loader distinguish legacy config from a new but partially omitted config block.

### Derived Profiles

`config.NormalizeTradingFrequency()` returns a runtime profile:

| Mode | Analysis Interval | Prompt Limit | Daily Open Limit | Live Gate Changes | Report-only |
| --- | ---: | ---: | ---: | --- | --- |
| legacy nil block | 15 | existing dynamic pool default | none | current behavior | off |
| `safe` | 15 | 8 | none | current behavior | optional |
| `balanced` | 12 | 10 | none | current gate + auto shrink | high ADX/RR/rolling simulations on |
| `active` | 9 | 12 | 4 per 24h | current gate + auto shrink | high ADX/RR/rolling simulations on |

Explicit numeric config values override the profile only within validation bounds:

- `AnalysisIntervalMinutes`: min 9 for live mode; values below 9 fail validation or normalize to 9 with a warning.
- `PromptCandidateLimit`: min 8, max dynamic pool max size.
- `DailyOpenLimit`: only active by default; min 1 when set.
- `RollbackWindowHours`: default 24.
- `RollbackMinProfitFactor`: default 0.8, only active when at least 2 closed trades exist in the window.
- `RollbackMaxDrawdownPct`: default 2.0 for the rollback window.

`PromptCandidateLimit` is a startup/configuration-level candidate pool rule. Runtime rollback from `active` to an effective `safe` opening behavior must not dynamically shrink the candidate pool prompt limit; the system keeps the original normalized or explicitly configured candidate pool rule until configuration is changed and the service is restarted.

## Runtime Policy

### `decision.FrequencyPolicy`

Add a runtime DTO in `decision/types.go`:

```go
type FrequencyPolicy struct {
    Mode                    string  `json:"mode"`
    EffectiveMode           string  `json:"effective_mode,omitempty"`
    AnalysisIntervalMin     int     `json:"analysis_interval_min"`
    PromptCandidateLimit    int     `json:"prompt_candidate_limit"`
    DailyOpenLimit          int     `json:"daily_open_limit,omitempty"`
    RollbackWindowHours     int     `json:"rollback_window_hours,omitempty"`
    RollbackMinProfitFactor float64 `json:"rollback_min_profit_factor,omitempty"`
    RollbackMaxDrawdownPct  float64 `json:"rollback_max_drawdown_pct,omitempty"`
    HighADXReportOnly       bool    `json:"high_adx_report_only"`
    RRReportOnly            bool    `json:"rr_report_only"`
    RollingGateReportOnly   bool    `json:"rolling_gate_report_only"`
}
```

### `decision.FrequencyState`

Also add an observable runtime state:

```go
type FrequencyState struct {
    OpenCount24h       int     `json:"open_count_24h"`
    ClosedTrades24h    int     `json:"closed_trades_24h"`
    ProfitFactor24h    float64 `json:"profit_factor_24h"`
    Drawdown24hPct     float64 `json:"drawdown_24h_pct"`
    AutoRollbackActive bool    `json:"auto_rollback_active"`
    AutoRollbackReason string  `json:"auto_rollback_reason,omitempty"`
}
```

`trader.buildTradingContext()` computes this state from recent decision logs and attaches both policy and state to `decision.Context`.

## Implementation Plan

### Config and Startup

Files:

- `config/config.go`
- `config/config_test.go`
- `main.go`
- `manager/trader_manager.go`
- `trader/auto_trader.go`

Changes:

1. Add `TradingFrequencyConfig` and profile normalization.
2. Preserve legacy behavior when `TradingFrequency == nil`.
3. When the block exists and mode is omitted, derive `balanced`.
4. Pass the derived policy into `AutoTraderConfig`.
5. Use the profile prompt limit when configuring `pool.DynamicCandidatePoolConfig`.
6. Pass the derived analysis interval into `decision.Initialize()` for startup log consistency, or remove the interval from that startup log and make `AutoTrader.GetStatus()` the source of truth.
7. Expose `frequency_policy` in `AutoTrader.GetStatus()`.

### Candidate Pool

Files:

- `main.go`
- `pool/dynamic_candidate_pool.go`
- `pool/dynamic_candidate_pool_test.go`

Changes:

1. `trading_frequency.prompt_candidate_limit` overrides the dynamic pool prompt limit only when the frequency block exists.
2. Candidate snapshots remain unchanged: symbol, sources, score, tier, data quality, filter reason, included flag.
3. OI Top missing remains a warning and appears in diagnostics, not a loop failure.
4. `active` runtime rollback does not call `pool.SetDynamicCandidatePoolConfig()` and does not add per-cycle prompt limit overrides; it keeps the original candidate pool rule selected at startup.

### AI Call Interval

Files:

- `trader/auto_trader.go`
- `decision/decision.go`
- `trader/trader_test.go`

Changes:

1. `AnalysisIntervalMin` already flows through `AutoTraderConfig` into `decision.Context`; update construction to use the derived policy.
2. Keep `shouldCallAIForNewOpportunities()` behavior unchanged except the interval value.
3. Wait reason already includes the configured interval; tests should assert 12 for balanced and 9 for active.

### Auto Shrink Oversized Risk

Files:

- `decision/decision.go`
- `decision/position_sizing.go`
- `decision/types.go`
- `decision/decision_test.go`
- `logger/decision_logger.go`

Current behavior rejects when:

```go
d.PositionSizeUSD > sizing.MaxPositionSizeUSD*1.01
```

New behavior:

1. Add sizing audit fields to `decision.Decision`:

```go
RequestedPositionSizeUSD float64 `json:"requested_position_size_usd,omitempty"`
AdjustedPositionSizeUSD  float64 `json:"adjusted_position_size_usd,omitempty"`
SizingAdjusted           bool    `json:"sizing_adjusted,omitempty"`
SizingReason             string  `json:"sizing_reason,omitempty"`
StopDistancePct          float64 `json:"stop_distance_pct,omitempty"`
EffectiveRiskPct         float64 `json:"effective_risk_pct,omitempty"`
```

2. Add equivalent logger-local fields to `logger.DecisionAction` so historical decision records preserve the requested size, adjusted size, effective risk, stop distance, and sizing reason without importing `decision`.

3. Calculate `sizing` as today.
4. If requested size exceeds max but `sizing.PositionSizeUSD >= MinOrderValueUSDT`, set:

```go
d.RequestedPositionSizeUSD = originalSize
d.AdjustedPositionSizeUSD = sizing.PositionSizeUSD
d.PositionSizeUSD = sizing.PositionSizeUSD
d.RiskUSD = sizing.RiskUSD
d.SizingAdjusted = true
d.SizingReason = "单笔风险超限，已缩小到最大可执行仓位"
```

5. Continue validation.
6. Reject only when the adjusted size is below minimum notional, margin is insufficient, or protection prerequisites cannot be satisfied.
7. Log requested size, adjusted size, effective risk, and stop distance.

### Report-only Simulations

Files:

- `decision/open_gate.go`
- `decision/decision.go`
- `decision/types.go`
- `logger/decision_logger.go`
- `logger/replay.go`
- `cmd/replay/main.go`

Add:

```go
type OpenFrequencySimulation struct {
    Scenario        string         `json:"scenario"`
    Source          string         `json:"source,omitempty"` // structured, text_inferred
    WouldAllow      bool           `json:"would_allow"`
    Reason          string         `json:"reason,omitempty"`
    OriginalState   string         `json:"original_state,omitempty"`
    SimulatedState  string         `json:"simulated_state,omitempty"`
    MinConfidence   int            `json:"min_confidence,omitempty"`
    EffectiveRisk   float64        `json:"effective_risk,omitempty"`
    AdjustedSizeUSD float64        `json:"adjusted_size_usd,omitempty"`
    Diagnostics     map[string]any `json:"diagnostics,omitempty"`
}
```

Attach simulations to `decision.OpenRejection` and `logger.DecisionAction`.

Simulation rules:

- `high_adx_active_candidate`: for ADX 50-60, confidence >= 85, no hard BTC block, no extreme ADX block. It reports whether active-style min confidence 85 plus reduced sizing would have passed.
- `rr_threshold_candidate`: reports candidates with net RR >= 2.0, but only as report-only unless a future spec explicitly turns this live.
- `rolling_risk_only_candidate`: reports whether a rolling confidence rejection would have passed if rolling only reduced risk.

No simulation may flip `OpenGateResult.Allowed` in the default implementation.

Live decisions should emit `source=structured` simulations because they have the full gate context, market data, sizing result, and rejection metadata. Replay over older logs is best-effort only: when legacy records do not contain structured simulation fields, `logger.BuildReplayReport()` may infer candidates from action/rejection text and must mark those rows as `source=text_inferred` instead of presenting them as equivalent to live simulation.

Rolling-gate simulations should include sample count, window, PnL, Profit Factor, and cooldown diagnostics when available. If the rolling sample count is below the configured lower bound, the report-only scenario should classify the candidate as risk-only adjustment rather than confidence-threshold escalation.

### Daily Open Cap and Rollback

Files:

- `logger/decision_logger.go`
- `trader/auto_trader.go`
- `decision/decision.go`
- `decision/types.go`

Add helpers:

- `logger.CountSuccessfulOpens(records, since, traderID)`
- `logger.BuildRecentClosedTradeStats(outcomes, since)`
- `trader.buildFrequencyState(performance)`

Runtime behavior:

1. In active mode, if `OpenCount24h >= DailyOpenLimit`, `shouldCallAIForNewOpportunities()` skips new opportunity search with a clear reason.
2. `enforceFinalDecisionLimits()` also rejects extra open decisions if the cap is reached after merging decisions.
3. If 24h closed trades count >= 2 and Profit Factor < rollback threshold, effective mode becomes `safe` for this cycle.
4. If 24h drawdown exceeds rollback drawdown threshold, effective mode becomes `safe`.
5. Auto rollback is non-persistent initially: it changes effective runtime opening behavior and logs the reason, but does not rewrite `config.json`.
6. Auto rollback does not mutate the dynamic candidate pool prompt limit; the prompt limit remains the startup/configured value.
7. Daily cap and final-limit rejections must be appended as structured `decision.OpenRejection` records and/or `logger.DecisionAction{Action:"open_rejected"}` records, not only as CoTTrace text.

### API and Logging

Files:

- `trader/auto_trader.go`
- `logger/decision_logger.go`
- `api/server.go`
- `web/src/types/index.ts` if frontend uses the fields

Expose:

- `frequency_policy` in `/api/status`.
- `frequency_state` in `/api/status`.
- report-only simulations in decision records and replay reports.
- open rejection category counts in replay/performance diagnostics if available.

`RiskStateSnapshot` should add logger-local snapshots to avoid a logger -> decision import cycle:

```go
FrequencyPolicy *FrequencyPolicySnapshot `json:"frequency_policy,omitempty"`
FrequencyState  *FrequencyStateSnapshot  `json:"frequency_state,omitempty"`
```

## Risk Controls

- The default `balanced` rollout does not reduce high ADX confidence requirements in live execution.
- RR threshold remains 2.5 in live execution.
- Auto shrink cannot create trades below minimum notional or beyond risk budget.
- Active mode caps new opens at 4 per 24h by default.
- Active mode auto-rolls back to safe opening behavior when recent PF or drawdown deteriorates, while keeping the configured candidate pool prompt limit.
- Protection order failure gate remains a hard block.
- AI backoff remains a hard skip for new opportunities.

## Compatibility and Migration

1. Existing config without `trading_frequency` remains unchanged.
2. Recommended deployment adds:

```json
"trading_frequency": {
  "mode": "balanced"
}
```

This derives:

- `analysis_interval_minutes = 12`
- `prompt_candidate_limit = 10`
- high ADX, RR, and rolling relaxations as report-only

3. Manual rollout rollback is configuration-only:

```json
"trading_frequency": {
  "mode": "safe"
}
```

or remove the block to return to legacy behavior.

4. Update `config.json.example` and deployment notes with commented examples for `safe`, `balanced`, and `active`, including the fact that active runtime rollback does not change the candidate pool prompt limit until restart/config change.

## Validation

Required targeted checks:

```bash
go test ./config ./decision ./trader ./manager
go run ./cmd/replay -log-dir decision_logs -trader aster_deepseek -from 2026-05-10 -to 2026-05-12 -report-only=true
```

If API/frontend fields are changed:

```bash
go test ./api
cd web && npm run build
```

## Open Questions

1. Whether `balanced` should be represented by adding `trading_frequency.mode=balanced` to production config, or kept as documentation-only until manual config deployment.
2. Whether report-only simulation should be stored on every rejected action or only when it changes the hypothetical outcome.
3. Whether active rollback should remain non-persistent or write a runtime state file under `data/`.
