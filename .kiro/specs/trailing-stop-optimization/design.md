# 移动止损优化 - 设计文档

## 当前算法评估

### 已有合理设计

当前移动止损核心位于 `decision/takeprofit.go`：

- `BreakevenThreshold = 10.0`：盈利达到阈值后才考虑保本。
- `LockProfitThresholds`：
  - PnL >= 15%，锁定 30% 利润，要求 ADX >= 25。
  - PnL >= 20%，锁定 50% 利润，要求 ADX >= 20。
  - PnL >= 30%，锁定 70% 利润，要求 ADX >= 15。
- `ATRSafetyMultiplier = 1.5`，并用 `max(ATR * 1.5, currentPrice * 0.005)` 做最小安全距离。
- long 只允许上移止损，short 只允许下移止损。

从量化交易角度看，这套框架适合趋势跟随策略：盈利初期不急着移动止损，中段保本，趋势延续时逐步锁盈，同时用 ATR 避免止损贴得太近。

### 主要问题

#### P0: 本地交易计划没有同步移动止损结果

`trader/auto_trader.go` 的 `executeUpdateStopLossWithRecord()` 在 `SetStopLoss()` 成功后直接返回，没有调用：

```go
decision.OnStopLossUpdated(decision.Symbol, decision.NewStopLoss)
```

因此 `TradePlan.CurrentStopLoss` 仍是旧值。运行日志已验证此问题：

- 17:43: XRPUSDT `1.3798 -> 1.4191` 成功。
- 17:46: 系统继续认为有效止损是 `1.3798`，重复提交 `1.3798 -> 1.4191`。

影响：

- 重复取消/重挂止损单，增加 API 失败概率和交易所限频风险。
- 本地硬止损判断仍使用旧止损，和交易所实际保护价不一致。
- 重启后 `data/trade_plans.json` 无法恢复真实移动止损状态。

#### P0: 正常部分平仓成功后未同步档位状态

`executePartialCloseWithRecord()` 只有 `partialCloseModeSkip` 分支调用 `decision.OnPartialClose()`。正常部分平仓成功后只重新设置保护单，没有持久化：

- tranche 已执行状态。
- 新 `CurrentStopLoss`。

影响：

- 同一分批止盈档位可能重复触发。
- 新止损未持久化，后续评估仍按旧止损。

#### P1: 保本阈值口径和日志不一致

执行层代码实际条件是：

```go
if profitPercent < 1.0 && isBreakevenStopLoss { reject }
```

但通过日志输出：

```text
当前利润 2.09% >= 3%，允许移动止损至保本价
```

问题有两个：

- 代码阈值是 1%，日志写 3%。
- `profitPercent` 是未杠杆价格涨跌幅，而 `Position.UnrealizedPnLPct` 很可能是带杠杆 PnL；决策层和执行层阈值口径不同。

量化上这会导致风控判断不可复盘：同一笔仓位看起来满足 10% PnL 档位，但执行层却用 2% 价格涨幅解释。

#### P1: 非 Hyperliquid 交易所的止盈/止损恢复逻辑不完整

`queryHyperliquidTakeProfitOrder()` 和 `queryHyperliquidStopLossOrder()` 只对 `*HyperliquidTrader` 生效。Aster/Binance 使用 `CancelStopLossOrders()` 调整止损时，理论上不会取消止盈；但如果交易所实现或部分平仓流程取消全部保护单，当前通用层缺少统一的 open orders 查询与保护单恢复抽象。

#### P2: 缺少最小改单幅度和冷却

当前只要求新止损优于旧止损，没有最小 tick/百分比差异和 cooldown。高波动环境下可能产生频繁改单，增加失败率和风控噪音。

## 优化方案

### 1. 修复移动止损成功后的状态同步

在 `executeUpdateStopLossWithRecord()` 的 `SetStopLoss()` 成功之后调用：

```go
decision.OnStopLossUpdated(decision.Symbol, decision.NewStopLoss)
```

