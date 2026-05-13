# Auto-Close Reconciliation Design

## 方案

`runCycle` 中的自动平仓检测依赖 `lastPositions` 与当前 `ctx.Positions` 对比。修复策略：

1. `buildTradingContext` 只构建当前上下文，不更新 `lastPositions`。
2. `runCycle` 在调用 `detectAutoClosedPositions(ctx.Positions)` 后，再调用 `updatePositionSnapshots(ctx.Positions)`。
3. `detectAutoClosedPositions` 对消失仓位生成 `auto_close_long/auto_close_short`。
4. 如果交易所订单追踪因重启丢失，快照路径用最近市场价作为近似 exit price，并用上一周期快照计算 PnL。
5. close reason 根据本地计划的 `current_stop_loss/stop_loss/take_profit` 推断，无法判断时回退 `AUTO_CLOSE_DETECTED`。
6. `handleAutoCloseEvent` 在有 entry/exit/quantity 时调用完整 `OnPositionClosedScoped`，确保移除计划并更新 closed trade/statistics。
7. 每轮检测后扫描本 trader 的 ACTIVE 计划；若计划对应持仓不在当前交易所持仓中，则按计划保护价或当前价合成一次自动平仓事件，用于修复重启后遗留计划。

## 风险

快照/stale plan 路径的 exit price 是近似值，不如交易所成交价精确。但它只在订单追踪不可用时使用，优先保证状态闭环和绩效配对。
