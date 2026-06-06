# Live Loss Diagnosis Optimization Requirements

## Background

2026-05-16 对本机 `/home/ubuntu/appai2/nofx` 与 161 服务器 `/home/ubuntu/appai3/nofx` 的 `aster_deepseek` 实盘日志做了对照复盘。

本机样本：

- 决策日志约 1674 条，实际开仓 6 次，主动/系统平仓 6 次。
- 账户余额从 200.0262 USDT 降至约 197.4584 USDT，账户层面约 -2.5678 USDT。
- `data/trade_plans.json` 记录为 10 笔闭合交易、1 胜 9 负、连亏 8、PF 0.00596、`total_pnl=-24.51`。
- BCH、ETH、DOGE、BNB 均出现主动平仓后又被 `AUTO_CLOSE_DETECTED` 记录第二笔亏损的情况。

161 样本：

- 决策日志约 1653 条，实际开仓 5 次。
- 账户余额从 45.4439 USDT 降至约 44.1798 USDT，账户层面约 -1.2640 USDT。
- `data/trade_plans.json` 记录为 9 笔闭合交易、1 胜 8 负、连亏 8、PF 0.2481。
- BCH、BTC、ETH、BNB 均出现主动平仓后又被 `AUTO_CLOSE_DETECTED` 记录第二笔亏损的情况。

因此当前问题分为两类：

1. 交易统计和熔断状态被重复闭合交易污染，导致胜率、连亏、PF、AI 学习反馈失真。
2. 去重后仍存在策略层负期望迹象：亏损单多在 -3% 到 -7% 杠杆收益处退出，盈利单回吐明显，AI 多次提出 RR 不达标或 BTC gate 明确禁止的候选。

## Glossary

- **Snapshot auto-close**: `detectAutoClosedPositions()` 用上一周期持仓快照和当前持仓对比推断的自动平仓。
- **Order-tracker auto-close**: `OrderTracker.CheckAutoClosedOrders()` 根据交易所订单状态确认的止损/止盈成交。
- **Manual close**: 系统在 `evaluateExistingPositions()` 中输出 `close_long` 或 `close_short` 后主动调用交易所平仓。
- **Deduplicated trade**: 按 trader、symbol、side、订单或持仓生命周期去重后的唯一闭合交易。
- **Loss mode**: 连续亏损或短期 PF 低于阈值后进入的保守交易状态。

## Requirements

### Requirement 1: 修复闭合交易重复统计

**User story:** 作为量化交易员，我希望每个实际持仓生命周期只产生一条闭合交易记录，以便胜率、PF、连亏和熔断依据真实交易结果。

Acceptance criteria:

1. WHEN `close_long` 或 `close_short` 已经成功调用 `OnPositionClosedScoped` THEN 下一周期的 snapshot auto-close SHALL NOT 再为同一 symbol/side 生成 closed trade。
2. WHEN snapshot auto-close 没有关联活跃 `TradePlan`、订单 ID 或可验证的持仓生命周期 THEN it SHALL log an unmatched/reconciliation event but SHALL NOT update `closed_trades`、`returns` 或 `TradeStatistics`。
3. WHEN order-tracker auto-close 有订单 ID 或交易所成交明细 THEN it SHALL remain eligible to update closed trade statistics exactly once。
4. IF 同一 trader/symbol/side 在 10 分钟内出现相同 lifecycle 的 close event THEN the second event SHALL be deduplicated and recorded as skipped, not counted as a new trade。
5. WHEN statistics are recalculated from repaired records THEN 本机 BCH/ETH/DOGE/BNB 的 duplicate `AUTO_CLOSE_DETECTED` SHALL no longer increase consecutive losses。

### Requirement 2: 区分真实收益和统计收益单位

**User story:** 作为策略评估者，我希望清楚区分账户 USDT 盈亏、单笔百分比收益和统计累计百分比，避免把 `total_pnl` 当成真实账户损益。

Acceptance criteria:

1. `TradeStatistics` SHALL expose or document separate fields for cumulative `pnl_percent_points` and `pnl_usd` when data exists。
2. Decision logs/replay reports SHALL show account balance delta and deduplicated trade PnL side by side。
3. WHEN a closed trade has neither a valid plan nor validated exchange close metadata with symbol/side/entry/exit/quantity/leverage THEN it SHALL NOT contribute to win rate, PF, average win/loss, or consecutive loss count。

