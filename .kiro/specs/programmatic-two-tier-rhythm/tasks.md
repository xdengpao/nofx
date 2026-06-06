# 程序化策略双层节奏 Tasks

## Phase 1: 配置与运行时策略

- [x] 在 `config.ProgrammaticStrategyConfig` 中新增 `position_management` 配置结构，覆盖 enabled、timeframes、breakeven、floating_drawdown、structure_break、short_trade。
- [x] 在 `decision.ProgrammaticStrategyPolicy` 中新增对应的 `ProgrammaticPositionManagementPolicy` 运行时结构。
- [x] 更新 `config.NormalizeProgrammaticStrategies()`，归一化持仓管理默认值，并校验百分比、R 倍、confirm_bars、action 和 timeframe。
- [x] 为持仓管理百分比字段实现字段语义明确的归一化：`trigger_profit_pct=1.0` 表示 1%，`buffer_pct=0.05` 表示 0.05%，`drawdown_pct=35` 表示 35%。
- [x] 收紧 `programmatic_strategy.timeframes.trade` 校验，仅允许 `15m`、`1h`、`4h`。
- [x] 实现 `trade` 默认组件级别推导：`15m -> 3m`、`1h -> 15m`、`4h -> 1h`，保留显式配置覆盖。
- [x] 更新 `manager.decisionProgrammaticStrategyPolicy()`，把新的持仓管理配置传入 `AutoTrader` 和 `chanlun.Engine`。
- [x] 更新 `config.json.example`，增加 `position_management` 示例，并说明生产默认可继续使用 `trade=1h`。

## Phase 2: 程序化状态持久化

- [x] 在 `strategy/chanlun/state.go` 中新增 side 级 `ProgrammaticPositionState`，保存 peak、PeakR、各类已处理 signal id 和 LastManagedAt。
- [x] 扩展 `ProgrammaticSymbolState`，新增 `PositionStates map[string]ProgrammaticPositionState`，兼容旧状态文件缺失字段。
- [x] 新增 StateStore helper：读取/更新 position state、记录 peak、记录已处理保本/回撤/结构破坏/短差信号。
- [x] 实现持仓管理 `SignalID` 确定性生成 helper，覆盖 breakeven、floating_drawdown、structure_break、short_trade。
- [x] 确保 `LastAnalyzedClosedKline` 仍按 `symbol + timeframe` 隔离，仅由主级别信号层更新和读取。
- [x] 为状态读写补单元测试，覆盖旧状态文件、空状态、side 隔离和 signal id 去重。

## Phase 3: 引擎双层节奏重构

- [x] 重构 `strategy/chanlun.Engine.GetFullDecision()`，拆成持仓管理层和主级别信号层。
- [x] 扩展 `decision.PrepareCycleContext()` 的程序化调用语义，支持 `AllowRiskReducingOnHalt`、`RiskIncreaseBlocked` 和 `FullStop`。
- [x] 将现有 `analyzeSymbol()` 拆为主级别信号分析函数，只负责 open/add 候选。
- [x] 实现主级别信号层：无新闭合 `trade` K 线时只跳过 open/add，不影响持仓管理层。
- [x] 实现持仓管理层入口，只遍历 `ctx.Positions`，每个 `scan_interval` 周期都运行。
- [x] 确保当前持仓 symbol 即使不在候选池中，也被加入行情拉取和持仓管理评估范围。
- [x] 调整程序化策略内部合并顺序：持仓管理决策优先，主级别 open/add 决策随后。
- [x] 实现 risk-increase blocked 流程：禁止程序化 open/add，但继续运行公共持仓决策和程序化风险降低型持仓管理动作。
- [x] 实现 full stop 流程：行情、账户或上下文不可安全评估时整体返回 wait/full stop。

## Phase 4: 持仓管理规则

- [x] 实现保本止损规则：达到 profit pct 或 R 倍阈值后生成 `update_stop_loss`，且不得降低保护效果。
- [x] 实现浮盈回撤规则：达到激活阈值后按 peak 浮盈回撤比例生成 `partial_close` 或 close。
- [x] 实现结构破坏规则：基于已闭合 `15m` 或连续已闭合 `3m` K 线确认结构位破坏。
- [x] 实现短差减仓规则：已有多仓遇到 `sell2/sell3`、已有空仓遇到 `buy2/buy3` 时可按比例 `partial_close`。
- [x] 所有持仓管理动作写入 `StrategyMode`、`StrategyName`、`StrategyVersion`、`ConfigHash`、`SignalID` 和 `StrategyMetadata.layer/rule`。
- [x] 所有持仓管理动作使用确定性 `SignalID` 并写入对应 rule 的去重状态。
- [x] 同一持仓同一周期最多输出一个主策略管理动作，避免 `partial_close`、`close`、`update_stop_loss` 互相冲突。
- [x] 持仓管理层不得输出 `open_long`、`open_short`、`add_long`、`add_short`。
- [x] 持仓管理层不得输出 `update_take_profit`，止盈更新和交易计划同步继续由公共层负责。

## Phase 5: 诊断、日志与可观测性

