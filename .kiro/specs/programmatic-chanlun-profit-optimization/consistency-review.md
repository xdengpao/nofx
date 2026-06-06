# 程序化缠论策略盈利能力优化 Spec 交叉验证

## 复核范围

复核日期：2026-05-20

本次复核覆盖：

- `.kiro/specs/programmatic-chanlun-profit-optimization/requirements.md`
- `.kiro/specs/programmatic-chanlun-profit-optimization/design.md`
- `.kiro/specs/programmatic-chanlun-profit-optimization/tasks.md`
- 当前代码中的 `strategy/chanlun/`、`decision/`、`trader/`、`logger/`、`backtest/`、`historydb/`、`cmd/replay`、`cmd/backtest`
- 相关 steering：`.kiro/steering/product.md`、`.kiro/steering/tech.md`、`.kiro/steering/structure.md`

## 结论

当前需求方向与 NOFX 架构目标一致：只用日志、replay 和回测证据做优化；不旁路 deterministic 风控；运行产物保持 git ignored；测试不触发真实下单。

但 `requirements.md`、`design.md` 和 `tasks.md` 还不能直接进入实现。主要问题是：若干需求依赖的指标、字段和代码路径在现有回测/replay 产物中尚不存在，而设计与任务没有补齐这些前置采集、字段映射和语义对齐工作。建议先修正下列发现，再开始实现 `optimize/` 包。

## Confirmed Alignment

- `strategy/chanlun.Engine`、`backtest.Runner`、`logger.BuildReplayReport()`、`logger.BuildTradeReplay()`、`decision` 风控入口和 `trader.Trader` 抽象均存在。
- `backtest_runs/`、`backtest_data/`、`decision_logs/`、`data/`、`coin_pool_cache/` 已在 `.gitignore` 中忽略。
- `github.com/leanovate/gopter` 已在 `go.mod` 中可用。
- 止损/止盈取消接口在 `trader.Trader` 中已拆分为 `CancelStopLossOrders()` 和 `CancelTakeProfitOrders()`，实盘主路径已有分离调用。
- 当前 backtest 已注入 `Engine.Clock` 和历史行情 provider，基础虚拟时间方向符合 Requirement 8.2。

## Findings

### 1. Gate 和分桶指标缺少现有数据来源

Severity: High

`requirements.md` 要求 Baseline/Candidate 对比记录 data hash、BTC 市场状态、ATR/ADX profile、symbol 类别、side/signal type 细分、最小名义额拒绝率、手续费/滑点占比和置信区间。`design.md` 的 `RunMetrics` 也声明了这些字段。

当前 `backtest.Report` 只有 `Summary`、`BySymbol`、`BySignalType`、`RejectionBuckets` 和文件列表；`Report` 没有 `data_hash`，没有 `BySide`、`ByMarketState`、`ByATRProfile`、`BySymbolCategory`，也没有最小名义额拒绝率字段。`historydb.Store.DataHash()` 存在，但当前 `backtest.Runner` 没有把共同数据 hash 写入 report。

风险：`Optimization_Gate` 会只能填出部分字段，导致 Requirement 2.3、2.4、2.9、6.5、9.5 无法被客观验证。

建议修正：

- 在 design/tasks 中增加 backtest report 扩展任务：写入共同 `data_hash`、按 side/market_state/ATR_ADX/symbol_category 的 bucket metrics、min_notional rejection bucket。
- 明确 `ExtractRunMetrics()` 如何从 `report.json`、CSV 和 marker JSON 恢复完整 `RunMetrics`，而不是只接收 `backtest.Report`。
- 明确 bootstrap/CI 的样本来源和算法；当前 tasks 只写“记录 MetricComparison”，没有实现置信区间。

### 2. Requirement 8.1 与当前 backtest 执行路径冲突

Severity: High

Requirement 8.1 要求 Backtest_Pipeline 使用与实盘相同的 `trader/execution_preflight.go` 和 `trader/exchange_calibration.go`。但当前 `backtest.Runner` 直接调用 `PaperBroker.SubmitDecision()`，`PaperBroker.executeDecision()` 自行处理持仓和余额，不调用 `trader.EvaluateExecutionPreflight()`，也不调用 `trader` 包里的 exchange calibration。

风险：即使 `optimize` 层实现完成，回测和实盘的最小名义额、重复持仓、加仓可执行性仍可能不同，直接破坏“同一口径”前提。

建议修正二选一：

- 保留 Requirement 8.1：在 design/tasks 中增加 `backtest` 执行适配层，把 preflight 与 calibration 抽到可被回测复用的公共入口。
- 或收窄 Requirement 8.1：声明 backtest v1 使用等价 paper preflight，并要求在报告 assumptions 中标出与实盘 `trader` preflight 的差异。

### 3. 信号质量需求超出现有 SignalOutcome 能力

Severity: High

