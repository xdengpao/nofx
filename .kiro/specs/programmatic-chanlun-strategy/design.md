# 程序化缠论交易策略 Design

## Overview

本设计为 NOFX 增加可配置的程序化缠论策略模式。核心目标是：当某个 trader 配置 `decision_mode=programmatic` 时，不再调用 AI 产生交易动作，而由确定性策略引擎输出开仓、加仓、减仓和平仓决策；现有强制风控、交易计划、保护单同步、下单前检查、交易所执行和决策日志继续作为公共层保留。

程序化策略不旁路下单，不直接调用交易所。它只生成标准 `decision.Decision`，再进入现有执行链路。为避免把缠论概念停留在描述层，策略核心采用 `swing-pivot + 包含关系处理 + 分型 + 笔 + 线段` 的机器化结构识别，并在 `1h/15m/4h/3m` 多级别行情上计算买卖点、背驰、结构止损和结构止盈目标。

## Design Principles

1. **公共风控优先**：熔断、账户回撤硬停、交易计划失效、保护单同步、open gate、仓位 sizing、最小名义额和 preflight 可以覆盖策略输出。
2. **策略只产出决策，不执行订单**：程序化策略返回 `decision.Decision` 和诊断信息，实际执行仍由 `trader.AutoTrader` 负责。
3. **AI 兼容默认不变**：未配置 `decision_mode` 时保持现有 AI 行为；AI 模式不受程序化策略状态文件影响。
4. **结构可测试**：所有缠论结构必须落到分型、笔、线段、中枢、A/B/C 段和信号对象，避免不可复现的文字判断。
5. **状态可重算也可恢复**：每周期从 K 线重算近期结构，同时持久化已确认信号、加仓次数、短差状态和 `signal_id` 去重信息。
6. **前后端契约同步**：新增策略诊断、K 线和信号 API 时，同步 Go JSON tag、`web/src/lib/api.ts` 和 `web/src/types/index.ts`。
7. **运行时数据隔离**：`data/programmatic_strategy_state.json` 属于运行时状态，不作为普通功能改动提交。

## Architecture

```mermaid
flowchart TD
    Config[config.json traders[].decision_mode / programmatic_strategy] --> Normalize[config NormalizeProgrammaticStrategy]
    Normalize --> Manager[manager.TraderManager]
    Manager --> AT[trader.AutoTrader]

    AT --> Context[buildTradingContext]
    Context --> Prep[decision.PrepareCycleContext]
    Prep --> Market[market.GetWithHistory]
    Prep --> PublicRisk[熔断 / 账户硬停 / 交易计划 / 持仓评估]

    AT --> Mode{decision_mode}
    Mode -->|ai| AI[decision.GetFullDecision AI path]
    Mode -->|programmatic| Engine[strategy/chanlun.Engine]

    PublicRisk --> Engine
    Market --> Engine
    Engine --> Structure[包含关系 / 分型 / 笔 / 线段 / 中枢]
    Structure --> Signals[一二三类买卖点 / 背驰 / 短差]
    Signals --> StrategyDecisions[open/add/partial/close/update]

    StrategyDecisions --> CommonValidate[decision.ValidateStrategyDecisions]
    CommonValidate --> Merge[decision.MergePublicAndStrategyDecisions]
    AI --> Merge
    Merge --> Priority[sortDecisionsByPriority]
    Priority --> Execute[executeDecisionWithRecord]
    Execute --> Exchange[trader.Trader implementations]
    Execute --> Protect[SL/TP protection]
    Execute --> Plan[TradePlan updates]
    Engine --> State[data/programmatic_strategy_state.json]
    Engine --> Logs[decision_logs strategy diagnostics]
    Logs --> API[/api/strategy/*]
    API --> Web[Strategy Inspector]
```

## Component Design

### 1. Configuration

#### Files

- `config/config.go`
- `config/config_test.go`
- `config.json.example`
- `manager/trader_manager.go`
- `trader/auto_trader.go`
- `decision/types.go`

#### Config Shape

`TraderConfig` 新增 trader 级配置：

```go
type TraderConfig struct {
    // existing fields...
    DecisionMode          string                     `json:"decision_mode,omitempty"` // ai, programmatic
    ProgrammaticStrategy  ProgrammaticStrategyConfig `json:"programmatic_strategy,omitempty"`
}
```

新增配置结构：

