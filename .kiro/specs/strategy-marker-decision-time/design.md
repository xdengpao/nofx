# 策略信号标记确认时间与展示锚点 Design

## Overview

本设计解决程序化策略蜡烛图 marker 的两个问题：

1. 缠论结构信号需要右侧确认，结构点时间 `signal_close_time` 可能早于本周期最新闭合 K 线 `decision_close_time`。
2. 前端当前只按单一 `close_time` 精确匹配 K 线，导致交易动作发生在最新闭合 K 线时，图上只能看到较早结构点，无法看见本周期确认/拒绝动作。

设计原则：

- 后端继续用一条逻辑 `SignalMarker` 表达一个 `signal_id` 的生命周期，不持久化两条重复 marker。
- `close_time` 保持历史兼容语义，继续表示结构信号时间。
- 新增 `signal_close_time`、`decision_close_time`、`display_close_time`，让 API 明确传递结构点、决策点和默认展示锚点。
- 前端从单条逻辑 marker 派生视觉 marker：结构点落在 `signal_close_time`，交易动作点落在 `decision_close_time`。
- 买点位于 K 线下方，卖点位于 K 线上方；同侧多 marker 分层展示，不重叠。
- 不修改缠论信号算法、风控规则和交易执行行为。

## Architecture

```mermaid
flowchart TD
  A[market.Data trade timeframe Klines] --> B[analyzeMainSignal]
  B -->|lastClosed| C[ChanlunSignal.DecisionCloseTime]
  B -->|segment.EndTime| D[ChanlunSignal.SignalCloseTime]
  D --> E[signalToMainDecision]
  C --> E
  E -->|StrategyMetadata| F[ValidateStrategyDecisions / OpenRejection]
  F --> G[decisionToMarker]
  G --> H[StateStore RecentSignalMarkers]
  H --> I[/api/strategy/signals]
  I --> J[web SignalMarker types]
  J --> K[expandVisualMarkers]
  K --> L[StrategyCandlestickChart]

  F --> M[appendOpenRejectionsToRecord]
  M --> N[decision_logs DecisionAction]
```

## Backend Data Contract

### `strategy/chanlun.ChanlunSignal`

新增字段：

```go
SignalCloseTime   int64 `json:"signal_close_time,omitempty"`
DecisionCloseTime int64 `json:"decision_close_time,omitempty"`
```

约定：

- `SignalCloseTime` 表示结构信号时间，来源为当前 `segment.EndTime`。
- `DecisionCloseTime` 表示本轮主交易级别最新闭合 K 线，来源为 `analyzeMainSignal()` 的 `lastClosed`。
- `TriggerCloseTime` 作为兼容字段保留，并在新生成信号中继续等于 `SignalCloseTime`。
- `SegmentEndTime` 仍保留原始走势段结束时间，用于诊断和旧逻辑回退。

### `strategy/chanlun.SignalMarker`

新增字段：

```go
SignalCloseTime   int64 `json:"signal_close_time,omitempty"`
DecisionCloseTime int64 `json:"decision_close_time,omitempty"`
DisplayCloseTime  int64 `json:"display_close_time,omitempty"`
```

约定：

- `CloseTime` 继续表示结构信号时间，等同于 `SignalCloseTime` 的兼容别名。
- `SignalCloseTime` 优先来自 `signal_close_time`，回退 `trigger_close_time`、`segment_end_time`、`close_time`。
- `DecisionCloseTime` 仅在交易动作 marker 中强制填充；纯检测 marker 可以不填。
- `DisplayCloseTime` 是前端默认展示锚点：
  - 交易动作 marker：优先使用 `DecisionCloseTime`。
  - 纯检测 marker：使用 `SignalCloseTime`。
- `signalMarkerKey()` 保持以 `signal_id + timeframe + CloseTime` 去重，确保 detected marker 后续可被 rejected/executed/failed 生命周期状态覆盖，而不是写入重复逻辑 marker。

