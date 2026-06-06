# 程序化缠论信号去重、时效与可视化清晰度 Design

## Overview

本设计解决策略检查页在所有 symbol 上共同出现的三个问题：

- 同一结构语义反复生成 marker，尤其是 preview confirmed、main rejected、structure invalidated。
- 结构时间明显早于决策/展示时间，用户误以为旧结构是当前新信号。
- 前端把过多 marker 直接渲染成文字标签，缺少默认降噪、统一模板和碰撞处理。

核心方案是引入稳定的信号生命周期模型：

1. 后端为结构、预览、入场触发和动作生成稳定 key，不再只依赖 `signal_id|timeframe|close_time`。
2. StateStore 按生命周期更新 marker，保留审计计数和最近状态，默认报告只返回降噪后的代表 marker。
3. `/api/strategy/signals` 支持默认视图和审计视图，返回 marker 汇总、隐藏数量和统一展示字段。
4. 前端建立 `SignalDisplayModel`，所有 symbol 共享同一套模板、排序、过滤、tooltip 和图表聚合规则。

本设计不改变交易执行语义。旧结构仍不能追开；preview 仍默认观察；所有 open/add 仍经过现有风控。

## Design Principles

- **先语义去重，再视觉降噪**：后端负责识别同一生命周期，前端负责按统一模板展示。
- **默认清楚，审计完整**：默认视图只展示关键状态；审计模式保留完整历史。
- **结构与动作分离**：结构背景、预览观察、入场触发、交易动作、失效/拒绝分别建模。
- **无 symbol 特例**：BTCUSDT 只是样本，显示模板不得对任何具体 symbol 写特殊分支。
- **兼容旧 state**：新增字段均 `omitempty`；旧 marker 可懒迁移，不因缺字段报错。
- **安全不退让**：UI 和 marker 生命周期调整不得绕过 freshness、missed target、open gate 或账户风控。

## Architecture

```mermaid
flowchart TD
    A[Chanlun DetectSignals] --> B[Canonicalize signal time and structure key]
    B --> C[Entry timing / freshness / open gate]
    C --> D[SignalMarker lifecycle builder]
    D --> E[StateStore lifecycle upsert]
    E --> F[SignalReport builder]
    F --> G{view}
    G -->|default| H[Denoised markers + summary]
    G -->|audit| I[Full markers + filters]
    H --> J[/api/strategy/signals]
    I --> J
    J --> K[web api.ts / types]
    K --> L[SignalDisplayModel]
    L --> M[Latest signal panel]
    L --> N[StrategyCandlestickChart]
    N --> O[Cluster / collision layout]
```

## Backend Data Model

### ChanlunSignal

Add lifecycle fields in `strategy/chanlun/types.go`:

```go
type ChanlunSignal struct {
    // existing fields...
    StructureKey       string `json:"structure_key,omitempty"`
    LifecycleKey       string `json:"lifecycle_key,omitempty"`
    ParentStructureKey string `json:"parent_structure_key,omitempty"`
    ReasonCode         string `json:"reason_code,omitempty"`
}
```

`StructureKey` identifies the stable Chanlun structure. It must not include `config_hash`, preview phase, current decision time, or synthetic preview close time.

`LifecycleKey` identifies a reportable lifecycle item. It can represent a structure, preview phase, entry trigger, action, or position management marker.

### SignalMarker

Extend `SignalMarker`:

```go
type SignalMarker struct {
    // existing fields...
    StructureKey       string `json:"structure_key,omitempty"`
    LifecycleKey       string `json:"lifecycle_key,omitempty"`
    ParentStructureKey string `json:"parent_structure_key,omitempty"`
    ReasonCode         string `json:"reason_code,omitempty"`
    DisplayCategory    string `json:"display_category,omitempty"`
    DisplayPriority    int    `json:"display_priority,omitempty"`
    HiddenByDefault    bool   `json:"hidden_by_default,omitempty"`
    Collapsed          bool   `json:"collapsed,omitempty"`
    CollapsedCount     int    `json:"collapsed_count,omitempty"`
    FirstSeenCloseTime int64  `json:"first_seen_close_time,omitempty"`
    LastSeenCloseTime  int64  `json:"last_seen_close_time,omitempty"`
    LastUpdatedAt      int64  `json:"last_updated_at,omitempty"`
}
```

