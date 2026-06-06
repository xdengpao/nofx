# 程序化策略减仓约束、原因说明与信号展示 Design

## Overview

本设计在已部署的程序化双层节奏上继续增量改造，解决三类问题：

1. **交易节奏保护**：同一持仓被 `short_trade`、`floating_drawdown` 等多个规则连续 `partial_close` 时，引入跨规则冷却、次数上限和累计减仓比例上限。
2. **解释与审计**：每个真实交易动作和被拒绝动作都输出可读原因与结构化原因字段，便于前端、日志和 replay 对齐。
3. **策略检查可视化**：前端策略检查面板跟随 `programmatic_strategy.timeframes.trade` 展示主交易级别 K 线，并在 K 线行中标记 `B1/B2/B3`、`S1/S2/S3`。

核心原则：

- 不绕过现有 `decision.Decision`、公共风控、执行前检查、最小名义额和交易所执行路径。
- partial close 冷却和预算只限制程序化持仓管理层的节奏型 `partial_close`，不得阻断公共强制风控、交易计划硬退出或结构破坏全平。
- 状态消耗以“执行成功”为准；生成候选但交易所失败时，不应消耗 partial close 预算。
- API 和前端字段以可选扩展为主，保持旧日志、旧响应和 AI 模式兼容。

## Architecture

```mermaid
flowchart TD
    A[config.json programmatic_strategy] --> B[config.NormalizeProgrammaticStrategies]
    B --> C[manager decisionProgrammaticStrategyPolicy]
    C --> D[trader.AutoTrader]
    D --> E[strategy/chanlun.Engine]
    E --> F[Position Management Guard]
    F --> G[decision.Decision]
    G --> H[decision validation and merge]
    H --> I[trader execution]
    I --> J[Engine.OnExecutionResult]
    J --> K[StateStore programmatic_strategy_state.json]
    G --> L[logger.DecisionRecord]
    K --> M[/api/strategy/signals]
    L --> M
    M --> N[web StrategyInspector]
    O[/api/market/klines] --> N
```

## Data Model

### Config

Update [config/programmatic.go](/Users/poper32/src/nofx/config/programmatic.go) under `ProgrammaticPositionManagementConfig`.

Add flat fields to keep production JSON simple:

```go
type ProgrammaticPositionManagementConfig struct {
    Enabled          *bool                              `json:"enabled,omitempty"`
    Timeframes       ProgrammaticManagementTFConfig     `json:"timeframes,omitempty"`
    Breakeven        ProgrammaticBreakevenConfig        `json:"breakeven,omitempty"`
    FloatingDrawdown ProgrammaticFloatingDrawdownConfig `json:"floating_drawdown,omitempty"`
    StructureBreak   ProgrammaticStructureBreakConfig   `json:"structure_break,omitempty"`
    ShortTrade       ProgrammaticShortTradeConfig       `json:"short_trade,omitempty"`

    PartialCloseCooldownMinutes     *int    `json:"partial_close_cooldown_minutes,omitempty"`
    MaxPartialCloseCountPerPosition int     `json:"max_partial_close_count_per_position,omitempty"`
    MaxTotalPartialClosePct         float64 `json:"max_total_partial_close_pct,omitempty"`
}
```

Extend `ProgrammaticStructureBreakConfig` for the only rule that may need to bypass partial close guard:

```go
type ProgrammaticStructureBreakConfig struct {
    Enabled     *bool  `json:"enabled,omitempty"`
    ConfirmBars int    `json:"confirm_bars,omitempty"`
    Action      string `json:"action,omitempty"`

    // respect_guard, bypass_cooldown_clip_budget, close_on_budget_exhausted
    PartialCloseGuardAction string `json:"partial_close_guard_action,omitempty"`
}
```

Normalize into profiles:

```go
type ProgrammaticPositionManagementProfile struct {
    Enabled          bool
    Timeframes       ProgrammaticManagementTFProfile
    Breakeven        ProgrammaticBreakevenProfile
    FloatingDrawdown ProgrammaticFloatingDrawdownProfile
    StructureBreak   ProgrammaticStructureBreakProfile
    ShortTrade       ProgrammaticShortTradeProfile
    PartialCloseGuard ProgrammaticPartialCloseGuardProfile
}

type ProgrammaticPartialCloseGuardProfile struct {
    CooldownMinutes int
    MaxCountPerPosition int
    MaxTotalRatio float64
    CooldownEnabled bool
}

type ProgrammaticStructureBreakProfile struct {
    Enabled     bool
    ConfirmBars int
    Action      string
    PartialCloseGuardAction string
}
```

