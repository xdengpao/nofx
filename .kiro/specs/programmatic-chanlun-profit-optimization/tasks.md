# Implementation Plan: 程序化缠论策略盈利能力优化

## Overview

本实现计划将优化流程分为 6 个阶段：基础设施搭建、缺陷诊断工具、基线/候选对比框架、Optimization Gate、信号质量与资金曲线分析、CLI 集成与属性基测试。每个阶段产出可独立验证的代码，逐步构建完整的"评估 + 优化"管线。

## Tasks

- [ ] 1. 搭建 optimize 包基础结构和配置
  - [ ] 1.1 创建 `optimize/` 包目录和 `config.go`
    - 定义 `OptimizationConfig` 结构体，包含所有门控阈值和诊断区间字段
    - 实现 `LoadOptimizationConfig(path string) (*OptimizationConfig, error)` 加载和归一化
    - 设置默认值：`max_drawdown_relative_tolerance=1.10`、`profit_factor_relative_floor=0.95`、`rejection_rate_absolute_tolerance=0.10`、`min_notional_rejection_tolerance=0.05`、`replay_backtest_consistency_tolerance=0.05`、`circuit_breaker_frequency_tolerance=0.05`
    - _Requirements: 2.5, 2.6, 2.7, 6.5, 7.4, 10.5_

  - [ ] 1.2 创建 `optimize/types.go` 核心类型定义
    - 定义 `DefectEntry`、`EvidenceRef`、`DefectCatalog`、`DiagnosisInterval`、`DefectSummary`
    - 定义 `RunMetrics`、`BucketMetrics`、`DelayStats`
    - 定义 `GateInput`、`GateResult`、`MetricComparison`
    - 定义 `SignalQualityMetrics`、`SignalQualityReport`
    - 定义 `EquityCurveAnalysis`、`DrawdownAttribution`、`CircuitBreakerEvent`
    - _Requirements: 1.6, 2.2, 2.4, 2.9_

  - [ ]* 1.3 编写 config 加载单元测试
    - 测试默认值填充、JSON 解析、无效配置拒绝
    - _Requirements: 10.5_

- [ ] 2. 实现缺陷诊断工具 (DefectCatalog)
  - [ ] 2.1 实现 `optimize/defect_catalog.go`
    - 实现 `BuildDefectCatalog(replayReports []logger.ReplayReport, backtestReports []backtest.Report, config *OptimizationConfig) (*DefectCatalog, error)`
    - 从 `ReplayReport.StrategyDisease` 提取缺陷（micro_stop、low_adx_entry、counter_di_entry 等）
    - 从 `backtest.Report.RejectionBuckets` 和 `BySignalType` 提取缺陷
    - 按 `(trader_id, symbol, defect_code)` 聚合，保留每个 trader 的样本数与方向分布
    - 拒绝无 evidence_refs 的缺陷条目（返回 `ErrInsufficientEvidence`）
    - 写入 `DiagnosisInterval` 元数据
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6_

  - [ ]* 2.2 编写 DefectCatalog 属性基测试
    - **Property 8: DefectCatalog 拒绝无证据缺陷**
    - **Property 9: DefectCatalog 输出完整性**
    - 使用 gopter 生成随机 ReplayReport 和 BacktestReport 输入
    - 验证：无证据缺陷被拒绝、输出字段完整、evidence source 在允许集合内
    - **Validates: Requirements 1.1, 1.2, 1.3, 1.5, 1.6**

  - [ ]* 2.3 编写 DefectCatalog 单元测试
    - 测试空输入、单 trader、多 trader 聚合、边界条件
    - _Requirements: 1.3, 1.4_

