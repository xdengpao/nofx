# 策略量化优化 — 任务

- [x] 1. 聚合 `aster_deepseek` 全量历史决策日志
- [x] 2. 配对历史开平仓并计算总体、方向、币种表现
- [x] 3. 定位执行层 `partial_close` 小额订单失败原因
- [x] 4. 检查当前开仓 prompt、验证层、持仓执行路径
- [x] 5. 形成优化需求与设计文档
- [x] 6. 从 `AnalyzePerformance` 抽取并增强历史归因模块 `BuildTradeOutcomes`
- [x] 7. 为历史归因模块添加 fixture/单元测试，覆盖 `auto_close_*`、unmatched 和 reasoning 回填
- [x] 8. 修复 `executePartialCloseWithRecord` 的最小名义额保护
- [x] 9. 添加 `partial_close` 小额订单保护测试
- [x] 10. 实现 symbol/side rolling performance gate
- [x] 11. 将 rolling gate 注入 `validateOpenDecision`
- [x] 12. 将动态风险比例接入实际开仓验证
- [x] 13. 扩展 `/api/performance` 输出全历史、rolling 和执行质量指标
- [x] 14. 更新前端策略健康状态展示
- [x] 15. 运行 `go test ./...`
- [x] 16. 运行 `cd web && npm run test && npm run build`