```go
type ProgrammaticStrategyConfig struct {
    StrategyName      string                       `json:"strategy_name,omitempty"`
    StrategyVersion   string                       `json:"strategy_version,omitempty"`
    AllowLong         *bool                        `json:"allow_long,omitempty"`
    AllowShort        *bool                        `json:"allow_short,omitempty"`
    EnabledSignals    []string                     `json:"enabled_signals,omitempty"`
    Timeframes        ProgrammaticTimeframesConfig `json:"timeframes,omitempty"`
    HistoryDepth      ProgrammaticHistoryDepth     `json:"history_depth,omitempty"`
    SymbolPool        ProgrammaticSymbolPoolConfig `json:"symbol_pool,omitempty"`
    MovingAverage     ProgrammaticMAConfig         `json:"moving_average,omitempty"`
    Structure         ProgrammaticStructureConfig  `json:"structure,omitempty"`
    Divergence        ProgrammaticDivergenceConfig `json:"divergence,omitempty"`
    ADX               ProgrammaticADXConfig        `json:"adx,omitempty"`
    Position          ProgrammaticPositionConfig   `json:"position,omitempty"`
    TakeProfit        ProgrammaticTPConfig         `json:"take_profit,omitempty"`
    State             ProgrammaticStateConfig      `json:"state,omitempty"`
}
```

关键默认值：

| Field | Default |
| --- | --- |
| `decision_mode` | `ai` |
| `strategy_name` | `chanlun_programmatic` |
| `strategy_version` | 内置常量，例如 `v1` |
| `allow_long` / `allow_short` | `true` / `true` |
| `timeframes.higher/trade/sub/micro` | `4h` / `1h` / `15m` / `3m` |
| `history_depth` | `3m=240`, `15m=192`, `1h=240`, `4h=180` |
| `moving_average.short_period/long_period` | `20` / `50` |
| `structure.strictness` | `enhanced`，即优先使用笔/线段增强结果 |
| `divergence.ratio` | `0.8` |
| `divergence.price_tolerance_pct` | `0.1` |
| `divergence.price_tolerance_atr_multiplier` | `0.2` |
| `adx.period` | `14` |
| `adx.micro_adx_filter` | `false` |
| `position.max_add_count` | `2` |
| `position.partial_close_pct` | `30` |
| `take_profit.mode` | `structure` |
| `take_profit.fallback_mode` | `reject` |
| `state.path` | `data/programmatic_strategy_state.json` |

配置归一化：

```go
func (c *Config) NormalizeProgrammaticStrategies() (map[string]ProgrammaticStrategyProfile, error)
```

输出按 `trader_id` 索引的 profile，并在 `manager.AddTraderWithPolicies` 注入 `trader.AutoTraderConfig`：

```go
type AutoTraderConfig struct {
    // existing fields...
    DecisionMode               string
    ProgrammaticStrategyPolicy decision.ProgrammaticStrategyPolicy
}
```

`decision.ProgrammaticStrategyPolicy` 使用运行时友好的字段，不直接依赖 `config` 包，避免策略层和配置层耦合。

`main.go/setupTraderManager` 在创建 trader 前必须调用 `cfg.NormalizeProgrammaticStrategies()`，并把对应 `trader_id` 的 profile 传入 `manager.AddTraderWithPolicies`。`manager.AddTrader` 和 `AddTraderWithFrequency` 保持测试/旧调用兼容，内部使用 AI 缺省 profile。

程序化 trader 不应要求 AI key 可用，也不应在启动日志里声明正在使用某个 AI provider。`trader.NewAutoTrader` 根据 `DecisionMode` 初始化：

- `ai`：保持当前 `mcp.Client` 初始化和 provider 日志。
- `programmatic`：初始化 programmatic engine，`mcp.Client` 可以为 nil 或空客户端，但不得用于策略决策。

#### Validation Rules

- `decision_mode` 只允许空值、`ai`、`programmatic`。
- `decision_mode=programmatic` 时，`ai_model` 可为空或保留原配置但不参与策略运行；AI provider/API key 不作为策略运行前置条件；交易所凭证仍按 exchange 校验。
- `programmatic_strategy` 时间级别只允许 `3m`、`15m`、`1h`、`4h`。
- `history_depth` 必须满足 ADX/DI 最低 `period * 2`，且不得低于结构识别最低窗口。
- `moving_average.short_period < moving_average.long_period`。
- `symbol_pool.mode` 只允许 `append`、`override`、`filter`。
- `take_profit.fallback_mode` 只允许 `reject` 或 `rr_target`。
- `adx.micro_adx_filter=true` 时，设计要求 `market.IntradayData` 支持 `ADXValues/DIPlus/DIMinus`；实现未完成时配置校验失败。

