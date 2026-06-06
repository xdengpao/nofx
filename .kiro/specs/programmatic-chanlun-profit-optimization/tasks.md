# Implementation Plan: 程序化缠论策略盈利能力优化

## Overview

本实现计划先补齐可验证证据，再实现优化工具。阶段顺序为：回测产物与执行口径补齐、`optimize` 基础类型与 artifact loader、缺陷诊断、指标与 Gate、信号/资金曲线分析、CLI/报告/灰度、必做属性基测试、最终验证。

所有 `gopter` correctness property 任务均为必做项，不标记 optional。运行时产物继续写入 git ignored 的 `backtest_runs/`、`decision_logs/`、`data/`、`coin_pool_cache/`。

## Tasks

- [x] 1. 补齐 Backtest Run_Artifacts 和执行一致性前置能力
  - [x] 1.1 扩展 `backtest.Report` 和 artifacts schema
    - 在 `backtest/report.go` 中新增 additive 字段：`data_hash`、`data_hashes`、`trader_id`、`exchange`、`by_side`、`by_market_state`、`by_atr_profile`、`by_adx_range`、`by_symbol_category`、`min_notional_rejects`、`circuit_breaker_events`
    - 保持旧 `report.json` 可反序列化，新字段使用 `omitempty`
    - 更新 `WriteArtifacts()` 文件列表，预留 `structures.json`、`metrics.json`
    - _Requirements: 2.1, 2.2, 2.3, 2.10, 8.7_

  - [x] 1.2 实现共同 `data_hash`
    - 在 `backtest.Runner` 中使用 `historydb.Store.DataHash()` 计算每个 `(source, symbol, timeframe, warmup_from, backtest_to)` 的 hash
    - 以稳定排序合并为 run-level `data_hash`
    - 写入 `report.json`、`metrics.json` 和 `config_snapshot.json`
    - _Requirements: 2.3, 2.4, 8.5, 8.6, 8.7_

  - [x] 1.3 导出或抽取交易所最小名义额校准公共入口
    - 将 `trader/exchange_calibration.go` 中 open/partial close 最小名义额查询改为可被 `backtest` 复用的无副作用 helper
    - 保持现有 Binance、Aster、Hyperliquid 语义和测试不变
    - 不修改 `trader.Trader` 接口，除非另有明确设计变更
    - _Requirements: 6.7, 8.1, 8.2, 9.1, 9.2_

  - [x] 1.4 让 `backtest.PaperBroker` 复用执行 preflight 和 calibration
    - 在 open/add/partial_close 前调用 `trader.EvaluateExecutionPreflight()` 或抽取后的公共 helper
    - 使用 exchange-aware 最小名义额阈值生成与实盘一致的 rejection reason code
    - 将 preflight / min_notional 拒绝写入 `rejections.csv`、`report.json` 和后续 `metrics.json`
    - _Requirements: 2.2, 6.5, 6.6, 6.7, 8.1, 8.4, 9.5_

  - [x] 1.5 输出 `structures.json`
    - 从 `strategy/chanlun` signal/marker/StateStore 中生成 `StructureSnapshot`
    - 关联 `structure_key`、`signal_id`、`entry_trigger_id`、`source_layer`、timeframe、confirm/revocation、ATR/ADX、BTC market state 字段
    - 首期无法完整填充分型/笔/线段/中枢编号时，在 report assumptions 中声明缺口，Gate 不允许 approved
    - _Requirements: 3.1, 3.2, 11.3_

  - [x] 1.6 输出 Position_Lifecycle 增强指标
    - 在 `trades.csv` 或 `metrics.json` 中补齐 MFE、MAE、lifecycle max drawdown、final R multiple、recovery time
    - 支持按 side、signal_type、market_state、ATR/ADX profile、symbol_category 分桶
    - _Requirements: 2.2, 2.10, 5.2, 5.3, 5.8, 7.1, 7.6_

  - [x] 1.7 编写 backtest artifact 和 preflight 测试
    - 使用 historydb fixture 验证 `data_hash` 稳定性
    - 验证 `structures.json`、`metrics.json`、分桶字段和最小名义额拒绝输出
    - 验证 paper broker 不绕过公共 preflight
    - 已运行 `GOPROXY=https://goproxy.cn,direct go test ./backtest ./trader`
    - _Requirements: 8.1, 8.2, 10.2, 14.4_