Defaults and validation:

- `partial_close_cooldown_minutes`: nil means default `15`; explicit `0` disables cross-rule cooldown; allowed `0-1440`.
- `max_partial_close_count_per_position`: default `2`, allowed `1-10`.
- `max_total_partial_close_pct`: default `50`, allowed `1-100`, normalized to ratio in profile/policy.
- `structure_break.partial_close_guard_action`: default `bypass_cooldown_clip_budget`.

Normalize rule:

- `PartialCloseCooldownMinutes == nil` => `CooldownMinutes=15`, `CooldownEnabled=true`.
- `*PartialCloseCooldownMinutes == 0` => `CooldownMinutes=0`, `CooldownEnabled=false`.
- `*PartialCloseCooldownMinutes < 0 || > 1440` => config error.

Update [config.json.example](/Users/poper32/src/nofx/config.json.example) to show:

```jsonc
"position_management": {
  "enabled": true,
  "partial_close_cooldown_minutes": 15,
  "max_partial_close_count_per_position": 2,
  "max_total_partial_close_pct": 50,
  "structure_break": {
    "enabled": true,
    "confirm_bars": 2,
    "action": "partial_close",
    "partial_close_guard_action": "bypass_cooldown_clip_budget"
  }
}
```

### Runtime Policy

Update [decision/types.go](/Users/poper32/src/nofx/decision/types.go):

```go
type ProgrammaticPositionManagementPolicy struct {
    Enabled          bool
    Timeframes       ProgrammaticManagementTFPolicy
    Breakeven        ProgrammaticBreakevenPolicy
    FloatingDrawdown ProgrammaticFloatingDrawdownPolicy
    StructureBreak   ProgrammaticStructureBreakPolicy
    ShortTrade       ProgrammaticShortTradePolicy
    PartialCloseGuard ProgrammaticPartialCloseGuardPolicy
}

type ProgrammaticPartialCloseGuardPolicy struct {
    CooldownMinutes int
    MaxCountPerPosition int
    MaxTotalRatio float64
    CooldownEnabled bool
}
```

Update [manager/trader_manager.go](/Users/poper32/src/nofx/manager/trader_manager.go) so `decisionProgrammaticStrategyPolicy()` copies the new fields into `AutoTraderConfig.ProgrammaticStrategyPolicy`.

### Programmatic State

Extend [strategy/chanlun/state.go](/Users/poper32/src/nofx/strategy/chanlun/state.go).

Current `ProgrammaticPositionState` is side-scoped. Add partial close guard state inside it:

```go
type ProgrammaticPositionState struct {
    Side                   string    `json:"side"`
    PeakPrice              float64   `json:"peak_price,omitempty"`
    PeakR                  float64   `json:"peak_r,omitempty"`
    LastBreakevenSignalID  string    `json:"last_breakeven_signal_id,omitempty"`
    LastDrawdownSignalID   string    `json:"last_drawdown_signal_id,omitempty"`
    LastStructureSignalID  string    `json:"last_structure_signal_id,omitempty"`
    LastShortTradeSignalID string    `json:"last_short_trade_signal_id,omitempty"`
    LastManagedAt          time.Time `json:"last_managed_at,omitempty"`

    PartialCloseGuard      PartialCloseGuardState `json:"partial_close_guard,omitempty"`
}

type PartialCloseGuardState struct {
    LastPartialCloseAt       time.Time `json:"last_partial_close_at,omitempty"`
    LastPartialCloseRule     string    `json:"last_partial_close_rule,omitempty"`
    LastPartialCloseSignalID string    `json:"last_partial_close_signal_id,omitempty"`
    LastPartialClosePct      float64   `json:"last_partial_close_pct,omitempty"`
    PartialCloseCount        int       `json:"partial_close_count,omitempty"`
    TotalPartialClosePct     float64   `json:"total_partial_close_pct,omitempty"`
    InitialTrackedQuantity   float64   `json:"initial_tracked_quantity,omitempty"`
    InitialTrackedValueUSD   float64   `json:"initial_tracked_value_usd,omitempty"`
    LastKnownQuantity        float64   `json:"last_known_quantity,omitempty"`
    TotalPartialCloseQuantity float64  `json:"total_partial_close_quantity,omitempty"`
    QuantityEstimated        bool      `json:"quantity_estimated,omitempty"`

    LastDrawdownPeakPrice    float64   `json:"last_drawdown_peak_price,omitempty"`
    LastDrawdownPeakPnLPct   float64   `json:"last_drawdown_peak_pnl_pct,omitempty"`
    LastDrawdownPeakR        float64   `json:"last_drawdown_peak_r,omitempty"`
    RequireNewPeakForDrawdown bool     `json:"require_new_peak_for_drawdown,omitempty"`
}
```

