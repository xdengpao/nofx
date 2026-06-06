# 开仓频率优化 Tasks

## Phase 0: Spec Consistency

- [x] 0.1 复核 `requirements.md` 与 `design.md`，确认默认上线行为是 `balanced`，不是 `active`。
- [x] 0.2 确认旧配置缺少 `trading_frequency` 时保持 15 分钟/原候选数/原 gate 行为。
- [x] 0.3 确认高 ADX、RR、rolling gate 放宽默认均为 report-only，不进入实盘执行。
- [x] 0.4 确认 `active` 自动回退只影响运行时开仓行为，不动态回退候选池 prompt limit。

## Phase 1: 配置与派生档位

- [x] 1.1 在 `config/config.go` 添加 `TradingFrequencyConfig` 和 `Config.TradingFrequency *TradingFrequencyConfig`。
- [x] 1.2 实现 `NormalizeTradingFrequency()`，支持 legacy、safe、balanced、active 四种派生路径。
- [x] 1.3 为分析间隔、候选数量、每日开仓上限和 rollback 参数添加默认值、边界校验和错误信息。
- [x] 1.4 在 `config/config_test.go` 覆盖 legacy 兼容、balanced 默认、active 默认、非法 mode、低于安全下限等场景。
- [x] 1.5 更新 `config.json.example` 和部署说明，给出 `safe`、`balanced`、`active` 示例及 active 回退不改变候选池 prompt limit 的说明。

## Phase 2: 运行时策略注入

- [x] 2.1 在 `decision/types.go` 添加 `FrequencyPolicy` 和 `FrequencyState`。
- [x] 2.2 在 `trader.AutoTraderConfig` 添加 `FrequencyPolicy` 字段，并在 `manager.AddTrader()` 中传入派生策略。
- [x] 2.3 在 `main.initializeModules()` 中使用派生 profile 设置动态候选池 prompt limit。
- [x] 2.4 在 `trader.buildTradingContext()` 中把 frequency policy/state 注入 `decision.Context`。
- [x] 2.5 在 `AutoTrader.GetStatus()` 输出 `frequency_policy` 和 `frequency_state`。
- [x] 2.6 将派生分析间隔传入 `decision.Initialize()` 用于启动日志一致性，或移除初始化日志中的固定间隔描述。

## Phase 3: Balanced 实盘改动

- [x] 3.1 确保 balanced 档将 `AnalysisIntervalMin` 派生为 12 分钟，并由现有 `shouldCallAIForNewOpportunities()` 消费。
- [x] 3.2 确保 balanced 档 prompt 候选数为 10，且候选快照字段保持兼容。
- [x] 3.3 在 `decision.Decision` 添加 `requested_position_size_usd`、`adjusted_position_size_usd`、`sizing_adjusted`、`sizing_reason`、`stop_distance_pct`、`effective_risk_pct` 字段。
- [x] 3.4 在 `logger.DecisionAction` 添加对应 logger-local sizing 字段，避免 logger 依赖 decision 包。
- [x] 3.5 在 `decision.validateOpenDecision()` 中把可缩仓的单笔风险超限改为自动缩小到 `CalculatePositionSizing()` 给出的最大可执行仓位。
- [x] 3.6 为自动缩仓记录 requested size、adjusted size、effective risk、stop distance 和 sizing reason。
- [x] 3.7 保留缩仓后低于最小名义额、保证金不足、保护单不可用时的拒绝行为。

## Phase 4: Report-only 诊断

- [x] 4.1 添加 `OpenFrequencySimulation` 数据结构，并挂到 `decision.OpenRejection`。
- [x] 4.2 为 `OpenFrequencySimulation` 添加 `source` 字段，取值至少包括 `structured` 和 `text_inferred`。
- [x] 4.3 在 `logger.DecisionAction` 或 logger-local DTO 中保存 report-only simulation 结果。
- [x] 4.4 实现高 ADX 50-60 active-style 模拟：confidence >= 85、无硬 BTC block、无 extreme ADX block 时输出 hypothetical pass/fail。
- [x] 4.5 实现 RR 阈值模拟：记录 `RR >= 2.0` 但 `< 2.5` 的候选，不改变 live validation。
- [x] 4.6 实现 rolling gate 风险-only 模拟：记录如果只降仓不提高置信度时的 hypothetical pass/fail。
- [x] 4.7 为 rolling gate 诊断记录样本数、窗口、PnL、Profit Factor 和冷却截止时间；样本不足时只输出风险-only 模拟。
- [x] 4.8 在 `cmd/replay` 或 `logger.BuildReplayReport()` 输出新增可能通过数量、symbol 分布和原拒绝原因变化。
- [x] 4.9 对旧日志 replay 的 report-only 输出标注 best-effort，并在无结构化字段时使用 `source=text_inferred`。

