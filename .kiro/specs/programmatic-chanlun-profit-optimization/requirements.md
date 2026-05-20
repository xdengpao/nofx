# Requirements Document

> 中文标题：程序化缠论策略盈利能力优化

## Introduction

本规格在已有程序化缠论策略基础上，建立一套数据驱动的"评估 + 优化"流程，目标是把 NOFX 当前的程序化缠论策略在加密货币永续合约实盘的盈利能力做系统性提升。

本规格不重新定义缠论结构（分型、笔、线段、中枢、买卖点）和入场择时；这些已分别由：

- `.kiro/specs/programmatic-chanlun-strategy/`
- `.kiro/specs/programmatic-chanlun-entry-timing/`
- `.kiro/specs/programmatic-signal-noise-latency-clarity/`
- `.kiro/specs/programmatic-signal-staleness-guard/`
- `.kiro/specs/programmatic-two-tier-rhythm/`
- `.kiro/specs/programmatic-confidence-execution-fix/`
- `.kiro/specs/programmatic-guardrails-explainability-signals/`
- `.kiro/specs/programmatic-strategy-backtest/`
- `.kiro/specs/strategy-loss-mitigation/`
- `.kiro/specs/strategy-quant-optimization/`
- `.kiro/specs/trailing-stop-optimization/`
- `.kiro/specs/open-frequency-optimization/`
- `.kiro/specs/profitability-guardrails/`
- `.kiro/specs/live-loss-diagnosis-optimization/`

定义和实现。

本规格在它们之上做三件事：

1. **缺陷诊断**：以现有 `decision_logs/{trader_id}/`、`cmd/replay`、`backtest/` 为唯一证据来源，输出可复现的策略缺陷清单和可观测指标。
2. **优化设计约束**：要求所有优化方案落到现有 `strategy/chanlun.Engine`、`decision/`、`trader/` 公共层，并显式说明针对哪一类缺陷、影响哪些指标、与哪些已有规格交叉。
3. **量化对比与回归约束**：所有优化必须在统一回测/replay 口径下给出基线对比，且必须满足"不显著恶化"的安全门槛，才允许进入实盘灰度。

交叉验证发现已纳入本版需求：回测产物必须先补齐 `data_hash`、trader/exchange 上下文、分桶指标、结构快照和日志字段解析口径；否则 Optimization_Gate 不得给出结论性通过结果。

本规格输出语言为中文，按 `.kiro/steering/product.md` 语言约定。

## Glossary