Display categories:

- `structure_background`
- `preview_watch`
- `entry_trigger`
- `trade_action`
- `invalid_rejected`
- `position_management`

These fields are advisory. Frontend still derives a display model defensively when old API data lacks them.

### SignalReport

Extend `SignalReport`:

```go
type SignalReport struct {
    // existing fields...
    View          string              `json:"view,omitempty"`
    MarkerSummary SignalMarkerSummary `json:"marker_summary,omitempty"`
    Filters       SignalReportFilters `json:"filters,omitempty"`
}

type SignalMarkerSummary struct {
    TotalRaw              int            `json:"total_raw"`
    TotalReturned         int            `json:"total_returned"`
    HiddenByDefault       int            `json:"hidden_by_default"`
    CollapsedLifecycle    int            `json:"collapsed_lifecycle"`
    SuppressedRepeats     int            `json:"suppressed_repeats"`
    PreviewHidden         int            `json:"preview_hidden"`
    ByCategory            map[string]int `json:"by_category,omitempty"`
    ByStatus              map[string]int `json:"by_status,omitempty"`
    MaxLatencyHours       float64        `json:"max_latency_hours,omitempty"`
    MedianLatencyHours    float64        `json:"median_latency_hours,omitempty"`
}

type SignalReportFilters struct {
    Layers   []string `json:"layers,omitempty"`
    Statuses []string `json:"statuses,omitempty"`
    From     int64    `json:"from,omitempty"`
    To       int64    `json:"to,omitempty"`
    Limit    int      `json:"limit,omitempty"`
}
```

### Programmatic State

Extend `ProgrammaticSymbolState`:

```go
type ProgrammaticSymbolState struct {
    // existing fields...
    SignalLifecycles map[string]SignalLifecycle `json:"signal_lifecycles,omitempty"`
}

type SignalLifecycle struct {
    LifecycleKey       string       `json:"lifecycle_key"`
    StructureKey       string       `json:"structure_key,omitempty"`
    ParentStructureKey string       `json:"parent_structure_key,omitempty"`
    Marker             SignalMarker `json:"marker"`
    FirstSeenAt        time.Time    `json:"first_seen_at,omitempty"`
    LastSeenAt         time.Time    `json:"last_seen_at,omitempty"`
    SeenCount          int          `json:"seen_count,omitempty"`
    SuppressedCount    int          `json:"suppressed_count,omitempty"`
    CollapsedCount     int          `json:"collapsed_count,omitempty"`
    ConfigHashes       []string     `json:"config_hashes,omitempty"`
}
```

Keep `RecentSignalMarkers` for backward compatibility and frontend/API stability. New writes update both:

- `SignalLifecycles` is canonical for grouping.
- `RecentSignalMarkers` remains a bounded compatibility list.

## Key Generation

### Structure Key

Add in `strategy/chanlun/signals.go` or a small `lifecycle.go`:

```go
func StableStructureKey(traderID, symbol, direction, signalType, analysisTF, triggerTF, centerID string, segment Segment) string
```

Hash input:

```text
trader_id | normalized_symbol | direction | signal_type | analysis_tf | trigger_tf | center_id | segment_start_time | segment_end_time
```

Do not include:

- `config_hash`
- preview phase
- synthetic close time
- decision close time
- current price

`StableSignalID()` can remain for action lineage and old compatibility, but marker grouping should prefer `StructureKey`.

Execution safety note: `ExecutedSignals` and real action de-duplication must stay keyed by the actual signal id / entry trigger id until separately redesigned and tested. `StructureKey` is for lifecycle grouping, semantic suppression, diagnostics, and display folding; it must not collapse distinct executable triggers into one execution record.

### Lifecycle Key

Add:

```go
func MarkerLifecycleKey(marker SignalMarker) string
```

Priority:

1. `entry_trigger:<entry_trigger_id>` for entry triggers.
2. `action:<signal_id>:<action-or-trade-intent>` for executed/failed/rejected trade actions.
3. `preview:<structure_key>:<preview_phase>:<trade_candle_close>` for preview watch markers.
4. `structure:<structure_key>` for structure/background/invalidated markers.
5. `position:<signal_id>:<action>` for position management.
6. fallback to old `signal_id|timeframe|close_time`.

