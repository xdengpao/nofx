# 部分平仓历史成交优化 Design

## Overview

本设计把“完整闭合交易”和“成交事件”拆开处理：

- `recent_trades` 继续表示完整闭合交易，只服务现有胜率、profit factor、rolling gate 和开仓门控。
- 新增 `recent_trade_events` 表示前端历史成交展示用事件，包含完整平仓、自动平仓和部分平仓。
- `partial_close` 只扣减 open lot 的剩余数量，不结束整笔持仓生命周期。
- 对部分平仓优先使用交易所成交明细；无法对账时使用本地估算，并明确标记 `pnl_source` 和 `reconciliation_status`。

这样可以让 161 上已发生的 ETHUSDT 部分平仓出现在历史成交中，同时不污染完整交易统计。

## Architecture

```mermaid
flowchart TD
    A[decision_logs/{trader_id}/decision_*.json] --> B[logger.BuildTradeReplay]
    B --> C[Full Trade Outcomes]
    B --> D[Trade Events]
    C --> E[Performance Stats]
    D --> F[recent_trade_events]
    E --> G[/api/performance]
    F --> G
    G --> H[web AILearning 历史成交]

    I[trader.executePartialCloseWithRecord] --> J[DecisionAction fields]
    J --> A
    I --> K[Order ID parser]
    K --> L[GetOrderStatus/GetTradeHistory]
    L --> J
```

## Design Principles

- **统计隔离**：完整交易统计只消费 full close outcome；部分平仓只进入事件展示和独立指标。
- **数量守恒**：open/add 建立 lot，partial close 和 full close 消耗 lot，累计关闭数量不得超过可追踪数量。
- **兼容旧日志**：旧日志缺少 `close_quantity`、`final_action`、`order_id` 时仍能生成估算事件。
- **对账优先**：新日志尽量在执行层写入订单 ID 和成交信息；logger replay 使用日志内已有信息，不直接依赖交易所网络。
- **前端渐进增强**：旧字段继续可用，新字段缺失时页面仍展示现有完整交易。

## Backend Data Model

### logger.TradeOutcome 扩展

在 [logger/decision_logger.go](/Users/poper32/src/nofx/logger/decision_logger.go:424) 扩展 `TradeOutcome`，保持现有 JSON 字段不变，新增字段全部使用 `omitempty`。

```go
type TradeOutcome struct {
    Symbol        string    `json:"symbol"`
    Side          string    `json:"side"`
    Quantity      float64   `json:"quantity"`
    Leverage      int       `json:"leverage"`
    OpenPrice     float64   `json:"open_price"`
    ClosePrice    float64   `json:"close_price"`
    PositionValue float64   `json:"position_value"`
    MarginUsed    float64   `json:"margin_used"`
    PnL           float64   `json:"pn_l"`
    PnLPct        float64   `json:"pn_l_pct"`
    Duration      string    `json:"duration"`
    OpenTime      time.Time `json:"open_time"`
    CloseTime     time.Time `json:"close_time"`
    WasStopLoss   bool      `json:"was_stop_loss"`
    OpenReason    string    `json:"open_reason,omitempty"`
    CloseReason   string    `json:"close_reason,omitempty"`

    EventType                string  `json:"event_type,omitempty"` // full_close, partial_close, auto_close
    IsPartial                bool    `json:"is_partial,omitempty"`
    CloseQuantity            float64 `json:"close_quantity,omitempty"`
    RemainingQuantity        float64 `json:"remaining_quantity,omitempty"`
    RequestedClosePercentage float64 `json:"requested_close_percentage,omitempty"`
    ExecutedClosePercentage  float64 `json:"executed_close_percentage,omitempty"`
    OrderID                  int64   `json:"order_id,omitempty"`
    SignalID                 string  `json:"signal_id,omitempty"`
    StrategyName             string  `json:"strategy_name,omitempty"`
    StrategyVersion          string  `json:"strategy_version,omitempty"`
    PnLSource                string  `json:"pnl_source,omitempty"` // exchange, estimated
    Reconciled               *bool   `json:"reconciled,omitempty"`
    ReconciliationStatus     string  `json:"reconciliation_status,omitempty"`
    ReconciliationReason     string  `json:"reconciliation_reason,omitempty"`
}
```

`recent_trades` 里的完整交易也可带 `event_type=full_close`，但旧前端不依赖该字段。

### PerformanceAnalysis 扩展

在 `PerformanceAnalysis` 增加事件列表和独立统计：