### `decision.OpenRejection`

新增字段：

```go
StrategyMode      string         `json:"strategy_mode,omitempty"`
StrategyName      string         `json:"strategy_name,omitempty"`
StrategyVersion   string         `json:"strategy_version,omitempty"`
ConfigHash        string         `json:"config_hash,omitempty"`
SignalID          string         `json:"signal_id,omitempty"`
SignalType        string         `json:"signal_type,omitempty"`
SignalTimeframe   string         `json:"signal_timeframe,omitempty"`
SignalCloseTime   int64          `json:"signal_close_time,omitempty"`
DecisionCloseTime int64          `json:"decision_close_time,omitempty"`
TradeIntent       string         `json:"trade_intent,omitempty"`
StrategyMetadata  map[string]any `json:"strategy_metadata,omitempty"`
```

新增 `decision.NewOpenRejectionFromDecision(d Decision, reason string) OpenRejection`，统一从 `Decision` 复制程序化策略元数据。现有 `buildOpenRejection()` 使用该 helper 初始化，再追加 gate state、gate reasons、diagnostics、simulations。

需要替换的裸 `OpenRejection{...}` 创建点：

- `decision.ValidateStrategyDecisions()` 中不允许加仓、参数补充失败等开仓拒绝。
- `decision.enforceFinalDecisionLimits()` 中最终限制拒绝。
- `strategy/chanlun.Engine.validateProgrammaticDecisions()` 中 `prep.RiskIncreaseBlocked` 对 open/add 的拒绝。

### `logger.DecisionAction`

新增字段：

```go
TradeIntent       string `json:"trade_intent,omitempty"`
SignalCloseTime   int64  `json:"signal_close_time,omitempty"`
DecisionCloseTime int64  `json:"decision_close_time,omitempty"`
```

写入规则：

- `applyDecisionSizingToActionRecord()` 从 `Decision.StrategyMetadata` 提取两类时间和 `trade_intent`，同时保留完整 `strategy_metadata`。
- `appendOpenRejectionsToRecord()` 从 `OpenRejection` 复制 `signal_id`、`signal_type`、`signal_timeframe`、`signal_close_time`、`decision_close_time`、`trade_intent`、策略版本字段和 `strategy_metadata`。
- 旧日志缺少这些字段时前端和 replay 按空值兼容。

## Backend Flow Changes

### 1. 主信号分析

`strategy/chanlun/engine.go` 的 `analyzeMainSignal()` 已经计算：

```go
lastClosed := tradeKlines[len(tradeKlines)-1].CloseTime
```

在 `DetectSignals()` 返回后，统一补齐：

```go
for i := range signals {
    signalClose := signals[i].TriggerCloseTime
    if signalClose == 0 {
        signalClose = signals[i].SegmentEndTime
    }
    signals[i].SignalCloseTime = signalClose
    signals[i].TriggerCloseTime = signalClose
    signals[i].DecisionCloseTime = lastClosed
}
```

如果 `DecisionCloseTime < SignalCloseTime`，视为异常时间锚点，不阻塞交易，但写入诊断并回退展示锚点为结构时间。

### 2. 信号转主决策

`signalToMainDecision()` 在 `StrategyMetadata` 和 `DecisionExplanation.Details` 中写入：

```go
"signal_close_time": signal.SignalCloseTime,
"decision_close_time": signal.DecisionCloseTime,
"trigger_close_time": signal.SignalCloseTime, // legacy
"segment_start_time": signal.SegmentStartTime,
"segment_end_time": signal.SegmentEndTime,
"trade_intent": action,
```

`Decision.SignalID`、`SignalType`、`SignalTimeframe` 继续作为顶层字段写入，方便日志和 API 直接读取。

### 3. 纯检测 marker

`signalToMarker(signal, "main_signal", "detected", "", "")` 生成纯结构检测 marker：

