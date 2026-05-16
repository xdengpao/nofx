# ATR ADX Risk Optimization Consistency Review

## Review Scope

本次交叉检查覆盖 `.kiro/specs/atr-adx-risk-optimization/requirements.md`、`design.md`、`tasks.md` 与现有代码实现，重点核对 `config`、`manager`、`trader`、`decision`、`market`、`logger`、`cmd/replay` 的落点是否一致。

未读取或复制运行时凭证，未修改 `data/`、`decision_logs/`、`config.json` 等运行时状态。

## Overall Result

规格方向与用户诊断一致，也符合 NOFX 现有架构：系统已经有分批止盈、ATR trailing、open gate、loss mode、相关性 gate、replay 基础和交易计划持久化，因此优化可以在现有模块上扩展。

原始 spec 还不能直接进入实现。最大问题是执行层会在开仓后立刻挂整仓交易所 TP 保护单，如果该 TP 沿用原始 AI 微止盈价，就会绕过本地“分批止盈/移动止盈”的严格计划。用户已明确要求：执行层整仓 TP 保护必须保留，但距离可由算法调整。因此修订方向是保留 `SetTakeProfit()`，并把价格改为规范化后的 algorithmic full TP。

## Findings

### F1 Critical: 严格出场策略必须保留整仓交易所 TP，但不能使用微止盈价

证据：

- `trader/execution_protection.go:24` 的 `setProtectiveOrdersWithRecord()` 会先挂止损，再调用 `SetTakeProfit()`。
- `trader/auto_trader.go:1347` 与 `trader/auto_trader.go:1434` 在创建交易计划前就调用保护单设置。
- `decision/takeprofit.go:164` 的本地 `PositionEvaluator` 仍把固定 TP 放在分批和 ATR trailing 前面。

影响：

如果 `PositionEvaluator` 忽略微止盈，但交易所上的整仓 TP 订单仍使用原始 AI 微止盈价，该订单可能先成交，把仓位全平。这样“微止盈绕过趋势持有”的病因不会被真正消除。

修正要求：

- design 需要新增执行层 algorithmic full TP 保护策略。
- strict/trend profile 必须继续挂整仓 TP，但不得挂近距离 raw AI TP；TP 价格必须重写到满足 min RR、profile、final planned R 的算法距离。
- order tracker/replay 必须识别该 TP 模式，区分交易所 TP 自动平仓与系统主动分批/跟踪平仓。

### F2 High: 策略风险配置尚无传递路径

证据：

- `config.Config` 只有 `TradingFrequency`，没有 `StrategyRisk`。
- `manager.AddTraderWithFrequency()` 只传入频率 profile。
- `trader.AutoTraderConfig` 只有 `FrequencyPolicy`。
- `decision.Context` 只有 `FrequencyPolicy`、`FrequencyState`、`LossMode`，没有策略风险 policy。

影响：

requirements/design 中的 instrument profile、min stop、ADX timeframe、profile max risk、fixed TP mode 没有运行时入口。

修正要求：

- tasks 2.6 应明确新增 `AddTraderWithPolicies()` 或在 manager/AutoTraderConfig 中并行传入 `StrategyRiskPolicy`。
- `AutoTrader.GetStatus()` 需要暴露 active strategy risk summary，不能只显示 frequency policy。
- 缺省配置必须保持旧行为；启用 `strategy_risk` 后再进入 strict/safe 逻辑。

### F3 High: 开仓验证顺序与设计不一致

证据：

- `decision/decision.go:802` 的 `validateOpenDecision()` 先运行 `EvaluateOpenGate()`。
- 止损/止盈、RR 和 sizing 校验在 gate 之后才执行。
- design 要求先 resolve profile、normalize stop/TP/sizing，再进入 profile-aware ADX gate 和相关性 gate。

影响：

gate 当前无法基于 final stop distance、profile、normalized risk 和 selected ADX timeframe 做硬判定；也无法把 rewrite/reject 诊断完整写入 open rejection。

修正要求：

- design/tasks 需要明确新顺序：market data -> profile -> risk normalization -> ADX/profile gate -> invalidation -> duplicate/leverage -> net RR -> sizing -> final decision fields。
- `OpenGateInput` 应显式携带 profile/policy/normalization snapshot，避免 gate 从全局字段猜。

### F4 High: 1h ADX/DI gate 的数据与算法仍不满足需求

证据：

- `market.MidTermData1h` 有 `ADXValues` 和 `ATRValues`，但没有 `DIPlus`/`DIMinus`。
- 4h `LongerTermData` 已有 DI series。
- `market.calculateADX()` 当前最后仍是 `adx = dx`，注释说明未做完整 ADX 平滑。
- open gate 仍主要使用 `CurrentADX/CurrentDIPlus/CurrentDIMinus`，这些字段来自 4h。

