# 移动止损优化 - 任务

- [x] 1. 登录 `43.133.65.217` 并定位 nofx 项目路径 `/home/ubuntu/appai2/nofx`
- [x] 2. 阅读移动止损相关代码：`decision/takeprofit.go`、`trader/auto_trader.go`、`decision/persistence.go`
- [x] 3. 交叉验证最近运行日志，确认 `update_stop_loss` 重复触发问题
- [x] 4. 形成移动止损算法合理性评估
- [x] 5. 生成 requirements/design/tasks spec 文档
- [x] 6. 修复 `executeUpdateStopLossWithRecord()` 成功后未调用 `decision.OnStopLossUpdated()`
- [x] 7. 修复 `executePartialCloseWithRecord()` 正常成功后未调用 `decision.OnPartialClose()`
- [x] 8. 为部分平仓缺失 `new_stop_loss` 的情况补原有效止损保护
- [x] 9. 修正保本检查日志和实际阈值不一致的问题
- [ ] 10. 抽取移动止损 metrics，区分未杠杆价格涨跌幅和杠杆后 PnL
- [ ] 11. 增加最小改单幅度和止损调整冷却
- [x] 12. 添加 `update_stop_loss` 成功/失败状态同步测试
- [x] 13. 添加 `partial_close` 分批档位和剩余保护单测试
- [ ] 14. 添加移动止损单调性、ATR 安全距离、保本阈值测试
- [ ] 15. 在可用 Go 环境中运行完整 `go test ./decision ./trader`
- [ ] 16. 如修改执行层，重启服务并检查日志不再重复提交同一止损调整
