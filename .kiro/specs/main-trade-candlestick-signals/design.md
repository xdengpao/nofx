# 主交易 K 线蜡烛图与买卖点标记 Design

## Overview

本设计将策略检查区的主交易 K 线从表格升级为蜡烛图，并在图上展示程序化策略的主信号买卖点。改动分为三层：

1. 后端 K 线 API：当请求未显式传 `limit` 时，按 trader 的 `programmatic_strategy.history_depth` 和当前 timeframe 解析展示数量。
2. 后端信号 marker：为新生成的 marker 增加结构化交易意图字段，避免前端解析中文 reason。
3. 前端策略检查区：用自绘 SVG 蜡烛图替代表格，按主交易级别展示 K 线，并叠加 `B1/B2/B3/S1/S2/S3` 与交易意图标签。

用户确认的策略边界：

- 持仓管理信号不强制映射到主交易蜡烛图；仍在“最新信号/持仓管理摘要”中展示。
- 如果主交易频率配置为 `15m`，策略检查区必须展示 `15m` K 线，并使用 `history_depth["15m"]` 的数量。
- 继续按原需求执行，支持 `15m`、`1h`、`4h` 主交易级别。

## Architecture

```mermaid
flowchart TD
    Cfg[config.json programmatic_strategy] --> Profile[config.ProgrammaticStrategyProfile]
    Profile --> Policy[decision.ProgrammaticStrategyPolicy]
    Policy --> Engine[strategy/chanlun.Engine]
    Engine --> Signals[/api/strategy/signals]
    Engine --> Markers[signal_markers]

    UI[web StrategyInspector] --> Signals
    UI --> KlineAPI[/api/market/klines]
    KlineAPI --> LimitResolver[programmatic history_depth resolver]
    LimitResolver --> Market[market.GetKlines closed only]
    Market --> Chart[StrategyCandlestickChart]
    Markers --> Chart
```

## Design Principles

- **信号分类与交易意图分离**：`B1/S1` 等表示缠论信号分类，`开多/平空` 等表示交易动作含义。
- **后端提供结构化事实**：前端不得通过中文 reason 推断动作；优先读取 `trade_intent`、`final_action`、`position_side`。
- **配置驱动 K 线数量**：前端不再硬编码 `80` 或 `8`，默认不传 `limit`，由后端按配置解析。
- **保守映射**：主图只叠加 `source_layer=main_signal` 且 timeframe 等于主交易级别的 marker；持仓管理 marker 保留在摘要区。
- **兼容旧状态**：旧 marker 缺字段时前端 best effort 展示，不导致页面异常。

## Backend Design

### 1. Market Kline Limit Resolution

当前 `api.handleMarketKlines()` 使用 `DefaultQuery("limit", "240")`，这会让后端无法区分“用户显式传 240”和“前端未传 limit”。需要改为：

- `rawLimit := strings.TrimSpace(c.Query("limit"))`
- 如果 `rawLimit` 非空，按 query limit 解析。
- 如果 `rawLimit` 为空，尝试使用程序化策略配置深度。
- 如果 trader 非程序化或配置不可用，使用默认值 `240`。

新增后端解析语义：

```go
const defaultMarketKlineLimit = 240
const maxMarketKlineLimit = 1000

type MarketKlineLimitResolution struct {
    Limit           int
    ConfiguredLimit int
    LimitSource     string // query, programmatic_history_depth, default, query_capped, programmatic_history_depth_capped
}
```

实现位置：

- `trader/auto_trader.go`
  - 新增 `ResolveMarketKlineLimit(timeframe string, explicitLimit int) MarketKlineLimitResolution`
  - 新增私有 helper `programmaticHistoryDepthForTimeframe(timeframe string) (int, bool)`
- `manager/trader_manager.go`
  - 新增 `ResolveMarketKlineLimit(traderID, timeframe string, explicitLimit int) (trader.MarketKlineLimitResolution, error)`
- `api/server.go`
  - `handleMarketKlines()` 使用 resolver。
  - 响应新增 `configured_limit`、`limit_source`。
- 测试可控性
  - 将 `AutoTrader.GetMarketKlines()` 内部调用改为包级变量或等价可替换 fetcher，默认指向 `market.GetKlines`。
  - 提供测试可用的替换方式，确保 `/api/market/klines` 的配置数量测试不依赖真实 Binance 网络。

