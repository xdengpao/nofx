# 程序化策略减仓约束、原因说明与信号展示 Consistency Review

## Review Scope

本次交叉验证覆盖：

- `requirements.md`
- `design.md`
- 当前代码架构与实现边界

当前状态：

- 本 review 首次执行时 `tasks.md` 尚不存在，因此初始结论聚焦 requirements/design 与现有代码的一致性。
- 已根据本 review 修正 `design.md`，并重新生成 `tasks.md`。
- 工作树没有半成品业务代码 diff；当前仅新增本 spec 目录。
- 修正后复核结论是：**requirements、design、tasks 已对齐，可以进入任务执行阶段。**

## Post-Fix Verification

已完成以下修正并同步到 `design.md` 与 `tasks.md`：

- `partial_close_cooldown_minutes` 使用 `*int` 表达，缺失默认 15，显式 `0` 表示关闭冷却。
- partial close signal id 和预算消耗改为执行成功后回写；评估阶段只做 `HasPositionSignal` 检查。
- 累计减仓预算改为基于原始可跟踪仓位数量，并记录实际成交数量与估算标记。
- `DecisionAction` 增加请求比例、实际执行比例、最终动作和 close quantity，用于执行结果回写与审计。
- 前端类型更新明确优先修改实际导入的 `web/src/types.ts`，并同步 `web/src/types/index.ts`。
- 程序化策略分析文案新增 i18n key，避免继续固定显示 “AI思维链分析”。
- 空 signal report 的 timeframe 元数据由 Engine/AutoTrader 提供，manager 不自行猜测。
- marker 恢复通过 bounded `RecentSignalMarkers` 持久化，支持刷新或重启后恢复最近信号。

## Findings

### 1. `partial_close_cooldown_minutes=0` 语义与 design 结构冲突

Severity: High

Requirements 明确要求：`partial_close_cooldown_minutes=0` 表示关闭跨规则冷却。
Design 中 `ProgrammaticPositionManagementConfig.PartialCloseCooldownMinutes int` 无法区分“字段缺失”和“显式配置为 0”。如果直接按 int 实现，`0` 很容易被归一化成默认 `15`，从而违反 Requirements 1.4。

Reference:

- `requirements.md` Requirement 1.4
- `design.md` Config snippet

Recommendation:

- design 改为 `PartialCloseCooldownMinutes *int`。
- normalize 逻辑：
  - nil => default 15
  - pointer value 0 => disabled
  - value <0 or >1440 => error
- tasks 中单独增加测试：缺失为 15，显式 0 为关闭。

### 2. Design 内部对 signal id 写入时机存在矛盾

Severity: High

Design 前半段在 `evaluatePositionManagement()` 伪代码中仍写着 “mark signal id for candidate dedupe”，这接近当前代码的 eager mark 行为。后半段 “Candidate vs Executed State” 又明确推荐：评估阶段只检查，不立即 mark；执行成功后再 `MarkPositionSignal()` 和 `RecordProgrammaticPartialClose()`。

这两种行为会产生不同实盘结果。当前线上问题要避免“失败执行也消耗去重/预算”，因此应以后半段方案为准。

Reference:

- `design.md` Guard Evaluation Placement
- `design.md` Candidate vs Executed State
- current code: `strategy/chanlun/position_management.go` 当前在输出动作前调用 `StateStore.MarkPositionSignal()`

Recommendation:

- design 统一为：
  - evaluation 阶段调用 `HasPositionSignal()` / guard check，不落地成功状态。
  - validation reject 写 marker status `rejected`，不消耗预算。
  - execution success 后 mark signal + consume budget。
  - execution failure 写 marker status `failed` 或 diagnostics，不消耗预算。
- tasks 中新增明确任务：“拆分 HasPositionSignal 与 MarkPositionSignal，移除 partial close eager mark”。

### 3. “累计减仓 50%”需要按原始仓位计量，design 仅存 pct 不够精确

Severity: High

Requirements 说 `max_total_partial_close_pct=50` 表示同一持仓生命周期最多减掉“原始可跟踪仓位”的 50%。
但 design state 只保存 `TotalPartialClosePct`，如果简单累加决策比例会误算：