```go
type PerformanceAnalysis struct {
    TotalTrades       int            `json:"total_trades"`
    RecentTrades      []TradeOutcome `json:"recent_trades"`
    RecentTradeEvents []TradeOutcome `json:"recent_trade_events,omitempty"`
    TradeEventStats   TradeEventStats `json:"trade_event_stats,omitempty"`
}

type TradeEventStats struct {
    TotalEvents               int     `json:"total_events"`
    FullCloseEvents           int     `json:"full_close_events"`
    AutoCloseEvents           int     `json:"auto_close_events"`
    PartialCloseEvents        int     `json:"partial_close_events"`
    PartialCloseRealizedPnL   float64 `json:"partial_close_realized_pnl"`
    PartialCloseEstimatedPnL  float64 `json:"partial_close_estimated_pnl"`
    PartialCloseReconciled    int     `json:"partial_close_reconciled"`
    PartialClosePending       int     `json:"partial_close_pending"`
}
```

`TotalTrades`、`WinningTrades`、`WinRate` 等字段继续只来自完整闭合交易。

## Replay And Lot Accounting

### New Builder

新增 replay 结构，统一生成完整交易和成交事件。结果类型使用导出名，便于测试和后续 `cmd/replay` 复用。

```go
type TradeReplayResult struct {
    FullOutcomes []TradeOutcome
    Events       []TradeOutcome
    Unmatched    []UnmatchedAction
}

func BuildTradeReplay(records []*DecisionRecord) TradeReplayResult
```

保留现有公开函数：

```go
func BuildTradeOutcomes(records []*DecisionRecord) ([]TradeOutcome, []UnmatchedAction) {
    result := BuildTradeReplay(records)
    return result.FullOutcomes, result.Unmatched
}

func BuildTradeEvents(records []*DecisionRecord) ([]TradeOutcome, []UnmatchedAction) {
    result := BuildTradeReplay(records)
    return result.Events, result.Unmatched
}
```

这样旧调用方不需要一次性全部改动。

### open lot

替换当前只保存原始 `quantity` 的 `openPositionTrace`，加入剩余数量：

```go
type openPositionTrace struct {
    symbol            string
    side              string
    price             float64
    time              time.Time
    quantity          float64
    remainingQuantity float64
    leverage          int
    reasoning         string
}
```

open/add 行为：

- `open_long/open_short/add_long/add_short` 新增 lot。
- `quantity <= 0` 的 open 不进入 lot，并记录 unmatched 或跳过诊断。

partial close 行为：

- 使用 `effectiveCloseQuantity(action)` 获取数量：优先 `CloseQuantity`，其次 `Quantity`。
- `quantity <= 0` 或 `final_action=partial_close_skipped` 不生成成交事件。
- 使用 `resolvePartialCloseSide()` 确定 long/short。
- 按 FIFO 消耗该 `symbol + side` 的 lot。
- 生成一条聚合的 `partial_close` 事件；如果跨多个 lot，`open_price` 使用被消耗数量的加权均价，`open_time` 使用最早被消耗 lot 时间。
- 不删除仍有剩余数量的 lot。

full close 行为：

- `close_long/close_short/auto_close_long/auto_close_short` 消耗该方向全部剩余 lot。
- 对每个剩余 lot 生成完整闭合交易 outcome，保持现有统计语义。
- 同时把这些 full outcome 追加到 `Events`，用于前端统一展示。

### Side Resolution For partial_close

`partial_close` action 本身没有 long/short 后缀，不能使用当前 [actionSide](/Users/poper32/src/nofx/logger/decision_logger.go:1208)。新增解析顺序：

1. `action.StrategyMetadata["side"]`，值为 `long` 或 `short`。
2. 当前 `DecisionRecord.Positions` 中同 symbol 的持仓 side。
3. 当前 open lot 中同 symbol 只存在一个方向时使用该方向。
4. 若仍无法确定，则写入 unmatched：`missing_side_for_partial_close`。

新版本执行层应在 `DecisionAction` 中显式写入 side 信息，至少写入 `strategy_metadata.side` 或新增 `position_side` 字段。为了避免扩大契约，首期优先复用已有 `strategy_metadata.side`。

### PnL Calculation

新增 helper：

```go
func buildTradeEventFromFragments(
    fragments []closedLotFragment,
    action DecisionAction,
    closeTime time.Time,
    closeReason string,
    eventType string,
) TradeOutcome
```

`closedLotFragment` 保存 lot entry、消耗数量和单段 PnL。

估算 PnL：

- long: `closedQty * (closePrice - openPrice)`
- short: `closedQty * (openPrice - closePrice)`
- `position_value = sum(closedQty * lotOpenPrice)`
- `margin_used = position_value / leverage`
- `pn_l_pct = pnl / margin_used * 100`