要求：

- 只有交易所 API 成功后更新本地计划。
- 更新失败要返回错误，不得误标已保护。
- actionRecord 记录 old_stop_loss/new_stop_loss 或至少记录 `NewStopLoss`。

### 2. 修复正常部分平仓后的计划同步

在 `executePartialCloseWithRecord()` 正常成功分支末尾调用：

```go
decision.OnPartialClose(d.Symbol, d.TrancheIndex, d.ClosePercentage, effectiveNewStopLoss)
```

`effectiveNewStopLoss` 规则：

- 若 `d.NewStopLoss > 0`，使用新止损。
- 若 `d.NewStopLoss == 0`，使用部分平仓前的有效止损，重设到剩余数量上。
- 若无法找到有效止损，则记录 P0 告警，并拒绝让剩余仓位无保护。

### 3. 统一止损口径

建议引入明确字段或 helper：

```go
type StopMoveMetrics struct {
    PriceMovePct      float64
    LeveragedPnLPct   float64
    DistanceToEntryPct float64
}
```

用途：

- 决策层档位继续使用交易所 PnL 百分比。
- 执行层保本安全约束使用未杠杆价格涨跌幅加手续费缓冲。
- 日志同时输出两者，避免误读。

建议默认：

- 保本允许：未杠杆价格有利变动 >= 0.8%-1.0%，且新止损至少覆盖手续费。
- 锁盈允许：按 `Position.UnrealizedPnLPct` 分档，但最终仍受 ATR 安全距离限制。

### 4. 加入最小改单幅度和冷却

新增配置：

```go
MinStopUpdatePct: 0.0005 // 0.05%
StopUpdateCooldownMinutes: 10
MinStopImprovementR: 0.10
```

执行规则：

- long: `(newSL - currentSL) / currentPrice >= MinStopUpdatePct`。
- short: `(currentSL - newSL) / currentPrice >= MinStopUpdatePct`。
- 同 symbol 在 cooldown 内只允许显著提高锁盈的移动。

### 5. 增强交易所保护单抽象

长期应在 `Trader` interface 增加：

```go
GetProtectiveOrders(symbol string) ([]ProtectiveOrder, error)
ReplaceStopLoss(symbol, side string, qty, price float64) error
ReplaceTakeProfit(symbol, side string, qty, price float64) error
```

好处：

- 交易所实现负责区分止盈/止损和订单 ID。
- 通用 `auto_trader` 不再写 Hyperliquid 专用查询逻辑。
- Aster/Binance/Hyperliquid 可以各自处理精度、reduce-only、trigger 类型。

### 6. 量化参数建议

当前小资金账户、5x 杠杆、3 分钟扫描频率下，建议保守调整：

- 保本阈值：从 `10% leveraged PnL` 降到 `6%-8% leveraged PnL`，但要求未杠杆价格变动 >= 0.8%。
- 第一锁盈档：`12%-15% leveraged PnL` 锁定 25%-30% 利润。
- 第二锁盈档：`20% leveraged PnL` 锁定 50%。
- 第三锁盈档：`30% leveraged PnL` 锁定 65%-70%。
- ATR 安全距离：趋势强 ADX > 35 时用 `2.0 ATR`，普通趋势用 `1.5 ATR`，震荡 ADX < 20 不移动。
- 最小持仓保护期内仍允许交易所硬止损触发，但不主动移动止损，除非未杠杆价格有利变动超过 1.2%。

## 验收口径

- 日志中同一 symbol 不再每个周期重复 `旧止损 -> 同一新止损`。
- `data/trade_plans.json` 中 `current_stop_loss` 与最近一次成功移动止损一致。
- 正常 `partial_close` 成功后，`executed_tranches` 被标记，剩余仓位有止损单。
- 保本检查日志显示真实阈值，不再出现 `2.09% >= 3%` 这类矛盾。
- Go 测试覆盖移动止损成功、失败、重复跳过、部分平仓保护四类路径。