### 2. Decision Mode Routing

#### Files

- `trader/auto_trader.go`
- `decision/decision.go`
- `decision/types.go`
- `strategy/chanlun/engine.go`

当前 `AutoTrader.runCycle()` 固定调用 `decision.GetFullDecision(ctx, at.mcpClient)`，日志也固定称为 AI 周期。改为 mode-aware 路由：

```go
switch at.config.DecisionMode {
case "", "ai":
    fullDecision, err = decision.GetFullDecision(ctx, at.mcpClient)
case "programmatic":
    fullDecision, err = at.programmaticEngine.GetFullDecision(ctx)
}
```

周期日志、错误信息和 record 字段使用 `decision_mode` 区分，例如“程序化策略周期 #N”，避免 programmatic trader 的运行日志误导为 AI 调用。

`AutoTrader.runCycle()` 中的 `syncAutoClosedOrders()`、`detectAutoClosedPositions()` 和 `reconcileStaleTradePlans()` 仍保留在 mode routing 之前，作为所有模式共享的执行前同步层；`applyAICallState()` 仅在 AI 模式且 `AICallAttempted=true` 时更新 AI backoff 状态。

为避免复制 `decision.GetFullDecision()` 中的公共逻辑，将公共前置层抽出：

```go
type CyclePreparationOptions struct {
    MarketSymbols      []string
    MarketHistoryDepth map[string]int
    ClosedKlinesOnly   bool
    IncludeMicroADX    bool
}

type CyclePreparation struct {
    PositionDecisions []Decision
    WaitDecision      *Decision
    StopReason        string
}

func PrepareCycleContext(ctx *Context, opts CyclePreparationOptions) (*CyclePreparation, error)
```

`PrepareCycleContext` 负责：

- `initializeDefaults`
- 冷却中的熔断检查
- 按 `opts.MarketSymbols`、当前持仓和 `BTCUSDT` 拉取行情数据；未传入 `MarketSymbols` 时兼容使用 `ctx.CandidateCoins`
- 熔断触发检查
- 相关性矩阵
- 候选币质量标记
- 现有交易计划、止盈止损、移动止损、失效条件、保护单同步决策
- 账户回撤硬停 wait 决策

AI 模式继续在 `decision.GetFullDecision` 中调用 AI；程序化模式调用 `strategy/chanlun.Engine` 产生策略决策，然后通过公共校验与公共层决策合并。

### 3. Market Data

#### Files

- `market/data.go`
- `market/data_test.go`

新增可配置历史深度接口：

```go
type HistoryDepth struct {
    M3  int
    M15 int
    H1  int
    H4  int
}

type HistoryOptions struct {
    Depth           HistoryDepth
    ClosedOnly      bool
    IncludeMicroADX bool
}

func GetWithHistory(symbol string, opts HistoryOptions) (*Data, error)
func GetKlines(symbol, timeframe string, limit int, closedOnly bool) ([]Kline, error)
```

`market.Get()` 保持现有默认行为和缓存，用于 AI 模式兼容。`GetWithHistory()` 供程序化策略和 K 线 API 使用：

- 按 `history_depth` 拉取 `3m/15m/1h/4h`。
- 过滤未闭合 K 线。
- 生成完整长度序列，而不是只保留最近 10 个点。
- 为 `15m/1h/4h` 继续计算 ADX/DI。
- 当 `IncludeMicroADX=true` 时，为 `IntradayData` 计算 `ADXValues/DIPlus/DIMinus`。

`Data` 结构建议新增：

```go
type Data struct {
    // existing fields...
    Klines map[string][]Kline `json:"-"`
}

type IntradayData struct {
    // existing fields...
    ADXValues []float64
    DIPlus    []float64
    DIMinus   []float64
}
```

`Klines` 只在后端内部使用；API 返回时使用专门 DTO，避免把完整市场数据结构直接暴露给前端。

闭合 K 线过滤：

```go
func FilterClosedKlines(klines []Kline, now time.Time) []Kline {
    // keep k.CloseTime <= now.UnixMilli()
}
```

### 4. Symbol Pool Resolution

#### Files

- `trader/auto_trader.go`
- `pool/dynamic_candidate_pool.go`
- `decision/types.go`
- `strategy/chanlun/symbols.go`

`buildTradingContext()` 继续构建全局动态/静态候选池。程序化策略再按 trader 级 `programmatic_strategy.symbol_pool` 得到最终分析池：