如果 `action.StrategyMetadata` 或后续字段提供交易所 `realized_pnl`、`commission` 和 `avg_price`，则事件使用交易所值，`pnl_source=exchange`；否则使用估算，`pnl_source=estimated`。

## Execution Layer Design

### Order ID Parsing

当前 [executePartialCloseWithRecord](/Users/poper32/src/nofx/trader/auto_trader.go:2379) 只处理 `order["orderId"].(int64)`，Aster 返回常见为 `float64`，导致 161 日志中 `order_id=0`。

在 [trader/auto_trader.go](/Users/poper32/src/nofx/trader/auto_trader.go) 新增：

```go
func parseOrderID(order map[string]interface{}) int64
```

支持：

- `int64`
- `int`
- `float64`
- `json.Number`
- `string`

并统一用于 open、full close、partial close 的 action record。

### Partial Close Execution Record

更新 `executePartialCloseWithRecord()`：

- 下单前记录 `RequestedClosePercentage`、计划 `CloseQuantity`、`position_quantity_before`。
- 下单成功后使用 `parseOrderID()` 写入 `OrderID`。
- 记录 `FinalAction=partial_close`。
- 记录 `CloseQuantity` 和 `ExecutedClosePercentage`。
- 自动修正为 full close 时，`FinalAction=close_long/close_short`，走完整平仓记录路径。
- 小额跳过时，`FinalAction=partial_close_skipped`，`CloseQuantity=0`，不进入成交事件。

### Reconciliation Metadata

首期不让 logger 在 API 请求时直接访问交易所。交易所对账尽量发生在执行层，写进 `DecisionAction.StrategyMetadata`，供 replay 离线消费。

新增执行层 helper：

```go
func (at *AutoTrader) enrichCloseFillMetadata(
    d *decision.Decision,
    actionRecord *logger.DecisionAction,
    orderID int64,
    fallbackQuantity float64,
    fallbackPrice float64,
)
```

行为：

- 有 `orderID > 0` 且交易所支持 `GetOrderStatus()` 时查询订单状态。
- 有 `GetTradeHistory()` 时按 `symbol` 和 action timestamp 附近窗口查询成交明细，并按 order ID 聚合。
- 成功匹配成交明细时写入 `strategy_metadata`：
  - `reconciled=true`
  - `reconciliation_status=matched`
  - `avg_fill_price`
  - `filled_quantity`
  - `realized_pnl`
  - `commission`
  - `fill_time`
- 无法匹配时写入：
  - `reconciled=false`
  - `reconciliation_status=pending|unsupported|failed`
  - `reconciliation_reason`

如果交易所接口可能带来运行周期阻塞，设计阶段可把查询限制为短 timeout；失败不影响交易主流程，只影响展示对账状态。

## API Design

### /api/performance

[api/server.go](/Users/poper32/src/nofx/api/server.go:519) 保持路由不变。

[logger.DecisionLogger.AnalyzePerformance](/Users/poper32/src/nofx/logger/decision_logger.go:1250) 改为：

1. 读取 `allRecords`。
2. 调用 `BuildTradeReplay(allRecords)`。
3. 使用 `FullOutcomes` 填充现有绩效字段和 `RecentTrades`。
4. 使用 `Events` 按窗口过滤后填充 `RecentTradeEvents`。
5. 使用 `Events` 构建 `TradeEventStats`。
6. `ExecutionQuality` 继续使用原有 `BuildExecutionQuality()`。

排序建议：

- `RecentTrades` 保持现有行为，避免前端突然变化。
- `RecentTradeEvents` 按 `close_time` 降序，前端历史成交优先展示最新事件。

兼容策略：

- 若 `recent_trade_events` 为空，前端使用 `recent_trades`。
- 旧客户端忽略新增字段，不受影响。

## Frontend Design

### Types

更新实际被 `../types` 导入解析优先命中的 [web/src/types.ts](/Users/poper32/src/nofx/web/src/types.ts)、镜像类型文件 [web/src/types/index.ts](/Users/poper32/src/nofx/web/src/types/index.ts)，以及 [web/src/components/AILearning.tsx](/Users/poper32/src/nofx/web/src/components/AILearning.tsx:10) 中的本地类型定义。

新增可选字段：

```ts
interface TradeOutcome {
  event_type?: 'full_close' | 'partial_close' | 'auto_close' | string;
  is_partial?: boolean;
  close_quantity?: number;
  remaining_quantity?: number;
  requested_close_percentage?: number;
  executed_close_percentage?: number;
  order_id?: number;
  signal_id?: string;
  strategy_name?: string;
  strategy_version?: string;
  pnl_source?: 'exchange' | 'estimated' | string;
  reconciled?: boolean;
  reconciliation_status?: string;
  reconciliation_reason?: string;
  open_reason?: string;
  close_reason?: string;
}
```