Requirement 3.1 要求对分型、笔、线段、中枢、买卖点、preview、entry trigger 单独输出确认延迟、撤回率、1R 命中率、最终 R 倍数。当前 `backtest.SignalOutcome` 是从 `chanlun.SignalMarker` 压缩得到的信号结果，字段覆盖 signal id/type/time/price，但不包含分型、笔、线段、中枢序列，也没有 `confirm_close_time`、撤回事件、中心 ZG/ZD 或 A/B/C 段边界。`SignalOutcome.MFE/MAE/Reached1R` 字段存在，但当前 `signalOutcomesFromMarkers()` 没有填充它们。

风险：`BuildSignalQualityReport(signals, trades, executions)` 无法按需求计算结构级质量指标，只能做信号 marker 级别的近似统计。

建议修正：

- 若需求保持结构级诊断，design/tasks 需要新增结构快照产物，例如 `structures.json` 或扩展 marker artifacts，包含 fractal/bi/segment/center 生命周期和撤回事件。
- 若本规格只做盈利优化的 MVP，应把 Requirement 3.1 收窄为 `buy/sell/preview/entry_trigger` marker 级质量，并把分型/笔/线段/中枢留给既有缠论结构规格。

### 4. DefectCatalog 缺少 trader/exchange 维度来源

Severity: High

Requirement 1.4 要求按 `(trader_id, symbol, defect_code)` 聚合，Requirement 9.3 要求按 `(trader_id, exchange)` 输出指标。当前 `logger.ReplayReport` 没有顶层 `trader_id` 或 `exchange` 字段；`backtest.Report` 固定是 backtest 视角，也没有真实 exchange 维度。`design.md` 的 `DefectEntry` 和 `EvidenceRef` 只有 `TraderID`，没有 `Exchange`；`BuildDefectCatalog(replayReports []logger.ReplayReport, backtestReports []backtest.Report, ...)` 也没有携带 replay 来源路径或 trader/exchange context。

风险：多 trader、多交易所缺陷会被错误聚合，无法满足 Requirement 9 的兼容性审查。

建议修正：

- 为 `BuildDefectCatalog` 输入增加 `ReplayReportInput{Report, Path, TraderID, Exchange}` 和 `BacktestReportInput{Report, Path, TraderID, Exchange}`。
- 在 `DefectEntry` 或 evidence/group summary 中显式加入 `affected_exchanges`、`sample_count_by_trader_exchange`。
- 对 `cmd/replay` 生成的报告补充来源目录解析或从 `DecisionRecord.RiskState.Exchange` 派生 exchange。

### 5. 决策日志必填字段和 replay 重建上下文未明确映射

Severity: High

Requirement 11.1 要求日志写入 `structure_key`、`parent_signal_id`、`source_layer`、`analysis_timeframe`、`trigger_timeframe`、`age_candles`、`freshness_state`、`reason_code` 等字段。当前 `logger.DecisionAction` 顶层有 `signal_id`、`signal_type`、`signal_timeframe`、`structure_target`、`signal_close_time`、`decision_close_time`、`strategy_version`、`config_hash`，其余字段主要在 `StrategyMetadata` 或 `Explanation` 中出现。Requirement 11.2 还要求 replay 离线重建最近笔/线段/中枢编号、ZG/ZD、A/B/C 边界，但当前 `DecisionAction`、`SignalMarker` 和 `SignalOutcome` 没有完整结构上下文字段。

风险：Property 6 和 Requirement 11 的测试会在“顶层字段 vs metadata 字段”上出现歧义；replay 重建结构上下文也缺少可验证输入。

建议修正：

- 明确日志字段的标准读取路径：顶层字段、`strategy_metadata`、`explanation.details` 三者哪个是 canonical。
- 若要求 replay 重建完整结构上下文，需要在 `SignalMarker`/decision log 中补充 center/segment/bi/fractal 快照引用或嵌入必要字段。
- tasks 需要增加 logger schema、replay parser、API 和前端类型同步任务；当前只在 Requirement 11 写了约束，tasks 没有对应落地。

### 6. 阈值覆盖配置与 git-ignored 运行配置矛盾

Severity: Medium

Requirement 2.8 要求阈值覆盖记录覆盖原因和覆盖人，且不得使用未提交到仓库的本地配置。`design.md` 又把 `optimize_config.json` 设计为位于 `backtest_runs/` 或通过 CLI 传入的 git-ignored 运行时配置。

风险：如果所有阈值配置都来自 git-ignored 文件，就无法满足“不得使用未提交到仓库的本地配置”；如果要求提交配置，又与运行时输出目录设计冲突。

建议修正：

- 拆分 committed policy 与 run snapshot：默认门控阈值和 override policy 放在可提交配置/规格文档中；`backtest_runs/<run_id>/` 只保存运行快照。
- `GateResult` 中保留 `override_reason`、`override_by`、`policy_ref` 或 `commit_hash`，用于证明覆盖配置来源已入库。

