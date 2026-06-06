# Chanlun V2 Signal Freshness Time Alignment Design

## Overview

本设计解决缠论 V2 线上 BNBUSDT 类似场景：

- `open_rejected` 和策略检查页 marker 使用同一 `signal_id`，但用户看到周期时间与结构时间差距很大。
- `chanlun_v2` 当前在 `signalToDecision()` 中把 `decision_close_time` 设置为 `signal_close_time`，导致“决策时间”不能表达本轮评估锚点。
- 旧的 1h `buy2` 信号在多个周期内持续进入 open gate，被 BTC/ADX 等风控重复拒绝，产生噪声。

设计目标是为 `chanlun_v2` 引入独立信号新鲜度门控，并统一后端日志、策略信号 API 和前端图表的时间语义。改动范围控制在 `strategy/chanlunv2`、`decision` 元数据、`logger` 决策记录、`api/manager` 对账测试和 `web` 策略检查展示，不改变交易所执行接口。

## Design Principles

- **时间语义分离**：结构时间、评估 K 线时间、动作发生时间分别有明确字段。
- **旧信号前置过滤**：stale signal 在 open gate 前被过滤，避免污染 open rejection 和频率统计。
- **可对账**：`signal_id` 是日志、marker 和 API 的主键；时间字段辅助判断生命周期。
- **安全优先**：freshness gate 只会减少过期开仓候选，不放宽任何现有风控。
- **向后兼容**：保留已有字段，新增字段用 `omitempty`，旧日志仍可读取。

## Architecture

```mermaid
flowchart TD
    A[PrepareCycleContext closed klines] --> B[chanlunv2.analyzeSymbol]
    B --> C[multiLevelJudgment]
    C --> D[signalToDecision]
    D --> E[applyChanlunV2FreshnessGuard]
    E -->|stale| F[StateStore marker invalid/rejected]
    E -->|fresh| G[ValidateStrategyDecisions]
    G --> H[OpenGate / sizing / final limits]
    H --> I[DecisionRecord actions]
    F --> J[/api/strategy/signals]
    I --> K[/api/decisions/latest]
    J --> L[StrategyCandlestickChart]
    K --> L
```

## Technical Design

### 1. Chanlun V2 Freshness Policy

Add a small runtime policy for `chanlun_v2` open signals. Prefer config-backed fields if the current `config.ChanlunV2StrategyConfig` already has an appropriate extension point; otherwise add a nested optional config:

```go
type ChanlunV2SignalFreshnessConfig struct {
    Enabled                      *bool          `json:"enabled,omitempty"`
    SoftAgeCandles               int            `json:"soft_age_candles,omitempty"`
    MaxLifetimeCandles           int            `json:"max_lifetime_candles,omitempty"`
    SoftAgeBySignalType          map[string]int `json:"soft_age_by_signal_type,omitempty"`
    MaxLifetimeBySignalType      map[string]int `json:"max_lifetime_by_signal_type,omitempty"`
    MissedTargetGuard            *bool          `json:"missed_target_guard,omitempty"`
    ConfidenceDecayPerAgedCandle int            `json:"confidence_decay_per_aged_candle,omitempty"`
    MinRemainingNetRR            float64        `json:"min_remaining_net_rr,omitempty"`
}
```

Suggested defaults:

- `Enabled`: true
- `SoftAgeCandles`: 1
- `MaxLifetimeCandles`: 2
- `MinRemainingNetRR`: use existing strategy/open gate RR expectations if available, otherwise `1.2`
- `MissedTargetGuard`: true
- `ConfidenceDecayPerAgedCandle`: 10

Rationale:

- For `1h` trade timeframe, a signal that is 18 hours old should never be a fresh open candidate.
- Soft age allows one delayed cycle or deploy restart without immediately discarding a still-valid signal.
- Hard age stops repeated open gate rejection loops.
- Field names intentionally match the existing programmatic `signal_freshness` convention, reducing duplicate semantics between Chanlun V1 and V2.

Affected files:

- `config/config.go`
- `config/programmatic.go` only if shared config normalization helpers are reused; programmatic behavior must not change
- `config/config_test.go`
- `trader/auto_trader.go` if manager conversion needs to pass config into engine
- `strategy/chanlunv2/engine.go`

### 2. Evaluation Anchor And Time Semantics

Define three time concepts for `chanlun_v2`:

- `signal_close_time`: original signal structure K-line close time.
- `decision_close_time`: latest closed trade timeframe K-line used in the current evaluation cycle.
- `action_timestamp`: wall-clock time when a `DecisionAction` / `OpenRejection` was recorded.

Implementation approach:

1. Compute evaluation anchor from the same closed trade timeframe klines used by the current analysis:

```go
func evaluationCloseTime(mr *multiLevelResult, tradeLevel string) int64
```

