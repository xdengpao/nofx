---
inclusion: fileMatch
fileMatchPattern: "decision/**/*.go,trader/**/*.go,market/**/*.go"
---

# 交易领域知识

## 交易周期流程

```
runCycle() → syncAutoClosedOrders() → GetBalance/GetPositions
→ GetMergedCoinPool() → buildTradingContext()
→ GetFullDecision() → sortDecisionsByPriority()
→ executeDecisions() → LogDecision()
```

## 决策类型 (Action)

- `buy_long`: 开多仓
- `sell_short`: 开空仓
- `close_long`: 平多仓
- `close_short`: 平空仓
- `update_stop_loss`: 更新止损
- `partial_close`: 部分平仓
- `wait`: 等待/观望
- `hold`: 继续持有

## 持仓评估优先级链 (9 级)

1. 硬性止损 (价格触及止损价)
2. 固定止盈 (价格触及止盈价)
3. 最小持仓时间保护 (默认 30 分钟，极端亏损 -3% 例外)
4. 利润保护 (峰值 ≥8%，回落至 50% 以下)
5. ATR 跟踪止盈 (盈利 >5%，回落超 ATR×2.5)
6. 自适应分批止盈 (4 档: 20%→30%→30%→20%)
7. 移动止损 (15% 锁 30%、20% 锁 50%、30% 锁 70%)
8. 动态止盈调整 (市场状态 + 波动率 + 时间衰减)
9. 计划失效条件检查 (≥60 分钟才检查)

## 风险管理参数

| 参数 | 值 |
|------|-----|
| MaxRiskPerTrade | 2% |
| TotalRiskBudget | 8% |
| MaxPositions | 3 |
| MinRiskRewardRatio | 2.5:1 |
| CorrelationThreshold | 0.8 |
| DynamicRiskRange | ±50% |

## 熔断条件

| 条件 | 冷却时间 |
|------|---------|
| BTC 1h 跌 >5% | 120 分钟 |
| 账户回撤超限 | 120 分钟 |
| 连续亏损 ≥5 | 30 分钟 |
| 保证金 >90% | 30 分钟 |

## 失效条件类型 (9 种)

`ema_cross_down`, `ema_cross_up`, `price_below`, `price_above`,
`rsi_above`, `rsi_below`, `adx_below`, `macd_cross`, `trend_reversal`

时间框架: `4H`, `1H`, `30M`, `15M`, `1D`

格式: `{timeframe}:{type}:{indicator}:{indicator2}`
示例: `4H:EMA_CROSS_DOWN:EMA20:EMA50`

## 技术指标

- EMA20, EMA50
- MACD (线、信号线、柱状图)
- RSI7, RSI14
- ATR3, ATR14
- ADX14 (含 DI+/DI-)
- 布林带 (上轨、下轨、宽度、价格位置)
- OI (持仓量) + 资金费率