- [ ] 3. Checkpoint - 确保 defect_catalog 测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 4. 实现基线/候选指标提取
  - [ ] 4.1 实现 `optimize/metrics.go`
    - 实现 `ExtractRunMetrics(report backtest.Report, trades []backtest.TradeLifecycle, equity []backtest.EquityPoint, signals []backtest.SignalOutcome, rejections []decision.OpenRejection) *RunMetrics`
    - 计算：胜率、PF、Net_PnL、Net_PnL_Pct、最大回撤、平均 R、平均持仓时间、信号到执行延迟、手续费占比、滑点占比、拒绝率
    - 实现分桶：按 symbol、side、signal_type、market_state（BTC 状态）、ATR profile、symbol 类别（BTC/ETH/altcoin）
    - _Requirements: 2.2, 2.3, 2.9_

  - [ ] 4.2 实现 `optimize/baseline.go`
    - 实现 `RunBaseline(cfg *backtest.BacktestConfig, store *historydb.Store) (*RunMetrics, string, error)` 封装 backtest.Runner 调用
    - 落盘参数快照到 `backtest_runs/<run_id>/config_snapshot.json`
    - 返回 run_id 和提取的 RunMetrics
    - _Requirements: 2.1, 8.6_

  - [ ] 4.3 实现 `optimize/candidate.go`
    - 实现 `RunCandidate(cfg *backtest.BacktestConfig, store *historydb.Store) (*RunMetrics, string, error)`
    - 验证与 Baseline 共享同一 data_hash、timezone、symbols、initial_equity、费率模型
    - _Requirements: 2.3, 8.5_

  - [ ]* 4.4 编写 metrics 提取单元测试
    - 使用构造的 trades/equity/signals 验证指标计算正确性
    - 验证分桶逻辑
    - _Requirements: 2.2, 2.9_

- [ ] 5. 实现 Optimization Gate
  - [ ] 5.1 实现 `optimize/gate.go`
    - 实现 `EvaluateGate(input GateInput) GateResult`
    - 检查最大回撤相对恶化：`candidate.MaxDrawdownPct / baseline.MaxDrawdownPct > config.MaxDrawdownRelativeTolerance` → reject
    - 检查 PF 相对退化：`candidate.ProfitFactor / baseline.ProfitFactor < config.ProfitFactorRelativeFloor` → reject
    - 检查拒绝率绝对上升：`candidate.RejectionRate - baseline.RejectionRate > config.RejectionRateAbsoluteTolerance` → manual_review
    - 检查最小名义额拒绝率：按 trader 维度独立评估
    - 检查熔断频率上升：超过 `circuit_breaker_frequency_tolerance` → manual_review
    - 记录所有 MetricComparison（绝对值、相对差值）
    - 支持阈值覆盖（记录 override_reason 和 override_by）
    - _Requirements: 2.4, 2.5, 2.6, 2.7, 2.8, 6.5, 7.4, 9.5_

  - [ ]* 5.2 编写 Optimization Gate 属性基测试
    - **Property 1: Optimization_Gate 正确执行阈值判定**
    - 使用 gopter 生成随机 RunMetrics 对和 OptimizationConfig
    - 验证：回撤恶化超限必拒绝、PF 退化超限必拒绝、拒绝率上升超限必 manual_review
    - 验证：所有通过的 candidate 满足阈值约束
    - **Validates: Requirements 2.5, 2.6, 2.7, 6.5, 9.5, 12.3**

  - [ ]* 5.3 编写 Gate 单元测试
    - 测试边界值：恰好等于阈值、略超阈值、远超阈值
    - 测试 override 记录
    - 测试按 trader 维度评估
    - _Requirements: 2.5, 2.6, 2.7, 2.8, 9.5_

- [ ] 6. Checkpoint - 确保 gate 测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. 实现信号质量分析
  - [ ] 7.1 实现 `optimize/signal_quality.go`
    - 实现 `BuildSignalQualityReport(signals []backtest.SignalOutcome, trades []backtest.TradeLifecycle, executions []backtest.ExecutionEvent) *SignalQualityReport`
    - 按信号类型计算：生成频率（每天）、确认延迟（bars）、撤回率、1R 命中率、最终 R 倍数
    - 按 ATR profile 和 ADX 区间分桶
    - _Requirements: 3.1, 3.2_

  - [ ]* 7.2 编写信号质量单元测试
    - 验证各指标计算正确性
    - 验证分桶逻辑
    - _Requirements: 3.1, 3.2_