- **Chanlun_Engine**：现有程序化缠论引擎实现，对应 `strategy/chanlun.Engine` 及其 `GetFullDecision`、`evaluateMainSignals`、`evaluatePreviewSignals`、`StateStore`。
- **Decision_Layer**：现有 `decision/` 包内确定性风控层，包含 `ValidateAndEnrichDecision`、`validateOpenDecision`、`EvaluateOpenGate`、`CalculatePositionSizing`、`enforceFinalDecisionLimits`、`strategy_risk`、`open_gate`、`loss_mode`、`takeprofit`、`risk`。
- **Trader_Layer**：现有 `trader.Trader` 抽象及其 Binance、Hyperliquid、Aster 实现，包括 `OrderTracker`、`execution_preflight`、`execution_protection`、`exchange_calibration`。
- **Replay_Pipeline**：现有 `cmd/replay` + `logger/replay.go` 离线复盘与对账链路，输入为 `decision_logs/{trader_id}/`，输出 replay JSON 报告。
- **Backtest_Pipeline**：现有 `backtest/` 包 + `cmd/backtest` 行情级回测链路，按 `programmatic-strategy-backtest` spec 实现。
- **Run_Artifacts**：一次回测或优化运行落盘的完整文件集合，至少包含 `report.json`、`config_snapshot.json`、`trades.csv`、`equity.csv`、`signals.csv`、`rejections.csv`、`markers/`，本规格新增 `structures.json`、`metrics.json` 和 `optimization_report.json`。
- **Data_Hash**：同一历史区间、同一 source、同一 symbol/timeframe K 线集合的确定性 hash，用于证明 Baseline_Run 与 Candidate_Run 使用同一数据集。
- **Baseline_Run**：在指定历史区间、指定 trader、指定 `config_hash` 下，使用当前未优化代码版本跑出的回测结果，作为对比基线。
- **Candidate_Run**：在同一历史区间、同一 trader、同一标的池下，使用某个待评估优化方案跑出的回测结果。
- **Defect_Catalog**：以 Replay_Pipeline 与 Backtest_Pipeline 输出为来源，按 trader_id、exchange、symbol、side、signal type、cycle 聚合得到的策略缺陷清单。
- **Optimization_Proposal**：单条可落地的优化建议，必须包含目标缺陷、影响指标、改动范围、配置开关、回归条件、灰度计划和回滚路径。
- **Proposal_Checklist**：Optimization_Proposal 的机器可读审查清单，用于记录证据、配置、run_id、data_hash、Gate 结果、灰度计划、回滚开关和人工复盘记录。
- **Committed_Gate_Policy**：提交到仓库的门控阈值策略或 spec 文档引用，定义 Optimization_Gate 的默认阈值、允许覆盖规则和审批元数据。运行目录只能保存其快照，不得成为唯一策略来源。
- **Structure_Snapshot**：Backtest_Pipeline 为每个 signal/marker 落盘的结构上下文快照，至少可关联分型、笔、线段、中枢、ZG/ZD、A/B/C 段边界、确认时间、撤回事件和 `structure_key`。
- **Canonical_Log_Field**：决策日志中某个语义字段的标准读取路径。字段可来自 `DecisionAction` 顶层、`strategy_metadata` 或 `explanation.details`，但必须由本规格定义唯一优先级。
- **Net_PnL**：扣除手续费、滑点、funding（若启用）后的 PnL。
- **Profit_Factor (PF)**：闭合交易样本内 `sum(positive_pnl) / abs(sum(negative_pnl))`。
- **Position_Lifecycle**：同一 symbol、同一 side 从首次开仓到完全退出的一段持仓生命周期，与 Backtest_Pipeline 中定义一致。
- **Execution_Event**：Position_Lifecycle 内的开仓、加仓、减仓、平仓、止损移动、保护单触发等事件。
- **Risk_Increase_Action**：会增加同 symbol 净风险敞口的动作，包括 `open_long`、`open_short`、`add_long`、`add_short`。
- **Risk_Reduction_Action**：不会增加同 symbol 净风险敞口的动作，包括 `close_long`、`close_short`、`partial_close`、`update_stop_loss`、保护单修复。
- **Deterministic_Gate**：Decision_Layer 中所有不依赖 AI 的确定性校验门，包括 `ValidateAndEnrichDecision`、`validateOpenDecision`、`EvaluateOpenGate`、`CalculatePositionSizing`、ATR/ADX profile、最小名义额校验、相关性、亏损模式。
- **Optimization_Gate**：本规格新增的 pre-promotion 校验门，校验 Candidate_Run 是否满足"不显著恶化"约束。

## Requirements

### Requirement 1: 缺陷诊断输入与口径统一

**User Story:** 作为量化交易员，我希望策略缺陷诊断只来自决策日志、replay 和回测，不依赖主观判断，以便诊断结果可复现、可对账。

#### Acceptance Criteria

1. THE Defect_Catalog SHALL 仅以 `decision_logs/{trader_id}/`、Replay_Pipeline 输出和 Backtest_Pipeline 输出作为证据来源。
2. WHEN 生成 Defect_Catalog，THE Optimization 流程 SHALL 记录每条缺陷对应的证据路径、时间区间、trader_id、exchange、symbol、side、signal_id 或 entry_trigger_id、reason_code。
3. IF 某条缺陷无法定位到 decision_logs、replay 报告或 backtest 报告中的具体记录，THEN THE Optimization 流程 SHALL 拒绝接收该条缺陷。
4. WHEN 同一缺陷在多个 trader_id 或 exchange 上重复出现，THE Optimization 流程 SHALL 按 `(trader_id, exchange, symbol, defect_code)` 维度聚合证据，并保留每个 trader/exchange 的样本数与方向分布。
5. THE Optimization 流程 SHALL 使用统一的诊断时间区间元数据，包含 `replay_from`、`replay_to`、`backtest_from`、`backtest_to`、`warmup_from`、`timezone`，并写入 Defect_Catalog 输出。
6. WHEN 生成 Defect_Catalog，THE Optimization 流程 SHALL 输出可机器读取的结构化文件（JSON），字段至少包括：`defect_code`、`description_zh`、`evidence_refs`、`affected_traders`、`affected_exchanges`、`affected_symbols`、`affected_sides`、`affected_signal_types`、`primary_metric`、`metric_delta_vs_healthy_subset`、`sample_count_by_trader_exchange`。
7. THE Defect_Catalog 输入 SHALL 使用带上下文的 source wrapper，而不是裸 `ReplayReport` 或裸 `backtest.Report`，以保留 `path`、`trader_id`、`exchange`、`run_id` 和 `data_hash`。