- `CloseTime = SignalCloseTime`
- `SignalCloseTime = SignalCloseTime`
- `DisplayCloseTime = SignalCloseTime`
- 不强制填 `DecisionCloseTime`
- label/tooltip 语义为“结构点/信号点”，不表示交易动作

这样纯检测信号不会因为存在确认时间而自动扩展成两个视觉点。

### 4. 交易动作 marker

`decisionToMarker()` 读取 `StrategyMetadata`：

```go
signalClose := metadataInt64Any("signal_close_time", "trigger_close_time", "segment_end_time")
decisionClose := metadataInt64("decision_close_time")
displayClose := signalClose
if isTradeActionMarker(d, status) && decisionClose > 0 {
    displayClose = decisionClose
}
```

返回 marker：

- `CloseTime = signalClose`
- `SignalCloseTime = signalClose`
- `DecisionCloseTime = decisionClose`
- `DisplayCloseTime = displayClose`
- `TradeIntent = metadata trade_intent || deriveTradeIntent(...)`

`markRejectedStrategyDecisions()` 匹配拒绝原因时优先用 `signal_id`，再回退 `symbol|action`，避免同一 symbol 同一 action 多信号时串原因。

### 5. 执行结果回写

`OnExecutionResult()` 继续用 `decisionToMarker()` 写入 executed/failed marker。由于 `signalMarkerKey()` 基于结构时间，执行结果会更新同一逻辑 marker 的状态和动作字段。

`FinalAction` 会参与 `TradeIntent` 推导：

- `open_long/open_short`：开多/开空
- `add_long/add_short`：加多/加空
- `partial_close + long`：减多
- `partial_close + short`：减空
- `close_long/close_short`：平多/平空
- `partial_close_skipped`：减仓跳过

### 6. marker 生命周期防降级

现有 `setLatestSignals()` 会在每次主信号检测后先写入 detected marker；如果同一个 `signal_id` 在后续新闭合 K 线重复被识别，而该信号此前已经 rejected/executed/failed，直接 upsert 会把已处理 marker 覆盖回 detected。实现时必须防止这种生命周期降级：

- 当 existing marker 已包含交易动作字段，或状态为 `rejected/executed/failed/deduped`，incoming marker 只是纯 `detected` 时，不得覆盖 existing 的 `status/action/final_action/trade_intent/reason/decision_close_time/display_close_time`。
- incoming detected marker 可以补齐 existing 缺失的 `signal_close_time`、`close_time`、`price` 等结构字段，但不得清空交易动作信息。
- incoming rejected/executed/failed marker 可以覆盖 detected marker，并补齐交易动作字段。
- 该逻辑可放在 `upsertSignalMarker()`、新增 `mergeSignalMarkerLifecycle()` helper，或 `setLatestSignals()` 写入前的状态检查中；优先选择集中在 `StateStore` 层，避免未来调用方重复犯错。

## Frontend Data Contract

同步更新：

- `web/src/types.ts`
- `web/src/types/index.ts`

`ChanlunSignal` 新增：

```ts
signal_close_time?: number;
decision_close_time?: number;
```

`SignalMarker` 新增：

```ts
signal_close_time?: number;
decision_close_time?: number;
display_close_time?: number;
```

`DecisionAction` 新增：

```ts
trade_intent?: string;
signal_close_time?: number;
decision_close_time?: number;
```

## Frontend Marker Model

在 `web/src/utils/strategyMarkers.ts` 新增视觉模型：

```ts
export type VisualMarkerKind = 'signal' | 'decision' | 'merged';

export type VisualSignalMarker = {
  id: string;
  marker: SignalMarker;
  kind: VisualMarkerKind;
  anchorCloseTime: number;
  pairCloseTime?: number;
  pairInRange?: boolean;
  pairOutOfRange?: boolean;
  label: string;
  placement: 'buy' | 'sell';
  stackKey: string;
};
```