- [ ] 8. 实现资金曲线与回撤分析
  - [ ] 8.1 实现 `optimize/equity_analysis.go`
    - 实现 `BuildEquityCurveAnalysis(equity []backtest.EquityPoint, trades []backtest.TradeLifecycle, config *OptimizationConfig) *EquityCurveAnalysis`
    - 计算日度净值变化、累计回撤时间序列
    - 计算滚动 30 笔 PF 和胜率
    - 实现高回撤区间归因：反查到具体 lifecycle_id、signal_type、BTC 状态
    - 提取熔断事件：时间、触发指标、前后开仓频率
    - _Requirements: 7.1, 7.2, 7.6_

  - [ ]* 8.2 编写资金曲线分析单元测试
    - 验证回撤计算、滚动指标、归因逻辑
    - _Requirements: 7.1, 7.6_

- [ ] 9. 实现 Replay/Backtest 一致性检查
  - [ ] 9.1 实现 `optimize/consistency.go`
    - 实现 `CheckReplayBacktestConsistency(replayMetrics, backtestMetrics *RunMetrics, tolerance float64) (bool, []string)`
    - 对比同口径指标（胜率、PF、拒绝率等），差异超过 tolerance 返回 false 和差异说明
    - _Requirements: 8.3, 8.7_

  - [ ]* 9.2 编写一致性检查单元测试
    - 测试一致/不一致场景
    - _Requirements: 8.7_

- [ ] 10. 实现优化报告生成
  - [ ] 10.1 实现 `optimize/report.go`
    - 实现 `GenerateOptimizationReport(catalog *DefectCatalog, baseline, candidate *RunMetrics, gate GateResult, signalQuality *SignalQualityReport, equityAnalysis *EquityCurveAnalysis) *OptimizationReport`
    - 汇总所有分析结果到单一 JSON 报告
    - 落盘到 `backtest_runs/<run_id>/optimization_report.json`
    - _Requirements: 8.6, 14.7_

  - [ ]* 10.2 编写报告生成单元测试
    - 验证报告结构完整性
    - _Requirements: 14.7_

- [ ] 11. Checkpoint - 确保所有 optimize 包测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 12. 实现 CLI 入口
  - [ ] 12.1 创建 `cmd/optimize/main.go`
    - 实现 `diagnose` 子命令：加载 replay 报告和 backtest 报告，生成 DefectCatalog JSON
    - 实现 `baseline` 子命令：加载 backtest config，触发 Baseline_Run
    - 实现 `compare` 子命令：加载两个 run_id 的报告，执行 Gate 评估
    - 实现 `report` 子命令：生成完整优化报告
    - 所有子命令通过 flag 接收参数，不硬编码路径
    - _Requirements: 1.6, 2.1, 2.4, 8.6, 14.7_

  - [ ]* 12.2 编写 CLI 集成测试
    - 使用 fixture 数据验证 diagnose/compare 子命令端到端
    - 不触发真实交易所调用
    - _Requirements: 10.2, 14.6_

  - [ ] 12.3 实现灰度上线配置与回滚条件检查
    - 在 `optimize/config.go` 或新增 `optimize/rollout.go` 中定义灰度配置结构：`GradualRolloutConfig`
    - 包含字段：dry_run_enabled、max_position_ratio（默认 0.30）、rolling_window_trades、net_pnl_degradation_threshold、max_drawdown_threshold、rejection_rate_threshold
    - 实现 `CheckRollbackCondition(liveMetrics *RunMetrics, baselineMetrics *RunMetrics, config *GradualRolloutConfig) (bool, string)` 判断是否触发回滚
    - 回滚条件与 Optimization_Gate 阈值保持同口径
    - 灰度按 trader_id 顺序推进，至少在第一个 trader 上完成一个完整 rolling window 后再扩展
    - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5_