- 第一次减仓剩余仓位 30%，实际减掉原始仓位 30%。
- 第二次对剩余仓位再减 30%，实际只再减掉原始仓位 21%。
- 简单相加是 60%，真实原始仓位累计是 51%。

这会导致预算裁剪不准确。

Recommendation:

- state 增加 baseline：
  - `InitialTrackedQuantity`
  - `LastKnownQuantity`
  - `TotalPartialCloseQuantity`
  - 可选 `InitialTrackedValueUSD`
- 预算以实际成交数量 / 初始跟踪数量计算。
- 如果交易所返回无法可靠获得实际成交数量，则 fallback 使用保守估算，并在 explanation 标注 `estimated=true`。
- tasks 中新增执行层任务：`executePartialCloseWithRecord()` 需要把实际 close quantity / final action 回写到 engine callback。

### 4. `DecisionAction` 当前没有 close percentage 字段，执行回写信息不足

Severity: Medium

当前 `logger.DecisionAction` 有 `Quantity`，但没有 `ClosePercentage` 或 `RequestedClosePercentage/ExecutedClosePercentage`。Design 要基于执行成功结果更新预算，若只读 `Decision.ClosePercentage` 会无法区分：

- 请求减 30%，被预算裁剪为 20%
- 请求 partial close，但最小仓位逻辑自动修正为 full close
- 请求 partial close，但名义额过小跳过且没有真实平仓

Recommendation:

- `DecisionAction` 增加：
  - `requested_close_percentage`
  - `executed_close_percentage`
  - `final_action`
  - 可选 `close_quantity`
- `executePartialCloseWithRecord()` 在 skip、partial、auto full 三种路径都写明 final action 和 executed percentage。

### 5. Frontend 类型文件路径在 design 中不完整

Severity: Medium

当前前端同时存在：

- `web/src/types.ts`
- `web/src/types/index.ts`

`web/src/App.tsx` 和 `web/src/lib/api.ts` 实际通过 `./types` / `../types` 导入，TypeScript 通常会优先解析 `web/src/types.ts`。Design 只列出更新 `web/src/types/index.ts`，可能导致实现后类型未生效或两个类型文件继续漂移。

Recommendation:

- design/tasks 明确：
  - 优先更新实际 import 使用的 `web/src/types.ts`。
  - 同步或合并 `web/src/types/index.ts`，避免重复定义长期漂移。
- tasks 中新增“前端类型单源化或双文件同步检查”。

### 6. 程序化“AI思维链”文案需要同时改翻译键

Severity: Medium

Design 提到在 `web/src/App.tsx` 条件切换 “AI思维链分析” 为 “策略分析”。当前实际文案来自 `web/src/i18n/translations.ts` 的 `aiThinking`，不是 App 里硬编码字符串。只改 App 不改翻译键或新增翻译键会不完整。

Recommendation:

- 新增翻译键：
  - `strategyAnalysis`: `策略分析`
  - English 可用 `Strategy Analysis`
- `DecisionCard` 根据 `decision.decision_mode === "programmatic"` 选择 `strategyAnalysis`，否则 `aiThinking`。
- tasks 中补充 i18n 修改。

### 7. 空信号 report 返回 timeframe 元数据需要 AutoTrader/Engine 新接口

Severity: Medium

Design 要求 `/api/strategy/signals` 即使无信号也返回 `trade_timeframe`。当前 `TraderManager.GetLatestStrategySignals()` 在 engine 没有 report 时手工构造空 `SignalReport`，但 manager 层拿不到 engine policy 细节。

Reference:

- current code: `manager/trader_manager.go` `GetLatestStrategySignals`
- current code: `trader/auto_trader.go` `GetLatestStrategySignals`

Recommendation:

- 在 `chanlun.Engine` 增加 `EmptySignalReport(traderID, symbol string)` 或 `TimeframeMetadata()`。
- `AutoTrader.GetLatestStrategySignals()` 在无内存 report 时返回带 policy timeframe 的空 report。
- manager 层不应自行猜 timeframe。

### 8. Position management marker 持久化边界仍偏弱

Severity: Medium

