# 移动止损优化 - 需求文档

## 背景

2026-05-06 在 `43.133.65.217:/home/ubuntu/appai2/nofx` 对移动止损逻辑做代码和运行日志交叉分析。当前项目已经有移动止损算法，核心入口为 `decision/takeprofit.go`：

- `PositionEvaluator.Evaluate()` 在持仓评估阶段生成 `update_stop_loss`。
- `calculateTrailingStopImproved()` 根据盈利、ADX、ATR 安全距离和档位锁盈计算新止损。
- `trader/auto_trader.go` 的 `executeUpdateStopLossWithRecord()` 负责取消旧止损并设置新止损。

整体方向是合理的：使用单调止损、趋势强度门槛、ATR 安全距离和保本/锁盈档位，符合趋势跟随持仓管理框架。但当前实现存在关键执行缺陷：真实执行路径成功调整止损后没有调用 `decision.OnStopLossUpdated()`，导致本地 `TradePlan.CurrentStopLoss` 不更新。运行日志已经出现重复症状：

- 17:43 XRPUSDT 止损成功调整 `1.3798 -> 1.4191`。
- 17:46 系统仍按旧止损 `1.3798 -> 1.4191` 再次触发同一移动止损。

此外，正常 `partial_close` 成功后没有调用 `decision.OnPartialClose()`，分批档位状态和新止损也可能不持久化；执行层日志写着 `利润 >= 3%`，但代码实际只要求 `profitPercent >= 1%`，存在风控可观测性误导。

## 需求

### Requirement 1: 移动止损状态必须与交易所执行结果一致

1.1 WHEN `update_stop_loss` 交易所 API 执行成功，THEN 系统 SHALL 调用 `decision.OnStopLossUpdated(symbol, newStopLoss)` 更新 `TradePlan.CurrentStopLoss` 并自动保存。

1.2 WHEN `update_stop_loss` 交易所 API 执行失败，THEN 系统 SHALL NOT 更新本地 `CurrentStopLoss`，并 SHALL 在决策记录中标记失败原因。

1.3 WHEN 下一轮持仓评估读取同一持仓，THEN `evaluateTrailingStop()` SHALL 以最新 `CurrentStopLoss` 为有效止损，不得重复提交相同或更差的止损调整。

1.4 WHEN 服务重启并从 `data/trade_plans.json` 恢复计划，THEN 已调整过的 `CurrentStopLoss` SHALL 被保留。

### Requirement 2: 移动止损算法必须保持单调和交易所安全距离

2.1 WHEN 持仓为 long，THEN 新止损 SHALL 大于当前有效止损且小于当前市场价。

2.2 WHEN 持仓为 short，THEN 新止损 SHALL 小于当前有效止损且大于当前市场价。

2.3 WHEN ATR 安全距离约束触发，THEN 系统 SHALL 将新止损限制在 `currentPrice - max(ATR * multiplier, price * minPct)` 或 `currentPrice + ...` 之外，避免贴价触发。

2.4 WHEN 趋势强度低于门槛，THEN 系统 SHALL 暂缓移动止损，避免震荡市中频繁把仓位扫出。

### Requirement 3: 保本移动条件必须口径一致

3.1 WHEN 计算是否允许移动到保本附近，THEN 系统 SHALL 明确使用未杠杆价格涨跌幅或交易所返回的杠杆后 PnL 之一，不得混用。

3.2 WHEN 日志输出保本门槛，THEN 文案 SHALL 与真实阈值一致。

3.3 WHEN 交易手续费和滑点不可忽略，THEN 保本止损 SHALL 至少加入手续费/滑点缓冲，例如 entryPrice 的 0.2%-0.5%，或按交易所 fee rate 计算。

### Requirement 4: 分批止盈后剩余仓位必须保持保护

4.1 WHEN `partial_close` 成功，THEN 系统 SHALL 调用 `decision.OnPartialClose(symbol, trancheIndex, closePercentage, newStopLoss)`，持久化已执行档位和新止损。

4.2 WHEN `partial_close` 成功且剩余仓位存在，THEN 系统 SHALL 确认剩余仓位存在止损保护。

4.3 WHEN `partial_close` 决策没有提供 `new_stop_loss`，THEN 系统 SHALL 自动回退到原有效止损，而不是让剩余仓位裸露。

4.4 WHEN `partial_close` 触发交易所取消旧止盈止损挂单，THEN 系统 SHALL 重新设置剩余数量对应的保护单。

### Requirement 5: 减少无意义重复改单和过度交易

5.1 WHEN 新止损与当前有效止损差异小于最小 tick 或小于价格的 0.05%，THEN 系统 SHALL 跳过改单。

5.2 WHEN 同一 symbol 最近一次止损调整距离当前不足一个冷却周期，THEN 系统 SHALL 跳过，除非新止损明显提高锁盈。

5.3 WHEN 交易所已有相同价格止损单，THEN 系统 SHALL 不重复取消和重挂。

### Requirement 6: 测试和验收

6.1 SHALL 添加 `update_stop_loss` 成功后调用 `OnStopLossUpdated` 的测试。

6.2 SHALL 添加 `partial_close` 成功后调用 `OnPartialClose` 并更新 `CurrentStopLoss` 的测试。

6.3 SHALL 添加移动止损单调性、ATR 安全距离、保本阈值口径一致性的单元测试。

6.4 SHALL 运行 Go 测试；若远端宿主机没有可用 `go` 命令，SHALL 使用项目 Docker 构建环境或明确记录阻塞。