影响：

“1h ADX < 20 禁止新开趋势单”和 “ADX > 25 只允许 DI 顺势”无法可靠实现，且算法修正会改变已有 gate 行为。

修正要求：

- Phase 3 需要同时给 1h/15m 补 DI series，或至少给 1h gate 提供 `DirectionalIndicatorSnapshot`。
- ADX 算法变更应先加 report-only 对比，避免一次性改变所有 prompt 和 gate 语义。
- 现有 4h top-level 字段保留兼容，新 gate 只通过 timeframe helper 取值。

### F5 Medium: `stop_distance_pct` 语义是 ratio，不能直接改成 percent

证据：

- `decision.Decision.StopDistancePct`、`logger.DecisionAction.StopDistancePct`、`PositionSizingResult.StopDistancePct` 都使用 `stop_distance_pct`。
- `CalculatePositionSizing()` 里该值是 `abs(price-stop)/price`，即 ratio，例如 `0.032` 表示 3.2%。

影响：

如果新代码把 `stop_distance_pct` 改成 percent，旧日志、replay、前端和测试会被误读或双重归一化。

修正要求：

- 保留旧 `stop_distance_pct` 的 ratio 语义直到迁移完成。
- 新增 `stop_distance_ratio` 与 `stop_distance_percent`，并在日志/API/replay 中明确单位。
- percent 配置归一化必须有单一 helper 和测试覆盖 `0.01`、`1.0`、`1%` 语义边界。

### F6 Medium: sizing 输入已有手续费滑点，任务描述需要调整

证据：

- `decision.PositionSizingInput` 已有 `FeeSlippagePct`。
- `CalculatePositionSizing()` 已用 `stopDistancePct + FeeSlippagePct` 计算风险上限。
- result/log 仍主要记录 stop-only risk，没有 total risk/reserve 字段。

影响：

任务 5.1 不能简单写成“新增 FeeSlippagePct 输入”，否则容易重复实现。

修正要求：

- 修改任务为扩展 `PositionSizingResult` 与 logger 字段：`FeeSlippageReserveUSD`、`TotalRiskUSD`、`TotalRiskPct`、`RiskCapReason`。
- 保留 `RiskUSD` stop-only 兼容字段，同时输出 total risk 审计字段。

### F7 Medium: TradePlan 扩展需要覆盖 legacy 和 recovered plan

证据：

- `CreateTradePlanFromDecision()` 只持久化基础 SL/TP、RiskUSD、ExecutedTranches、EntryATR 等字段。
- `OnPositionOpenedScoped()` 直接创建 plan 并保存。
- 已有恢复/持久化路径会加载旧 `data/trade_plans.json`。

影响：

新增 `InitialRiskDistance`、`ProfileName`、`ExchangeFullTakeProfit`、`ExchangeFullTPMode` 等字段是 JSON 向后兼容的，但旧计划或恢复计划没有这些字段时，R multiple 和 fixed/full TP 策略必须有 fallback。

修正要求：

- tasks 8.2 需要明确 legacy plan loading 与 recovered existing positions 的测试。
- R multiple fallback 使用 `abs(entry-stop)`，但 strict exit mode 只能对带新字段或明确 fallback 的计划启用。

### F8 Medium: 同向/相关性 gate 已有基础，但不是 profile/loss-mode 完整版本

证据：

- `applySameSideExposureGate()` 对高 beta 多单有特殊限制，但不是对 short 对称。
- `applyCorrelationConcentrationGate()` 对同向高相关有静态阈值 2，未按 profile/loss/range 调整。
- 浮亏同向 block 已存在，但阈值是固定常量。

影响：

ETH/BCH 同向空单叠加能部分被相关性 gate 覆盖，但无法满足“亏损期/震荡期 max 1、profile-specific limit、第二笔降风险”的完整需求。

修正要求：

- Phase 7 应把 same-side exposure 和 correlation gate 合并为 profile-aware 规则。
- diagnostics 需要输出 target symbol、existing symbols、side、profile、correlation state、final risk multiplier。

### F9 Medium: replay 病因诊断必须兼容旧日志

证据：

- `logger.ReplayReport` 已有 rejection、dedup close、exchange reconciliation、rolling、execution quality。
- 旧日志没有 requested/final stop/TP、profile、risk normalization 字段。
- `cmd/replay` 已支持只读 `-exchange-close-json`，适合作为扩展入口。

影响：