For preview folding, `trade_candle_close` is the parent 1h close time, not every 3m scan timestamp. This collapses repeated preview observations inside the same 1h candle.

## Backend Flow

### Detect And Canonicalize

Files:

- `strategy/chanlun/signals.go`
- `strategy/chanlun/engine.go`

During `buildSignal()`:

1. Compute `SignalID` as today for compatibility.
2. Compute `StructureKey` using `StableStructureKey()`.
3. Set `LifecycleKey = "structure:" + StructureKey` for base structure signals.

During preview generation:

1. Preserve `PreviewPhase`, `PreviewSourceTF`, `PreviewComponents`.
2. Compute `StructureKey` from the preview synthetic signal without preview config hash.
3. Compute preview lifecycle key from structure key + phase + parent trade candle close.

During entry trigger generation:

1. Keep current `EntryTriggerID` behavior.
2. Set `ParentStructureKey = signal.StructureKey`.
3. Set `LifecycleKey = "entry_trigger:" + EntryTriggerID`.

During action marker generation:

1. `decisionToMarker()` copies `structure_key`, `parent_structure_key`, `reason_code`.
2. Action lifecycle key uses trigger id if available, otherwise signal id/action.

### Semantic Suppression

Files:

- `strategy/chanlun/state.go`
- `strategy/chanlun/engine.go`

Add helpers:

```go
func (s *StateStore) HasSuppressedStructure(traderID, symbol, structureKey, action, reasonCode string) bool
func (s *StateStore) StoreStructureSuppression(traderID, symbol string, suppression SignalSuppression)
```

Extend `SignalSuppression` with:

```go
StructureKey string `json:"structure_key,omitempty"`
LastSeenAt   time.Time `json:"last_seen_at,omitempty"`
SeenCount    int `json:"seen_count,omitempty"`
```

When `target_already_crossed`, `entry_window_invalid`, `signal_expired`, or repeated stale structure occurs:

- Store semantic suppression by structure key.
- Update lifecycle marker with `SuppressedCount`.
- Do not append a new default-visible marker.
- Still allow a new `entry_trigger_id` or new `structure_key` to proceed.

### State Upsert And Compaction

Replace direct use of `signalMarkerKey()` for new writes with lifecycle-aware upsert:

```go
func upsertSignalMarker(markers []SignalMarker, marker SignalMarker) []SignalMarker
func mergeSignalMarkerLifecycle(existing, incoming SignalMarker) SignalMarker
```

New merge rules:

- Preserve executed/failed action markers as audit-critical.
- For non-action repeated structure/preview markers, keep earliest `FirstSeenCloseTime`, latest `LastSeenCloseTime`, latest reason/status, and increment `CollapsedCount`.
- Prefer terminal status order: `executed > failed > rejected > invalidated > ready > background > confirmed > watchlist > detected`.
- Keep newest `DecisionCloseTime` and `DisplayCloseTime` for representative marker.
- Keep `SignalCloseTime` as original structure time.

Add:

```go
func (s *StateStore) CompactSignalMarkers(traderID, symbol string) SignalMarkerSummary
```

Compaction can run lazily:

- On `StateStore.Load()` for in-memory migration.
- On `StoreSignalMarker()` for the touched symbol.
- On `LatestSignals()` before report build.

No standalone destructive migration is required for the first implementation.

Load/report compaction should be non-destructive from the caller's perspective. Persist compacted state only on the normal save path or an explicit maintenance command after validation, not as an unexpected write during a read-only API request.

### Report Builder

Files:

- `strategy/chanlun/engine.go`
- `manager/trader_manager.go`
- `api/server.go`

Introduce:

```go
type SignalReportOptions struct {
    View     string
    Layers   []string
    Statuses []string
    From     int64
    To       int64
    Limit    int
}

func (e *Engine) LatestSignalsWithOptions(traderID, symbol string, opts SignalReportOptions) (*SignalReport, bool)
func (e *Engine) EmptySignalReportWithOptions(traderID, symbol string, opts SignalReportOptions) *SignalReport
```