- [x] 2. 搭建 optimize 包基础结构、配置和 artifact loader
  - [x] 2.1 创建 `optimize/types.go`
    - 定义 `ReplayReportInput`、`BacktestRunInput`、`RunArtifacts`、`DefectEntry`、`EvidenceRef`、`DefectCatalog`、`RunMetrics`、`BucketMetrics`、`MetricComparison`、`GateInput`、`GateResult`、`StructureSnapshot`、`OptimizationProposal`、`RequirementCoverageSummary`
    - 类型字段包含 trader_id、exchange、run_id、data_hash、diagnosis_interval、policy_ref 和 baseline/candidate 可比性元数据
    - _Requirements: 1.6, 1.7, 2.2, 2.5, 9.3, 15.1_

  - [x] 2.2 创建 `optimize/config.go`
    - 实现 `LoadOptimizationConfig(path string) (*OptimizationConfig, error)`
    - 设置默认阈值：`max_drawdown_relative_tolerance=1.10`、`profit_factor_relative_floor=0.95`、`rejection_rate_absolute_tolerance=0.10`、`min_notional_rejection_tolerance=0.05`、`replay_backtest_consistency_tolerance=0.05`、`circuit_breaker_frequency_tolerance=0.05`、`bootstrap_iterations=1000`
    - 若覆盖阈值，要求 `policy_ref` 和 `policy_commit` 存在
    - _Requirements: 2.6, 2.7, 2.8, 2.9, 7.4, 10.5_

  - [x] 2.3 创建 `optimize/artifacts.go`
    - 实现 `LoadRunArtifacts(outputDir string) (*RunArtifacts, error)`
    - 读取 `report.json`、`config_snapshot.json`、CSV、`markers/`、`structures.json`、`metrics.json`
    - 兼容旧 run：缺 `metrics.json` 时从 report/CSV 派生并标记 `metrics_source=derived`
    - 缺 `structures.json` 时返回可检查错误供 Gate 降级
    - _Requirements: 2.1, 2.3, 3.1, 8.7, 11.3_

  - [x] 2.4 创建 `optimize/log_fields.go`
    - 实现 `ResolveCanonicalLogFields(action logger.DecisionAction) (CanonicalLogFields, []FieldResolutionError)`
    - 读取优先级：DecisionAction 顶层 -> `strategy_metadata` -> `explanation.details`
    - 对必填字段缺失输出结构化错误
    - _Requirements: 11.1, 11.2, 12.7_

  - [x] 2.5 编写基础单元测试
    - 测试 config 默认值、override policy 校验、artifact loader、canonical field fallback
    - 运行 `go test ./optimize`
    - _Requirements: 2.9, 10.5, 11.2, 14.1_

- [x] 3. 实现缺陷诊断工具 Defect_Catalog
  - [x] 3.1 实现 `optimize/defect_catalog.go`
    - 实现 `BuildDefectCatalog(replayInputs []ReplayReportInput, backtestInputs []BacktestRunInput, config *OptimizationConfig) (*DefectCatalog, error)`
    - 从 `ReplayReport.StrategyDisease` 提取 micro_stop、low_adx_entry、counter_di_entry、profile_mismatch、same_side_correlation 等缺陷
    - 从 backtest `rejections.csv`、`metrics.json`、`BySignalType`、`structures.json` 提取缺陷
    - 按 `(trader_id, exchange, symbol, defect_code)` 聚合
    - 拒绝无 evidence_refs 或缺 trader/exchange context 的缺陷
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 9.3_

  - [x] 3.2 编写 DefectCatalog 单元测试
    - 覆盖空输入、单 trader、多 trader、多 exchange、缺 context、缺 evidence
    - 验证 `affected_exchanges` 和 `sample_count_by_trader_exchange`
    - _Requirements: 1.3, 1.4, 1.6, 9.3_

  - [x] 3.3 编写 Property 8: DefectCatalog 拒绝无证据缺陷
    - 使用 gopter 生成随机缺陷输入和 context
    - 验证 evidence 为空或 context 缺失时必拒绝
    - **Validates: Requirements 1.3, 12**

  - [x] 3.4 编写 Property 9: DefectCatalog 输出完整性
    - 使用 gopter 生成 replay/backtest wrapper 输入
    - 验证成功 catalog 包含 diagnosis interval、affected traders/exchanges、evidence refs、primary metric
    - **Validates: Requirements 1.1, 1.2, 1.5, 1.6, 12**