### Requirement 2: 基线指标与 Optimization_Gate 量化口径

**User Story:** 作为量化交易员，我希望优化前后用同一套指标和回测口径对比，避免"看起来更好但实盘更差"。

#### Acceptance Criteria

1. THE Baseline_Run SHALL 使用 Backtest_Pipeline 在指定历史区间、指定标的池、指定 config_hash 下跑出，并落盘 `config_snapshot.json`、`report.json`、`trades.csv`、`equity.csv`、`signals.csv`、`rejections.csv`、`markers/`、`structures.json`、`metrics.json`。
2. THE Baseline_Run 输出指标 SHALL 至少包含：胜率、Profit_Factor、Net_PnL、Net_PnL_Pct、最大回撤、平均 R、平均持仓时间、信号到执行延迟（信号 close_time 到决策 close_time 到模拟成交时间的两段差值）、手续费占比、滑点占比、拒绝率、最小名义额拒绝率、熔断频率、按 symbol/side/signal_type 的同口径细分。
3. THE Baseline_Run SHALL 记录共同 `data_hash`，该 hash SHALL 覆盖所有参与回测的 symbol、timeframe、source、正式统计区间和 warmup 区间。
4. THE Candidate_Run SHALL 与对应 Baseline_Run 共享同一 `data_hash`、timezone、标的池、初始资金、手续费滑点模型、funding/liquidation 模式和回测执行模型。
5. WHEN 对比 Baseline_Run 与 Candidate_Run，THE Optimization_Gate SHALL 计算并记录每个核心指标的绝对值、相对差值与 bootstrap 置信区间；若样本量不足以 bootstrap，SHALL 明确标记 `ci_status=insufficient_samples`。
6. IF Candidate_Run 的最大回撤相对 Baseline_Run 恶化超过配置阈值 `max_drawdown_relative_tolerance`（默认 1.10，即恶化不得超过 10%），THEN THE Optimization_Gate SHALL 拒绝该 Candidate_Run 进入实盘灰度。
7. IF Candidate_Run 的 Profit_Factor 低于 Baseline_Run 的 `profit_factor_relative_floor` 倍（默认 0.95），THEN THE Optimization_Gate SHALL 拒绝该 Candidate_Run 进入实盘灰度。
8. IF Candidate_Run 的拒绝率相对 Baseline_Run 上升超过配置阈值 `rejection_rate_absolute_tolerance`（默认 0.10，绝对值），THEN THE Optimization_Gate SHALL 标记为"信号通过率显著下降"并要求人工复核。
9. WHEN 配置阈值被覆盖，THE Optimization_Gate SHALL 在快照中记录 `override_reason`、`override_by`、`policy_ref` 和 `policy_commit`；覆盖阈值 SHALL 来自 Committed_Gate_Policy，不得只来自未提交到仓库的本地运行配置。
10. THE Baseline_Run 和 Candidate_Run SHALL 同时给出分桶指标：按 BTC 市场状态（TREND_UP/TREND_DOWN/RANGING/SQUEEZE/HIGH_VOL）、按 ATR/ADX profile、按 symbol 类别（BTC/ETH/altcoin）、按 exchange、按 trader_id，以避免整体指标掩盖结构性退化。

### Requirement 3: 信号质量优化要求

**User Story:** 作为量化交易员，我希望分型/笔/线段/中枢/买卖点的稳定性、抗噪和滞后被量化评估，并通过多周期共振、ATR/ADX/趋势过滤减少假信号。

#### Acceptance Criteria

