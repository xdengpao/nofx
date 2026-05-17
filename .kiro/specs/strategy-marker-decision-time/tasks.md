# 策略信号标记确认时间与展示锚点 Tasks

## Phase 1: 后端时间字段与主信号链路

- [x] 1. 在 `strategy/chanlun/types.go` 为 `ChanlunSignal` 增加 `signal_close_time`、`decision_close_time` 字段，并保持 `trigger_close_time` 作为结构时间兼容字段。
- [x] 2. 在 `strategy/chanlun/types.go` 为 `SignalMarker` 增加 `signal_close_time`、`decision_close_time`、`display_close_time` 字段，保持 `close_time` 表示结构信号时间。
- [x] 3. 在 `strategy/chanlun/signals.go` 的 `buildSignal()` 中补齐 `SignalCloseTime=segment.EndTime`，并让 `TriggerCloseTime` 继续等于结构时间。
- [x] 4. 在 `strategy/chanlun/engine.go` 的 `analyzeMainSignal()` 中用 trade timeframe 的 `lastClosed` 补齐每个信号的 `DecisionCloseTime`，并处理 `decision_close_time < signal_close_time` 的诊断和回退。
- [x] 5. 更新 `signalToMainDecision()`，把 `signal_close_time`、`decision_close_time`、legacy `trigger_close_time`、`segment_start_time`、`segment_end_time`、`trade_intent` 写入 `StrategyMetadata` 和 `DecisionExplanation.Details`。

## Phase 2: marker 生命周期与拒绝元数据

- [x] 6. 更新 `signalToMarker()`，让纯检测 marker 的 `CloseTime`、`SignalCloseTime`、`DisplayCloseTime` 都锚定结构信号时间，且不强制生成决策点。
- [x] 7. 更新 `decisionToMarker()`，从 `StrategyMetadata` 读取结构时间和决策时间，交易动作 marker 使用 `decision_close_time` 作为 `display_close_time`。
- [x] 8. 增加通用 metadata helper，支持从多个候选 key 读取 int64 时间字段，并兼容 JSON 反序列化后的 `float64`。
- [x] 9. 更新 `markRejectedStrategyDecisions()`，拒绝原因优先按 `signal_id` 匹配，回退到 `symbol|action`，避免多信号串原因。
- [x] 9a. 更新 `StateStore` marker upsert 或等价生命周期合并逻辑，防止后续纯 detected marker 覆盖已经 rejected/executed/failed/deduped 的同一逻辑 marker。
- [x] 10. 在 `decision/types.go` 扩展 `OpenRejection`，加入策略版本、`signal_id`、`signal_type`、`signal_timeframe`、`signal_close_time`、`decision_close_time`、`trade_intent`、`strategy_metadata`。
- [x] 11. 在 `decision/decision.go` 增加 `NewOpenRejectionFromDecision()` 或等价 helper，并让 `buildOpenRejection()` 使用它保留信号元数据。
- [x] 12. 替换 `ValidateStrategyDecisions()`、`enforceFinalDecisionLimits()`、程序化 risk-increase-blocked 路径中的裸 `OpenRejection{...}`，确保程序化开仓/加仓拒绝不丢失信号元数据。

## Phase 3: 决策日志落盘

- [x] 13. 在 `logger/decision_logger.go` 的 `DecisionAction` 增加 `trade_intent`、`signal_close_time`、`decision_close_time` 字段。
- [x] 14. 更新 `trader/auto_trader.go` 的 `applyDecisionSizingToActionRecord()`，从 `Decision` 和 `StrategyMetadata` 写入新增时间字段与 `trade_intent`。
- [x] 15. 更新 `appendOpenRejectionsToRecord()`，将 `OpenRejection` 中的策略元数据、两类时间、交易意图、gate 诊断完整写入 `DecisionAction`。
- [x] 16. 确认 `reportProgrammaticExecutionResult()` 和 `OnExecutionResult()` 在 executed/failed 回写 marker 时继续保留新增时间字段，不改变下单执行语义。

## Phase 4: 前端类型与 marker 工具函数