核心 helper：

- `resolveSignalCloseTime(marker)`：`signal_close_time || close_time`
- `resolveDecisionCloseTime(marker)`：`decision_close_time`
- `resolveDisplayCloseTime(marker)`：`display_close_time || decision_close_time(action marker) || signal_close_time || close_time`
- `isActionMarker(marker)`：存在 `action/final_action/trade_intent` 或状态为 `rejected/executed/failed`
- `markerBelongsToKlineAt(anchorCloseTime, kline)`：时间归一化到毫秒后允许 `<=1000ms` 容差
- `expandVisualMarkers(markers, klines)`：从逻辑 marker 派生当前 K 线范围内可见的视觉 marker

`markerBelongsToKlineAt()` 只比较归一化后的 close time，不使用 `[open_time, close_time]` 宽范围匹配，避免把其它级别或其它收盘点的 marker 误挂到当前 K 线。

派生规则：

1. 纯检测 marker：只生成 `kind='signal'` 视觉 marker，锚定 `signal_close_time || close_time`。
2. 交易动作 marker 且 `signal_close_time != decision_close_time`：
   - 结构点在可视范围内：生成 `kind='signal'`，label 使用 `S2 · 结构点`。
   - 决策点在可视范围内：生成 `kind='decision'`，label 使用 `S2 · 开空 · 已拒绝` 或更紧凑等价文本。
3. 交易动作 marker 且两类时间相同：生成 `kind='merged'`，tooltip 同时展示结构时间和决策时间。
4. 只有一侧在可视范围内：显示可见侧，并在 tooltip 标注另一侧时间不在当前图表范围内。

## Chart Rendering

`StrategyCandlestickChart.tsx` 从直接渲染 `SignalMarker[]` 改为渲染 `VisualSignalMarker[]`：

1. `mainMarkers` 仍过滤 `source_layer === 'main_signal' && timeframe === selected timeframe`。
2. `visualMarkers = expandVisualMarkers(mainMarkers, klines)`。
3. 每根 K 线通过 `markerBelongsToKlineAt(visual.anchorCloseTime, kline)` 得到 `rowVisualMarkers`。
4. hover state 从 `SignalMarker[]` 改为 `VisualSignalMarker[]`，tooltip 展示：
   - 当前 K 线 close time
   - marker 类型：结构点 / 决策点 / 合并点
   - 买卖点分类：B1/B2/B3/S1/S2/S3
   - 交易意图：开多、开空、加多、加空、减多、减空、平多、平空
   - 状态：已检测、已拒绝、已执行、失败
   - 结构点时间、确认/决策时间
   - `signal_id`
   - 拒绝/执行原因

最新信号侧栏同步使用 `signal_close_time || trigger_close_time` 展示结构点时间，并在存在 `decision_close_time` 时展示确认/决策时间，避免侧栏仍只显示 legacy `trigger_close_time`。

### 分层规则

在同一 K 线内先按 `placement` 分组：

- `buy`：从 K 线 low 下方开始，按层级向下排列。
- `sell`：从 K 线 high 上方开始，按层级向上排列。

稳定排序键：

```text
placement -> action priority -> visual kind -> signal_type rank -> trade_intent -> signal_id
```

建议优先级：

- 交易动作点优先靠近 K 线，结构点排在更外层。
- `executed/rejected/failed` 高于 `detected`。
- `buy1/sell1`、`buy2/sell2`、`buy3/sell3` 按数字稳定排序。

相邻 K 线横向碰撞处理：

- 第一阶段采用紧凑 label 和固定 glyph 高度，避免文本撑破布局。
- 当相邻 marker 文本仍接近时，可按同侧层级增加 `x` 方向微偏移，偏移不超过半个 candle step。
- 完整信息始终放在 tooltip，图上 label 优先短而稳定。

### 配对连接

当同一逻辑 marker 的结构点和决策点都在当前可视范围内时，图上可绘制浅色虚线连接两点：