- [x] 新增分层诊断结构，区分 `main_signal` 和 `position_management`。
- [x] 聚合无新闭合 K 线日志，避免每个无持仓候选都输出一条 `无新闭合K线`。
- [x] 在 `CoTTrace` 中明确表达：主信号层等待新闭合 K 线，持仓管理层仍已评估。
- [x] 记录下一根 `trade` 级别 K 线预计闭合时间。
- [x] 决策日志中记录持仓管理评估数量、触发动作数量和主要跳过原因。
- [x] 保持 `/api/decisions/latest`、`/api/status`、`/api/strategy/signals` 兼容；新增诊断字段只作为可选字段。

## Phase 6: 决策合并与执行安全

- [x] 新增 `decision.ValidateRiskReducingStrategyDecisions()` 或等价函数，校验程序化 close/partial/update_stop_loss 的持仓存在、方向匹配、比例范围、止损保护效果和必需策略元数据。
- [x] 更新程序化决策校验流程：风险降低动作走非 open-like 校验，open/add 走现有 `ValidateStrategyDecisions()`。
- [x] 更新 `decision.MergePublicAndStrategyDecisions()` 或新增合并 helper，公共层 close/partial/update_stop_loss/update_take_profit 仍阻断同 symbol 的程序化 open/add。
- [x] 实现公共层与程序化非 open-like 冲突消解：公共 close 压制程序化持仓动作，公共 partial 压制程序化 partial/close/stop，双 stop 保留保护效果更强且方向合法的一条。
- [x] 确认 `sortDecisionsByPriority()` 的顺序满足 close/partial > update stop/tp > add > open > hold/wait。
- [x] 确认 `update_stop_loss` 仍走 `executeUpdateStopLossWithRecord()`，保留利润不足时拒绝移动到保本的现有保护。
- [x] 确认 `partial_close` 仍走现有最小名义额、剩余仓位和交易计划更新逻辑。
- [x] 实现全局熔断或账户硬停时禁止新增 open/add，但允许公共层和风险降低型动作在行情可用时继续执行。

## Phase 7: 测试覆盖

- [x] 更新 `config/config_test.go`：覆盖 `trade=15m/1h/4h` 成功、`trade=3m` 失败、默认组件级别推导和持仓管理配置归一化。
- [x] 更新 `config/config_test.go`：覆盖 `buffer_pct=0.05` 归一化为 0.05%，`drawdown_pct=35` 归一化为 35%。
- [x] 新增或更新 `strategy/chanlun` 测试：无新 `trade` 闭合且无持仓时不输出 open/add。
- [x] 新增或更新 `strategy/chanlun` 测试：无新 `trade` 闭合但有持仓时，持仓管理层仍被调用。
- [x] 新增或更新 `strategy/chanlun` 测试：有新 `trade` 闭合时主信号层分析并更新 `LastAnalyzedClosedKline`。
- [x] 新增或更新 `strategy/chanlun` 测试：切换 `trade=15m/1h/4h` 时已分析 K 线状态互不干扰。
- [x] 新增或更新 `strategy/chanlun` 测试：risk-increase blocked 时不输出 open/add，但持仓管理层仍可输出风险降低动作。
- [x] 新增保本止损测试：达到阈值输出 `update_stop_loss`，未达阈值只记录诊断。
- [x] 新增浮盈回撤测试：达到 peak 回撤阈值输出 `partial_close` 或 close。
- [x] 新增结构破坏测试：已闭合 `15m` 破位触发，未闭合 K 线不触发。
- [x] 新增结构破坏测试：连续已闭合 `3m` 破位触发。
- [x] 新增短差减仓测试：同一 signal id 不重复触发。
- [x] 新增持仓管理 signal id 测试：保本、回撤、结构破坏和短差 signal id 稳定且可去重。
- [x] 更新 `decision` 或 `trader` 测试：公共层动作阻断程序化 open/add，执行排序保持不变。
- [x] 更新 `decision` 测试：公共层 close/partial/update_stop_loss 与程序化非 open-like 动作冲突时按规则消解。
- [x] 更新 `decision` 测试：非 open-like 校验拒绝无持仓、方向不匹配、比例非法和止损降低保护效果的程序化动作。

## Phase 8: 文档与示例

- [x] 更新 `.kiro/specs/programmatic-two-tier-rhythm/design.md` 中的最终字段名，确保和实现一致。
- [x] 更新 `config.json.example` 注释，说明 `trade` 支持 `15m/1h/4h`，不同级别效果由后续回测验证。
- [x] 如前端类型需要展示新增诊断字段，更新 `web/src/types/index.ts`，保持字段可选。
- [x] 如 API client 需要类型补充，更新 `web/src/lib/api.ts`，不得破坏旧字段。

## Phase 9: 验证

- [x] 运行 `go test ./config`。
- [x] 运行 `go test ./strategy/chanlun`。
- [x] 运行 `go test ./decision`。
- [x] 运行 `go test ./trader`。
- [x] 运行 `go test ./api ./manager`。
- [x] 若更新前端类型或展示，运行 `cd web && npm run build`。
- [x] 使用本地或测试配置确认程序化日志显示“主信号层等待新闭合K线，持仓管理已评估”。

## Phase 10: 部署准备

- [x] 确认生产 `config.json` 可继续保持 `trade=1h`。
- [x] 确认不提交 `data/programmatic_strategy_state.json`、`decision_logs/`、真实密钥或服务器运行时配置。
- [x] 部署前备份服务器 `config.json`。
- [x] 部署后验证 `/health`、`/api/traders`、`/api/status?trader_id=...`。
- [x] 部署后检查首个程序化周期日志，确认无新闭合 K 线不再阻断已有持仓管理诊断。
