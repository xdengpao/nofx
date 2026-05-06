# 交易全流程评估与优化 — 一致性检查

## 检查范围

本次检查对照以下文档与当前代码：

- `.kiro/specs/trading-flow-evaluation-optimization/requirements.md`
- `.kiro/specs/trading-flow-evaluation-optimization/design.md`
- `.kiro/specs/trading-flow-evaluation-optimization/tasks.md`
- 当前实现：`config/`、`main.go`、`manager/`、`trader/`、`decision/`、`logger/`、`api/`、`web/`

本地仓库当前没有可用历史决策日志：`decision_logs` 下 `decision_*.json` 数量为 0。因此本次结论是代码审计与已有测试覆盖检查，不包含真实 PnL、胜率、Profit Factor 或退出原因的实盘归因。

## 总体结论

规格三件套整体方向与代码事实一致，且覆盖了当前最重要的交易流程风险：配置默认值不写回、AI 分析间隔不跨周期、保护单失败只记录警告、自动平仓可能双路径重复、交易计划缺少 trader/side 作用域、执行质量统计不够细。

需要调整的是任务表达的“新建”与“增强”边界：部分能力已经存在，应从“从零实现”改成“抽取、补齐、统一口径和补测试”。尤其是历史归因、rolling gate、partial close 小额保护、移动止损状态更新已经有实现和测试，后续任务应避免重复造一套并行逻辑。

## 需求覆盖检查

| 需求 | 当前代码状态 | 设计/任务覆盖 | 一致性结论 |
| --- | --- | --- | --- |
| R1 全流程评估基线 | 本地无历史日志，只能代码审计 | Phase 0、Phase 6 | 一致，需要在线日志后补实绩归因 |
| R2 配置与运行时状态 | 默认值写回 bug 存在；风险参数部分硬编码 | Phase 2 | 一致，优先级应保持高 |
| R3 行情与候选标的 | 候选来源有 `Sources`，但无评分/过滤原因 | Phase 1、Phase 3 | 一致 |
| R4 AI 调用状态 | 错误可记录 prompt，但 AI 频率状态不跨周期 | Phase 2 | 一致 |
| R5 开仓准入 | 基础硬约束和 rolling gate 已有，市场/执行质量 gate 缺失 | Phase 3 | 一致，但应以抽取增强为主 |
| R6 仓位 sizing | 已有自适应 sizing 调用和风险校验，缺少统一 sizing 结果结构 | Phase 3 | 一致 |
| R7 执行保护 | 更新 SL/TP 路径较强，开仓后保护单失败仍只 warn | Phase 4 | 一致，开仓保护失败补救是关键 |
| R8 持仓和平仓 | TP/SL、移动止损、分批止盈较完整；动态 TP 只更新本地计划 | Phase 4、Phase 5 | 一致，需增加交易所 TP 同步决策 |
| R9 自动平仓 exactly-once | 有两条发现路径，无 dedupe；OrderTracker 有降级查询 | Phase 5 | 一致 |
| R10 历史归因与前端观测 | `/api/performance` 已返回全历史分析和 rolling/execution | Phase 1 | 部分已实现，任务应避免重复 |
| R11 分层风控 | 有全局熔断和账户硬停，trader 级作用域不足 | Phase 2、Phase 5 | 一致 |
| R12 回放测试灰度 | 单元测试较多，无 replay/report-only 工具 | Phase 6 | 一致 |

## 代码事实与关键发现

### 1. 配置默认值写回问题真实存在

`Config.Validate()` 使用 `for i, trader := range c.Traders`，但默认 `Exchange` 和 `ScanIntervalMinutes` 写到局部变量 `trader`，不会写回 `c.Traders[i]`：

- `config/config.go:113`
- `config/config.go:130`
- `config/config.go:172`

`tasks.md` 的 `2.1` 和 `2.2` 与代码事实一致，应作为第一批实现任务。

### 2. 风险和分析间隔没有完整端到端配置

`main.go` 初始化 `decision.Config` 时仍硬编码：

- `MaxRiskPerTrade: 0.02`
- `TotalRiskBudget: 0.08`
- `AnalysisIntervalMin: 15`

位置：`main.go:205-213`。

`buildTradingContext()` 当前只注入 `MaxRiskPerTrade` 和 `EffectiveMaxRiskPerTrade`，未注入 `TotalRiskBudget`、`MaxAccountDrawdownPct`、`AnalysisIntervalMin`、`LastAnalysisTime`：

- `trader/auto_trader.go:721-743`

`decision.initializeDefaults()` 会补默认值：

- `decision/decision.go:197-209`

这意味着需求 R2.1 是准确的，但设计里应强调“先把现有字段传透”，再考虑新增 `RiskConfig`。

### 3. AI 分析间隔跨周期不生效

`Context` 有 `LastAnalysisTime` 字段：

- `decision/decision.go:38`

`shouldCallAIForNewOpportunities()` 会读取该字段：