State helper additions:

- `PartialCloseGuardState(traderID, symbol, side string) PartialCloseGuardState`
- `EvaluatePartialCloseGuard(...) PartialCloseGuardDecision`
- `RecordProgrammaticPartialClose(...)`
- `RecordProgrammaticFullClose(...)`
- `ResetPositionGuardIfMissing(traderID string, activePositions []decision.PositionInfo)`

Budget accounting:

- `InitialTrackedQuantity` is initialized from the first active position quantity observed for the side.
- `TotalPartialClosePct` is derived from `TotalPartialCloseQuantity / InitialTrackedQuantity * 100` whenever quantity data is available.
- If exchange execution output cannot provide actual closed quantity, fallback to `LastKnownQuantity * requested_close_percentage`, set `QuantityEstimated=true`, and include `estimated=true` in explanation.
- Budget clipping uses original tracked quantity, not simple addition of requested percentages. This avoids incorrectly treating two 30% reductions of remaining quantity as exactly 60% of the original position.

Backward compatibility:

- Missing `partial_close_guard` means empty state.
- Old state files must unmarshal without migration scripts.
- `Save()` writes the new fields only after first update.

## Strategy Engine Design

### Guard Evaluation Placement

Current flow in [strategy/chanlun/position_management.go](/Users/poper32/src/nofx/strategy/chanlun/position_management.go):

1. `analyzePosition()` checks `structure_break` -> `floating_drawdown` -> `short_trade` -> `breakeven`.
2. `evaluatePositionManagement()` marks the signal id through `StateStore.MarkPositionSignal()`.

Update flow:

```go
func (e *Engine) evaluatePositionManagement(ctx *decision.Context, now time.Time) ([]decision.Decision, []string) {
    for _, pos := range ctx.Positions {
        d, diag := e.analyzePosition(...)
        if d.SignalID != "" && e.StateStore.HasPositionSignal(ctx.TraderID, d.Symbol, pos.Side, rule, d.SignalID) {
            diagnostics = append(diagnostics, fmt.Sprintf("%s %s 已处理过signal_id=%s", d.Symbol, rule, d.SignalID))
            continue
        }
        if d.Action == "partial_close" {
            guarded, guardDiag := e.applyPartialCloseGuard(ctx, pos, d, now)
            diag = append(diag, guardDiag...)
            if guarded.Action == "" {
                diagnostics = append(diagnostics, diag...)
                continue
            }
            d = guarded
        }
        append decision
    }
}
```

Important: evaluation SHALL NOT call `MarkPositionSignal()` for programmatic partial close candidates. Signal dedupe and budget consumption are committed only by the execution callback after successful execution. Validation rejects and exchange failures update marker diagnostics/status only.

Guard behavior by rule:

- `short_trade`: respects cooldown, count budget and total percentage budget.
- `floating_drawdown`: respects cooldown, count budget, total percentage budget and “require new peak” condition.
- `structure_break`:
  - `action=close`: bypasses partial close guard.
  - `action=partial_close`:
    - `respect_guard`: same behavior as short trade.
    - `bypass_cooldown_clip_budget`: ignores cooldown; if remaining budget is positive, clips to remaining percentage budget; if remaining budget is zero, skips and records diagnostics.
    - `close_on_budget_exhausted`: if budget is exhausted, outputs `close_long/close_short` instead of skipping.
- `breakeven`: not a partial close, never blocked by guard.

### Guard Result Shape

Add internal type in `strategy/chanlun/position_management.go`:

```go
type partialCloseGuardResult struct {
    Allowed bool
    ClosePercentage float64
    BlockedReason string
    CooldownRemaining time.Duration
    BudgetRemainingPct float64
    CountRemaining int
    Clipped bool
    UpgradeToClose bool
}
```

When allowed but clipped:

- mutate `Decision.ClosePercentage`.
- add `strategy_metadata.budget_status.clipped=true`.
- include old/new close percentage.

When blocked:

- return no decision.
- append diagnostics such as:
  - `ETHUSDT short_trade partial_close冷却中: 剩余12m, 上次规则=short_trade, signal_id=...`
  - `ETHUSDT floating_drawdown partial_close预算耗尽: count=2/2 total=50/50`

