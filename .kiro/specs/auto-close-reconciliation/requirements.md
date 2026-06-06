# Auto-Close Reconciliation Requirements

## 背景

当交易所侧止损/止盈保护单触发后，持仓会在下一轮查询中消失。系统此前只撤销孤儿委托单，没有把该事件记录为 `auto_close_*`，也没有移除对应交易计划，导致：

- `trade_plans.json` 中已平仓标的仍为 `ACTIVE`
- 绩效分析把开仓标记为 `missing_close`
- 前端当前持仓与本地交易计划状态不一致

## 需求

1. 系统必须在发现“上一周期存在、当前周期消失”的持仓时生成自动平仓动作。
2. 自动平仓动作必须包含 symbol、side、exit price、quantity、leverage 和 close reason。
3. 系统必须在自动平仓后移除对应 trader/symbol/side 的交易计划。
4. 自动平仓动作必须进入决策日志，使绩效分析可以配对开仓和平仓。
5. 快照检测不得被当前周期持仓快照提前覆盖。
6. 重启后如果本地仍有 ACTIVE 交易计划但交易所已无对应持仓，系统必须清理该 stale plan 并记录自动平仓。
