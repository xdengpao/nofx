# 程序化策略减仓约束、原因说明与信号展示 Tasks

## Phase 1: 配置与运行时策略

- [x] 在 `config.ProgrammaticPositionManagementConfig` 中新增 `partial_close_cooldown_minutes`、`max_partial_close_count_per_position`、`max_total_partial_close_pct` 和 `structure_break.partial_close_guard_action`。
- [x] 将 `partial_close_cooldown_minutes` 设计为 `*int`，确保缺失值默认 15，显式 `0` 表示关闭跨规则冷却。
- [x] 在 `ProgrammaticPositionManagementProfile` 和 `decision.ProgrammaticPositionManagementPolicy` 中新增 `ProgrammaticPartialCloseGuard` 运行时字段。
- [x] 更新 `NormalizeProgrammaticStrategies()`：校验 cooldown `0-1440`、max count `1-10`、max total pct `1-100`，并按人类百分数解析累计比例。
- [x] 更新 `manager.decisionProgrammaticStrategyPolicy()`，把 guard 配置完整传入 `AutoTrader` 和 `chanlun.Engine`。
- [x] 更新 `config.json.example`，补充生产推荐值和 `structure_break.partial_close_guard_action` 示例。

## Phase 2: 状态持久化与 marker 持久化

- [x] 在 `ProgrammaticPositionState` 中新增 `PartialCloseGuardState`，保存最近 partial close、累计次数、累计原始仓位比例、初始跟踪数量、最近数量和浮盈回撤 peak 锁定信息。
- [x] 在 `ProgrammaticSymbolState` 中新增 bounded `RecentSignalMarkers`，用于后端重启或前端刷新后恢复最近信号标记。
- [x] 新增 StateStore helper：`HasPositionSignal`、`MarkPositionSignal`、`RecordProgrammaticPartialClose`、`RecordProgrammaticFullClose`、`ResetPositionGuardIfMissing`。
- [x] 新增 StateStore marker helper：存储 marker、更新 marker status、按 symbol 读取最近 markers，并按 `signal_id + timeframe + close_time` 去重。
- [x] 保持旧 `programmatic_strategy_state.json` 兼容，缺少新字段时按空状态读取。
- [x] 限制每个 symbol 最近 marker 数量，默认最多保留 200 条，避免状态文件无限增长。

## Phase 3: 持仓管理 partial_close guard

- [x] 重构 `evaluatePositionManagement()`：评估阶段只检查 `HasPositionSignal`，不得在候选生成时 eager mark 程序化 partial close signal。
- [x] 实现 `applyPartialCloseGuard()`，对 `short_trade` 和 `floating_drawdown` 应用冷却、次数上限和累计原始仓位比例上限。
- [x] 实现预算裁剪：按 `InitialTrackedQuantity` 和 `TotalPartialCloseQuantity` 计算剩余预算，而不是简单累加请求百分比。
- [x] 实现预算估算 fallback：当无法获得实际成交数量时，用最近持仓数量与请求比例估算，并在 explanation 中标记 `estimated=true`。
- [x] 实现 `floating_drawdown` 执行 partial close 后要求出现新 peak 才可二次触发。
- [x] 实现新 peak 解锁：刷新有利 peak 时清除 drawdown 锁和对应已处理状态。
- [x] 实现 `structure_break` guard 行为：`respect_guard`、`bypass_cooldown_clip_budget`、`close_on_budget_exhausted`。
- [x] 确保 `structure_break.action=close`、公共强制风控和 `breakeven update_stop_loss` 不受 partial close guard 阻断。

## Phase 4: 执行结果回写

- [x] 新增 `chanlun.ProgrammaticExecutionResult` 和 `Engine.OnExecutionResult()`。
- [x] 在 `AutoTrader` 执行每个程序化动作后回写执行结果；失败不得消耗去重或 partial close 预算。
- [x] 扩展 `logger.DecisionAction`，新增 `requested_close_percentage`、`executed_close_percentage`、`final_action`、`close_quantity`。
- [x] 更新 `executePartialCloseWithRecord()`，在正常部分平仓、小额跳过、自动修正全平三条路径都写明 final action 和实际执行比例。
- [x] 执行成功后再调用 `MarkPositionSignal()` 和预算记录；validation reject 或 exchange failure 只更新 marker status/diagnostics。
- [x] full close 或持仓方向变化后清理对应 side 的 guard 状态。

## Phase 5: 原因说明与日志

