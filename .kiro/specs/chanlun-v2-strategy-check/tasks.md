# 缠论 V2 策略检查任务

- [x] 1. 为 `chanlunv2.Engine` 增加策略检查缓存、标的池和 v1 兼容信号报告。
- [x] 2. 将 v2 信号和持仓管理动作转换为前端可绘制的 `ChanlunSignal` / `SignalMarker`。
- [x] 3. 在 `AutoTrader`、`TraderManager` 和配置传递链路中接入 v2 策略检查方法与 K 线深度。
- [x] 4. 调整前端 Trader 详情页，使 `chanlun_v2` 也启用策略检查 UI。
- [x] 5. 补充后端测试覆盖 v2 空报告、过滤参数、K 线深度和信号转换。
- [x] 6. 运行针对性 Go 测试和前端构建，记录验证结果。