### 7. `EvaluateGate` 错误处理接口不一致

Severity: Medium

`design.md` 的接口定义为 `func EvaluateGate(input GateInput) GateResult`，但 Error Handling 又要求基线与候选不可比时返回 `ErrIncomparableRuns`。当前签名无法返回 error。

风险：实现时只能把不可比塞进 `GateResult.Reasons`，与文档承诺的错误行为不一致，CLI 也难以区分 rejected 与 invalid input。

建议修正：

- 改为 `func EvaluateGate(input GateInput) (GateResult, error)`；或
- 明确不可比时 `GateResult{Passed:false, Verdict:"invalid_input"}`，并删除 `ErrIncomparableRuns` 的返回描述。

### 8. Mandatory tests 被标记为 optional

Severity: Medium

Requirement 14.1 要求 Requirement 12 中每条 correctness property 至少一个 `gopter` 属性基测试。但 `tasks.md` 中 2.2、5.2、13.1-13.6 都标记为 `[ ]*`，Notes 又写 `Tasks marked with * are optional and can be skipped for faster MVP`。

风险：实现者可以跳过这些属性测试，但仍声称满足 Requirement 14，导致 traceability 失真。

建议修正：

- 去掉 Requirement 12/14 相关测试任务的 `*`。
- 如果确实需要 MVP，必须同步降低 Requirement 14.1 的 SHALL 级别，改成分阶段验收。

### 9. 属性基测试的包边界需要重新设计

Severity: Medium

Requirement 4.6/12.1 和 tasks 13.6 要求调用 `ValidateAndEnrichDecision`、`validateOpenDecision`、`EvaluateOpenGate`、`CalculatePositionSizing`。其中 `validateOpenDecision` 和 `enforceFinalDecisionLimits` 是 `decision` 包未导出函数；如果测试放在 `optimize` 包中无法直接调用。

风险：PBT 任务照当前描述实现会遇到编译边界，或者被迫绕开关键 gate，削弱验证意义。

建议修正：

- 将相关 PBT 放在 `decision` 包内，或通过导出的 `GetFullDecision`/`ValidateAndEnrichDecision` 组合验证完整行为。
- tasks 中明确每个 property 的测试包位置，避免都落到 `optimize/`。

### 10. Tasks 对 requirements 覆盖不完整

Severity: Medium

当前 tasks 重点实现 `optimize/` 聚合、gate、报告和 CLI，但多条需求只停留在约束文字，缺少实现任务：

- R3.3-R3.7：多周期共振声明、ATR/ADX 复用、频率下降说明、确定性和去重一致性。
- R4.1-R4.5、R4.7：配置化调整、相关性、loss_mode、执行质量门槛引用。
- R5.1-R5.4、R5.6：TradePlan/takeprofit、MFE/MAE 直方图、partial_close 最小名义额、trailing-stop spec、parser 复用。
- R6.1-R6.4、R6.6：position_sizing 入口、波动率仓位、账户风险、相关集中、边界参数。
- R7.3、R7.5：复用 risk/loss_mode、circuit-breaker auto recovery spec。
- R8.1、R8.2、R8.4、R8.6：回测/实盘共享代码路径、虚拟时间报告、三者样本 hash、spec 引用 run_id/data hash。
- R9.1-R9.4：Trader 接口兼容、exchange calibration、按 exchange 输出、执行链路测试。
- R10.1、R10.3、R10.4、R10.6：凭证、Decision_Layer 旁路、行情入口、运行目录不提交。
- R11.2-R11.5：replay 结构重建、schema/API/frontend 同步、人工复盘记录。
- R13.3-R13.4：通过现有配置开关回滚 baseline 行为、监控指标查询路径。

风险：任务完成后仍不能证明 spec 已满足。

建议修正：

- 为每条 requirement 建立 traceability table，标注 design section 和 task id。
- 对纯约束型 requirement，增加“proposal template/checklist”和“review gate”任务，而不是只在正文里声明 SHALL。

## Recommended Amendments Before Implementation

1. 先修正 `design.md` 的数据模型：补齐 run artifact loader、data hash、trader/exchange context、bucket metrics、bootstrap/CI、日志字段 canonical path。
2. 决定 Requirement 8.1 的取舍：是让 backtest 复用 trader preflight/calibration，还是把要求收窄为等价 paper execution 并显式声明差异。
3. 收窄或扩展 Requirement 3.1：要么只做 marker 级信号质量，要么新增结构级快照产物。
4. 把 mandatory correctness/PBT 任务从 optional 改成必做，并明确测试包位置。
5. 增加需求-设计-任务 traceability table，补齐 R3-R13 中现在没有 task 的约束。

## Recommendation

暂不进入代码实现。建议先更新 requirements/design/tasks，把上述 High 和 Medium finding 消化后，再开始 `optimize/` 包和 `cmd/optimize` 的实现。