### Floating Drawdown New Peak Rule

Current `syncPositionPeakState()` resets `LastDrawdownSignalID` whenever a new favorable peak appears. Extend it to also clear `RequireNewPeakForDrawdown`.

Algorithm:

1. On every position management cycle, update peak price and peak R if current price improves.
2. If new peak is detected:
   - clear `LastDrawdownSignalID`.
   - set `PartialCloseGuard.RequireNewPeakForDrawdown=false`.
3. When `floating_drawdown` partial close executes successfully:
   - set `RequireNewPeakForDrawdown=true`.
   - store peak snapshot.
4. If `RequireNewPeakForDrawdown=true` and no new peak occurred, block another `floating_drawdown` partial close.

This preserves the “same drawdown section only handled once” behavior while still allowing a new peak followed by a new drawdown to trigger again.

### Execution Success Callback

Guard state must be consumed after execution success, not merely after decision generation.

Add method to [strategy/chanlun/engine.go](/Users/poper32/src/nofx/strategy/chanlun/engine.go):

```go
type ProgrammaticExecutionResult struct {
    TraderID string
    Decision decision.Decision
    Success bool
    FinalAction string
    RequestedClosePercentage float64
    ExecutedClosePercentage float64
    ExecutedQuantity float64
    Price float64
    Error string
    ExecutedAt time.Time
}

func (e *Engine) OnExecutionResult(result ProgrammaticExecutionResult)
```

Call it from [trader/auto_trader.go](/Users/poper32/src/nofx/trader/auto_trader.go) after each programmatic action execution attempt:

```go
if at.programmaticEngine != nil && d.StrategyMode == "programmatic" {
    at.programmaticEngine.OnExecutionResult(chanlun.ProgrammaticExecutionResult{...})
}
```

Rules:

- `Success=false`: do not consume partial close budget; add optional failed-attempt diagnostics if useful.
- successful `partial_close`: call `RecordProgrammaticPartialClose`.
- successful `close_long/close_short`: call `RecordProgrammaticFullClose` or clear side state.
- `partial_close` auto-corrected to full close by small-position logic must pass `FinalAction=close_long/close_short`.

Implementation note: `executePartialCloseWithRecord()` currently may mutate `d.Action` when a partial close is auto-corrected to full close. Ensure `actionRecord.Action` and `ProgrammaticExecutionResult.FinalAction` reflect the final executed action.

Extend [logger/decision_logger.go](/Users/poper32/src/nofx/logger/decision_logger.go) `DecisionAction`:

```go
RequestedClosePercentage float64 `json:"requested_close_percentage,omitempty"`
ExecutedClosePercentage  float64 `json:"executed_close_percentage,omitempty"`
FinalAction              string  `json:"final_action,omitempty"`
CloseQuantity            float64 `json:"close_quantity,omitempty"`
```

`executePartialCloseWithRecord()` must fill these fields in all paths:

- normal partial close: `FinalAction=partial_close`, `ExecutedClosePercentage` equals actual close quantity divided by pre-close quantity.
- skip due to min notional: `FinalAction=hold` or `partial_close_skipped`, `ExecutedClosePercentage=0`.
- auto full close: `FinalAction=close_long/close_short`, `ExecutedClosePercentage=100`.

## Decision Explanation Design

### Structured Explanation Type

Add optional field to `decision.Decision` and `logger.DecisionAction`:

```go
type DecisionExplanation struct {
    Summary string `json:"summary,omitempty"`
    Layer string `json:"layer,omitempty"`
    Rule string `json:"rule,omitempty"`
    ReasonCode string `json:"reason_code,omitempty"`
    Timeframe string `json:"timeframe,omitempty"`
    SignalType string `json:"signal_type,omitempty"`
    SignalID string `json:"signal_id,omitempty"`
    TriggerPrice float64 `json:"trigger_price,omitempty"`
    ReferencePrice float64 `json:"reference_price,omitempty"`
    Threshold float64 `json:"threshold,omitempty"`
    CooldownStatus map[string]any `json:"cooldown_status,omitempty"`
    BudgetStatus map[string]any `json:"budget_status,omitempty"`
    RiskChecks []map[string]any `json:"risk_checks,omitempty"`
    Details map[string]any `json:"details,omitempty"`
}
```

Add:

```go
Explanation *DecisionExplanation `json:"explanation,omitempty"`
```

to:

- [decision/types.go](/Users/poper32/src/nofx/decision/types.go) `Decision`
- [logger/decision_logger.go](/Users/poper32/src/nofx/logger/decision_logger.go) `DecisionAction`