Keep `LatestSignals()` and `EmptySignalReport()` as wrappers using default options to reduce blast radius.

Default view:

- Return latest representative marker per lifecycle.
- Hide ordinary `preview_signal confirmed/watchlist` history unless it is the only useful current context.
- Hide repeated stale/suppressed checks.
- Keep executed, failed, rejected action markers visible.
- Keep latest invalidated/background structure summary visible only once.

Audit view:

- Return full bounded history after applying filters.
- Mark collapsed fields when multiple raw events map to the same lifecycle.

API query parameters:

```text
view=default|audit
layers=structure,preview_signal,entry_trigger,main_signal,position_management
statuses=ready,rejected,invalidated,confirmed,executed,failed
from=epoch_ms
to=epoch_ms
limit=number
include_history=true
```

`include_history=true` is an alias for `view=audit`.

## API Contract

Endpoint:

```http
GET /api/strategy/signals?trader_id=aster_deepseek&symbol=BTCUSDT&view=default
```

Response remains compatible:

```json
{
  "trader_id": "aster_deepseek",
  "symbol": "BTCUSDT",
  "decision_mode": "programmatic",
  "strategy_name": "chanlun_programmatic",
  "strategy_version": "v1",
  "config_hash": "e0dc5de3c03b",
  "trade_timeframe": "1h",
  "component_timeframe": "15m",
  "micro_timeframe": "3m",
  "signals": [],
  "signal_markers": [],
  "marker_summary": {
    "total_raw": 27,
    "total_returned": 4,
    "hidden_by_default": 23,
    "collapsed_lifecycle": 18,
    "preview_hidden": 16,
    "by_category": {
      "preview_watch": 1,
      "invalid_rejected": 2,
      "trade_action": 1
    },
    "max_latency_hours": 23,
    "median_latency_hours": 15.5
  },
  "view": "default",
  "latest_diagnostics": {
    "messages": ["BTCUSDT 旧结构已失效，等待 fresh entry trigger"],
    "marker_summary": {}
  }
}
```

Existing frontend can continue reading `signal_markers`. New frontend uses `marker_summary` and new marker fields.

Compatibility nuance: "continue reading" means the response remains parseable and does not return 500. The current `StrategyInspector` and `StrategyCandlestickChart` also filter `source_layer === "main_signal"`, while newer lifecycle markers may use `structure`, `preview_signal`, `entry_trigger`, and `trade_action`. Frontend migration to `SignalDisplayModel` is therefore required in the same rollout for correct visual behavior. `view=audit` / `include_history=true` remains the raw-history fallback for diagnostics and rollback.

## Frontend Design

### Types

Files:

- `web/src/types/index.ts`
- `web/src/types.ts`
- `web/src/lib/api.ts`

Extend TypeScript types to mirror backend fields:

```ts
export type SignalDisplayCategory =
  | 'structure_background'
  | 'preview_watch'
  | 'entry_trigger'
  | 'trade_action'
  | 'invalid_rejected'
  | 'position_management';

export interface SignalMarker {
  // existing fields...
  structure_key?: string;
  lifecycle_key?: string;
  parent_structure_key?: string;
  reason_code?: string;
  display_category?: SignalDisplayCategory;
  display_priority?: number;
  hidden_by_default?: boolean;
  collapsed?: boolean;
  collapsed_count?: number;
  first_seen_close_time?: number;
  last_seen_close_time?: number;
  last_updated_at?: number;
}

export interface SignalMarkerSummary {
  total_raw: number;
  total_returned: number;
  hidden_by_default: number;
  collapsed_lifecycle: number;
  suppressed_repeats: number;
  preview_hidden: number;
  by_category?: Record<string, number>;
  by_status?: Record<string, number>;
  max_latency_hours?: number;
  median_latency_hours?: number;
}
```

Update `api.getStrategySignals()` to accept options:

```ts
getStrategySignals(traderId, symbol, options?: StrategySignalQuery)
```

Important existing-code note: most frontend imports currently use `../types` or `./types`, which resolves to `web/src/types.ts` before the directory `web/src/types/index.ts`. Implementation must either update both files or consolidate them safely after auditing imports; updating only `web/src/types/index.ts` will not update the active app type contract.