1. THE Backtest_Pipeline SHALL 在 Run_Artifacts 中输出 `structures.json`，用于把 signal/marker 关联到分型、笔、线段、中枢、ZG/ZD、A/B/C 段边界、确认时间和撤回事件。
2. THE Optimization 流程 SHALL 对 Chanlun_Engine 的每一类输出（分型、笔、线段、中枢、`buy1/buy2/buy3`、`sell1/sell2/sell3`、preview、entry trigger）单独输出诊断指标，至少包含：单位时间生成数量、确认延迟（confirm_close_time 与 segment_end_time 之差）、撤回率（确认后又被结构变化推翻的比例）、单信号后续 1R 命中率、单信号后续生命周期最终 R 倍数。
3. WHEN 评估抗噪能力，THE Optimization 流程 SHALL 在历史样本中按 ATR profile（Low/Medium/High）和 ADX 区间（<20、20-30、>30）分别给出上述指标，避免在低波动样本上得到误导性结论。
4. WHEN 优化方案引入新的多周期共振规则（例如 4h 方向 + 1h 主信号 + 15m 触发），THE Optimization_Proposal SHALL 显式声明该规则并提供启用/禁用配置开关，默认值 SHALL 保守，与当前实盘行为一致。
5. WHEN 优化方案引入 ATR/ADX/趋势过滤，THE Optimization_Proposal SHALL 复用现有 `decision/strategy_risk.go` 的 ATR/ADX profile 体系，不得在策略层重复实现并行的 ATR/ADX 判断分支。
6. IF 优化后某一类信号在历史样本上的生成频率下降超过 30%，THEN THE Optimization_Proposal SHALL 在文档中显式说明该频率下降的原因、是否符合预期、对覆盖率的影响。
7. THE Chanlun_Engine 在相同 K 线输入、相同配置、相同状态文件下 SHALL 输出确定性的分型、笔、线段、中枢和买卖点序列；任何优化方案 SHALL 保留该确定性。
8. WHEN 优化方案改变信号去重或 structure_key 计算，THE Optimization 流程 SHALL 与 `programmatic-signal-noise-latency-clarity` 规格保持一致，不得引入新的并行去重逻辑。

### Requirement 4: 入场与开仓门槛优化要求

**User Story:** 作为量化交易员，我希望在不破坏现有确定性 gate 的前提下，通过频率档位、相关性、亏损模式、执行质量等手段提升胜率与盈亏比。

#### Acceptance Criteria

1. THE Optimization_Proposal SHALL NOT 修改 Deterministic_Gate 的语义；任何收紧或放宽 SHALL 通过现有配置项实现，例如 `trading_frequency`、`strategy_risk`、`open_gate` 阈值、ATR/ADX profile、相关性配置、亏损模式参数。
2. WHEN 优化方案调整 `trading_frequency` 档位（safe/balanced/active）阈值或回滚条件，THE Optimization_Proposal SHALL 给出新档位下的预期开仓频率、预期 Net_PnL、预期最大回撤的回测对比。
3. WHEN 优化方案调整相关性集中度限制，THE Optimization_Proposal SHALL 在 BTC 高相关与低相关分桶下分别给出 Position_Lifecycle 平均 R 和最大相关回撤的对比。
4. WHEN 优化方案调整亏损模式（loss_mode）触发阈值或恢复阈值，THE Optimization_Proposal SHALL 复用现有 rolling performance 数据源，并在 Replay_Pipeline 输出中可定位每次切换。
5. IF 优化方案引入新的入场置信度或执行质量门槛，THEN THE Optimization_Proposal SHALL 通过 `EvaluateOpenGate` 现有扩展点接入，不得在策略层旁路。
6. FOR ALL Optimization_Proposal 引起的 Risk_Increase_Action，THE Optimization_Gate SHALL 验证它仍能通过现有 `ValidateAndEnrichDecision`、`validateOpenDecision`、`EvaluateOpenGate`、`CalculatePositionSizing`、`enforceFinalDecisionLimits`、ATR/ADX profile、最小名义额校验、相关性、亏损模式所有门，存在反例则视为违例。
7. WHEN 优化方案需要调整执行质量参数（信号到执行延迟、preflight 拒绝率），THE Optimization_Proposal SHALL 引用 `decision_logs/{trader_id}/` 中的 execution quality 字段作为证据。
8. THE Optimization_Proposal SHALL 使用 Proposal_Checklist 记录上述配置项、默认值、回滚开关和 replay/backtest 证据引用。

### Requirement 5: 止损止盈与持仓管理优化要求

**User Story:** 作为量化交易员，我希望动态止损、分批止盈、移动止损、失效条件、最大回撤约束被量化评估，使持仓阶段的盈亏比稳定提升。

#### Acceptance Criteria

