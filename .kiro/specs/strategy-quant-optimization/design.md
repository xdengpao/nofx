# 策略量化优化 — 设计文档

## 历史评估结论

### 总体表现

- 决策日志：34,001 条，时间范围 2026-02-24 11:28 至 2026-05-06 13:07。
- 可配对闭合交易：154 笔。
- 估算总 PnL：-8.6894 USDT。
- 胜率：37.66%。
- 平均盈利：0.4167 USDT。
- 平均亏损：-0.3423 USDT。
- Profit Factor：0.7355。
- 账户净值区间：最高约 73.2241，最近约 59.9955，最大净值回撤约 -18.57%。

### 方向归因

| 方向 | 交易数 | PnL | 胜率 |
| --- | ---: | ---: | ---: |
| long | 108 | -1.7036 | 38.0% |
| short | 46 | -6.9857 | 37.0% |

空单样本更少但亏损贡献更大，说明当前做空过滤条件不够严格，或者在加密市场偏上行阶段逆势试空过多。

### 币种归因

| Symbol | 交易数 | PnL | 胜率 | PF |
| --- | ---: | ---: | ---: | ---: |
| BCHUSDT | 25 | -5.0204 | 20.0% | 0.172 |
| ASTERUSDT | 8 | -3.3084 | 12.5% | 0.139 |
| LTCUSDT | 12 | -2.4098 | 25.0% | 0.199 |
| XRPUSDT | 16 | -1.8797 | 37.5% | 0.630 |
| BNBUSDT | 12 | -0.4628 | 58.3% | 0.803 |
| DOGEUSDT | 30 | -0.4334 | 36.7% | 0.891 |
| SOLUSDT | 7 | -0.2541 | 57.1% | 0.802 |
| HYPEUSDT | 11 | +0.3390 | 45.5% | 1.212 |
| ETHUSDT | 16 | +1.4353 | 43.8% | 1.430 |
| ZECUSDT | 17 | +3.3050 | 52.9% | 2.429 |

当前最应该限制的是 BCH、ASTER、LTC、XRP；ZEC、ETH、HYPE 是相对正贡献样本，但仍需滚动验证，避免过拟合历史。

### 执行层问题

`partial_close` 共 287 次，成功 3 次，失败 284 次。典型错误：

```text
Order's notional must be no smaller than 5.0
```

当前代码在 `trader/auto_trader.go` 只检查 partial close 后的剩余仓位是否过小，但没有检查本次要平掉的名义额是否小于交易所最小下单额。因此系统会在每个周期重复尝试无法成交的部分平仓。

### 当前运行风险

当前 DeepSeek API 返回 402 `Insufficient Balance`。在 AI 余额不足前，新开仓不可用；任何策略优化上线前都需要把 AI 调用可用性作为硬前置。

## 优化设计

### 0. 当前代码交叉验证

当前代码已有 `logger.DecisionLogger.AnalyzePerformance(lookbackCycles)` 和 `logger.TradeOutcome`，但它不能直接满足本 spec：

- `/api/performance` 固定调用 `AnalyzePerformance(100)`，只覆盖最近约 5 小时；当前 2026-05-06 重置后返回 0 笔交易，无法反映全历史 154 笔闭合样本。
- `AnalyzePerformance` 只处理 `open_long/open_short/close_long/close_short`，没有处理 `auto_close_long/auto_close_short`。
- 未配对 close 会被静默忽略，没有输出 unmatched 诊断。
- `logger.DecisionAction` 没有 reasoning 字段，不能直接生成 `open_reason/close_reason`。
- `trader.executePartialCloseWithRecord()` 已检查 partial close 后剩余仓位小于 10 USDT 时自动全平，但未检查本次 `closeQuantity * markPrice < 5 USDT`。
- `decision.DynamicRiskAdjuster` 已存在，但 `validateOpenDecision()` 和仓位建议仍使用固定 `ctx.MaxRiskPerTrade`。

因此后续实现应优先“抽取并增强现有能力”，而不是新建一套与 `AnalyzePerformance` 并行且口径不同的统计逻辑。