```go
type StrategySymbol struct {
    Symbol  string
    Sources []string
}

func ResolveProgrammaticSymbols(
    candidates []decision.CandidateCoin,
    positions []decision.PositionInfo,
    policy decision.ProgrammaticStrategyPolicy,
) []StrategySymbol
```

规则：

- `append`：动态/静态池 + 自定义池 + 当前持仓。
- `override`：自定义池 + 当前持仓 + 配置允许的核心标的。
- `filter`：动态/静态池 ∩ 自定义池 + 当前持仓。
- 当前持仓永远保留。
- 每个 symbol 的来源写入 `CandidateCoin.Sources` 和日志 `CandidateDetails`。

### 5. Chanlun Engine

#### Package

新增包：

```text
strategy/chanlun/
├── engine.go
├── types.go
├── structure.go
├── divergence.go
├── signals.go
├── sizing.go
├── state.go
└── *_test.go
```

#### Engine API

```go
type Engine struct {
    Policy     decision.ProgrammaticStrategyPolicy
    StateStore *StateStore
    Clock      func() time.Time
}

func NewEngine(policy decision.ProgrammaticStrategyPolicy) (*Engine, error)
func (e *Engine) GetFullDecision(ctx *decision.Context) (*decision.FullDecision, error)
func (e *Engine) LatestSignals(traderID, symbol string) (*SignalReport, bool)
func (e *Engine) SymbolUniverse(traderID string) []StrategySymbol
```

`GetFullDecision` 流程：

1. 先基于 `ctx.CandidateCoins`、当前持仓和 trader 级 `symbol_pool` 解析最终 symbol universe。
2. 调用 `decision.PrepareCycleContext(ctx, opts)`，并通过 `opts.MarketSymbols` 传入最终 symbol universe；当前持仓和 `BTCUSDT` 仍强制保留。
3. 若公共层返回熔断/账户硬停 wait，则直接返回公共决策。
4. 对每个 symbol 构建多级别结构。
5. 生成策略信号和诊断。
6. 对每个信号生成 `decision.Decision`。
7. 调用 `decision.ValidateStrategyDecisions` 做公共风控校验。
8. 调用 `decision.MergePublicAndStrategyDecisions` 合并公共持仓决策和策略决策。
9. 写入策略状态和 latest signal cache。
10. 返回 `decision.FullDecision`，其中 `UserPrompt` 为空，`AICallAttempted=false`。

#### Core Types

```go
type Candle struct {
    Timeframe string
    OpenTime  int64
    CloseTime int64
    Open      float64
    High      float64
    Low       float64
    Close     float64
    Volume    float64
}

type Fractal struct {
    Type      string // top, bottom
    Index     int
    Price     float64
    OpenTime  int64
    CloseTime int64
}

type Stroke struct {
    ID        string
    Direction string // up, down
    Start     Fractal
    End       Fractal
    High      float64
    Low       float64
    ATRRatio  float64
}

type Segment struct {
    ID        string
    Direction string
    StartTime int64
    EndTime   int64
    Start     float64
    End       float64
    High      float64
    Low       float64
    Strokes   []Stroke
}

type Center struct {
    ID        string
    Timeframe string
    ZG        float64
    ZD        float64
    High      float64
    Low       float64
    Segments  []Segment
}

type ChanlunSignal struct {
    SignalID        string
    Symbol          string
    Direction       string // long, short
    SignalType      string // buy1,buy2,buy3,sell1,sell2,sell3
    ActionHint      string // open, add, reduce, close
    AnalysisTF      string
    TriggerTF       string
    Level           string
    Price           float64
    StopLoss        float64
    TakeProfit      float64
    StructureTarget float64
    CenterID        string
    Confidence      int
    Diagnostics     SignalDiagnostics
}
```

### 6. Structure Algorithm

#### Inclusion, Fractal, Stroke, Segment

每个 timeframe 独立处理：

1. `NormalizeInclusion(candles, directionBias)` 处理包含关系。
2. `FindFractals(candles, leftBars, rightBars)` 识别顶/底分型。
3. `BuildStrokes(fractals, minBars, minSwingPct, atrMultiplier)` 生成笔。
4. `BuildSegments(strokes, swingPivots, strictness)` 生成线段。
5. `BuildCenters(segments)` 用三个连续次级别走势重叠识别中枢。

默认级别组合：

| Target | Component |
| --- | --- |
| `1h` 主级别中枢 | `15m` segments |
| `15m` 次级别中枢 | `3m` segments |
| `4h` 大级别背景 | `1h` segments |

冲突处理：

