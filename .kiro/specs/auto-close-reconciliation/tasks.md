# Auto-Close Reconciliation Tasks

- [x] 移动 `lastPositions` 更新时间点，避免当前周期覆盖上一周期快照
- [x] 为快照自动平仓计算 PnL 并推断 close reason
- [x] 确保自动平仓调用完整持仓关闭回调并移除交易计划
- [x] 添加回归测试：消失的 short 仓位生成 `auto_close_short` 并移除计划
- [x] 添加重启遗留计划清理：无当前持仓的 ACTIVE plan 合成自动平仓并移除
- [ ] 部署后验证 161 的 ETH 残留计划被清理或不再产生新的残留
