# 开仓频率优化 Consistency Review

## 复核范围

本次复核覆盖：

- `.kiro/specs/open-frequency-optimization/requirements.md`
- `.kiro/specs/open-frequency-optimization/design.md`
- `.kiro/specs/open-frequency-optimization/tasks.md`
- 当前代码中的配置、启动、交易循环、决策验证、日志与 replay 路径

结论：规格方向与现有架构总体一致，可以进入实现。下列发现已按用户确认的取舍同步到 `requirements.md`、`design.md` 和 `tasks.md`：active 回滚保留原候选池规则，其余一致性问题作为实现任务落地。

## Findings

### 1. Active 自动回退与候选池 prompt limit 存在时序冲突

Severity: High

Status: Resolved by spec decision

`design.md` 设计 active 回退后 runtime effective mode 变为 `safe`，但候选池 prompt limit 当前是通过 `pool.SetDynamicCandidatePoolConfig()` 设置的全局配置，发生在 `main.initializeModules()` 阶段。当前 `trader.buildTradingContext()` 又是在获取 performance 后立即调用 `pool.GetDynamicMergedCoinPool(...)`，没有 per-cycle prompt limit 参数。

这意味着 active 自动回退即使把执行模式改成 `safe`，也不能自然把候选数量从 active 的 12 降回 safe/balanced，除非实现额外调整。

用户确认的修正：

- 保持原候选池规则。
- `active` 自动回退只影响运行时开仓行为、AI 新机会搜索节流、daily cap 和风险闸门，不动态回退候选池 prompt limit。
- 候选池 prompt limit 保持启动时派生或显式配置的值，只有人工改配置并重启才变化。

### 2. 自动缩仓设计引用了当前不存在的 `Decision` 字段

Severity: High

Status: Resolved in design/tasks

`design.md` 的自动缩仓伪代码写入：

- `d.RequestedPositionSizeUSD`
- `d.SizingAdjusted`
- `d.SizingReason`

但当前 `decision.Decision` 只有 `PositionSizeUSD`、`RiskUSD` 等字段。`logger.DecisionAction` 也没有 requested/adjusted sizing 字段。

原建议：

- 在 design 的 Auto Shrink 小节把 `decision/types.go` 明确列入字段变更。
- 在 tasks Phase 3 增加扩展 `Decision` 和 `logger.DecisionAction` 的任务。
- 字段建议：`requested_position_size_usd`、`adjusted_position_size_usd`、`sizing_adjusted`、`sizing_reason`、`stop_distance_pct`、`effective_risk_pct`。

已同步：`design.md` 和 `tasks.md` 均要求扩展 `decision.Decision` 与 `logger.DecisionAction` 的 sizing audit 字段。

### 3. Replay 对旧日志的 report-only 结果只能 best-effort，不能等同 live 模拟

Severity: Medium

Status: Resolved in design/tasks

未来实盘路径可以在 `validateOpenDecision()` 和 `EvaluateOpenGate()` 中基于完整 market data 生成结构化 simulation。但历史日志只有部分 gate reason 文本和少量 diagnostics；高 ADX、BTC 多周期、RR、rolling 的完整上下文并不总是存在。

当前 `logger.BuildReplayReport()` 只统计 action/rejection 文本，不会重新拉市场数据。若直接要求 replay 输出“高 ADX active-style would pass/fail”，实现只能解析文本，精度低于 live report-only。

原建议：

- design 中区分 `live structured simulation` 和 `legacy replay text inference`。
- tasks Phase 4/8 明确旧日志 replay 输出为 best-effort，并在报告中标注 `source=structured|text_inferred`。

已同步：`OpenFrequencySimulation` 增加 `source` 字段；旧日志文本推断标注为 `source=text_inferred`。

### 4. 最终开仓上限拒绝目前不会形成结构化 `open_rejected`

Severity: Medium

Status: Resolved in design/tasks

