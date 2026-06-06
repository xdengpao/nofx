# NOFX 服务最近 2 日无交易单输出 Spec 交叉检查

## 检查范围

- 执行时间：2026-06-03
- `.kiro/specs/nofx-service-no-order-20260602/requirements.md`
- `.kiro/specs/nofx-service-no-order-20260602/design.md`
- `.kiro/specs/nofx-service-no-order-20260602/tasks.md`
- `logger/replay.go`
- `logger/replay_test.go`
- `cmd/replay/main.go`
- `decision/decision.go`
- `decision/open_gate.go`
- `strategy/chanlunv2/engine.go`
- `strategy/chanlunv2/loosen_mode.go`
- `strategy/chanlunv2/engine_test.go`

## 结论

三份 Spec 的核心边界一致：不降低 RR、不关闭 BTC hard veto、不绕过最终风控。当前代码也支持这个边界：最终开仓验证仍在 `decision/decision.go` 中硬检查 `RR >= 2.5`，BTC 多周期转弱在 `decision/open_gate.go` 中对高 beta 山寨多单硬阻断，已有测试覆盖 loosen 不绕过 BTC hard veto。

需要修订的是任务颗粒度和部分验收口径。当前代码已有不少 no-open 诊断能力，任务应改为补缺；另外 replay 目前只读取决策日志和可选 `config.json`，不能直接证明 Docker 服务日志里的“订单追踪器创建”与真实交易所订单差异，除非新增服务日志输入或把该项留作人工排查步骤。

本次复核确认：可以继续按 Spec 执行，但建议先修订 `requirements.md`、`design.md`、`tasks.md` 中的口径冲突，再开始实现。未发现需要降低 RR 阈值或关闭 BTC hard veto 的任务。

## 修订状态

已于 2026-06-03 按本交叉检查修订：

- `requirements.md`：区分历史基线 `adx/rr` bucket 与增强后 `btc_hard_veto/adx_report_only/final_rr` bucket，并明确阶段 2 会改变未来 `open_rejected` 统计口径。
- `requirements.md`：将 loosen `max_duration_hours` 验收改为记录当前实现状态，不默认新增回到 balanced/safe 的实际退出逻辑。
- `design.md`：删除“是否贯穿 signal-type RR 到最终开仓验证”的残留表述，明确 signal-type RR 只控制 entry trigger，最终 RR 2.5 保留。
- `design.md`：明确服务日志字段只有在新增服务日志输入时统计，纯 replay 不从 Docker 日志文案推断真实交易所订单。
- `tasks.md`：将 replay 能力任务收窄为补缺和 bucket 细化，阶段 2 增加共享 BTC veto helper 前置任务，阶段 4 改为诊断优先。

## 本次代码证据

- `decision/decision.go` 的最终开仓验证仍硬检查 `riskRewardRatio < 2.5` 并返回“风险回报比过低”，与“不降低 RR 阈值”一致。
- `decision/open_gate.go` 的 `applyBTCMultiTimeframeGate()` 只在 high beta alt `open_long` 且 BTC 1h/4h confirmed bearish 时硬阻断，helper 当前未导出。
- `logger/replay.go` 已有 `NoSuccessfulOpenHours`、`FreshnessCompatibility`、`ChanlunV2NoOpen`、`BTCGateDiagnostics`、`ConfidenceOverrides` 等能力，阶段 1/4 应聚焦补缺和 bucket 细化。
- `logger/replay.go` 的 `classifyRejectionBucket()` 目前先匹配 `adx` 再匹配 `btc`，所以含 ADX report-only 和 BTC veto 的历史样本会先落入 `adx` bucket。
- `cmd/replay/main.go` 当前只从 `config.json` 读取 signal-type RR 阈值与 `ConfigSource`，没有 Docker 服务日志输入。
- `strategy/chanlunv2/loosen_mode.go` 会默认 `MaxDurationHours=24`，但 `loosenModeController()` 当前未用该字段执行退出。

## 发现

### 1. 需求 6 与任务阶段 1 的 bucket 口径不一致