1. THE Optimization_Proposal SHALL 复用现有 `decision/takeprofit.go` 的分批止盈、移动止损、动态 TP 接口，所有改动 SHALL 通过现有 TradePlan 字段表达。
2. WHEN 优化方案调整动态止损触发或步长，THE Optimization_Proposal SHALL 在 Backtest_Pipeline 中给出 MFE、MAE、R 倍数分布、最终 R 倍数的对比直方图，不得仅依赖均值对比。
3. THE Backtest_Pipeline SHALL 在 `trades.csv` 或 `metrics.json` 中输出 Position_Lifecycle 级 MFE、MAE、max_drawdown、final R multiple 和 recovery time，供 Requirement 5.2 使用。
4. WHEN 优化方案调整分批止盈档位或比例，THE Optimization_Proposal SHALL 验证 `partial_close` 不会触发交易所最小名义额拒绝；若历史样本中存在 `partial_close` 失败，应在 Defect_Catalog 中先行标注。
5. WHEN 优化方案调整移动止损（trailing stop）参数，THE Optimization_Proposal SHALL 与 `.kiro/specs/trailing-stop-optimization/` 规格保持一致，不得新增并行移动止损实现。
6. THE Optimization_Proposal 调整止损 SHALL 使用 `CancelStopLossOrders()` 路径；调整止盈 SHALL 使用 `CancelTakeProfitOrders()` 路径；存在混用即视为违例。
7. WHEN 优化方案引入新的失效条件解析规则，THE Optimization_Proposal SHALL 复用 `decision/parser.go`，不得在策略层旁路。
8. THE Optimization 流程 SHALL 给出 Position_Lifecycle 级别的最大账户回撤、单 Position_Lifecycle 最大回撤、连续亏损次数和回撤恢复时间，作为持仓管理优化的核心评估指标。

### Requirement 6: 仓位管理优化要求

**User Story:** 作为量化交易员，我希望账户风险预算、波动率自适应仓位、相关币种集中度可以被量化调参，避免单一标的或单一方向集中风险。

#### Acceptance Criteria

1. THE Optimization_Proposal 调整仓位 sizing SHALL 通过 `decision/position_sizing.go` 现有入口表达，不得在策略层直接计算最终下单数量。
2. WHEN 优化方案引入波动率自适应仓位（基于 ATR 或 realized vol），THE Optimization_Proposal SHALL 给出按波动桶的 R 分布对比，确保高波动桶不会出现仓位异常放大。
3. WHEN 优化方案调整账户单笔风险或单日风险预算，THE Optimization_Proposal SHALL 在 Backtest_Pipeline 中验证最大账户回撤不超过基线乘以 `max_drawdown_relative_tolerance`。
4. WHEN 优化方案调整相关币种集中度，THE Optimization_Proposal SHALL 在 BTC 上涨/下跌/震荡三类 regime 下分别给出整体 PnL 与最大相关回撤的对比。
5. IF 优化方案使最低名义额拒绝率上升超过 `min_notional_rejection_tolerance`（默认绝对值 +0.05），THEN THE Optimization_Gate SHALL 拒绝该方案进入实盘灰度。
6. THE Optimization_Proposal 调整 sizing SHALL 验证在交易所最小名义额、最小步长、保证金、可用余额边界条件下不产生不可执行的下单参数。
7. THE Backtest_Pipeline SHALL 使用与实盘相同的最小名义额校准和 preflight 结果记录最小名义额拒绝，不得用未校准的 paper-only 阈值代替。

### Requirement 7: 资金曲线、回撤约束与熔断恢复

**User Story:** 作为量化交易员，我希望日度/累计回撤、熔断恢复、亏损模式切换被量化评估，避免优化方案在某些 regime 下产生灾难性回撤。

#### Acceptance Criteria

1. THE Optimization 流程 SHALL 在 Backtest_Pipeline 输出中计算日度净值变化、累计回撤、滚动 30 笔 Profit_Factor、滚动 30 笔胜率，并按时间序列输出。
2. WHEN 优化方案触发熔断或亏损模式切换，THE Optimization_Proposal SHALL 在报告中标记每次切换的时间、触发指标、前后开仓频率。
3. THE Optimization_Proposal SHALL 复用现有 `decision/risk.go`（熔断、账户硬停）和 `decision/loss_mode.go`（亏损模式），不得在策略层新增并行熔断逻辑。
4. IF 优化方案使熔断触发频率显著上升（绝对值 +0.05 次/天），THEN THE Optimization_Gate SHALL 要求人工复核，并在文档中说明是否符合"主动降低风险"的预期。
5. WHEN 优化方案改变熔断恢复阈值，THE Optimization_Proposal SHALL 与 `.kiro/specs/circuit-breaker-auto-recovery/` 规格保持一致。
6. THE Optimization 流程 SHALL 输出"高回撤区间归因"：把超过配置阈值的回撤区间反查到具体 Position_Lifecycle、信号类型、BTC 状态、ATR/ADX profile 和 exchange，便于后续优化定位。

### Requirement 8: 回测/实盘一致性与可复现基线

**User Story:** 作为量化交易员，我希望回测和实盘使用同一套策略代码与公共风控，对比口径稳定，否则任何优化结论都不可信。

#### Acceptance Criteria