- [ ] 13. 实现 Correctness Properties 属性基测试
  - [ ]* 13.1 编写 Property 2: Chanlun_Engine 确定性输出
    - **Property 2: Chanlun_Engine 确定性输出**
    - 使用 gopter 生成随机 K 线序列和配置
    - 对同一输入调用两次 `GetFullDecision`，验证输出 byte-equal
    - **Validates: Requirements 3.6, 12.2**

  - [ ]* 13.2 编写 Property 3: 止损/止盈调用路径分离
    - **Property 3: 止损/止盈调用路径分离**
    - 使用 fake trader 记录 Cancel 调用
    - 生成随机止损/止盈调整场景，验证调用路径正确分离
    - **Validates: Requirements 5.5, 12.4**

  - [ ]* 13.3 编写 Property 4: 同 symbol 无双向持仓
    - **Property 4: 同 symbol 无双向持仓**
    - 使用 gopter 生成随机持仓状态和开仓决策
    - 验证同 symbol 反向开仓被拒绝
    - **Validates: Requirements 12.5**

  - [ ]* 13.4 编写 Property 5: Replay 可完整重建 Position_Lifecycle
    - **Property 5: Replay 可完整重建 Position_Lifecycle**
    - 生成随机 decision_logs 序列（含开/加/减/平仓）
    - 调用 `BuildTradeReplay`，验证无未匹配事件
    - **Validates: Requirements 12.6**

  - [ ]* 13.5 编写 Property 6: 决策日志必填字段完整
    - **Property 6: 决策日志必填字段完整**
    - 生成随机 DecisionAction（Risk_Increase 和 Risk_Reduction）
    - 验证所有必填字段非空
    - **Validates: Requirements 11.1, 12.7**

  - [ ]* 13.6 编写 Property 7: Risk_Increase_Action 通过所有 Deterministic_Gate
    - **Property 7: Risk_Increase_Action 通过所有 Deterministic_Gate**
    - 使用 gopter 生成随机 Decision + Context
    - 调用 `ValidateAndEnrichDecision` → `validateOpenDecision` → `EvaluateOpenGate` → `CalculatePositionSizing`
    - 验证由 Chanlun_Engine 生成的合法决策通过所有门
    - **Validates: Requirements 4.6, 12.1**

- [ ] 14. Final checkpoint - 确保所有测试通过
  - 运行 `go test ./optimize/...`
  - 运行 `go build ./cmd/optimize`
  - 运行 `go test ./decision/...`（回归验证）
  - 运行 `go test ./strategy/chanlun/...`（回归验证）
  - Ensure all tests pass, ask the user if questions arise.

## Task Dependency Graph

```json
{
  "waves": [
    {
      "id": "wave-1",
      "name": "基础设施",
      "tasks": ["1.1", "1.2"],
      "dependencies": []
    },
    {
      "id": "wave-2",
      "name": "配置测试与缺陷诊断",
      "tasks": ["1.3", "2.1"],
      "dependencies": ["wave-1"]
    },
    {
      "id": "wave-3",
      "name": "缺陷诊断测试与指标提取",
      "tasks": ["2.2", "2.3", "3", "4.1"],
      "dependencies": ["wave-2"]
    },
    {
      "id": "wave-4",
      "name": "基线候选运行与Gate",
      "tasks": ["4.2", "4.3", "4.4", "5.1"],
      "dependencies": ["wave-3"]
    },
    {
      "id": "wave-5",
      "name": "Gate测试与分析模块",
      "tasks": ["5.2", "5.3", "6", "7.1", "8.1", "9.1"],
      "dependencies": ["wave-4"]
    },
    {
      "id": "wave-6",
      "name": "分析测试与报告",
      "tasks": ["7.2", "8.2", "9.2", "10.1"],
      "dependencies": ["wave-5"]
    },
    {
      "id": "wave-7",
      "name": "报告测试与CLI",
      "tasks": ["10.2", "11", "12.1", "12.3"],
      "dependencies": ["wave-6"]
    },
    {
      "id": "wave-8",
      "name": "属性基测试与最终验证",
      "tasks": ["12.2", "13.1", "13.2", "13.3", "13.4", "13.5", "13.6", "14"],
      "dependencies": ["wave-7"]
    }
  ]
}
```

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from design document
- All tests use fake/mock - no real exchange calls
- Runtime data (`backtest_runs/`) stays git-ignored
- 中文用于文档和注释，英文用于代码标识符