Timeframe 到配置字段映射：

| timeframe | history_depth 字段 |
|---|---|
| `3m` | `M3` |
| `15m` | `M15` |
| `1h` | `H1` |
| `4h` | `H4` |

API 响应示例：

```json
{
  "symbol": "ETHUSDT",
  "timeframe": "15m",
  "limit": 192,
  "configured_limit": 192,
  "limit_source": "programmatic_history_depth",
  "klines": []
}
```

### 2. SignalMarker Schema

当前 `strategy/chanlun.SignalMarker` 已包含 `action`，但执行结果回写时会把 `action` 设置为 `final_action`，导致原始动作与最终动作混在一起。需要扩展字段并收紧语义：

```go
type SignalMarker struct {
    Symbol       string  `json:"symbol"`
    Timeframe    string  `json:"timeframe"`
    CloseTime    int64   `json:"close_time"`
    SignalType   string  `json:"signal_type"`
    Direction    string  `json:"direction"`
    Level        string  `json:"level"`
    SourceLayer  string  `json:"source_layer"`
    Status       string  `json:"status"`
    SignalID     string  `json:"signal_id"`
    Action       string  `json:"action,omitempty"`
    FinalAction  string  `json:"final_action,omitempty"`
    TradeIntent  string  `json:"trade_intent,omitempty"`
    PositionSide string  `json:"position_side,omitempty"`
    Price        float64 `json:"price,omitempty"`
    Reason       string  `json:"reason,omitempty"`
}
```

字段语义：

- `action`：策略原始动作，例如 `partial_close`、`open_long`。
- `final_action`：执行层最终动作，例如 `close_long`、`partial_close_skipped`。
- `position_side`：目标持仓方向，仅用于持仓管理和 partial close 等动作，值为 `long` 或 `short`。
- `trade_intent`：机器稳定枚举，前端负责转换为中文。

`trade_intent` 枚举：

| effective action / side | trade_intent | 中文展示 |
|---|---|---|
| `open_long` | `open_long` | 开多 |
| `open_short` | `open_short` | 开空 |
| `add_long` | `add_long` | 加多 |
| `add_short` | `add_short` | 加空 |
| `close_long` | `close_long` | 平多 |
| `close_short` | `close_short` | 平空 |
| `partial_close` + `position_side=long` | `reduce_long` | 减多 |
| `partial_close` + `position_side=short` | `reduce_short` | 减空 |
| `partial_close_skipped` | `reduce_skipped` | 减仓跳过 |

有效动作优先级：

1. `final_action` 非空时优先使用。
2. 否则使用 `action`。
3. `partial_close` 还必须结合 `position_side`，不能只看 `direction`。

### 3. Marker Generation Updates

修改点：

- `strategy/chanlun/engine.go`
  - `signalToMarker()`：主信号初始 marker 只保留信号分类，不填交易意图，避免误判未执行信号。
  - `decisionToMarker()`：填充 `action`、`position_side`、`trade_intent`。
  - `OnExecutionResult()`：保留 `Action=d.Action`，新增 `FinalAction=result.FinalAction`，重新计算 `TradeIntent`。
  - `markRejectedStrategyDecisions()`：从只更新 status/reason，改为通过 `decisionToMarker(d, "rejected")` upsert marker，使 rejected marker 也保留 action/trade_intent。

新增 helper：

```go
func derivePositionSide(d decision.Decision, finalAction string) string
func deriveTradeIntent(action, finalAction, positionSide, direction string) string
func effectiveMarkerAction(action, finalAction string) string
```

兼容策略：

- 旧状态文件中的 marker 没有新增字段，JSON 反序列化自然为零值。
- 前端 helper 会用 `action/final_action/position_side/direction` best effort 推导，但不解析中文 reason。

## API Contract

### GET /api/market/klines

请求：

```http
GET /api/market/klines?trader_id=aster_deepseek&symbol=ETHUSDT&timeframe=15m
```

响应新增字段：

```ts
interface MarketKlineResponse {
  symbol: string;
  timeframe: string;
  limit: number;
  configured_limit?: number;
  limit_source?: 'query' | 'query_capped' | 'programmatic_history_depth' | 'programmatic_history_depth_capped' | 'default';
  klines: MarketKline[];
}
```

前端显式传 `limit` 时，后端尊重 query limit 并在超过上限时截断。