- [x] 4. 实现 RunMetrics、分桶和 bootstrap 指标
  - [x] 4.1 实现 `optimize/metrics.go`
    - 实现 `ExtractRunMetrics(artifacts *RunArtifacts) (*RunMetrics, error)`
    - 计算胜率、PF、Net_PnL、Net_PnL_Pct、最大回撤、平均 R、平均持仓时间、信号到执行延迟、手续费占比、滑点占比、拒绝率、最小名义额拒绝率、熔断频率
    - 实现 symbol、side、signal_type、market_state、ATR profile、ADX range、symbol category、trader/exchange 分桶
    - 提取并保存 `initial_equity`、fee/slippage model hash、funding/liquidation mode、execution model hash，供 Gate 校验 run 可比性
    - _Requirements: 2.2, 2.3, 2.10, 6.5, 7.1, 9.5_

  - [x] 4.2 实现 bootstrap 区间
    - 使用 trades/lifecycles 作为主要样本，支持固定 seed
    - 样本不足时输出 `ci_status=insufficient_samples`
    - _Requirements: 2.5_

  - [x] 4.3 编写 metrics 单元测试
    - 使用构造 trades/equity/signals/rejections/structures 验证各指标、分桶和 CI 状态
    - _Requirements: 2.2, 2.5, 2.10_

- [x] 5. 实现 Optimization_Gate
  - [x] 5.1 实现 `optimize/gate.go`
    - 签名使用 `EvaluateGate(input GateInput) (GateResult, error)`
    - 不可比 run 返回 `ErrIncomparableRuns` 和 `Verdict=invalid_input`
    - High severity requirement coverage missing 时不得 approved
    - 回撤/PF/min_notional 超限 rejected
    - 拒绝率/熔断频率/分桶退化超限 manual_review
    - 记录 policy_ref、policy_commit、override_reason、override_by
    - _Requirements: 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 6.5, 7.4, 9.5, 15.4_

  - [x] 5.2 编写 Gate 单元测试
    - 覆盖边界值、不可比输入、policy override 缺失、per trader/exchange 评估、coverage missing
    - _Requirements: 2.6, 2.7, 2.8, 2.9, 9.5, 15.4_

  - [x] 5.3 编写 Property 1: Optimization_Gate 阈值判定
    - 使用 gopter 生成可比 RunMetrics 对和 OptimizationConfig
    - 验证回撤、PF、min_notional、拒绝率、熔断频率判定
    - **Validates: Requirements 2.6, 2.7, 2.8, 6.5, 7.4, 9.5, 12.3**

- [x] 6. 实现信号质量、资金曲线和一致性分析
  - [x] 6.1 实现 `optimize/signal_quality.go`
    - 输入 `signals`、`structures`、`trades`、`executions`
    - 计算结构/信号生成频率、确认延迟、撤回率、1R 命中率、最终 R 倍数
    - 缺 `structures.json` 时返回 `ErrMissingStructureSnapshots`
    - _Requirements: 3.1, 3.2, 3.3_

  - [x] 6.2 实现 `optimize/equity_analysis.go`
    - 计算日度净值、累计回撤、滚动 30 笔 PF/胜率
    - 实现高回撤区间归因到 lifecycle_id、signal_type、BTC market state、ATR/ADX profile、exchange
    - 提取熔断/亏损模式切换事件
    - _Requirements: 5.8, 7.1, 7.2, 7.6_

  - [x] 6.3 实现 `optimize/consistency.go`
    - 对比 replay/backtest 同口径指标：胜率、PF、拒绝率、min_notional、execution failure、unmatched
    - 超过 `replay_backtest_consistency_tolerance` 时标记并暂停结论性判断
    - _Requirements: 8.4, 8.8_

  - [x] 6.4 编写分析模块测试
    - 覆盖结构缺失、信号撤回率、回撤归因、一致/不一致场景
    - _Requirements: 3.1, 3.2, 7.1, 7.6, 8.8_