### 1. 历史归因模块

在 `logger` 或新 `analysis` 子模块中抽取通用归因函数，提供 `BuildTradeOutcomes(records)`：

- 输入：`[]DecisionRecord`
- 输出：`[]TradeOutcome`、`[]UnmatchedAction`
- 规则：按 timestamp 排序；按 `symbol + side` 配对；close quantity 为 0 时视为全平；`auto_close_*` 视为对应方向平仓；未配对记录单独输出。
- reasoning 来源：优先使用扩展后的 `DecisionAction.Reasoning`；兼容旧日志时从 `decision_json` 按 symbol/action 回填。

该模块不依赖交易所 API，用于离线复盘、单元测试和前端绩效展示。

### 2. Rolling Performance Gate

新增策略门控：

- `SymbolPerformanceGate`: 最近 N 笔按 symbol 统计 PF、PnL、胜率。
- `SidePerformanceGate`: 最近 N 笔按 long/short 统计 PF、PnL、胜率。
- `CandidatePenalty`: 将门控结果注入决策上下文。

门控动作：

- `allow`: 正常进入 AI prompt。
- `penalize`: 进入 prompt，但提示高风险；同时验证层要求更高 confidence、更低 risk。
- `block`: 不进入候选列表，直到冷却结束。

初始建议：

- BCHUSDT、ASTERUSDT、LTCUSDT、XRPUSDT：启动时设置为 penalize。
- short：启动时设置为 penalize，confidence 最低 90，risk multiplier 0.5。

### 3. 开仓验证强化

在 `decision.validateOpenDecision()` 增加：

- symbol gate 检查。
- side gate 检查。
- BTC ranging/squeeze 时的山寨币趋势跟随限制。
- high correlation 同方向持仓限制。
- rolling PF < 1 时动态降低 `MaxRiskPerTrade`。

这些必须放在验证层，而不是只写入 prompt。历史亏损说明仅依赖 AI 自律不够。

### 4. 部分平仓执行保护

在 `trader.executePartialCloseWithRecord()` 增加：

- `closeValue := closeQuantity * markPrice`
- 若 `closeValue < 5`：
  - 若 `currentPositionValue <= 25` 且 close percentage 为 20%，直接全平；
  - 否则跳过下单，尝试执行保护性止损调整；
  - 标记该 tranche 已处理或进入 cooldown，避免每 3 分钟重复失败。
- 保留现有 `remainingValue <= 10` 自动全平逻辑。

### 5. 风险动态收缩

当前已有 `DynamicRiskAdjuster`，但开仓链路仍主要使用固定 `ctx.MaxRiskPerTrade`。设计上应将 rolling performance 输出注入 `ctx.EffectiveMaxRiskPerTrade`：

- 默认：2%。
- 最近 10 笔 PF < 1：1%。
- 最近 20 笔 PF < 0.8：0.5%。
- 账户处于新基准重置后的观察期：0.5%，直到至少 5 笔闭合交易后再恢复。

### 6. 数据和前端可观测性

API 增加或扩展：

- `/api/performance` 返回全历史和 rolling windows，而不是只依赖最近窗口。
- 增加 execution quality：partial close failure rate、AI failure count、unmatched action count。
- 前端显示策略健康状态：`AI unavailable`、`symbol blocked`、`risk reduced`。

## 实施顺序

1. 先修执行层 `partial_close` 最小名义额，避免继续产生无效订单。
2. 再做历史归因模块和测试，把当前复盘过程固化。
3. 接着实现 rolling gate，先只记录和告警。
4. 最后把 gate 接入开仓验证，正式影响交易。

## 验收口径

- 历史 34,001 条日志可在本地复现同量级统计。
- `partial_close` 小额订单不再触发交易所 -4164 错误。
- BCH/ASTER/LTC/XRP 在默认配置下不会直接以普通风险开新仓。
- short 新开仓需要更高 confidence 且风险减半。
- DeepSeek API 402 时系统只记录失败，不进入新开仓。