1. THE Backtest_Pipeline SHALL 使用与实盘相同的 `strategy/chanlun.Engine`、`decision/` 公共风控、`trader/execution_preflight.go`、`trader/exchange_calibration.go` 代码路径，不得在回测专用分支重新实现策略逻辑。
2. WHEN `trader/exchange_calibration.go` 中的最小名义额函数当前不可被 backtest 复用，THEN THE 实现 SHALL 先导出或抽取公共校准入口，再接入 Backtest_Pipeline；不得复制一份并行规则。
3. WHEN 跑 Baseline_Run 与 Candidate_Run，THE Optimization 流程 SHALL 注入虚拟时钟，所有依赖当前真实时间的逻辑 SHALL 使用回测时间或显式禁用并在报告中声明。
4. THE Replay_Pipeline 与 Backtest_Pipeline SHALL 共享 Position_Lifecycle、Execution_Event、信号类型、拒绝原因、最小名义额拒绝、preflight 拒绝的字段语义和编码；同一字段在两侧含义不一致即视为违例。
5. WHEN 优化方案上线前，THE Optimization 流程 SHALL 至少完成：对最近一段 Replay_Pipeline 样本的离线复盘对账（不引入未来函数）、Backtest_Pipeline 在相同样本上的 Baseline_Run、Candidate_Run、Optimization_Gate 评估，三者样本范围与 hash SHALL 全部记录在 Optimization_Proposal 中。
6. WHEN 历史区间或标的池变化，THE Optimization 流程 SHALL 以新区间重新生成 Baseline_Run，不得跨区间复用旧基线。
7. THE Optimization_Proposal SHALL 在仓库中引用可机器读取的快照文件 run_id；快照文件本体 SHALL 位于 git ignored 的 `backtest_runs/<run_id>/` 内，并在 spec 文档中引用 run_id 与 data_hash。
8. IF Replay_Pipeline 与 Backtest_Pipeline 在同一历史区间得出的同口径指标差异超过配置阈值 `replay_backtest_consistency_tolerance`（默认 0.05 绝对值），THEN THE Optimization 流程 SHALL 在文档中先行说明差异来源，并暂停结论性判断。

### Requirement 9: 多 trader 与多交易所兼容

**User Story:** 作为系统维护者，我希望优化方案不破坏 Binance、Hyperliquid、Aster 实现的兼容性，也不在 trader 层引入新的耦合。

#### Acceptance Criteria

1. THE Optimization_Proposal SHALL NOT 修改 `trader.Trader` 接口语义；如确需扩展，SHALL 在所有现有实现（Binance、Hyperliquid、Aster）同步落地，并补齐对应测试。
2. WHEN 优化方案需要新的最小名义额、精度或费率参数，THE Optimization_Proposal SHALL 通过 `trader/exchange_calibration.go` 或其抽取出的公共校准入口表达，不得在策略层硬编码交易所差异。
3. THE Optimization 流程 SHALL 在 Defect_Catalog 中按 `(trader_id, exchange)` 维度分别输出指标，避免某一交易所的执行质量问题被全局指标掩盖。
4. WHEN 优化方案改变执行链路（preflight、保护单、自动平仓回调），THE Optimization_Proposal SHALL 在 `trader/` 包测试中至少覆盖一个 fake exchange + 一个真实交易所实现的 mock。
5. IF 优化方案在某一交易所实现下使最小名义额拒绝率显著上升，THEN THE Optimization_Gate SHALL 单独按 trader/exchange 维度评估而不是只看全局。

### Requirement 10: 安全与合规约束

**User Story:** 作为系统维护者，我希望优化过程不向仓库引入真实凭证、不真实下单、不绕过 decision 包风控。

#### Acceptance Criteria

1. THE Optimization 流程 SHALL NOT 在仓库中提交真实 API key、secret、私钥、助记词或真实账户配置；任何 Backtest_Pipeline 配置文件 SHALL 使用脱敏占位符。
2. THE Optimization 流程的所有自动化测试 SHALL NOT 触发真实交易所下单；新增测试 SHALL 使用 fake/mock client。
3. THE Optimization_Proposal SHALL NOT 引入旁路 Decision_Layer 的代码路径；任何"快速通道"或"AI 信号直通"即视为违例。
4. WHEN 优化方案需要在策略层读取额外行情或账户数据，THE Optimization_Proposal SHALL 复用 `market/` 和 `decision/` 现有入口，不得直接调用交易所私有 REST 或 WebSocket。
5. THE Optimization 流程 SHALL 在 spec 文档中显式列出所有新增配置项及默认值；新增配置项默认值 SHALL 保守（保留当前实盘行为）。
6. WHEN 优化方案落地到 `data/`、`decision_logs/`、`coin_pool_cache/` 或 `backtest_runs/`，THE 路径 SHALL 保持 git ignored，不作为普通功能改动提交。