- [x] 17. 同步更新 `web/src/types.ts` 和 `web/src/types/index.ts`，为 `ChanlunSignal`、`SignalMarker`、`DecisionAction` 增加新增字段。
- [x] 18. 在 `web/src/utils/strategyMarkers.ts` 增加 `VisualSignalMarker`、`VisualMarkerKind` 类型，以及 `resolveSignalCloseTime()`、`resolveDecisionCloseTime()`、`resolveDisplayCloseTime()`、`isActionMarker()` helper。
- [x] 19. 将 K 线匹配函数改为 `markerBelongsToKlineAt(anchorCloseTime, kline)`，统一毫秒归一化并允许 `<=1000ms` close time 容差，避免使用宽范围匹配。
- [x] 20. 实现 `expandVisualMarkers(markers, klines)`，支持结构点/决策点成对派生、同时间合并、单侧可见、配对时间出当前图表范围的状态。
- [x] 21. 增加 marker 稳定排序和分层 helper，确保同一 K 线卖点向上排列、买点向下排列，刷新后顺序稳定。

## Phase 5: 蜡烛图展示优化

- [x] 22. 更新 `web/src/components/StrategyCandlestickChart.tsx`，从直接渲染逻辑 `SignalMarker` 改为渲染 `VisualSignalMarker`。
- [x] 23. 更新 hover state、tooltip 和最新信号侧栏，展示 marker 类型、买卖点分类、交易意图、状态、结构点时间、确认/决策时间、`signal_id` 和原因。
- [x] 24. 更新 `MarkerGlyph`，让结构点使用“结构点/信号点”语义，决策点显示交易意图和状态，例如 `S2 · 开空 · 已拒绝`。
- [x] 25. 调整 marker Y 轴布局，确保同一 K 线同侧多个 marker 按层次排列，不互相遮挡。
- [x] 26. 视实现复杂度增加同一 `signal_id` 结构点与决策点之间的轻量虚线连接；如不绘制连接线，tooltip 必须清楚显示配对时间。

## Phase 6: 测试覆盖

- [x] 27. 增加或更新 `strategy/chanlun` 测试，覆盖 `segment.EndTime < lastClosed` 时信号和 marker 同时包含结构时间与决策时间。
- [x] 28. 增加或更新 `strategy/chanlun` 测试，覆盖 rejected marker 保留 `signal_id`、`signal_type`、`trade_intent`、拒绝原因和两类时间，并覆盖重复 detected 不会降级已处理 marker。
- [x] 29. 增加或更新 `decision` 测试，覆盖 `OpenRejection` 从程序化 `Decision` 复制信号元数据和 gate 诊断。
- [x] 30. 增加或更新 `trader` 测试，覆盖 `appendOpenRejectionsToRecord()` 将新增字段写入 `DecisionAction`。
- [x] 31. 增加或更新 `web/src/utils/strategyMarkers.test.ts`，覆盖 action marker 使用 `display_close_time/decision_close_time` 匹配 K 线。
- [x] 32. 增加或更新前端测试，覆盖纯检测 marker 使用 `signal_close_time/close_time`、结构/决策成对派生、同时间合并、单侧可见和分层排序。

## Phase 7: 验证与回归

- [x] 33. 运行 `go test ./strategy/chanlun ./decision ./trader ./logger`，修复失败后复跑。
- [x] 34. 运行 `go test ./api`，确认策略信号 API 和兼容响应测试通过，修复失败后复跑。
- [x] 35. 运行 `cd web && npm run test`，修复失败后复跑。
- [x] 36. 运行 `cd web && npm run build`，确认 TypeScript 和生产构建通过。
- [x] 37. 本地或 161 环境复核 BCHUSDT 类似场景：结构点显示在原始 K 线，决策/拒绝动作显示在最新闭合 K 线，两者通过同一 `signal_id` 可对账。
- [x] 38. 自查不提交或修改生产 `config.json`、`data/`、`decision_logs/`、`coin_pool_cache/`，不引入真实密钥或真实下单测试。