- [x] 7. 实现 baseline/candidate/report/proposal/rollout
  - [x] 7.1 实现 `optimize/baseline.go`
    - 实现 `RunBaseline(cfg *backtest.BacktestConfig, store *historydb.Store, traderID, exchange string) (*RunArtifacts, *RunMetrics, error)`
    - 触发 `backtest.Runner`，加载 Run_Artifacts，校验 data_hash 和 required artifacts
    - _Requirements: 2.1, 2.3, 8.5, 8.7_

  - [x] 7.2 实现 `optimize/candidate.go`
    - 实现 `RunCandidate(...)`
    - 校验 candidate 与 baseline 的 data_hash、timezone、symbol set、initial equity、费用模型、funding/liquidation、execution model 一致
    - _Requirements: 2.4, 8.5, 8.6_

  - [x] 7.3 实现 `optimize/proposal.go`
    - 定义 Proposal_Checklist 模板和 `RequirementCoverageSummary`
    - 生成 coverage summary，标记 satisfied/not_applicable/missing/high severity
    - _Requirements: 4.8, 13.4, 15.1, 15.2, 15.3, 15.4_

  - [x] 7.4 实现 `optimize/rollout.go`
    - 定义 `GradualRolloutConfig`
    - 实现 `CheckRollbackCondition(liveMetrics, baselineMetrics *RunMetrics, config *GradualRolloutConfig) (bool, string)`
    - 支持 dry-run/paper、小仓位实盘、按 trader_id 顺序推进、配置开关回滚 baseline 行为
    - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5_

  - [x] 7.5 实现 `optimize/report.go`
    - 汇总 DefectCatalog、RunMetrics、GateResult、SignalQualityReport、EquityCurveAnalysis、ConsistencyResult、Proposal_Checklist、RequirementCoverageSummary
    - 落盘到 `backtest_runs/<run_id>/optimization_report.json`
    - _Requirements: 8.7, 11.6, 14.7, 15.3_

  - [x] 7.6 编写 report/proposal/rollout 单元测试
    - 验证报告结构完整、coverage high missing 阻断 approved、回滚条件触发
    - _Requirements: 13.2, 13.3, 14.7, 15.4_

- [x] 8. 实现 CLI 入口和集成测试
  - [x] 8.1 创建 `cmd/optimize/main.go`
    - 子命令：`diagnose`、`baseline`、`compare`、`report`
    - 所有输入通过 flag 指定，不硬编码路径
    - compare 命令读取 committed gate policy 并执行 Gate
    - _Requirements: 1.6, 2.1, 2.4, 8.7, 14.7_

  - [x] 8.2 编写 CLI fixture 集成测试
    - 使用 fixture replay report 和 fixture backtest run directory
    - 验证 diagnose/compare/report 端到端
    - 不触发真实交易所调用
    - _Requirements: 10.2, 14.6_

  - [x] 8.3 添加 spec 引用快照记录规范
    - 在本 spec 或 proposal checklist 中记录 baseline/candidate run_id、data_hash、policy_ref、manual review notes 的写法
    - _Requirements: 8.7, 11.6, 15.1_

- [x] 9. 实现 Correctness Properties 属性基测试（必做）
  - [x] 9.1 Property 2: Chanlun_Engine 确定性输出
    - 测试位置：`strategy/chanlun` 包
    - 使用 gopter 生成随机 K 线序列、策略配置和 StateStore 初始内容
    - 对同一输入调用两次 `GetFullDecision` 或结构构建入口，验证结构和信号序列 byte-equal
    - **Validates: Requirements 3.7, 12.2, 14.1**

  - [x] 9.2 Property 3: 止损/止盈调用路径分离
    - 测试位置：`trader` 包或 `trader_test` 包
    - 使用 fake trader 记录 Cancel 调用
    - 生成随机止损/止盈调整场景，验证路径正确分离
    - **Validates: Requirements 5.6, 12.4, 14.1**

  - [x] 9.3 Property 4: 同 symbol 无双向持仓
    - 测试位置：`backtest` 或 `decision` 包
    - 使用 gopter 生成随机持仓状态和开仓决策
    - 验证同 symbol 反向开仓被拒绝
    - **Validates: Requirements 12.5, 14.1**

  - [x] 9.4 Property 5: Replay 可完整重建 Position_Lifecycle
    - 测试位置：`logger` 包
    - 生成随机 `DecisionRecord` 序列（含开/加/减/平仓/保护单事件）
    - 调用 `BuildTradeReplay`，验证合法序列无未匹配事件
    - **Validates: Requirements 12.6, 14.1**

  - [x] 9.5 Property 6: Canonical_Log_Field 必填字段完整
    - 测试位置：`optimize` 或 `logger` 包
    - 生成随机 `DecisionAction`，字段分布在顶层、`strategy_metadata`、`explanation.details`
    - 验证 resolver 能解析 Requirement 11.1 所有必填字段；缺失时返回结构化错误
    - **Validates: Requirements 11.1, 11.2, 12.7, 14.1**

  - [x] 9.6 Property 7: Risk_Increase_Action 通过所有 Deterministic_Gate
    - 测试位置：`decision` 包，便于访问 `validateOpenDecision` 和 `enforceFinalDecisionLimits`
    - 使用 gopter 生成随机 Decision + Context，并包含历史 decision_logs 抽样 fixture 和合成 K 线两类样本
    - 验证由 Chanlun_Engine 生成的合法开仓决策通过确定性 gate；反例必须输出最小失败样本
    - **Validates: Requirements 4.6, 12.1, 14.1**