- `strictness=enhanced`：笔/线段增强结果优先；swing-pivot 只作 fallback/诊断。
- `strictness=pivot`：只使用 swing-pivot。
- `strictness=confirm_both`：两者方向一致才确认信号。

### 7. Signal Algorithm

#### MACD Divergence

`divergence.go` 在 A+B+C 结构上计算：

- 上涨段只累加正 histogram。
- 下跌段只累加负 histogram 的绝对值。
- 默认 `C_area <= A_area * 0.8`。
- 价格创新高/低容差为 `max(0.1%, 0.2 * ATR / price)`。
- 严格模式要求 B 段 MACD 黄白线回到 0 轴附近。

#### Moving Average Kiss

用配置 EMA 周期，默认 EMA20/EMA50：

- 男上位：短 EMA < 长 EMA。
- 女上位：短 EMA > 长 EMA。
- 飞吻：距离缩小后未触达阈值又扩张。
- 唇吻：距离进入 `kiss_distance_pct` 但未交叉。
- 湿吻：N 根 K 线内发生交叉，默认 N=5。
- 最后一吻必须发生在背驰段之前或背驰段早期。

#### Buy/Sell Points

1. 一买：下跌 + 中枢/盘整 + 下跌，C 段底背驰，且方向/均线吻满足配置。
2. 一卖：上涨 + 中枢/盘整 + 上涨，C 段顶背驰。
3. 二买：一买后首次上 0 轴再回抽，回抽低点不破一买低点。
4. 二卖：一卖后首次下 0 轴再反抽，反抽高点不破一卖高点。
5. 三买：次级别向上离开中枢后回试，低点不破 ZG。
6. 三卖：次级别向下离开中枢后回抽，高点不破 ZD。

失败条件：

- 三买失败：`15m close < ZG` 或连续两根 `3m close < ZG`。
- 三卖失败：`15m close > ZD` 或连续两根 `3m close > ZD`。
- 止损可以用盘中价格触发；结构失败用闭合 K 线确认。

### 8. Decision Generation

#### Decision Metadata

`decision.Decision` 增加策略诊断字段：

```go
type Decision struct {
    // existing fields...
    StrategyMode        string         `json:"strategy_mode,omitempty"`
    StrategyName        string         `json:"strategy_name,omitempty"`
    StrategyVersion     string         `json:"strategy_version,omitempty"`
    ConfigHash          string         `json:"config_hash,omitempty"`
    SignalID            string         `json:"signal_id,omitempty"`
    SignalType          string         `json:"signal_type,omitempty"`
    SignalLevel         string         `json:"signal_level,omitempty"`
    AnalysisTimeframe   string         `json:"analysis_timeframe,omitempty"`
    TriggerTimeframe    string         `json:"trigger_timeframe,omitempty"`
    StructureTarget     float64        `json:"structure_target,omitempty"`
    StrategyDiagnostics map[string]any `json:"strategy_diagnostics,omitempty"`
}
```

Action mapping：

| Signal | No same-side position | Same-side position | Opposite position |
| --- | --- | --- | --- |
| buy1/buy2/buy3 | `open_long` | `add_long` if allowed | close/reduce opposite only |
| sell1/sell2/sell3 | `open_short` if allowed | `add_short` if allowed | close/reduce opposite only |
| sell signal with long | `partial_close` or `close_long` | N/A | N/A |
| buy signal with short | `partial_close` or `close_short` | N/A | N/A |

结构止损/止盈：

- 多头止损：一买低点、二买回抽低点、三买 ZG/回试低点。
- 空头止损：一卖高点、二卖反抽高点、三卖 ZD/回抽高点。
- 多头 TP：前一中枢上沿、最近 swing high、离开段高点或下一结构压力位。
- 空头 TP：前一中枢下沿、最近 swing low、离开段低点或下一结构支撑位。
- 默认结构 TP 不满足最小净 RR 时拒绝；`tp_fallback_mode=rr_target` 才允许用 RR 目标兜底。

### 9. Common Validation And Merge

#### Files

- `decision/decision.go`
- `decision/open_gate.go`
- `decision/position_sizing.go`
- `decision/utils.go`

新增 helper：

```go
func IsOpenAction(action string) bool        // open_long/open_short
func IsAddAction(action string) bool         // add_long/add_short
func IsOpenLikeAction(action string) bool    // open/add
func DecisionDirection(action string) string // long/short
```

新增公共校验入口：