Requirements 13 要求页面刷新后能从后端恢复最新信号、K 线标记和诊断。Design 的 Open Implementation Notes 允许 “First implementation may return empty markers until next cycle”，这与 Requirements 13.5 有张力。

Recommendation:

- 若本期必须满足 Requirements 13.5，则 design 要规定从 `StateStore` 和最近决策日志恢复最近 N 个 markers。
- 若允许首期弱化，则 requirements 需要降级为 SHOULD 或明确“进程重启后下一周期恢复”。
- 由于用户要求实盘可观察性，推荐保留 requirements，design 增加 “reconstruct markers from state + recent decision logs” 为实现任务。

### 9. `structure_break` guard 行为需要更明确的默认

Severity: Low

Requirements 4.1 说结构破坏 partial close 遇到冷却/预算时“根据配置决定跳过、裁剪或升级”。Design 默认 `bypass_cooldown_clip_budget` 合理，但没有明确预算耗尽时行为：是跳过还是全平。

Recommendation:

- 明确：
  - `bypass_cooldown_clip_budget`: 忽略冷却；若有剩余预算则裁剪执行；若预算为 0 则跳过并诊断。
  - `close_on_budget_exhausted`: 预算为 0 时升级为 close。
  - `respect_guard`: 冷却/预算任一不满足就跳过。

### 10. tasks.md 缺失

Severity: Blocking

当前 spec 目录只有 `requirements.md` 和 `design.md`，没有 `tasks.md`。用户已要求“按照 spec 模式先做交叉验证”，因此本次不应执行实现；下一步应先修正 design，再重新生成 tasks。

Recommendation:

- 先按本 review 修正 design。
- 再生成 `tasks.md`，任务需覆盖本 review 中的关键风险。
- 再由用户确认后执行。

## Requirements Coverage Matrix

| Requirement | Design Coverage | Review |
| --- | --- | --- |
| 1 跨规则冷却 | 覆盖 | 需修正 `0` 关闭冷却的 config 表达 |
| 2 减仓预算 | 覆盖 | 需补原始仓位 baseline/实际数量计量 |
| 3 浮盈回撤二次触发 | 覆盖 | 基本一致 |
| 4 结构破坏与强制风控豁免 | 覆盖 | 需补预算耗尽默认语义 |
| 5 持仓管理配置 | 覆盖 | 需修正 pointer config 与测试 |
| 6 状态持久化 | 覆盖 | 需补 baseline quantity 和 marker 恢复 |
| 7 每次开平仓原因说明 | 覆盖 | 需补 i18n 变更 |
| 8 结构化原因 schema | 覆盖 | 基本一致 |
| 9 主交易级别 K 线 | 覆盖 | 需补空 report timeframe 接口 |
| 10 K 线买卖点标记 | 覆盖 | 需补重启/刷新恢复策略 |
| 11 最新信号栏增强 | 覆盖 | 基本一致 |
| 12 API 契约 | 覆盖 | 基本一致 |
| 13 日志与前端一致 | 部分覆盖 | marker 持久化/恢复需加强 |
| 14 测试要求 | 覆盖 | tasks 需要落到具体测试文件 |

## Existing Code Fit

当前代码适合承接该设计的点：

- `config.NormalizeProgrammaticStrategies()` 已有程序化配置归一化入口。
- `decision.ProgrammaticStrategyPolicy` 已作为 manager 到 engine 的运行时 policy。
- `strategy/chanlun.StateStore` 已按 trader/symbol/side 保存持仓状态。
- `AutoTrader.executeDecisionWithRecord()` 是统一执行和记录入口，适合挂执行结果回调。
- `/api/strategy/signals` 和 `/api/market/klines` 已存在，前端策略检查面板已有基础布局。

当前代码需要特别改造的点：

- `StateStore.MarkPositionSignal()` 当前会在策略输出阶段写入处理状态，需要拆成“检查”和“成功确认”。
- `executePartialCloseWithRecord()` 需要显式回传 skip/partial/full 的最终结果。
- 前端类型文件重复，需避免只改一个导致类型漂移。

## Recommended Next Step

1. 修正 `design.md` 中上述 High/Medium 问题。
2. 重新生成 `tasks.md`。
3. 再执行实现任务。