## Phase 5: Active 保护机制

- [x] 5.1 在 logger 增加最近 24 小时成功开仓计数 helper。
- [x] 5.2 在 logger 增加最近 24 小时闭合交易 Profit Factor 和 drawdown 统计 helper。
- [x] 5.3 在 active mode 下，当 24h 新增开仓达到 `DailyOpenLimit` 时跳过 AI 新机会搜索。
- [x] 5.4 在最终决策合并后再次执行每日开仓上限检查，防止多条 open 决策绕过 cap。
- [x] 5.5 将 daily cap 和 final-limit 拒绝记录为结构化 `OpenRejection` 或 `DecisionAction{Action:"open_rejected"}`，不得只写入 CoTTrace 文本。
- [x] 5.6 实现 active 24h 自动回退到 safe 的 runtime effective mode，不写回 `config.json`。
- [x] 5.7 确保 active 自动回退不调用候选池重配置，不改变启动时派生或显式配置的 prompt limit。
- [x] 5.8 在 wait reason、risk state 和 `/api/status` 中输出每日上限或自动回退原因。

## Phase 6: API、日志与前端兼容

- [x] 6.1 扩展 `logger.RiskStateSnapshot`，保存 frequency policy/state 的 logger-local snapshot。
- [x] 6.2 扩展 `/api/status` 响应，包含当前 mode、effective mode、analysis interval、prompt limit、open count 24h、rollback reason。
- [x] 6.3 如果前端类型依赖状态字段，更新 `web/src/types` 和 API client 类型。
- [x] 6.4 确保旧决策日志缺少新字段时 API 和前端仍能读取。

## Phase 7: 测试

- [x] 7.1 运行并修复 `go test ./config`。
- [x] 7.2 运行并修复 `go test ./decision`，覆盖自动缩仓、report-only high ADX、RR simulation、daily cap final limit。
- [x] 7.3 运行并修复 `go test ./trader`，覆盖 policy 注入、status 输出、context 注入。
- [x] 7.4 运行并修复 `go test ./manager`，覆盖 AddTrader 传递 frequency policy。
- [x] 7.5 如改动 API，运行并修复 `go test ./api`。
- [x] 7.6 如改动前端类型或 UI，运行并修复 `cd web && npm run build`。

## Phase 8: Replay 与上线前评估

- [x] 8.1 对最近日志执行 report-only replay：`go run ./cmd/replay -log-dir decision_logs -trader aster_deepseek -from 2026-05-10 -to 2026-05-12 -report-only=true`。
- [x] 8.2 输出 balanced 预计影响：AI 分析次数变化、候选数变化、自动缩仓可能通过数量。
- [x] 8.3 输出 active report-only 预计影响：高 ADX 模拟通过数、RR 模拟通过数、rolling 风险-only 模拟通过数。
- [x] 8.4 若 active 模拟新增候选集中在近期亏损 symbol 或高 beta 山寨，保持 active 不上线。
- [x] 8.5 在 replay 报告中区分 `source=structured` 和旧日志 `source=text_inferred`，避免把文本推断等同于实盘模拟。
- [x] 8.6 记录回滚方式：移除 `trading_frequency` 配置块或设置 `mode=safe`。

## Phase 9: 部署建议

- [x] 9.1 首次部署只添加 `trading_frequency.mode=balanced`。
- [x] 9.2 观察至少 24 小时，记录开仓数、胜率、Profit Factor、回撤和保护单失败率。
- [x] 9.3 仅当 24 小时 Profit Factor >= 1 且无保护单失败时，才考虑继续观察 active report-only。
- [x] 9.4 部署说明注明 active 运行时回退不会动态缩小候选池，需要改配置并重启才会改变 prompt candidate limit。
- [x] 9.5 不在本轮任务中启用禁用 trader、不修改凭证、不提高杠杆。