### SignalDisplayModel

Add `web/src/utils/strategyDisplay.ts`.

```ts
export interface SignalDisplayItem {
  id: string;
  marker: SignalMarker;
  category: SignalDisplayCategory;
  title: string;
  shortLabel: string;
  tone: 'buy' | 'sell' | 'muted' | 'warning' | 'action';
  priority: number;
  defaultVisible: boolean;
  summary: string;
  tooltipRows: Array<{ label: string; value: string }>;
}

export interface SignalDisplayModel {
  latest?: SignalDisplayItem;
  chartMarkers: SignalMarker[];
  hiddenCount: number;
  collapsedCount: number;
  items: SignalDisplayItem[];
}
```

Rules:

- No symbol-specific branches.
- Category is read from `display_category` when available, otherwise derived from `source_layer`, `status`, `action`, `trade_intent`, and `entry_trigger_id`.
- Priority:
  1. executed/failed action
  2. ready entry trigger
  3. rejected action
  4. latest invalidated/background structure
  5. preview summary
  6. hidden audit history
- `latest` uses the highest-priority visible item, not `signals[0]`.

### StrategyInspector

File:

- `web/src/App.tsx`

Replace local marker filtering:

```ts
const markers = signals?.signal_markers ?? [];
const mainMarkers = markers.filter(...)
```

with:

```ts
const displayModel = buildSignalDisplayModel(signals, uiFilters);
```

Use:

- `displayModel.latest` for 最新信号 panel.
- `displayModel.chartMarkers` for chart.
- `signals.marker_summary` for hidden/collapsed count.

Add compact controls:

- Layer filter: structure / preview / trigger / action / PM.
- View toggle: default / audit.
- Status filter can be a small menu or segmented control.

The controls apply uniformly across symbols. Persist them in component state while switching symbol.

### Candlestick Chart

Files:

- `web/src/components/StrategyCandlestickChart.tsx`
- `web/src/utils/strategyMarkers.ts`
- new `web/src/utils/strategyDisplay.ts`

Changes:

1. Accept already filtered `markers`.
2. Expand markers into visual markers using display model labels.
3. Cluster markers when:
   - same candle + same placement has more than 2 markers;
   - adjacent labels overlap horizontally;
   - available vertical space is exhausted.
4. Render cluster badge as compact count, for example `S x4` or `B x3`.
5. Hover/click cluster shows tooltip list with all marker display items.
6. Use compact labels:
   - structure: `S2 结构`
   - preview: `S2 预览`
   - trigger: `S2 触发`
   - action rejected: `S2 开空拒`
   - executed: `S2 开空成`
7. Keep buy markers below candles and sell markers above candles.

Collision algorithm:

```ts
group visual markers by normalized anchor close time and placement
sort by priority desc, then signal type, then lifecycle key
for each group:
  if group size > maxLabelsPerSide:
    create cluster visual
  else:
    assign vertical stack index
after initial layout:
  scan adjacent labels by x range
  if overlap:
    compact lower-priority labels into cluster
```

No viewport-scaled font sizes. Use stable label dimensions and compact text.

## Logging And Diagnostics

Add marker lifecycle summary to strategy diagnostics:

```go
fullDecision.StrategyDiagnostics["marker_lifecycle"] = map[string]any{
    "created": created,
    "updated": updated,
    "collapsed": collapsed,
    "hidden_by_default": hidden,
    "suppressed_repeats": suppressed,
}
```

Service logs should summarize repeated preview noise:

```text
BTCUSDT preview_3x15m 同结构观察已更新，折叠计数 +1
```

Do not print long repeated per-symbol preview lists every scan when nothing actionable changed.

## Compatibility And Migration

- Existing `close_time`, `signal_close_time`, `decision_close_time`, `display_close_time` remain.
- Old state without `StructureKey` can compute fallback keys from symbol, signal type, direction, timeframe, and close time.
- Old frontend remains API-compatible because `signal_markers` is still present; visual correctness still requires removing current frontend `main_signal`-only assumptions.
- New default API returns fewer markers by design. Audit view keeps full history available.
- During rollout, if backend deploys before frontend, use `view=audit` / `include_history=true` as an explicit raw-history fallback for diagnostics or temporary rollback.
- No production `data/` file is committed.
- If compaction has a bug, fallback is `view=audit` plus old marker fields.