### Requirement 3: 建立可复现的实盘复盘管线

**User story:** 作为系统维护者，我希望可以用同一命令复盘本机和远端日志，输出开仓、平仓、拒单、收益、重复事件和熔断原因。

Acceptance criteria:

1. A replay command SHALL load `decision_logs/{trader_id}` and `data/trade_plans.json` without requiring exchange credentials。
2. The report SHALL include record count, open count, close count, duplicate close candidates, unmatched auto-close count, deduplicated win rate, PF, max loss streak, balance delta, and top rejection reasons。
3. WHEN run against the 2026-05-13 to 2026-05-16 local/161 samples THEN it SHALL identify duplicate `AUTO_CLOSE_DETECTED` events for the affected symbols。
4. The replay SHALL be dry-run by default and SHALL NOT mutate runtime `data/` or `decision_logs/` unless explicitly requested。
5. IF exchange order/trade history is provided by read-only `Trader.GetOrderHistory()` / `Trader.GetTradeHistory()` snapshots or exported JSON THEN replay SHALL reconcile local close events against that history and flag mismatches separately from inferred snapshot events。

### Requirement 4: 降低亏损状态下的继续开仓风险

**User story:** 作为交易员，我希望连续亏损后系统自动降低风险和开仓频率，而不是仅依赖污染后的熔断或 prompt 提醒。

Acceptance criteria:

1. WHEN deduplicated recent losses >= 2 THEN the trader SHALL enter loss mode for a configurable cooldown window。
2. In loss mode, max risk per trade SHALL default to <= 0.5% account equity, max concurrent positions SHALL default to 1, and high beta altcoin longs SHALL be blocked unless BTC higher-timeframe trend is supportive。
3. WHEN deduplicated recent PF over the configured recovery window exceeds the configured threshold THEN the trader MAY return to normal mode。
4. Balanced mode SHALL have a configurable daily open limit under loss mode, even if normal balanced mode has no daily cap。
5. Loss mode SHALL be based on deduplicated outcomes, not raw `closed_trades` count。

### Requirement 5: 改进入场候选与 AI 输出质量

**User story:** 作为量化策略设计者，我希望 AI 主要评估可执行候选，而不是反复输出被确定性 gate 拒绝的交易。

Acceptance criteria:

1. WHEN BTC 1h/4h gate blocks high beta altcoin longs THEN those symbols SHALL either be excluded from prompt candidates or clearly marked non-executable。
2. WHEN the system computes net RR threshold as `(rewardPct - costPct) / riskPct >= 2.5` THEN the prompt SHALL show this exact rule and candidate-level minimum TP/maximum SL guidance。
3. WHEN AI proposes RR below live threshold THEN rejection SHALL include net RR diagnostics and be counted in replay by symbol/side。
4. The candidate prompt SHALL prioritize BTC/ETH/low-correlation symbols or short setups when high beta longs are blocked。

### Requirement 6: 提高出场质量和利润保留

**User story:** 作为交易员，我希望亏损单更早失效、盈利单少回吐，以改善盈亏比和期望。

Acceptance criteria:

1. WHEN MFE reaches a configurable breakeven threshold THEN stop-loss SHALL move to breakeven or a small locked profit if exchange constraints allow。
2. WHEN MFE reaches 1R and min notional supports partial close THEN the system SHALL close a configurable tranche or tighten stop to protect at least part of MFE。
3. WHEN holding time exceeds the no-momentum threshold and MFE remains below threshold THEN the system SHALL close before loss expands to current -3% to -5% patterns。
4. WHEN a position gives back more than a configured portion of MFE with weak short-term momentum THEN it SHALL close or tighten stop before turning materially negative。
5. Partial close rules SHALL respect Aster minimum notional and avoid emitting unexecutable partial orders for small accounts。

### Requirement 7: 保留现有安全边界

**User story:** 作为系统负责人，我希望优化不降低已有硬风控、保护单和交易所安全语义。

Acceptance criteria:

1. Stop-loss and take-profit cancellation semantics SHALL remain split between `CancelStopLossOrders()` and `CancelTakeProfitOrders()`。
2. No tests SHALL place real orders or require live exchange credentials。
3. Runtime credentials SHALL NOT be written to spec docs, test artifacts, or replay output。
4. The implementation SHALL keep trader-scoped state isolated by `trader_id`。