修订前，`requirements.md` 的验收标准要求 `cmd/replay -open-rejection-daily` 复现最近 48 小时 top buckets 为 `adx` 和 `rr`，但 `design.md` 与 `tasks.md` 又要求增强后拆分为 `btc_hard_veto`、`adx_report_only`、`final_rr` 等新 bucket，并期望 top buckets 包含 `btc_hard_veto` 和 `final_rr`。

当前代码中 `classifyRejectionBucket()` 会先匹配 `ADX/趋势开仓`，再匹配 `BTC/高 beta`，所以包含 ADX report-only 和 BTC hard veto 的 ASTER 样本会先落到 `adx`。这解释了现有基线，但增强后验收不应继续要求 top buckets 为 `adx/rr`。

建议：

- 将需求 6 的验收标准拆成两条：
  - 基线 replay 复现历史输出 `adx/rr`。
  - 增强后 replay 输出 `btc_hard_veto/final_rr`，并把 `adx_report_only` 从硬阻断中拆出。

### 2. 设计总览残留“是否贯穿 signal-type RR 到最终验证”的旧表述

修订前，`design.md` 总览中写着“明确是否要把缠论 V2 的 signal-type RR 贯穿到最终开仓验证”。但用户已经明确“不降低 RR 阈值”，后续方案 C 也已改为“只做诊断和文档澄清，不改变实盘开仓准入”。

建议：

- 将该句改为“明确 signal-type RR 只控制 entry trigger，最终开仓验证继续保留 RR 2.5”。

### 3. 阶段 1 的服务日志字段缺少数据来源

修订前，`tasks.md` 要求在 `logger/replay.go` 的 open rejection daily 报告中增加：

- `real_exchange_order_count`
- `tracker_only_order_mentions`
- `enabled_trader_count`

当前 `BuildOpenRejectionDailyReportWithOptions()` 的输入只有决策日志和 `OpenRejectionDailyOptions`。`cmd/replay` 可读取 `config.json`，但只把 signal-type RR 阈值和 `ConfigSource` 传给 logger。它没有 Docker 服务日志输入，无法从 replay 内部确认“订单追踪器创建”这类服务日志文本。

可行拆分：

- `enabled_trader_count` 可以从 `config.json` 传入 `OpenRejectionDailyOptions`。
- `real_exchange_order_count` 可以在决策日志内按成功开仓 action 或非零 open `order_id` 近似统计，但这不是 Docker 服务日志统计。
- `tracker_only_order_mentions` 需要新增服务日志输入参数，例如 `--service-log`，或保留为人工交叉验证项，不应放在 `logger/replay.go` 的纯决策日志报告中。

### 4. 当前 logger/replay 已有多项任务能力，任务应收窄为补缺

现有 `logger/replay.go` 已包含：

- `NoSuccessfulOpenHours`
- `VersionDiagnosticMissing`
- `FreshnessCompatibility`
- `ChanlunV2NoOpen.ActionDistribution`
- `TriggerReadyCount`
- `OpenGateRejectionCount`
- `BTCGateRejectionCount`
- `BTCGateDiagnostics`
- `ConfidenceOverrideCount`
- `ConfidenceOverrides`

现有 `logger/replay_test.go` 已覆盖：

- Chanlun V2 no-open 摘要
- BTC gate diagnostics 展开
- confidence override 诊断
- freshness compatibility audit
- 版本诊断历史窗口/当前窗口差异

建议：

- 阶段 1 和阶段 4 不要描述为“新增整套能力”，应改成“补充日报顶层字段、细化 bucket 分类、补 `final_rr`/`service log source` 缺口”。

### 5. BTC hard veto 前移任务需要明确 shared helper 设计

当前 BTC hard veto 的真实逻辑在 `decision/open_gate.go`：

- `applyBTCMultiTimeframeGate()`
- `isHighBetaAltcoin()`
- `isConfirmedBTCBearishStructure()`
- `buildBTCGateDiagnostics()`

这些 helper 当前未导出。`strategy/chanlunv2` 不能直接复用，若复制一份逻辑，后续会出现策略层 precheck 与 open gate 真实阻断条件分叉。

建议：