## Risk Controls

Trading behavior is intentionally unchanged:

- `prepareStructureEntry()` still blocks old structures without fresh trigger.
- `target_already_crossed` remains terminal for structure entry.
- `applyProgrammaticSignalGuard()` remains final safety.
- Existing open gate, ADX/DI, BTC environment, risk budget, position limit, stop/take-profit validation stay mandatory.
- Preview pilot/full open remains controlled by explicit existing config.

## Implementation Plan By File

Backend:

- `strategy/chanlun/types.go`
  - Add lifecycle/display fields and summary structs.
- `strategy/chanlun/signals.go`
  - Add `StableStructureKey()`.
  - Populate `StructureKey`.
- `strategy/chanlun/engine.go`
  - Populate lifecycle fields in `signalToMarker()` and `decisionToMarker()`.
  - Add report builder with default/audit options.
  - Apply semantic suppression before appending visible markers.
- `strategy/chanlun/state.go`
  - Add lifecycle-aware upsert and optional `SignalLifecycles`.
  - Add compaction and suppression-by-structure helpers.
- `manager/trader_manager.go`
  - Add `GetLatestStrategySignalsWithOptions()`.
- `api/server.go`
  - Parse `view/layers/statuses/from/to/limit/include_history`.
  - Return enhanced `SignalReport`.
- `logger/decision_logger.go` only if marker lifecycle summary needs explicit typed fields; otherwise keep diagnostics map.

Frontend:

- `web/src/types.ts` and `web/src/types/index.ts`
  - Add new fields and query/summary types, or consolidate the duplicate type sources after import audit.
- `web/src/lib/api.ts`
  - Add optional query args to `getStrategySignals()`.
- `web/src/utils/strategyDisplay.ts`
  - New unified display template and latest selection logic.
- `web/src/utils/strategyMarkers.ts`
  - Use display model labels, category and priority.
  - Add cluster/collision helper pure functions where practical.
- `web/src/components/StrategyCandlestickChart.tsx`
  - Render clusters and compact labels.
  - Remove the internal `source_layer === "main_signal"` filter and trust the already-filtered display model input.
- `web/src/App.tsx`
  - Use `SignalDisplayModel` in `StrategyInspector`.
  - Add uniform filters and default/audit toggle.

## Testing Strategy

Backend:

- `go test ./strategy/chanlun`
  - structure key stable across config hash.
  - repeated target-crossed structure does not create default-visible duplicates.
  - preview_2x15m/preview_3x15m fold within same 1h candle.
  - new entry trigger is not blocked by parent structure suppression.
  - state compaction keeps executed/failed action markers.
- `go test ./api ./manager`
  - default view vs audit view.
  - filters parse correctly.
  - old report fields remain present.

Frontend:

- `cd web && npm run test`
  - `buildSignalDisplayModel()` category/priority/latest selection.
  - marker clustering and hidden/collapsed count.
  - no symbol-specific behavior.
  - action marker and structure marker labels differ correctly.
- `cd web && npm run build`
  - Type contract and UI compile.

Manual/visual:

- Use脱敏 161 fixture covering BTCUSDT、ETHUSDT、SOLUSDT、XAGUSDT、XRPUSDT、CLUSDT.
- Verify default view is clear and audit view can expand history.
- Verify latest panel no longer presents stale structure as current open signal.

## Rollout

1. Implement backend fields and lifecycle key generation with old behavior still available.
2. Enable default report denoise behind `view=default`; support `view=audit`.
3. Update frontend to use default view and unified template.
4. Add state compaction lazily; do not mutate production state destructively on first deploy.
5. After deployment, compare marker counts and latency summaries on all active symbols.

Expected 161 outcome:

- BTCUSDT should no longer default-render 27 raw markers.
- ETHUSDT、SOLUSDT、XAGUSDT、XRPUSDT、CLUSDT use the same template and folding rules.
- Median raw marker latency may still be visible in audit mode, but default mode explains it as old structure/background, not current tradable signal.