### GET /api/strategy/signals

`SignalMarker` 类型扩展：

```ts
interface SignalMarker {
  symbol: string;
  timeframe: string;
  close_time: number;
  signal_type: string;
  direction: string;
  level: string;
  source_layer: string;
  status: string;
  signal_id: string;
  action?: string;
  final_action?: string;
  trade_intent?: string;
  position_side?: 'long' | 'short' | string;
  price?: number;
  reason?: string;
}
```

## Frontend Design

### 1. API Client

`web/src/lib/api.ts`：

- `getMarketKlines(traderId, symbol, timeframe, limit?)`
- 只有 `limit` 是 number 且大于 0 时才写入 query。
- `TraderDetailsPage` 调用时不传 limit：

```ts
api.getMarketKlines(traderId, strategySymbol, strategyTradeTimeframe)
```

这样主交易 K 线数量由后端配置决定。

### 2. Types

同步更新：

- `web/src/types.ts`
- `web/src/types/index.ts`

新增字段：

- `SignalMarker.final_action`
- `SignalMarker.trade_intent`
- `SignalMarker.position_side`
- `MarketKlineResponse.configured_limit`
- `MarketKlineResponse.limit_source`

### 3. Candlestick Component

新增文件：

- `web/src/components/StrategyCandlestickChart.tsx`
- `web/src/utils/strategyMarkers.ts`
- `web/src/utils/strategyMarkers.test.ts`

不新增外部依赖。使用自绘 SVG，原因：

- 当前项目已有 Recharts，但 Recharts 没有直接的金融蜡烛图组件。
- 自绘 SVG 足够满足 OHLC、marker、tooltip、水平滚动和响应式。
- 避免引入新 chart 包造成包体和样式冲突。

Component props：

```ts
interface StrategyCandlestickChartProps {
  symbol: string;
  timeframe: string;
  klines: MarketKline[];
  markers: SignalMarker[];
  configuredLimit?: number;
  limitSource?: string;
}
```

渲染规则：

- 使用响应式容器，内部 SVG 最小宽度按 K 线数量计算。
- `candleWidth` 根据数量动态取值，至少保留可点击/hover 区域。
- Y 轴范围包含 K 线 high/low 和 marker price，增加上下 padding。
- 上涨蜡烛使用 `#0ECB81`，下跌蜡烛使用 `#F6465D`。
- marker 买点默认放在 low 下方，卖点默认放在 high 上方。
- marker 有 `price` 时优先按 `price` 定位。
- 同一根 K 线多个 marker 时使用垂直 offset 防止完全重叠。

### 4. Marker Mapping

`strategyMarkers.ts` 提供纯函数：

```ts
export function normalizeEpochMs(value?: number): number | undefined
export function signalLabel(signalType?: string): string
export function tradeIntentLabel(marker: SignalMarker): string
export function resolveTradeIntent(marker: SignalMarker): string | undefined
export function markerBelongsToKline(marker: SignalMarker, kline: MarketKline): boolean
```

时间兼容：

- 小于 `10_000_000_000` 视为秒，乘以 1000。
- 大于 `10_000_000_000_000` 视为微秒或纳秒，按现有后端规则归一到毫秒。
- 正常毫秒直接使用。

交易意图推导：

1. 优先使用 `marker.trade_intent`。
2. 否则用 `final_action || action`。
3. `partial_close` 使用 `position_side || direction` 判断减多/减空。
4. 不解析 `reason`。

### 5. StrategyInspector Layout

当前 `StrategyInspector` 是三列：最新信号、诊断、表格。改为：

- 桌面宽屏：左侧蜡烛图占更大空间，右侧为最新信号与诊断摘要。
- 窄屏：单列排列，蜡烛图在最新信号和诊断之后或之前均可，但不得遮挡。
- 不再渲染主 K 线表格。

建议结构：

```tsx
<StrategyInspector>
  <header />
  <div className="strategy-inspector-layout">
    <section className="chart-area">
      <StrategyCandlestickChart />
    </section>
    <aside className="signal-summary">
      <LatestSignal />
      <Diagnostics />
      <PositionManagementMarkers />
    </aside>
  </div>
</StrategyInspector>
```

持仓管理 marker：

- 默认不叠加到主交易蜡烛图。
- 在摘要区继续显示最近 3 条 `source_layer=position_management` marker。
- 如果未来需要叠加，可在新需求中定义时间映射规则。