如果 disease buckets 只读取新字段，无法复盘 2026-05-13 至 2026-05-16 的问题样本。

修正要求：

- Phase 10 要明确旧日志 fallback：从 action、decision、trade plan、exchange close snapshot 推导 stop distance、TP distance、close source 和 R multiple。
- 只有同时具备 requested/final 字段时才做 AI requested vs final 对比。

### F10 Medium: 默认严格行为与 legacy 兼容描述需要统一

证据：

- requirements 要求 enabled instruments 默认 `min_stop_pct` 不低于 1%。
- design normalization 又写明缺失 `strategy_risk` 时保持当前行为。

影响：

实现时可能出现两种解读：部署后自动强制所有旧配置进入 1% 硬地板，或只有配置块存在才强制。

修正要求：

- 明确：缺失 `strategy_risk` 保持 legacy；一旦配置块启用，所有 enabled profiles 默认 min stop floor 不低于 1%。
- 上线到本机/161 时必须显式写入 safe/strict 配置块，而不是依赖代码隐式改变旧配置。

### F11 Low: XAG/非加密 profile 要用显式 symbol list

证据：

- 现有默认池主要是 USDT 加密资产，代码没有非加密合约分类器。
- 用户诊断包含 XAG，且 XAG 与加密币波动、交易时段、流动性不同。

影响：

如果靠 `MatchQuote` 或粗略 `MatchType`，XAG-like 品种容易落入 default/high_beta_alt。

修正要求：

- non_crypto profile 应要求 `Symbols` 显式包含 XAG-like symbols；未命中时默认禁用或使用最保守 profile。

### F12 Low: API/frontend/status 是兼容工作，不是风控主路径

证据：

- `AutoTrader.GetStatus()` 目前只返回 frequency policy/state。
- 前端类型需要跟 backend JSON 字段同步，但风控不能依赖 UI。

修正要求：

- tasks 11 保持为兼容/可观测性任务即可。
- 后端 strict validation、execution TP mode 和 replay diagnostics 必须先完成。

## Coverage Matrix

| Spec area | Existing coverage | Gap |
| --- | --- | --- |
| ATR stop floor | ATR and sizing exist | 无 profile floor/rewrite |
| Net RR | `validateOpenDecision()` 已有 2.5 RR | 未基于 rewrite 后 TP/SL 统一计算 |
| Risk sizing | 已按 stop+fee/slippage 限制仓位 | 缺 total risk logging/profile cap |
| ADX gate | 有 market state/high ADX chase | 缺 1h DI、低 ADX hard block、算法平滑 |
| Profiles | 无 | 需要 config/runtime DTO/profile resolver |
| Correlation | 有高相关和同向基础 gate | 缺 profile/loss/range 动态阈值 |
| Exit policy | 有分批/ATR trailing | 整仓交易所 TP 必须保留，但价格要改为 algorithmic full TP |
| Replay | 有 close dedup/exchange reconciliation | 缺 disease buckets 和旧日志 fallback |
| Prompt | 有市场/候选输出 | 缺 profile risk boundary/executable state |
| API/status | 有 frequency 状态 | 缺 strategy risk summary |

## Required Spec Adjustments Before Implementation

1. 在 design 中新增执行层 algorithmic full TP 保护策略，明确 strict profile 必须挂整仓 TP，但价格不得使用 raw AI 微止盈。
2. 在 tasks 中新增或强化 execution/order tracker 测试，覆盖 strict TP mode、`SetTakeProfit()` 价格选择、交易所 TP 自动成交、分批退出和 replay close source。
3. 修改 Phase 2 的配置传递任务，使用 `StrategyRiskPolicy` 从 `config` 传到 `manager`、`AutoTraderConfig`、`decision.Context`、`GetStatus()`。
4. 修改 Phase 4/6 的调用顺序，明确 normalization 在 profile-aware gate 之前完成。
5. 修改 Phase 3，要求 1h DI series 和 Wilder ADX 先以 report-only 或测试对比方式落地。
6. 修改 Phase 5，避免重复新增 `FeeSlippagePct` input，改为补 total risk/result/log 字段。
7. 修改 Phase 10，要求 disease replay 支持旧日志 fallback。
8. 明确 legacy 兼容：缺失 `strategy_risk` 不改变旧行为；启用配置块后严格执行 min stop floor。

## Go/No-Go

当前结论：原始版本 No-Go；按用户确认的修订项完成后，可进入实现任务拆解/执行。

先修订 design/tasks 后再进入编码。优先修正 F1、F2、F3、F4；否则实现会出现“本地逻辑看似严格，交易所订单仍按旧微止盈/旧 gate 行为成交”的风险。