The current V2 `multiLevelResult` only stores `Results`; extend it with `LastClosedByLevel map[string]int64` or an equivalent field populated from the source K-line slice before calling `AnalyzeKlines`. Prefer refactoring `analyzeSymbol` to accept prepared `ctx.MarketDataMap[symbol].Klines` from `PrepareCycleContext`, falling back to direct fetch only for ad hoc report paths that do not have prepared data.

2. Update `signalToDecision(ctx, symbol, sig, timeframe, decisionCloseTime)`:

```go
signalClose := normalizeV2EpochMillis(sig.Timestamp)
decisionClose := maxPositive(decisionCloseTime, signalClose)
StrategyMetadata: map[string]any{
    "signal_close_time": signalClose,
    "decision_close_time": decisionClose,
    "evaluation_close_time": decisionClose,
}
```

3. Keep `SignalID` stable and based on `signal_close_time`, not evaluation time:

```go
chanlun_v2:BNBUSDT:1h:buy2:<signal_close_time>
```

This preserves lifecycle identity while letting action markers move to the evaluation K-line.

Affected files:

- `strategy/chanlunv2/engine.go`
- `strategy/chanlunv2/report.go`
- `strategy/chanlunv2/types.go` only if `AnalysisResult` needs a last close field

### 3. Freshness Guard

Add a pre-validation guard after raw decisions are built and before `validateChanlunV2Decisions()`:

```go
func (e *Engine) applyChanlunV2FreshnessGuard(ctx *decision.Context, decisions []decision.Decision, timeframes map[string]string) ([]decision.Decision, []decision.OpenRejection)
```

Behavior:

- Ignore non-open-like decisions.
- Read `signal_close_time` and `decision_close_time` from `Decision.StrategyMetadata`.
- Compute `ageCandles = floor((decisionClose - signalClose) / tradeDuration)`.
- Use `ctx.MarketDataMap[decision.Symbol]` for current price, target-crossed and RR checks; if market data is missing, keep the age gate active and add a diagnostic that price/RR checks were skipped.
- Reject hard-expired signals before open gate:
  - reason code: `freshness_gate.signal_expired`
  - marker status: `rejected` or `invalidated`
  - action remains open-like for explainability but is not executable.
- For target-crossed or RR-invalid signals:
  - reason code: `freshness_gate.target_crossed` or `freshness_gate.rr_invalid`
  - preserve signal metadata and diagnostics.
- For soft-aged but still valid signals:
  - apply confidence decay or stricter diagnostics.
  - keep signal eligible for existing validation/open gate.

Return stale rejections as `decision.OpenRejection` using `decision.NewOpenRejectionFromDecision()` or equivalent, so `appendOpenRejectionsToRecord()` writes them as `open_rejected` with complete metadata. Add a marker-specific reason that distinguishes `freshness_gate` from open gate.

Important distinction:

- Stale gate rejections should not be counted as open gate market rejection when summarizing open gate reasons if that would inflate market gate noise. Use a distinct reason code prefix such as `freshness_gate.*`.
- They should still appear in decision logs as explainable rejected strategy actions.

Affected files:

- `strategy/chanlunv2/engine.go`
- `strategy/chanlunv2/report.go`
- `decision/decision.go`
- `decision/types.go`
- `trader/auto_trader.go`
- `logger/decision_logger.go` only if adding explicit `action_timestamp` JSON field beyond existing `timestamp`

### 4. Marker Lifecycle Updates

Current `signalToV2Marker()` and `decisionToV2Marker()` should be updated to match the three-time model:

- Pure signal marker:
  - `CloseTime = signal_close_time`
  - `SignalCloseTime = signal_close_time`
  - `DecisionCloseTime = decision_close_time` if known, otherwise empty/zero
  - `DisplayCloseTime = signal_close_time`

- Action/rejected marker:
  - `CloseTime = signal_close_time`
  - `SignalCloseTime = signal_close_time`
  - `DecisionCloseTime = decision_close_time`
  - `DisplayCloseTime = decision_close_time`
  - `LastUpdatedAt` or new `ActionTimestamp` = wall-clock event time when available

The existing frontend helper `resolveDisplayCloseTime()` already prefers `display_close_time` and action marker `decision_close_time`, so the backend should provide correct values rather than introducing a second chart behavior.

Shared `chanlun.SignalMarker` already has `FreshnessState` and `AgeCandles`. Add the missing optional fields only where the UI/API needs top-level access:

```go
ActionTimestamp int64 `json:"action_timestamp,omitempty"`
EvaluationCloseTime int64 `json:"evaluation_close_time,omitempty"`
StaleReason string `json:"stale_reason,omitempty"`
```

Frontend type additions should mirror missing fields for `SignalMarker` and `DecisionAction`; existing `freshness_state` and `age_candles` fields should be retained and populated for V2.

Affected files:

- `strategy/chanlun/types.go` if shared marker type is used
- `strategy/chanlunv2/report.go`
- `web/src/types.ts`
- `web/src/types/index.ts`

### 5. Strategy Check UI

Update strategy check display to reduce ambiguity:

- In latest signal card:
  - show `结构时间`
  - show `评估K线`
  - show `动作时间` when present
  - show `年龄` / `freshness_state`
- In chart tooltip:
  - use current structure/decision rows but rename `决策` to `评估K线`.
  - add `动作` row for `action_timestamp`.
  - if `signal_close_time` and `decision_close_time` differ, show clear paired-time wording.
- For stale markers:
  - show warning tone and reason code, not only generic `rejected`.

Affected files:

- `web/src/App.tsx`
- `web/src/components/StrategyCandlestickChart.tsx`
- `web/src/utils/strategyDisplay.ts`
- `web/src/utils/strategyMarkers.ts`
- `web/src/utils/strategyMarkers.test.ts`

### 6. API And Log Compatibility

No new endpoint is required. Existing endpoints continue:

- `GET /api/decisions/latest?trader_id=...`
- `GET /api/strategy/signals?trader_id=...&symbol=...`
- `GET /api/market/klines?trader_id=...&symbol=...&timeframe=1h`

If `DecisionAction` gains `action_timestamp`, it may duplicate `timestamp` but gives frontend a numeric epoch field. To minimize schema churn, first prefer existing `timestamp` for decisions, and add numeric `action_timestamp` only to marker/API objects that cannot otherwise access `DecisionAction.Timestamp`.

Backward compatibility:

- Old marker JSON without `evaluation_close_time` or `action_timestamp` still renders.
- Old logs where `decision_close_time == signal_close_time` keep existing behavior.
- New frontend fields must be optional.

## Data Structures

### `decision.Decision.StrategyMetadata`

Expected keys:

```go
"signal_close_time": int64
"decision_close_time": int64
"evaluation_close_time": int64
"freshness_state": string
"age_candles": int
"stale_reason": string
"guard_reason_code": string
"remaining_net_rr": float64
```

### `decision.OpenRejection`

Reuse existing fields from the previous decision marker work:

- `SignalID`
- `SignalType`
- `SignalTimeframe`
- `SignalCloseTime`
- `DecisionCloseTime`
- `TradeIntent`
- `StrategyMetadata`

Optional addition:

- `FreshnessState string`
- `AgeCandles int`
- `EvaluationCloseTime int64`
- `StaleReason string`

### `chanlun.SignalMarker`

Existing fields:

- `FreshnessState string`
- `AgeCandles int`

Optional additions:

```go
EvaluationCloseTime int64 `json:"evaluation_close_time,omitempty"`
ActionTimestamp int64 `json:"action_timestamp,omitempty"`
StaleReason string `json:"stale_reason,omitempty"`
```

## Risk Controls

- Freshness gate only removes or weakens old open-like decisions; it never turns a rejected signal into an allowed one.
- Existing open gate remains the final safety net for market structure, ADX/DI, BTC, correlation, execution quality and loss mode.
- No changes to exchange order execution or stop-loss/take-profit cancellation semantics.
- No tests should call real exchange order endpoints.

## Test Plan

### Backend Unit Tests

- `strategy/chanlunv2`:
  - fresh signal passes freshness guard and keeps metadata.
  - soft-aged signal applies confidence decay or diagnostics.
  - hard-expired BNB-like signal is rejected before open gate.
  - target-crossed long/short signals are rejected with reason codes.
  - RR-invalid aged signal is rejected.
  - rejected marker uses `display_close_time = decision_close_time`.
  - same `signal_id` marker status updates to `rejected` without losing signal metadata.

- `decision`:
  - `OpenRejection` preserves `signal_close_time`, `decision_close_time`, freshness metadata and trade intent.

- `trader` / `logger`:
  - `appendOpenRejectionsToRecord()` writes BNB-like stale rejection metadata into `DecisionAction`.
  - `DecisionAction.Timestamp` remains action wall-clock time.

- `api` / `manager`:
  - `/api/strategy/signals` marker can be matched with latest decision action by `signal_id`.

### Frontend Tests

- `web/src/utils/strategyMarkers.test.ts`:
  - action marker anchors to `display_close_time` / `decision_close_time`.
  - pure structure marker anchors to `signal_close_time`.
  - paired signal/action times are both exposed.

- `web/src/utils/strategyDisplay.test.ts`:
  - stale markers show freshness state and age.
  - tooltip rows include structure, evaluation and action timestamps when present.

### Validation Commands

```bash
CGO_ENABLED=0 go test ./strategy/chanlunv2 ./decision ./trader ./logger
CGO_ENABLED=0 go test ./api ./manager
CGO_ENABLED=0 go build ./...
cd web && npm run test
cd web && npm run build
```

## Rollout Plan

1. Implement and test backend freshness/time semantics.
2. Update frontend optional types and strategy check display.
3. Run local targeted validation.
4. Commit and push.
5. Deploy to 161.
6. Verify with BNBUSDT:
   - latest decision action and strategy signal share the same `signal_id`.
   - stale signal no longer repeats as open gate rejection once hard expired.
   - strategy check shows structure time, evaluation K-line time and action time separately.