```go
type StrategyValidationOptions struct {
    Source             string // ai, programmatic
    AllowAdd           bool
    AllowTPRRFallback  bool
    PreserveStructureTP bool
}

func ValidateStrategyDecisions(ctx *Context, decisions []Decision, opts StrategyValidationOptions) ([]Decision, []OpenRejection)
func MergePublicAndStrategyDecisions(publicDecisions, strategyDecisions []Decision) []Decision
```

`ValidateStrategyDecisions` 复用当前 `ValidateAndEnrichDecision`、`validateOpenDecision`、`EvaluateOpenGate`、`CalculatePositionSizing`、`enforceFinalDecisionLimits`。对 `add_long/add_short`：

- 视为 open-like 风险动作。
- 方向、止损、止盈、RR、ADX/DI、相关性、亏损模式、总风险预算继续生效。
- 跳过“已有同 symbol 同方向持仓拒绝”，改用加仓专用限制。
- 反向持仓默认拒绝。

合并优先级：

1. 自动平仓/熔断/账户硬停。
2. 交易计划硬止损/失效。
3. 程序化平仓/减仓。
4. 止损/止盈更新。
5. 程序化加仓。
6. 程序化开仓。
7. `wait` / `hold`。

### 10. Execution Changes

#### Files

- `trader/auto_trader.go`
- `trader/execution_preflight.go`
- `decision/persistence.go`
- `decision/types.go`
- `trader/trader_test.go`

`executeDecisionWithRecord` 新增：

```go
case "add_long":
    return at.executeAddLongWithRecord(decision, actionRecord)
case "add_short":
    return at.executeAddShortWithRecord(decision, actionRecord)
```

开仓和加仓共享底层函数：

```go
func (at *AutoTrader) executeOpenLikeWithRecord(d *decision.Decision, side string, intent string, actionRecord *logger.DecisionAction) error
```

`intent=open`：

- 维持当前同 symbol 同方向拒绝。

`intent=add`：

- 要求已有同 symbol 同方向持仓。
- 禁止已有同 symbol 反向持仓。
- `ExecutionPreflightInput` 增加 `Intent` 或 `AllowSameSidePosition` 语义，避免沿用当前 preflight 的同向持仓拒绝。
- 跳过普通同向拒绝。
- 复用 `OpenLong` / `OpenShort` 下单。
- 重新设置保护单或调整保护单，使总仓位风险不扩大。
- 调用 `decision.OnPositionAddedScoped` 更新交易计划。
- 通知 `OrderTracker` 记录加仓订单，不重置原 entry 生命周期。

`sortDecisionsByPriority` 增加 `add_long/add_short`：

```go
case "close_long", "close_short", "partial_close":
    return 1
case "update_stop_loss", "update_take_profit":
    return 2
case "add_long", "add_short":
    return 3
case "open_long", "open_short":
    return 4
case "hold", "wait":
    return 5
```

### 11. Trade Plan And State

#### TradePlan Extensions

`decision.TradePlan` 增加：

```go
StrategyMode        string    `json:"strategy_mode,omitempty"`
StrategyName        string    `json:"strategy_name,omitempty"`
StrategyVersion     string    `json:"strategy_version,omitempty"`
ConfigHash          string    `json:"config_hash,omitempty"`
EntrySignalID       string    `json:"entry_signal_id,omitempty"`
EntrySignalType     string    `json:"entry_signal_type,omitempty"`
StructureTarget     float64   `json:"structure_target,omitempty"`
AddCount            int       `json:"add_count,omitempty"`
LastAddSignalID     string    `json:"last_add_signal_id,omitempty"`
LastAddTime         time.Time `json:"last_add_time,omitempty"`
AverageEntryPrice   float64   `json:"average_entry_price,omitempty"`
```

新增回调：

```go
func OnPositionAddedScoped(traderID string, d *Decision, fillPrice, quantity float64) error
```

更新：

- `ActualQuantity`
- `ActualEntry` / `AverageEntryPrice`
- `PositionSizeUSD`
- `RiskUSD`
- `AddCount`
- `CurrentStopLoss`
- `EntrySignalID` / `LastAddSignalID`

#### Programmatic State

新增运行时状态文件：

```text
data/programmatic_strategy_state.json
```

结构：