- [x] 10. 约束型 Requirement 的 review gate 和文档追踪
  - [x] 10.1 建立需求-设计-任务 traceability table
    - 在本 `tasks.md` 末尾维护 Requirement -> Design -> Task 映射
    - 确认 R1-R15 均有 task 覆盖
    - _Requirements: 14.8, 15.2_

  - [x] 10.2 添加 Proposal_Checklist 示例
    - 在 spec 目录新增或生成 `proposal_checklist.example.json`
    - 包含配置默认值、回滚开关、监控路径、人工复盘记录示例
    - 不包含真实账户或凭证
    - _Requirements: 10.1, 10.5, 13.4, 15.1_

  - [x] 10.3 执行安全和旁路 review gate
    - 检查没有 AI 信号直通、没有绕过 Decision_Layer、没有直接调用交易所私有 REST/WebSocket
    - 检查新增运行目录仍在 `.gitignore`
    - _Requirements: 10.1, 10.3, 10.4, 10.6, 15.2_

  - [x] 10.4 执行多 trader / 多交易所兼容 review gate
    - 检查 `trader.Trader` 接口未被无必要扩展
    - 检查 Binance、Hyperliquid、Aster mock/fake 覆盖执行链路影响
    - _Requirements: 9.1, 9.2, 9.4_

- [x] 11. Final checkpoint - 确保所有验证通过
  - [x] 11.1 运行后端目标测试
    - `go test ./optimize`
    - `go test ./backtest ./historydb`
    - `go test ./decision`
    - `go test ./strategy/chanlun`
    - `go test ./trader ./manager ./api`
    - _Requirements: 14.2, 14.3, 14.4_

  - [x] 11.2 运行构建验证
    - `go build ./cmd/optimize`（仓库根目录会因已有 `optimize/` 目录产生输出名冲突；已用 `go build -o /tmp/nofx-optimize ./cmd/optimize` 验证 CLI 构建）
    - `go build ./...`
    - _Requirements: 14.6_

  - [x] 11.3 如改变前端可见字段，运行前端验证
    - 未改变前端可见字段，未运行前端构建
    - `cd web && npm run build`
    - 如改动工具函数，追加 `cd web && npm run test`
    - _Requirements: 11.5, 14.5_

  - [x] 11.4 记录验证结果和未解决风险
    - 在 tasks 或最终报告中记录测试命令、结果、未跑原因
    - 验证结果：`GOPROXY=https://goproxy.cn,direct go test ./optimize ./cmd/optimize ./backtest ./historydb ./decision ./strategy/chanlun ./trader ./manager ./api` 通过；`GOPROXY=https://goproxy.cn,direct go test ./...` 通过；`go build -o /tmp/nofx-optimize ./cmd/optimize` 通过；`go build ./...` 通过
    - 未解决风险：`structures.json` 首期仍主要由 markers 派生，分型/笔/线段/中枢编号在真实策略完整接入前可能不完整，Gate/报告通过 `ErrMissingStructureSnapshots` 和 assumptions 保持保守
    - _Requirements: 14.6, 14.7_

## Task Dependency Graph