- `decision/decision.go:354-360`

`GetFullDecision()` 在本次调用内设置 `ctx.LastAnalysisTime = time.Now()`：

- `decision/decision.go:173`

但 `AutoTrader.buildTradingContext()` 没有从 `AutoTrader` 注入上次分析时间，也没有在 `runCycle()` 把更新后的时间保存回 `AutoTrader`。因此 `tasks.md` 的 `2.5`、`2.6`、`2.8` 必须保留，并且应优先于更复杂 open gate。

### 4. 历史归因和 rolling gate 已有实现

`logger.BuildTradeOutcomes()` 已支持：

- `open_long/open_short`
- `close_long/close_short`
- `auto_close_long/auto_close_short`
- unmatched close/open
- reasoning 回填

位置：`logger/decision_logger.go:387-468`。

`logger.BuildRollingPerformance()` 已实现 symbol/side gate、弱势币种名单、short 默认降权和动态风险收缩：

- `logger/decision_logger.go:478-607`

对应测试已存在：

- `logger/logger_test.go:46`
- `logger/logger_test.go:94`
- `logger/logger_test.go:119`

因此 `tasks.md` 的 `1.4` 应理解为“确认兼容并补边界测试”，不是重写归因模块。`3.8` 也应复用现有 `PerformanceGates`，不要另建第二套 rolling gate。

### 5. 执行质量统计已有基础，但粒度不足

`ExecutionQualityStats` 已包含：

- total actions
- partial close attempts/failures/rate
- AI failure count
- unmatched count

位置：`logger/decision_logger.go:367-375`。

`BuildExecutionQuality()` 当前只识别 record failure、partial close failure 和 unmatched：

- `logger/decision_logger.go:662-685`

缺少规格要求的保护单失败、高危执行失败、开仓拒绝、可能产生真实副作用的执行失败。`tasks.md` 的 `1.2`、`1.3` 是准确的。

### 6. `/api/performance` 已经不是短窗口

`handlePerformance()` 当前用 `AnalyzePerformance(1000000)`，即 API 展示全历史窗口：

- `api/server.go:391-400`

但 `AutoTrader.buildTradingContext()` 用 `AnalyzePerformance(100)`：

- `trader/auto_trader.go:704-718`

这说明前端/API 观测已比旧规格更强，真正影响实盘 gate 的还是运行时 100-cycle 窗口。任务应优先修运行时上下文，而不是重复扩展 `/api/performance` 基础字段。

### 7. 开仓基础硬约束已存在，但还不是统一 OpenGate

`validateOpenDecision()` 已包含：

- 市场数据检查
- rolling gate
- 预开仓失效条件
- 重复持仓
- 风险预算
- 杠杆
- 仓位大小
- 高相关仓位缩放
- TP/SL 方向
- 净 RR
- 单笔风险

位置：`decision/decision.go:415-514`。

因此 `tasks.md` Phase 3 应以“抽取 `EvaluateOpenGate()`、统一拒绝原因、补新增 gate”为目标，而不是大面积替换现有逻辑。需要新增的是市场状态 gate、相关性集中度 gate、执行质量 gate、AI backoff gate 和结构化拒绝日志。

### 8. 开仓后保护单失败仍有裸仓风险

开多后设置 SL/TP 失败只打印警告，仍继续创建交易计划并返回成功：

- `trader/auto_trader.go:843-857`

开空同样如此：

- `trader/auto_trader.go:915-923`

这与需求 R7.2 完全一致，是高优先级交易安全问题。`tasks.md` 的 `4.3`、`4.4`、`4.5` 应优先于较重的 trader-scoped plan 迁移。

### 9. 部分平仓和移动止损已有一批修复

部分平仓最小名义额、剩余小仓位和保护止损回退已实现：

- `trader/auto_trader.go:1480-1518`
- `trader/auto_trader.go:1592-1631`
- `trader/auto_trader.go:1654-1679`

相关测试已存在：

- `trader/trader_test.go:224`
- `trader/trader_test.go:234`
- `trader/trader_test.go:254`

移动止损成功后已调用 `decision.OnStopLossUpdated()`：

- `trader/auto_trader.go:1345`

相关测试已存在：

- `decision/persistence_test.go:284`

因此 `tasks.md` 的 `4.8`、`4.10` 有一部分已经完成，后续应聚焦剩余缺口：保护单失败结构化记录、紧急平仓、执行质量反馈。

### 10. 动态止盈目前只更新本地计划，未同步交易所

`evaluateExistingPositions()` 如果 `result.NewTakeProfit > 0`，会直接更新本地计划：

- `decision/decision.go:299-300`

但它没有生成 `update_take_profit` 决策，执行层的 `executeUpdateTakeProfitWithRecord()` 不会被调用。需求 R8.6 和设计中“区分本地计划和交易所保护单”非常准确；建议在 tasks 中显式增加“动态 TP 生成执行动作或标记 report-only”的任务。

### 11. 自动平仓 exactly-once 风险真实存在