`PerformanceAnalysis` 增加：

```ts
recent_trade_events?: TradeOutcome[];
trade_event_stats?: TradeEventStats;
```

### AILearning Table

[web/src/components/AILearning.tsx](/Users/poper32/src/nofx/web/src/components/AILearning.tsx:600) 目前只展示 `performance.recent_trades`。

改为：

```ts
const allTrades = performance?.recent_trade_events?.length
  ? performance.recent_trade_events
  : performance?.recent_trades || [];
```

表格增加列：

- 类型：完整平仓、部分平仓、自动平仓
- 对账：真实成交、估算、待对账
- 原因：优先 `close_reason`，fallback `was_stop_loss ? 'Stop Loss' : ...`

视觉规则：

- `partial_close` 使用中性或黄色标签，避免和 long/short 方向颜色混淆。
- `pnl_source=estimated` 或 `reconciled=false` 显示“估算/待对账”。
- 旧记录缺字段时显示 `-`。

### CSV

更新 [web/src/utils/exportCSV.ts](/Users/poper32/src/nofx/web/src/utils/exportCSV.ts)：

新增列：

- Event Type
- Is Partial
- Close Quantity
- Requested Close %
- Executed Close %
- Order ID
- Signal ID
- PnL Source
- Reconciled
- Close Reason

旧测试需要同步更新表头列数。

## Reconciliation Limits

交易所成交对账是 best effort：

- Aster/Binance 已有 `GetOrderStatus()` 和 `GetTradeHistory()` 接口，优先复用。
- 当前 Binance Futures 与 Hyperliquid 的 `GetOrderStatus()` / `GetTradeHistory()` / `GetOrderHistory()` 实现存在 TODO panic 风险；在接入对账前必须改为返回 `unsupported` error，避免展示增强影响交易主循环。
- Aster 已有只读订单/成交历史实现，首期真实对账优先覆盖 Aster。
- Hyperliquid 当前实现可能返回空历史或不支持订单 ID，应标记 `unsupported`。
- API 性能分析不在请求路径实时访问交易所，避免页面刷新触发网络对账和阻塞。
- 如果新版本上线前的旧日志 `order_id=0`，只能按日志估算；不要伪造交易所真实 PnL。

## Replay Report

[logger/replay.go](/Users/poper32/src/nofx/logger/replay.go:234) 的 `BuildReplayReport()` 当前只通过 `BuildTradeOutcomes()` 读取完整闭合交易。实现时需要同时调用 `BuildTradeReplay()`：

- `TradeCount`、rolling 和 disease 相关统计继续使用 `FullOutcomes`。
- 新增或复用 `TradeEventStats` 字段展示成交事件数量、partial close 数量、估算/真实 PnL 和待对账数量。
- `buildLogCloseSnapshots()` 和交易所对账保持完整平仓语义；如果后续要把 partial close 纳入交易所对账，应通过单独字段展示，避免把减仓事件误报为完整缺失平仓。

## Migration And Compatibility

- 不需要清理 `decision_logs` 或 `data/`。
- 旧日志的 `partial_close`：
  - `quantity > 0` 生成 `partial_close` 估算事件。
  - `quantity == 0` 不生成成交事件。
  - `order_id == 0` 标记 `reconciliation_status=estimated_from_decision_log`。
- 新日志逐步写入 `close_quantity`、`final_action`、`executed_close_percentage`、`order_id` 和对账 metadata。
- `recent_trades` 继续存在并只包含完整交易。

## Test Strategy

Backend:

- `logger`: open -> partial_close -> close，验证事件数量、完整交易数量和数量守恒。
- `logger`: partial close 跨多个 open/add lot，验证加权 entry price 和 PnL。
- `logger`: skipped 或 quantity=0 不生成事件。
- `logger`: missing side / missing open 生成 unmatched。
- `trader`: `parseOrderID()` 覆盖 int64、int、float64、json.Number、string、非法类型。
- `api`: `/api/performance` 包含 `recent_trade_events`，且 `recent_trades` 仍只含完整交易。

Frontend:

- `exportCSV` 更新表头和数据行测试。
- `npm run build` 验证类型和渲染兼容。

Validation commands:

```bash
go test ./logger ./trader ./api
go build ./...
cd web && npm run build
```

If frontend-only test utilities are touched:

```bash
cd web && npm run test
```