```json
{
  "waves": [
    {
      "id": "wave-1",
      "name": "回测证据与执行一致性",
      "tasks": ["1.1", "1.2", "1.3", "1.4", "1.5", "1.6", "1.7"],
      "dependencies": []
    },
    {
      "id": "wave-2",
      "name": "optimize基础设施",
      "tasks": ["2.1", "2.2", "2.3", "2.4", "2.5"],
      "dependencies": ["wave-1"]
    },
    {
      "id": "wave-3",
      "name": "缺陷诊断",
      "tasks": ["3.1", "3.2", "3.3", "3.4"],
      "dependencies": ["wave-2"]
    },
    {
      "id": "wave-4",
      "name": "指标与Gate",
      "tasks": ["4.1", "4.2", "4.3", "5.1", "5.2", "5.3"],
      "dependencies": ["wave-2", "wave-3"]
    },
    {
      "id": "wave-5",
      "name": "分析与一致性",
      "tasks": ["6.1", "6.2", "6.3", "6.4"],
      "dependencies": ["wave-4"]
    },
    {
      "id": "wave-6",
      "name": "报告、灰度和CLI",
      "tasks": ["7.1", "7.2", "7.3", "7.4", "7.5", "7.6", "8.1", "8.2", "8.3"],
      "dependencies": ["wave-5"]
    },
    {
      "id": "wave-7",
      "name": "Correctness Properties",
      "tasks": ["9.1", "9.2", "9.3", "9.4", "9.5", "9.6"],
      "dependencies": ["wave-6"]
    },
    {
      "id": "wave-8",
      "name": "Review Gates与最终验证",
      "tasks": ["10.1", "10.2", "10.3", "10.4", "11.1", "11.2", "11.3", "11.4"],
      "dependencies": ["wave-7"]
    }
  ]
}
```

## Traceability Table

| Requirement | Design Coverage | Task Coverage |
| --- | --- | --- |
| R1 缺陷诊断输入与口径 | Input Wrappers, Defect Catalog | 2.1, 3.1, 3.2, 3.3, 3.4 |
| R2 基线指标与 Gate | Backtest Artifact Extensions, Metrics and Gate | 1.1, 1.2, 1.6, 4.1, 4.2, 5.1, 5.2, 5.3 |
| R3 信号质量 | Structure Snapshot, Signal Quality | 1.5, 6.1, 6.4, 9.1 |
| R4 入场与开仓门槛 | Proposal Checklist, Gate | 5.1, 7.3, 9.6, 10.3 |
| R5 止损止盈与持仓管理 | Lifecycle Metrics, Correctness Properties | 1.6, 6.2, 9.2 |
| R6 仓位管理 | Backtest Preflight Integration, Metrics and Gate | 1.3, 1.4, 4.1, 5.1 |
| R7 资金曲线与熔断恢复 | Equity Analysis, Gate | 4.1, 6.2, 5.1 |
| R8 回测/实盘一致性 | Backtest Preflight Integration, Error Handling | 1.2, 1.3, 1.4, 6.3, 7.1, 7.2 |
| R9 多 trader 与多交易所 | Defect Catalog, Integration Points | 3.1, 5.1, 10.4 |
| R10 安全与合规 | Security Considerations | 8.2, 10.2, 10.3 |
| R11 可解释性与日志 | Canonical Log Field Resolver, Structure Snapshot | 1.5, 2.4, 7.5, 8.3, 9.5 |
| R12 Correctness Properties | Property-Based Tests | 3.3, 3.4, 5.3, 9.1, 9.2, 9.3, 9.4, 9.5, 9.6 |
| R13 灰度上线与回滚 | Rollout, Proposal Checklist | 7.3, 7.4, 7.6, 10.2 |
| R14 测试与验证 | Testing Strategy | 9.1-9.6, 11.1, 11.2, 11.3, 11.4 |
| R15 Proposal 审查与可追踪性 | Proposal Checklist and Coverage | 7.3, 7.5, 10.1, 10.2, 10.3, 10.4 |

## Notes

- 本计划不包含 optional correctness property；所有 Requirement 12/14 相关测试均为必做。
- Checkpoint 只在对应阶段测试通过后勾选。
- 运行时数据和回测快照保持 git ignored，不作为普通功能改动提交。
- 文档、日志、错误信息优先中文；代码标识符、JSON 字段、API 路径使用英文。