- 连接线仅用于说明“同一 signal_id 的结构点与确认点”。
- 连接线放在 glyph 下方图层，避免遮挡蜡烛和文字。
- 如果两点在同一 K 线合并显示，则不绘制连接线。

## Compatibility

- 旧 `programmatic_strategy_state.json` 中的 marker 缺少新字段时：
  - `signal_close_time` 回退 `close_time`。
  - `display_close_time` 回退 `close_time`。
  - `decision_close_time` 为空时 tooltip 显示“缺少确认时间”或不展示该行。
- 旧 `decision_logs` 缺少新字段时：
  - 最新决策列表继续显示 action、symbol、error/reasoning。
  - 策略 marker 图表不依赖旧日志生成时间锚点。
- 不需要运行状态迁移脚本，也不提交 `data/`、`decision_logs/`、`coin_pool_cache/`。
- API 路由不变，字段只做向后兼容扩展。

## Risks And Controls

- **风险：把结构点误显示成交易动作。** 控制：纯检测 marker 不填强制 `decision_close_time`，前端结构点 label 使用“结构点/信号点”语义。
- **风险：同一信号生成重复逻辑 marker。** 控制：后端状态仍只保存单条逻辑 marker，`signalMarkerKey()` 基于结构时间。
- **风险：前端时间单位不一致。** 控制：所有匹配通过 `normalizeEpochMs()` 归一化，并使用 `<=1000ms` close time 容差。
- **风险：同一 symbol/action 多个拒绝原因串联错误。** 控制：拒绝 reason 优先按 `signal_id` 匹配。
- **风险：标识过多互相遮挡。** 控制：同侧分层、稳定排序、紧凑 label、必要时微偏移，完整信息放 tooltip。
- **风险：交易执行语义被影响。** 控制：本设计只扩展 metadata、日志和 UI 展示，不修改下单、风控、仓位管理逻辑。

## Testing Plan

后端：

- `strategy/chanlun`：
  - 覆盖 `segment.EndTime < lastClosed` 时 `ChanlunSignal` 同时写入 `signal_close_time` 和 `decision_close_time`。
  - 覆盖 `signalToMainDecision()` 写入 `StrategyMetadata.signal_close_time`、`decision_close_time`、legacy `trigger_close_time`。
  - 覆盖 `decisionToMarker()` 对交易动作 marker 设置 `CloseTime=signal_close_time`、`DisplayCloseTime=decision_close_time`。
  - 覆盖 rejected marker 保留 `signal_id/signal_type/trade_intent/reason` 和两类时间。
- `decision`：
  - 覆盖 `NewOpenRejectionFromDecision()` 从程序化 `Decision` 复制策略元数据。
  - 覆盖 `buildOpenRejection()` 在 gate 诊断之外保留信号时间字段。
- `trader` 或现有相关测试：
  - 覆盖 `appendOpenRejectionsToRecord()` 将 rejection 元数据写入 `DecisionAction`。
- `logger`：
  - 跑现有日志统计和 replay 相关测试，确认新增字段不影响旧日志解析、open rejection 统计和成交复盘。

前端：

- `web/src/utils/strategyMarkers.test.ts`：
  - action marker 优先使用 `display_close_time/decision_close_time` 匹配 K 线。
  - pure detected marker 使用 `signal_close_time/close_time` 匹配 K 线。
  - `signal_close_time != decision_close_time` 派生结构点和决策点两个视觉 marker。
  - 只有一侧在可视范围内时仍显示可见侧，并标注 pair out of range。
  - `signal_close_time == decision_close_time` 合并为一个视觉 marker。
  - 同一 K 线多个卖点向上分层、多个买点向下分层的排序稳定。

验证命令：

```bash
go test ./strategy/chanlun ./decision ./trader ./logger ./api
cd web && npm run test
cd web && npm run build
```