```go
type ProgrammaticStateFile struct {
    Version   int                           `json:"version"`
    UpdatedAt time.Time                     `json:"updated_at"`
    Traders   map[string]ProgrammaticTraderState `json:"traders"`
}

type ProgrammaticTraderState struct {
    Symbols map[string]ProgrammaticSymbolState `json:"symbols"`
}

type ProgrammaticSymbolState struct {
    LastStructureHash string                    `json:"last_structure_hash,omitempty"`
    LastAnalyzedClosedKline map[string]int64    `json:"last_analyzed_closed_kline,omitempty"`
    ConfirmedSignals  map[string]StoredSignal  `json:"confirmed_signals,omitempty"`
    ExecutedSignals   map[string]SignalExec    `json:"executed_signals,omitempty"`
    AddCountBySide    map[string]int           `json:"add_count_by_side,omitempty"`
    ShortTradeState   *ShortTradeState         `json:"short_trade_state,omitempty"`
}
```

写入策略：

- 进程内 mutex。
- 原子写：写临时文件后 rename。
- 读取失败时记录中文错误并使用空状态；不得 panic。
- 状态按 `trader_id + symbol` scope 隔离。

`signal_id` 生成：

```text
sha1(trader_id|symbol|direction|signal_type|analysis_tf|trigger_tf|center_id|segment_start|segment_end|config_hash)
```

### 12. Logging

#### Files

- `logger/decision_logger.go`
- `trader/auto_trader.go`
- `logger/replay.go` only for compatibility checks, not feature expansion

`DecisionRecord` 增加：

```go
DecisionMode        string                 `json:"decision_mode,omitempty"`
StrategyName        string                 `json:"strategy_name,omitempty"`
StrategyVersion     string                 `json:"strategy_version,omitempty"`
ConfigHash          string                 `json:"config_hash,omitempty"`
StrategyParams      map[string]any         `json:"strategy_params,omitempty"`
StrategyDiagnostics []StrategyDiagnostic   `json:"strategy_diagnostics,omitempty"`
```

`DecisionAction` 同步增加策略字段，与 `decision.Decision` 对齐。

`logger` / `replay` 兼容检查需要识别 `add_long/add_short` 为 open-like 执行动作，但不要求在本期扩展完整行情级 replay/backtest；至少不能把加仓日志误判为未知动作或破坏旧统计读取。

`/api/traders` 和 trader status 增加 `decision_mode`，前端不再只用 `ai_model` 判断 trader 类型。

日志兼容：

- AI 模式继续写 `input_prompt` / `cot_trace`。
- 程序化模式 `input_prompt` 为空字符串，`cot_trace` 写策略摘要，例如“程序化策略：二买确认，结构目标...”，避免旧前端空白。
- 旧 replay 不应因缺少 prompt/CoT 失败；本期不扩展行情级 replay/backtest。

### 13. API Design

#### Files

- `api/server.go`
- `manager/trader_manager.go`
- `trader/auto_trader.go`
- `web/src/lib/api.ts`
- `web/src/types/index.ts`

新增只读 API：

```text
GET /api/strategy/symbols?trader_id=xxx
GET /api/strategy/signals?trader_id=xxx&symbol=BTCUSDT
GET /api/market/klines?symbol=BTCUSDT&timeframe=1h&limit=240
```

#### `GET /api/strategy/symbols`

Response：

```json
{
  "trader_id": "aster_programmatic",
  "decision_mode": "programmatic",
  "symbols": [
    {
      "symbol": "BTCUSDT",
      "sources": ["core", "dynamic"],
      "selected": true,
      "has_position": false,
      "latest_signal_type": "buy2",
      "latest_signal_time": "2026-05-17T10:00:00Z"
    }
  ]
}
```

#### `GET /api/strategy/signals`

Response：

```json
{
  "trader_id": "aster_programmatic",
  "symbol": "BTCUSDT",
  "decision_mode": "programmatic",
  "strategy_name": "chanlun_programmatic",
  "strategy_version": "v1",
  "config_hash": "abc123",
  "signals": [
    {
      "signal_id": "...",
      "signal_type": "buy2",
      "direction": "long",
      "analysis_timeframe": "1h",
      "trigger_timeframe": "15m",
      "price": 65000.0,
      "stop_loss": 63500.0,
      "take_profit": 69000.0,
      "structure_target": 69000.0,
      "state": "confirmed",
      "diagnostics": {}
    }
  ],
  "latest_diagnostics": {}
}
```

#### `GET /api/market/klines`

Response：

```json
{
  "symbol": "BTCUSDT",
  "timeframe": "1h",
  "closed_only": true,
  "klines": [
    {
      "open_time": 1779000000000,
      "close_time": 1779003599999,
      "open": 65000.0,
      "high": 65500.0,
      "low": 64800.0,
      "close": 65200.0,
      "volume": 12345.0
    }
  ]
}
```