### Requirement 11: 可解释性与决策日志要求

**User Story:** 作为量化交易员，我希望优化后的每一笔开/平仓都能从决策日志还原触发原因和缠论结构上下文，否则无法做实盘复盘。

#### Acceptance Criteria

1. WHEN 优化后的策略输出 Risk_Increase_Action 或 Risk_Reduction_Action，THE 决策日志 SHALL 写入可由 Canonical_Log_Field 解析器读取的字段：`structure_key`、`signal_id`、`parent_signal_id`、`entry_trigger_id`、`source_layer`、`signal_type`、`analysis_timeframe`、`trigger_timeframe`、`structure_target`、`signal_close_time`、`decision_close_time`、`age_candles`、`freshness_state`、`reason_code`、`config_hash`、`strategy_version`。
2. THE Canonical_Log_Field 解析器 SHALL 定义每个字段的读取优先级：优先读取 `DecisionAction` 顶层字段，其次读取 `strategy_metadata`，最后读取 `explanation.details`；缺失字段 SHALL 输出结构化错误而不是静默忽略。
3. THE Replay_Pipeline SHALL 能基于上述字段和 Structure_Snapshot 在离线状态下重建当时的缠论结构上下文（最近笔/线段/中枢编号、ZG/ZD、A/B/C 段边界）。
4. WHEN 优化方案改变决策日志字段，THE Optimization_Proposal SHALL 同步更新 `logger/decision_logger.go` schema、Replay_Pipeline 解析器、`api/server.go` 暴露的 trader-scoped 接口，并保持向后兼容（旧记录使用 `omitempty` 默认值或 Canonical_Log_Field fallback）。
5. WHEN 优化方案改变前端可见字段，THE Optimization_Proposal SHALL 同步更新 `web/src/lib/api.ts` 与 `web/src/types/index.ts`。
6. THE Optimization 流程 SHALL 在每次 Optimization_Gate 通过后，在 spec 任务中记录从 decision_logs 抽样若干笔执行/拒绝事件并人工复盘的结果。
7. IF 优化方案使某类信号在 decision_logs 中无法定位到唯一 `(structure_key, entry_trigger_id, action)` 组合，THEN THE Optimization_Gate SHALL 拒绝该方案进入实盘灰度。

### Requirement 12: Correctness Properties（用于属性基测试）

**User Story:** 作为维护者，我希望关键正确性以可枚举的属性形式表达，并在 PBT 中验证，避免回归。

#### Acceptance Criteria

1. FOR ALL Risk_Increase_Action 由优化后的 Chanlun_Engine 生成，THE Decision_Layer SHALL 通过 `ValidateAndEnrichDecision`、`validateOpenDecision`、`EvaluateOpenGate`、`CalculatePositionSizing`、ATR/ADX profile、最小名义额校验、相关性、亏损模式所有 Deterministic_Gate；存在反例即视为违例（属性测试样本来源 SHALL 至少包含历史 decision_logs 抽样和合成 K 线两类）。
2. FOR ALL 相同 K 线输入、相同配置、相同 StateStore 内容，THE Chanlun_Engine SHALL 在分型、笔、线段、中枢、`buy1/buy2/buy3/sell1/sell2/sell3`、preview signal、entry trigger 输出上保持确定性；对同一输入两次调用 SHALL 产生 byte-equal 序列化结果。
3. FOR ALL Optimization_Proposal 在同一历史区间下生成的 Candidate_Run，对比 Baseline_Run，THE 最大回撤的相对值 SHALL <= `max_drawdown_relative_tolerance`，且 Profit_Factor 的相对值 SHALL >= `profit_factor_relative_floor`，否则 Optimization_Gate SHALL 拒绝。
4. FOR ALL 优化后调整止损动作，THE 调用路径 SHALL 通过 `CancelStopLossOrders()`；FOR ALL 优化后调整止盈动作，THE 调用路径 SHALL 通过 `CancelTakeProfitOrders()`；FOR ALL 同一周期同时调整止损与止盈，THE 两个 Cancel 接口 SHALL 分别独立调用，不得复用同一取消接口。
5. FOR ALL 优化后开仓决策，IF 同 symbol 已存在反向持仓，THEN THE 决策 SHALL 不被生成或被合并优先级排序拒绝；同 symbol 双向持仓即视为违例。
6. FOR ALL 优化后的同一 trader_id，从 decision_logs 抽样的任一 Position_Lifecycle，THE Replay_Pipeline SHALL 能完整重建开仓 -> 加仓 -> 减仓 -> 平仓 -> 保护单事件序列，无未匹配 Execution_Event；存在未匹配即视为违例。
7. FOR ALL 决策日志中的优化后 Risk_Increase_Action 或 Risk_Reduction_Action，THE Canonical_Log_Field 解析器 SHALL 能解析 Requirement 11.1 中列出的所有必填字段；缺失即视为违例。