- [x] 新增 `decision.DecisionExplanation`，包含 summary、layer、rule、reason_code、timeframe、signal_id、trigger/reference price、threshold、cooldown_status、budget_status、risk_checks 和 details。
- [x] 在 `decision.Decision` 与 `logger.DecisionAction` 增加可选 `explanation` 字段。
- [x] 更新 `applyDecisionSizingToActionRecord()`，把 explanation 复制到执行记录。
- [x] 为程序化主信号层动作填充 explanation。
- [x] 为 `breakeven`、`floating_drawdown`、`structure_break`、`short_trade` 填充持仓管理 explanation。
- [x] 将冷却阻断、预算耗尽、预算裁剪、缺少新 peak、最小名义额跳过等原因写入 diagnostics 和 explanation。
- [x] 更新前端决策卡文案：程序化模式显示 `策略分析`，AI 模式继续显示 `AI思维链分析`。
- [x] 更新 `web/src/i18n/translations.ts`，新增 `strategyAnalysis` 中英文翻译键。

## Phase 6: 信号 marker 与 API

- [x] 扩展 `chanlun.ChanlunSignal`：新增 trigger close time、segment start/end time、source layer、status。
- [x] 新增 `chanlun.SignalMarker`，包含 symbol、timeframe、close_time、signal_type、direction、level、source_layer、status、signal_id、action、price、reason。
- [x] 扩展 `SignalReport`：新增 `trade_timeframe`、`component_timeframe`、`micro_timeframe`、`signal_markers`。
- [x] 主级别信号生成时填充 trigger close time，并生成 `main_signal` marker。
- [x] 持仓管理动作生成或执行回写时生成/更新 `position_management` marker。
- [x] 新增 `Engine.EmptySignalReport()` 或等价接口，确保无内存 report 时仍返回 timeframe 元数据。
- [x] 更新 `AutoTrader.GetLatestStrategySignals()` 和 `TraderManager.GetLatestStrategySignals()`，由 engine 提供空报告和 timeframe，不在 manager 层猜测。
- [x] 更新 `/api/market/klines` timeframe 白名单校验，仅允许 `3m/15m/1h/4h`，非法值返回中文 400。

## Phase 7: 前端策略检查

- [x] 更新实际导入的 `web/src/types.ts`，补齐 explanation、signal marker、动态 timeframe 和新增 action 字段。
- [x] 同步更新 `web/src/types/index.ts`，或明确合并重复类型定义，避免类型漂移。
- [x] 更新 `TraderDashboard` SWR key 和 `api.getMarketKlines()` 调用，根据 `strategySignals.trade_timeframe` 请求 `15m/1h/4h` K 线。
- [x] 更新 `StrategyInspector`，K 线表标题动态显示 `${timeframe} K线`。
- [x] K 线表新增 `信号` 列，并按 `close_time + timeframe` 渲染 `B1/B2/B3`、`S1/S2/S3` marker。
- [x] 区分主信号和持仓管理信号 marker 样式，区分 detected/executed/rejected/deduped/failed 状态。
- [x] 最新信号栏展示最近主信号；无主信号时显示诊断；有持仓管理 marker 时显示 compact PM 行。
- [x] 保持 AI 模式兼容，不请求或展示程序化 marker。
- [x] 保持窄屏横向滚动，不让 marker badge 撑乱表格。

## Phase 8: 后端测试

- [x] 更新 `config/config_test.go`：覆盖 guard 默认值、显式 cooldown=0、合法边界、非法值和 config hash 变化。
- [x] 更新 `strategy/chanlun` 状态测试：旧状态兼容、guard state 持久化、marker bounded 存储、重启恢复。
- [x] 更新 `strategy/chanlun` 策略测试：`short_trade` 后下一周期 `floating_drawdown` 被冷却阻断。
- [x] 更新 `strategy/chanlun` 策略测试：新 peak 解锁 `floating_drawdown`。
- [x] 更新 `strategy/chanlun` 策略测试：累计原始仓位预算裁剪、预算耗尽跳过、结构破坏豁免/升级。
- [x] 更新 `decision`/`trader` 测试：公共 close 压制程序化 partial close，失败执行不消耗预算，自动 full close 清理状态。
- [x] 更新 `api` 或 `manager` 测试：空 signal report 返回 timeframe，`/api/market/klines` 非法 timeframe 返回 400。

## Phase 9: 前端与集成验证

- [x] 运行 `go test ./config ./strategy/chanlun ./decision ./trader ./api ./manager`。
- [x] 运行 `go build ./...`。
- [x] 运行 `cd web && npm run build`。
- [ ] 手动检查策略检查页面：动态 timeframe、marker 渲染、程序化策略文案、AI 模式 fallback。
- [ ] 若执行部署，验证 `/health`、`/api/traders`、`/api/status?trader_id=...`、`/api/strategy/signals`、`/api/market/klines`。