- 阶段 2 任务应明确为先提取或暴露只读 helper，例如 `decision.EvaluateBTCHighBetaLongVeto(symbol, action, btcData)`。
- helper 只返回诊断和 veto 结论，不改变 open gate 行为。
- `applyBTCMultiTimeframeGate()` 与 `strategy/chanlunv2` precheck 共同调用同一 helper。

### 6. BTC hard veto 前移会改变 open_rejected 统计口径

阶段 2 目标是“减少重复 `open_rejected`”。这符合设计，但会导致最近 48 小时 replay 里的 `rejected_open_count=44` 不再是增强后同类场景的期望输出，因为一部分 ASTER long 会在策略诊断阶段冷却或终态静默。

建议：

- 任务和验收应区分历史 replay 基线与增强后行为：
  - 历史 replay 仍能解释 `open_rejected=44`。
  - 新行为下 BTC hard veto precheck 会减少 open_rejected，同时增加 `btc_hard_veto_precheck` 或 terminal suppression 计数。

### 7. Loosen `max_duration_hours` 当前没有实际退出逻辑

`strategy/chanlunv2/loosen_mode.go` 的 `normalizeV2LoosenPolicy()` 会设置 `MaxDurationHours` 默认值，但 `loosenModeController()` 当前没有使用该字段判断退出。修订前，`tasks.md` 阶段 4 写“当 loosen 持续超过 `max_duration_hours` 时，记录继续 loosen、回到 balanced 或进入 safe 的原因”。

这可能从“可解释性”变成“模式退出行为变更”。

建议：

- 若只做可解释性，任务应改为记录 `max_duration_hours` 当前未强制退出或记录无法判断 started_at。
- 若要实现实际退出，需要补设计说明、状态来源、回滚条件和测试，因为这会影响交易行为。

### 8. RR 2.5 边界与当前代码一致

`decision/decision.go` 的 `validateOpenDecisionWithOptions()` 在最终开仓验证中硬编码：

```go
if riskRewardRatio < 2.5 {
    return fmt.Errorf("风险回报比过低(%.2f:1 < 2.5:1)", riskRewardRatio)
}
```

`strategy/chanlunv2/engine_test.go` 已有 freshness 使用 signal-type RR 的测试，以及 loosen 不绕过 BTC hard veto 的测试。Spec 的“不降低 RR 阈值”与当前代码一致。

建议：

- 阶段 3 应聚焦 replay 诊断，不需要改最终 RR 行为。
- `decision/decision_test.go` 可以补一个更明确的“不因 chanlun_v2/source 或 loosen 而降低 final RR”的单元测试。

### 9. OI Top 后续评估不应阻塞主线

阶段 5 已写明只读检查，且不直接启用动态候选池。这与设计一致。

建议：

- 保持阶段 5 为可选后续，不纳入阶段 1-4 的验收阻塞项。

## 建议修订

以下建议已按“修订状态”小节落到 spec 文档：

1. 修订 `requirements.md` 需求 6：区分历史基线 top buckets 与增强后 top buckets。
2. 修订 `design.md` 总览：删除“是否贯穿 signal-type RR 到最终验证”的残留表述。
3. 修订 `tasks.md` 阶段 1：把 `tracker_only_order_mentions` 改为“仅在提供服务日志输入时统计”，或移到人工验证步骤。
4. 修订 `tasks.md` 阶段 1：注明当前 replay 已有 `BTCGateDiagnostics`、`ConfidenceOverrides`、`FreshnessCompatibility`，任务只补顶层字段和 bucket 分类。
5. 修订 `tasks.md` 阶段 2：先提取共享 BTC veto helper，再做 chanlunv2 precheck。
6. 修订 `tasks.md` 阶段 4：明确 `max_duration_hours` 是只记录还是实际退出；若实际退出，应补行为设计。

## 可执行性判断

- 可直接执行：阶段 1 的 replay bucket 细化、阶段 3 的 RR 语义诊断、阶段 5 的只读候选池评估。
- 需先修订任务：阶段 1 的服务日志字段、阶段 2 的 BTC helper 共享、阶段 4 的 loosen duration 语义。
- 不建议执行：任何降低 RR、关闭 BTC hard veto、绕过最终验证的任务。当前 tasks 没有这类任务。