Copy in [trader/auto_trader.go](/Users/poper32/src/nofx/trader/auto_trader.go) `applyDecisionSizingToActionRecord()`.

### Explanation Builders

Add helper functions in `decision` or `strategy/chanlun`:

- `NewProgrammaticExplanation(...)`
- `WithGuardStatus(...)`
- `WithBudgetStatus(...)`
- `WithRiskCheck(...)`

For programmatic decisions, keep `Reasoning` as concise human text and put machine-readable detail in `Explanation`.

Examples:

```json
{
  "summary": "程序化短差减仓: 空仓遇到 buy3 3m 反向信号",
  "layer": "position_management",
  "rule": "short_trade",
  "reason_code": "reverse_micro_signal",
  "timeframe": "3m",
  "signal_type": "buy3",
  "signal_id": "708680...",
  "budget_status": {
    "requested_pct": 30,
    "final_pct": 20,
    "remaining_pct_before": 20,
    "clipped": true
  }
}
```

```json
{
  "summary": "程序化浮盈回撤保护: 峰值2.14%, 当前1.38%, 回撤35.4%",
  "layer": "position_management",
  "rule": "floating_drawdown",
  "reason_code": "peak_drawdown_reached",
  "threshold": 0.35,
  "details": {
    "peak_pnl_pct": 2.14,
    "current_pnl_pct": 1.38,
    "drawdown_ratio": 0.354
  }
}
```

AI-mode compatibility:

- AI decisions keep existing `reasoning`.
- Deterministic rejects from open gate and validation should fill `Explanation` where practical, but absence of explanation must not break old logs.

### Frontend Text

In [web/src/App.tsx](/Users/poper32/src/nofx/web/src/App.tsx), rename the programmatic cycle panel label from “AI思维链分析” to conditional text:

- `decision_mode === "programmatic"`: `策略分析`
- otherwise: `AI思维链分析`

This is a text-only compatibility fix and avoids implying the programmatic strategy is AI-generated.

## Signal Marker Design

### Backend Types

Extend [strategy/chanlun/types.go](/Users/poper32/src/nofx/strategy/chanlun/types.go):

```go
type ChanlunSignal struct {
    ...
    TriggerCloseTime int64 `json:"trigger_close_time,omitempty"`
    SegmentStartTime int64 `json:"segment_start_time,omitempty"`
    SegmentEndTime   int64 `json:"segment_end_time,omitempty"`
    Status           string `json:"status,omitempty"` // detected, executed, rejected, deduped, diagnostic
    SourceLayer      string `json:"source_layer,omitempty"` // main_signal, position_management
}

type SignalMarker struct {
    Symbol string `json:"symbol"`
    Timeframe string `json:"timeframe"`
    CloseTime int64 `json:"close_time"`
    SignalType string `json:"signal_type"`
    Direction string `json:"direction"`
    Level string `json:"level"`
    SourceLayer string `json:"source_layer"`
    Status string `json:"status"`
    SignalID string `json:"signal_id"`
    Action string `json:"action,omitempty"`
    Price float64 `json:"price,omitempty"`
    Reason string `json:"reason,omitempty"`
}

type SignalReport struct {
    TraderID          string          `json:"trader_id"`
    Symbol            string          `json:"symbol"`
    DecisionMode      string          `json:"decision_mode"`
    StrategyName      string          `json:"strategy_name"`
    StrategyVersion   string          `json:"strategy_version"`
    ConfigHash        string          `json:"config_hash"`
    TradeTimeframe    string          `json:"trade_timeframe,omitempty"`
    ComponentTimeframe string         `json:"component_timeframe,omitempty"`
    MicroTimeframe    string          `json:"micro_timeframe,omitempty"`
    Signals           []ChanlunSignal `json:"signals"`
    SignalMarkers     []SignalMarker  `json:"signal_markers,omitempty"`
    LatestDiagnostics map[string]any  `json:"latest_diagnostics,omitempty"`
}
```

In [strategy/chanlun/signals.go](/Users/poper32/src/nofx/strategy/chanlun/signals.go), `buildSignal()` should set:

- `TriggerCloseTime = segment.EndTime`
- `SegmentStartTime = segment.StartTime`
- `SegmentEndTime = segment.EndTime`
- `SourceLayer = "main_signal"` by default
- `Status = "detected"` by default

Position management markers:

- `short_trade`: marker uses the micro signal’s `TriggerCloseTime`, `timeframe=3m`, `source_layer=position_management`, `status=executed` after execution success.
- `floating_drawdown`: marker may use the most recent micro or structure K close time if available; otherwise omit `close_time` and put `missing_marker_close_time=true` in diagnostics.
- `structure_break`: marker uses structure or micro trigger close time from existing metadata.

### Latest Report Assembly

Current `Engine.setLatestSignals()` stores only signals and diagnostics. Extend it to include:

- policy timeframes.
- marker list.
- latest position management markers.
- marker statuses from execution callback.

Add helpers:

- `signalToMarker(signal ChanlunSignal, sourceLayer, status, action, reason string) SignalMarker`
- `decisionToMarker(d decision.Decision, status string) (SignalMarker, bool)`
- `updateMarkerStatus(traderID, symbol, signalID, status string)`

Store markers in memory as part of `SignalReport`, and persist execution status through `StateStore` where signal id already exists.

Fallback in [manager/trader_manager.go](/Users/poper32/src/nofx/manager/trader_manager.go) `GetLatestStrategySignals()`:

- If no report exists, return an empty report with `DecisionMode`.
- If the trader has a programmatic engine, include policy timeframes even when no signals exist.
- Manager should not guess programmatic timeframes. Add `Engine.EmptySignalReport(traderID, symbol string)` or `Engine.TimeframeMetadata()` and expose it through `AutoTrader.GetLatestStrategySignals()`.

### Marker Persistence and Recovery

Requirements require frontend refreshes to recover signal status from backend state, not local browser state. Add bounded marker persistence to `ProgrammaticSymbolState`:

```go
RecentSignalMarkers []SignalMarker `json:"recent_signal_markers,omitempty"`
```

StateStore helpers:

- `StoreSignalMarker(traderID, symbol string, marker SignalMarker)`
- `UpdateSignalMarkerStatus(traderID, symbol, signalID, status, reason string)`
- `RecentSignalMarkers(traderID, symbol string, limit int) []SignalMarker`

Rules:

- Keep only the most recent 200 markers per symbol to avoid unbounded state growth.
- Main signal detection stores `detected` markers.
- Validation rejection updates marker to `rejected`.
- Execution success updates marker to `executed`.
- Execution failure updates marker to `failed`.
- API reports merge in-memory latest markers with persisted recent markers and de-duplicate by `signal_id + timeframe + close_time`.

## API Design

### `/api/strategy/signals`

Existing route remains the main source for signal metadata.

Response example:

```json
{
  "trader_id": "aster_deepseek",
  "symbol": "ETHUSDT",
  "decision_mode": "programmatic",
  "strategy_name": "chanlun_programmatic",
  "strategy_version": "v1",
  "config_hash": "445f5ca65bf4",
  "trade_timeframe": "1h",
  "component_timeframe": "15m",
  "micro_timeframe": "3m",
  "signals": [
    {
      "signal_id": "abc",
      "symbol": "ETHUSDT",
      "direction": "short",
      "signal_type": "sell2",
      "analysis_timeframe": "1h",
      "trigger_timeframe": "1h",
      "trigger_close_time": 1779004799999,
      "status": "detected",
      "source_layer": "main_signal"
    }
  ],
  "signal_markers": [
    {
      "symbol": "ETHUSDT",
      "timeframe": "1h",
      "close_time": 1779004799999,
      "signal_type": "sell2",
      "direction": "short",
      "level": "1h",
      "source_layer": "main_signal",
      "status": "detected",
      "signal_id": "abc"
    }
  ],
  "latest_diagnostics": {
    "messages": ["ETHUSDT 1h 无新闭合K线"]
  }
}
```

### `/api/market/klines`

Keep response compatible. Add server-side validation in [api/server.go](/Users/poper32/src/nofx/api/server.go):

- allowed timeframe: `3m`, `15m`, `1h`, `4h`.
- invalid timeframe returns `400 {"error":"timeframe必须是 3m、15m、1h 或 4h"}`.

Do not embed markers into the kline DTO in the first implementation. The frontend joins `klines[]` with `signal_markers[]` by `(timeframe, close_time)`.

## Frontend Design

Files:

- [web/src/types.ts](/Users/poper32/src/nofx/web/src/types.ts)
- [web/src/types/index.ts](/Users/poper32/src/nofx/web/src/types/index.ts)
- [web/src/lib/api.ts](/Users/poper32/src/nofx/web/src/lib/api.ts)
- [web/src/App.tsx](/Users/poper32/src/nofx/web/src/App.tsx)
- [web/src/i18n/translations.ts](/Users/poper32/src/nofx/web/src/i18n/translations.ts)