当前 `enforceFinalDecisionLimits()` 返回 `[]string` rejections，并把原因拼到 CoTTrace；`appendOpenRejectionsToRecord()` 只消费 `decision.OpenRejections`。如果 active daily cap 在 final limit 阶段拦截 open，除非显式转换为 `OpenRejection` 或 logger action，否则 replay/API 里不会像普通开仓拒绝一样可统计。

原建议：

- design 中明确 final-limit/daily-cap rejection 应落为结构化 `OpenRejection` 或 `DecisionAction{Action:"open_rejected"}`。
- tasks Phase 5 增加“记录为结构化 open_rejected”的验收点。

已同步：daily cap/final-limit 拒绝必须落为结构化 `OpenRejection` 或 `open_rejected` action。

### 5. `decision.Initialize()` 启动日志可能继续显示 15 分钟，和 balanced runtime 不一致

Severity: Low

Status: Resolved in design/tasks

`main.initializeModules()` 目前构造 `decision.Config{AnalysisIntervalMin: 15}`，`decision.Initialize()` 仅用于初始化计划和日志输出；真正运行时的间隔来自 `AutoTraderConfig.AnalysisIntervalMin -> decision.Context.AnalysisIntervalMin`。如果只在 AutoTrader 注入 balanced=12，启动日志仍会打印“分析间隔=15分钟”，容易误导排查。

原建议：

- 将 derived frequency profile 的 interval 也传给 `decision.Initialize()` 用于日志一致性；或
- 从初始化日志中移除“分析间隔”，明确以 trader status 为准。

已同步：任务要求传入派生 interval，或移除固定间隔日志并以 trader status 为准。

### 6. 缺少配置示例更新任务

Severity: Low

Status: Resolved in tasks

Specs 说明首次部署添加 `trading_frequency.mode=balanced`，但 tasks 没有要求更新 `config.json.example` 或部署文档。当前示例文件本身不是严格 JSON，仍应给出注释式示例，避免后续部署时遗漏。

原建议：

- tasks Phase 1 或 Phase 9 增加更新 `config.json.example` / README 配置说明。

已同步：任务要求更新 `config.json.example` 和部署说明，并注明 active 回退不动态改变候选池 prompt limit。

## Traceability Check

| Requirement | Design Coverage | Task Coverage | Notes |
| --- | --- | --- | --- |
| R1 诊断基线 | replay/report-only diagnostics | Phase 4, Phase 8 | 已补 legacy best-effort 说明 |
| R2 分析间隔配置化 | TradingFrequencyConfig + policy injection | Phase 1, 2, 3 | 兼容路径清楚 |
| R3 灰度档位 | Derived Profiles | Phase 1, 5, 9 | active 回退保留候选池规则 |
| R4 候选覆盖 | profile prompt limit | Phase 2, 3, 5 | 已明确为启动/配置级规则，不随 rollback 动态变化 |
| R5 高 ADX report-only | OpenFrequencySimulation | Phase 4 | 默认不进实盘，符合要求 |
| R6 RR report-only | OpenFrequencySimulation | Phase 4 | 默认实盘仍 2.5，符合要求 |
| R7 自动缩仓 | validateOpenDecision sizing | Phase 3 | 已补字段扩展 |
| R8 rolling 可观测/恢复 | simulation + performance state | Phase 4, 5 | 已补样本不足时风险-only 诊断 |
| R9 风控/回滚 | active cap + rollback | Phase 5, 9 | cap 拒绝结构化记录 |
| R10 验证 | test plan | Phase 7, 8 | 覆盖足够 |

## Accepted Spec Edits Before Implementation

1. Active rollback 保留原候选池规则，不动态回退 prompt candidate limit。
2. 在 design/tasks 中加入 `Decision` 和 `DecisionAction` sizing 字段扩展。
3. 标注旧日志 replay report-only 为 best-effort text inference，并加入 `source`。
4. 将 daily cap/final cap 拒绝纳入结构化 `open_rejected`。
5. 修正启动日志 interval 一致性。
6. 增加 config example / deployment docs 更新任务。