K 线 API 使用专门 DTO 输出 `open_time/close_time/open/high/low/close/volume`，不要直接暴露当前 `market.Kline` 的 Go 字段名，避免前后端字段契约漂移。

API 错误响应保持：

```json
{"error":"..."}
```

### 14. Frontend Design

#### Files

- `web/src/App.tsx`
- `web/src/lib/api.ts`
- `web/src/types/index.ts`
- 可新增 `web/src/components/StrategyInspector.tsx`

UI 放在 trader 详情页，不做独立 landing page：

- Trader header 展示 `decision_mode`。
- 程序化 trader 展示策略检查区。
- 左侧或顶部使用 symbol 下拉框，来源于 `/api/strategy/symbols`。
- 选中 symbol 后展示：
  - K 线图或简化 K 线表。
  - 最新信号。
  - 买卖点类型、方向、级别、关键价、结构目标、止损、状态。
  - 诊断摘要：数据不足、无中枢、无背驰、K 线未闭合、风控拒绝等。

如果完整 K 线图超出本期实现范围，先展示最近 K 线列表和信号诊断摘要，API 契约仍保留。

### 15. Compatibility And Migration

- 旧 `config.json` 无 `decision_mode` 时默认 `ai`。
- 程序化配置缺失可安全默认字段时自动补全；危险或不可推断字段非法时启动失败。
- AI trader 不初始化 programmatic engine，不读取 programmatic state。
- 程序化 trader 仍可保留 AI key 配置，但不会调用 AI API 生成决策。
- 决策日志新增字段均使用 `omitempty`，旧日志读取不受影响。
- `config.json.example` 是带注释示例；实际 `config.json` 仍必须是严格 JSON。

### 16. Risk Controls

公共风控顺序：

1. 自动平仓和 stale plan 修复。
2. 熔断冷却和新熔断触发。
3. 账户回撤硬停。
4. 交易计划硬止损、失效、动态止盈、移动止损。
5. 程序化策略信号。
6. open-like 公共校验和 sizing。
7. execution preflight。
8. 保护单设置和高风险标记。

程序化策略不得：

- 在熔断或账户硬停时新增开仓/加仓。
- 同一 symbol 双向持仓。
- 同周期反手开仓。
- 用未闭合 `1h/15m/4h` 确认主信号。
- 用 0 ADX/DI 伪装弱趋势。
- 绕过 `CancelStopLossOrders()` / `CancelTakeProfitOrders()` 分离语义。

### 17. Test Plan

#### Config

- `go test ./config`
- 覆盖默认 AI 兼容、合法 programmatic 配置、非法 timeframe、非法 symbol pool mode、非法 MA 周期、`micro_adx_filter` 约束。

#### Market

- `go test ./market`
- 覆盖 `GetWithHistory` 深度、闭合 K 线过滤、ADX/DI 数据不足、3m micro ADX 启用/禁用。

#### Strategy

- `go test ./strategy/chanlun`
- 覆盖包含关系、分型、笔、线段、swing-pivot、中枢、A+B+C、MACD 面积背驰、均线吻、一二三类买卖点、三买/三卖失败、signal_id 去重。

#### Decision

- `go test ./decision`
- 覆盖 `add_long/add_short` action 合法性、open-like 风控、结构 TP 不足拒绝、RR fallback、公共决策合并优先级。

#### Trader

- `go test ./trader`
- 覆盖 mode routing、`add_*` 执行路径、preflight add intent、排序、交易计划加仓更新、保护单失败处理。

#### API / Manager

- `go test ./api ./manager`
- 覆盖新增只读 API、trader_id 缺失/非法、非 programmatic trader 响应、错误返回格式。

#### Frontend

- `cd web && npm run build`
- 如新增解析工具或状态映射，补 Vitest。

### 18. Implementation Order

1. 配置和运行时 policy。
2. market history/closed K line/micro ADX。
3. 公共 decision 准备与校验函数抽取。
4. `strategy/chanlun` 结构算法和信号生成。
5. 状态持久化和日志字段。
6. `add_long/add_short` 执行链路。
7. API 和前端策略检查区。
8. 集成测试与回归验证。

### 19. Open Design Notes

- 严格缠论实现的“笔/线段”有多种流派，本设计以可测试和可复现为第一优先级；配置 `structure.strictness` 保留后续调整空间。
- 完整行情级 replay/backtest 不进入本期，但日志字段和状态快照应为后续 spec 留足数据。
- 程序化策略初期建议使用小仓位、较低 `max_add_count` 和结构 TP 严格模式上线。