### Type Updates

`web/src/App.tsx` and `web/src/lib/api.ts` import from `./types` / `../types`, which resolves to `web/src/types.ts`. Therefore implementation must update `web/src/types.ts` first, then either mirror the same changes to `web/src/types/index.ts` or consolidate the duplicate type definitions in a separate cleanup. Do not update only `web/src/types/index.ts`.

Add:

```ts
export interface StrategySignalMarker {
  symbol: string;
  timeframe: string;
  close_time: number;
  signal_type: string;
  direction: string;
  level: string;
  source_layer: 'main_signal' | 'position_management' | string;
  status: 'detected' | 'executed' | 'rejected' | 'deduped' | 'diagnostic' | string;
  signal_id: string;
  action?: string;
  price?: number;
  reason?: string;
}
```

Extend `ChanlunSignal` with optional `trigger_close_time`, `segment_start_time`, `segment_end_time`, `status`, `source_layer`.

Extend `StrategySignalReport` with optional `trade_timeframe`, `component_timeframe`, `micro_timeframe`, `signal_markers`.

### I18n Updates

Add translation key in [web/src/i18n/translations.ts](/Users/poper32/src/nofx/web/src/i18n/translations.ts):

- English: `strategyAnalysis: "Strategy Analysis"`
- Chinese: `strategyAnalysis: "策略分析"`

In `DecisionCard`, use:

- `decision.decision_mode === "programmatic"` => `strategyAnalysis`
- otherwise => `aiThinking`

### Dynamic Timeframe

In `TraderDashboard`:

```ts
const strategyTradeTimeframe = strategySignals?.trade_timeframe || '1h';

const { data: strategyKlines } = useSWR<MarketKlineResponse>(
  traderId && strategySymbol
    ? `market-klines-${traderId}-${strategySymbol}-${strategyTradeTimeframe}`
    : null,
  () => api.getMarketKlines(traderId, strategySymbol, strategyTradeTimeframe, 80),
  { refreshInterval: 30000, revalidateOnFocus: false }
);
```

If `strategySignals` has not loaded yet, first request falls back to `1h`; once the report loads, the key changes and SWR fetches the correct timeframe.

### Marker Join

In `StrategyInspector`:

```ts
const markersByCloseTime = new Map<number, StrategySignalMarker[]>();
for (const marker of signals?.signal_markers ?? []) {
  if (marker.timeframe !== activeTimeframe) continue;
  const list = markersByCloseTime.get(marker.close_time) ?? [];
  list.push(marker);
  markersByCloseTime.set(marker.close_time, list);
}
```

K-line table adds a `信号` column:

- buy marker: green text/badge.
- sell marker: red text/badge.
- main signal: solid emphasis.
- position management: outline/subtle badge.
- executed: check-like label text `已执行`.
- rejected/deduped/diagnostic: muted label.

Do not use explanatory paragraphs inside the app. Use compact table labels and tooltips/title attributes for details.

### Layout

Keep existing three-column StrategyInspector, but update the right panel:

- Title: `${activeTimeframe} K线`
- Columns: 时间、信号、H、L、C
- Recent rows: default last 8.
- Horizontal scroll remains.

Latest signal panel:

- Shows newest main signal first.
- If no main signal, shows diagnostic.
- If there is a recent position management marker for selected symbol, show one compact row below latest main signal area:
  - `PM · short_trade · buy3 · partial_close`
  - `PM · floating_drawdown · partial_close`

## Execution and State Consistency

### Candidate vs Executed State

There are two categories of state:

- **Candidate dedupe state**: prevents outputting the exact same signal id repeatedly.
- **Execution budget state**: consumes partial close budget only after successful execution.

Implementation detail:

1. During evaluation, check `HasPositionSignal(...)`.
2. Do not mark the signal immediately.
3. After validation and successful execution, call `MarkPositionSignal(...)` and `RecordProgrammaticPartialClose(...)`.
4. If validation rejects the decision, store marker status `rejected` but do not consume budget.
5. If exchange execution fails, store marker status `failed` and do not consume budget.

This is a behavioral improvement over the current eager marking and avoids losing a valid protective action because of temporary exchange failure.

### Public Layer Conflicts

`decision.MergePublicAndStrategyDecisionsWithContext()` remains the conflict boundary.

Design rules:

- Public close suppresses all programmatic open/add/partial/stop for the same symbol.
- Public partial close suppresses programmatic partial close for the same symbol in the same cycle.
- Stronger stop-loss update wins between public and programmatic stop updates.
- Partial close cooldown and budget are applied before merge for programmatic decisions, but never applied to public decisions.