主循环先调用 `syncAutoClosedOrders()`：

- `trader/auto_trader.go:315-316`

随后又调用 `detectAutoClosedPositions()`：

- `trader/auto_trader.go:350-357`

`syncAutoClosedOrders()` 会更新统计并写单独日志：

- `trader/auto_trader.go:503-516`

`detectAutoClosedPositions()` 会基于上轮持仓快照生成 `auto_close_*` action：

- `trader/auto_trader.go:1899-1949`

当前没有共同 dedupe。Phase 5 的 dedupe 任务与代码事实一致。

### 12. OrderTracker 有降级查询，但保护单 ID 链路不完整

`OrderTracker` 支持记录 SL/TP order id：

- `trader/order_tracker.go:61-80`

但 `trader.Trader.SetStopLoss()` / `SetTakeProfit()` 接口只返回 `error`，开仓执行后无法拿到保护单 order id：

- `trader/interface.go:32-36`

`AutoTrader` 开仓时只 `TrackNewPosition()`，没有更新保护单 ID：

- `trader/auto_trader.go:841-849`
- `trader/auto_trader.go:908-920`

OrderTracker 会降级查成交历史和订单历史：

- `trader/order_tracker.go:177-241`

这与需求 R9.2 一致，但设计/任务应补一条“明确保护单 ID 获取策略：不改接口则依赖降级置信度，改接口则同步所有交易所实现”。

### 13. Manager 和 AutoTrader 可能维护两套 OrderTracker

`TraderManager.AddTrader()` 为每个 trader 创建一个 `tm.orderTrackers[cfg.ID]`：

- `manager/trader_manager.go:99-103`

`AutoTrader.NewAutoTrader()` 内部也创建自己的 `at.orderTracker`，而开仓和自动平仓同步使用的是 `at.orderTracker`：

- `trader/auto_trader.go:841-842`
- `trader/auto_trader.go:487`

`manager` 的 `TrackNewPosition/UpdateStopLossOrderID/StopTracking` 操作不会自动作用到 `AutoTrader` 内部 tracker。Phase 5 应新增任务：合并或明确这两套 tracker 的职责，避免 API/manager 看到的追踪状态与实盘循环使用的状态不一致。

### 14. 交易计划缺少 trader/side 作用域

`TradePlanManager` 是包级全局：

- `decision/persistence.go:21-23`

`plans` 的 map key 是 symbol：

- `decision/persistence.go:52-54`
- `decision/persistence.go:197-214`

开仓、更新止损、部分平仓、平仓移除也都按 symbol：

- `decision/persistence.go:711-727`
- `decision/persistence.go:731-743`

这与需求 R2.3、R9.5 的风险完全一致。Phase 5 的 trader-scoped plan 迁移是必要的，但属于高影响改造，建议在 Phase 1-4 观测与安全补丁稳定后执行。

## 任务清单建议调整

建议后续修改 `tasks.md` 时做这些微调：

1. `1.4` 改为“确认并补齐 `BuildTradeOutcomes()` 边界测试”，避免重复实现已有归因逻辑。
2. `1.5` 改为“补充 `/api/performance` 缺失字段或新增 `/api/strategy-health`”，因为全历史 performance 已存在。
3. `3.1` 到 `3.3` 明确为抽取和复用现有 `validateOpenDecision()`、`effectiveOpenGate()`、`market.CalculateAdaptivePositionSize()`。
4. Phase 4 增加一条：开仓后 SL/TP 任一失败时，`DecisionAction.Success` 不得简单为成功；必须有结构化保护单结果。
5. Phase 4 增加一条：动态 TP 计算后应选择生成 `update_take_profit` 执行动作或明确保持 report-only。
6. Phase 5 增加一条：统一 `manager.TraderManager.orderTrackers` 与 `AutoTrader.orderTracker` 的职责或状态同步。
7. Phase 5 增加一条：保护单 order id 获取策略评估，决定是否扩展 `Trader` 接口或保留降级置信度。
8. Phase 7 增加一条：用 `git diff --check` 或等价检查确认文档/代码没有格式尾随空白。

## 建议执行顺序

最安全的执行顺序：

1. Phase 2.1-2.2：修配置默认值写回，小而确定。
2. Phase 2.5-2.9：修 AI 分析间隔跨周期，减少无谓 AI 调用。
3. Phase 4.3-4.5：开仓后保护单失败结构化处理和紧急补救。
4. Phase 1.2-1.7：增强执行质量和策略健康观测。
5. Phase 3：抽取 open gate 和 sizing，先 report-only 再 hard gate。
6. Phase 5：trader-scoped plan 和 auto-close dedupe。
7. Phase 6：replay/report-only 工具。

## 测试状态

本次一致性检查没有运行 Go 或前端测试；只做了代码读取、文档对照和本地日志存在性检查。实现阶段建议按 tasks 中的目标包逐步运行，避免一开始就跑可能触达网络路径的全量测试。