### Requirement 13: 灰度上线与回滚要求

**User Story:** 作为系统维护者，我希望优化方案先以保守默认值灰度上线，遇到指标异常可快速回滚，避免一次性放量。

#### Acceptance Criteria

1. WHEN 优化方案通过 Optimization_Gate，THE Optimization_Proposal SHALL 给出至少两步灰度：第一步 dry-run 或 paper（不真实下单），第二步小仓位实盘（仓位上限不超过当前实盘的 30%）。
2. THE Optimization_Proposal SHALL 给出回滚条件，至少包含：滚动 N 笔 Net_PnL 退化阈值、滚动 N 笔最大回撤阈值、连续 K 个周期 preflight 拒绝率阈值；阈值 SHALL 与 Optimization_Gate 阈值保持同口径。
3. WHEN 灰度阶段触发回滚条件，THE 系统 SHALL 通过现有配置开关切回 Baseline 行为，不得依赖代码紧急回滚。
4. THE Optimization_Proposal SHALL 在 spec 文档中记录灰度配置项、初始值、回滚配置项、监控指标查询路径（API 或日志路径）。
5. IF 优化方案影响多个 trader_id，THEN THE 灰度 SHALL 按 trader_id 顺序推进，至少在第一个 trader 上完成一个完整 rolling window 后再扩展到下一个。

### Requirement 14: 测试与验证

**User Story:** 作为维护者，我希望优化方案有完整的回归测试覆盖，未来改动不会破坏 Optimization_Gate 与 correctness properties。

#### Acceptance Criteria

1. THE Optimization 流程 SHALL 为 Requirement 12 中每条 correctness property 提供至少一个必做 `gopter` 属性基测试；这些任务不得被标记为 optional。
2. WHEN Optimization_Proposal 调整 Decision_Layer，THE 测试 SHALL 至少覆盖 `decision/` 包内对应函数的单元测试，并跑 `go test ./decision`。
3. WHEN Optimization_Proposal 调整 Chanlun_Engine，THE 测试 SHALL 跑 `go test ./strategy/chanlun`，并补齐分型、笔、线段、中枢、买卖点的反例测试。
4. WHEN Optimization_Proposal 调整执行链路，THE 测试 SHALL 跑 `go test ./trader ./manager ./api`，使用 fake exchange，不得触发真实下单。
5. WHEN Optimization_Proposal 改变前端可见字段，THE 测试 SHALL 跑 `cd web && npm run build`；如改动工具函数，SHALL 跑 `cd web && npm run test`。
6. WHEN Optimization_Proposal 接近合并，THE 验证 SHALL 至少包含一次 `go build ./...` 和 `go test ./...`，并在 spec 任务中记录结果。
7. THE Defect_Catalog、Baseline_Run、Candidate_Run、Optimization_Gate 报告 SHALL 在 spec 任务中以可机器读取的快照形式归档，便于后续 spec 引用。
8. THE tasks.md SHALL 包含需求-设计-任务 traceability table，覆盖每条 Requirement；缺少 task 覆盖的 SHALL 不得视为已满足。

### Requirement 15: Optimization_Proposal 审查与可追踪性

**User Story:** 作为维护者，我希望每个优化方案都能被机器检查和人工复核，避免只完成工具实现却没有证明具体优化符合规格。

#### Acceptance Criteria

1. THE Optimization 流程 SHALL 提供 Proposal_Checklist 模板，字段至少包括：proposal_id、目标 defect_code、证据引用、改动模块、配置项与默认值、影响指标、回测 run_id、data_hash、Gate 结果、灰度计划、回滚开关、人工复盘记录。
2. WHEN 某条 Requirement 是约束型而非代码型，THE tasks.md SHALL 为其提供 review gate 或 checklist 任务。
3. WHEN Optimization_Proposal 通过 Optimization_Gate，THE 报告 SHALL 输出 requirement coverage summary，标明每条 Requirement 的满足证据或未满足原因。
4. IF requirement coverage summary 中存在 High severity 未满足项，THEN THE Optimization_Gate SHALL 不得输出 `approved`。