## Migration Plan

1. Deploy code with defaults; old `config.json` remains valid.
2. Existing `data/programmatic_strategy_state.json` loads with empty `partial_close_guard`.
3. First successful programmatic partial close writes the new guard state.
4. Existing decision logs remain readable because new fields are optional.
5. Frontend falls back to existing `reasoning` and fixed `1h` until `/api/strategy/signals` returns `trade_timeframe`.
6. On process restart, `/api/strategy/signals` rebuilds recent markers from `ProgrammaticSymbolState.RecentSignalMarkers`; if state is unavailable, diagnostics must explicitly say marker history is unavailable until the next strategy cycle.

Production recommended config after implementation:

```json
{
  "partial_close_cooldown_minutes": 15,
  "max_partial_close_count_per_position": 2,
  "max_total_partial_close_pct": 50
}
```

For the current small-account production context, a later config-only hardening option is:

```json
{
  "short_trade": {
    "partial_close_pct": 20
  }
}
```

This is optional and not required by this design.

## Testing Strategy

### Config

- Defaults: missing guard fields normalize to cooldown `15`, max count `2`, max total ratio `0.5`.
- Valid values: cooldown `0`, `15`, `1440`; max count `1`, `10`; total pct `1`, `50`, `100`.
- Invalid values return Chinese errors.
- Config hash changes when guard fields change.

### State

- Old state JSON without `partial_close_guard` loads.
- Successful partial close writes rule, signal id, time, count and total pct.
- Service restart preserves cooldown.
- Full close or missing position clears side guard state.

### Strategy

- `short_trade` partial close in cycle N blocks `floating_drawdown` partial close in cycle N+1 if within 15 minutes.
- `floating_drawdown` cannot trigger twice without new peak.
- New favorable peak clears drawdown lock.
- Budget clips requested 30% to remaining 20%.
- Budget exhausted blocks `short_trade` and `floating_drawdown`.
- `structure_break.action=close` bypasses partial close guard.
- `breakeven update_stop_loss` bypasses partial close guard.

### Decision and Execution

- Public close suppresses programmatic partial close.
- Programmatic partial close budget updates only after successful execution.
- Failed execution does not consume budget.
- Partial close auto-corrected to full close clears guard state.
- ActionRecord includes `Explanation`.

### API

- `/api/strategy/signals` includes timeframes and marker list.
- Empty report still returns `trade_timeframe` for programmatic trader.
- Invalid `/api/market/klines?timeframe=5m` returns 400 Chinese error.

### Frontend

- TypeScript build passes.
- StrategyInspector requests dynamic timeframe.
- K-line table title changes to `15m K线` / `1h K线` / `4h K线`.
- Markers render in the right rows.
- Multiple markers on same row render without overlap.
- AI mode still shows the AI compatibility message.
- `web/src/types.ts` and `web/src/types/index.ts` remain synchronized or the duplicate type source is explicitly consolidated.

## Deployment Validation

After implementation:

1. Local:
   - `go test ./config ./strategy/chanlun ./decision ./trader ./api ./manager`
   - `go build ./...`
   - `cd web && npm run build`
2. 161 deployment:
   - Back up production `config.json`.
   - Pull latest `jzhbnofxdev`.
   - Rebuild backend and frontend image if frontend changes are deployed.
   - Validate:
     - `/health`
     - `/api/traders`
     - `/api/status?trader_id=aster_deepseek`
     - `/api/strategy/signals?trader_id=aster_deepseek&symbol=ETHUSDT`
     - `/api/market/klines?trader_id=aster_deepseek&symbol=ETHUSDT&timeframe=1h`
   - Check first programmatic cycle logs for:
     - cooldown/budget diagnostics if applicable.
     - no repeated consecutive `partial_close` for the same position unless guard is bypassed by allowed rule.

## Open Implementation Notes

- The current `Decision.ClosePercentage` uses `omitempty`, so `30` is serialized but `0` is omitted. That is fine, but skipped/cancelled partial close reasons should be in `Explanation` or action record, not inferred from missing percentage.
- `SignalReport` is currently in-memory. Marker history must be recoverable from `StateStore.RecentSignalMarkers` after browser refresh or backend restart. If state is missing or unreadable, API diagnostics should state that marker history is unavailable until the next cycle.
- Existing frontend table uses compact text, not chart rendering. Marker badges must stay narrow and avoid changing row height dramatically.