## Visual Semantics

### Marker Text

- 信号分类：`B1`、`B2`、`B3`、`S1`、`S2`、`S3`
- 交易意图：`开多`、`开空`、`加多`、`加空`、`减多`、`减空`、`平多`、`平空`、`减仓跳过`
- 同时存在时展示：`B1 · 开多`、`S2 · 减多`

### Marker Color

- 买点/多头意图：绿色系。
- 卖点/空头意图：红色系。
- 已执行：增加黄色描边或高亮环。
- 被拒绝/失败/跳过：灰色或低饱和样式，不误导为有效成交。

### Tooltip

K 线 tooltip：

- symbol/timeframe
- 时间
- O/H/L/C
- Volume
- 同根 K 线上的 marker 列表

Marker tooltip：

- `signal_type`
- `trade_intent` 中文
- `status`
- `level/timeframe`
- `action/final_action`
- `reason`
- `signal_id`

## Compatibility

- 旧 `signal_markers` 缺少新增字段时，前端只展示 `B/S` 分类和 status。
- `/api/market/klines` 显式传 limit 的旧客户端继续按原语义工作。
- trader 非 programmatic 时，`/api/market/klines` 使用默认 limit，不要求 history depth。
- AI 决策模式下策略检查区继续显示“当前 trader 使用 AI 决策模式”，不请求图表数据。

## Risks and Mitigations

### Risk: K 线数量过大导致横向渲染拥挤

Mitigation:

- SVG 使用水平滚动容器。
- 后端保留 `maxMarketKlineLimit=1000` 安全上限。
- 图表根据数量动态调整 candle gap 和宽度。

### Risk: partial_close 被升级为全平但前端仍显示减仓

Mitigation:

- 后端新增 `final_action`。
- 前端推导交易意图时 `final_action` 优先。
- 增加纯函数测试覆盖 `partial_close -> close_long/close_short`。

### Risk: marker 与 K 线 close_time 单位不一致

Mitigation:

- 前端统一 `normalizeEpochMs()`。
- 测试覆盖秒、毫秒、微秒/纳秒边界。

### Risk: rejected marker 缺少 action 导致无法显示交易意图

Mitigation:

- 后端 `markRejectedStrategyDecisions()` 改为 upsert 完整 marker。
- 旧 marker 仍可 fallback 展示信号分类。

### Risk: 15m 主交易频率仍显示 1h

Mitigation:

- 前端以 `/api/strategy/signals.trade_timeframe` 作为唯一主交易 timeframe。
- `getMarketKlines()` 不传 limit，让后端按 `history_depth["15m"]` 返回。
- 测试或手工验证 trade=`15m` 时请求 URL timeframe 为 `15m`。

## Test Plan

### Backend

- `api/server_test.go`
  - 保留非法 timeframe 400 测试。
  - 使用可替换的 K 线 fetcher 增加 API 测试：programmatic trader、timeframe=`15m`、未传 query limit 时响应 `limit=history_depth.M15`、`limit_source=programmatic_history_depth`。
  - 使用可替换的 K 线 fetcher 增加 API 测试：显式 query limit 优先于配置深度。
- `strategy/chanlun/*_test.go`
  - 测试 `deriveTradeIntent()`：
    - `open_long -> open_long`
    - `open_short -> open_short`
    - `partial_close + long -> reduce_long`
    - `partial_close + short -> reduce_short`
    - `partial_close + final close_long -> close_long`
    - `partial_close_skipped -> reduce_skipped`
  - 测试 rejected marker 保留 `action/trade_intent`。

### Frontend

- `web/src/utils/strategyMarkers.test.ts`
  - `normalizeEpochMs()` 单位归一。
  - `markerBelongsToKline()` close_time 匹配。
  - `resolveTradeIntent()` final_action 优先和 partial_close position_side 映射。
- `cd web && npm run build`
  - 验证类型和生产构建。

### Manual / Deployment

- 本地或 161 验证：
  - programmatic trader 策略检查区显示蜡烛图。
  - trade=`1h` 时显示 1h K 线。
  - trade=`15m` 时显示 15m K 线。
  - `limit` 与 `/api/market/klines` 响应中的 `configured_limit` 一致。
  - `B1/B2/B3/S1/S2/S3` 和交易意图标签可读。
