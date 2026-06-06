# Chanlun V2 Signal Freshness Time Alignment Requirements

## 背景

线上 `Aster Chanlun V2 Trader` 在周期 #3 附近出现 BNBUSDT `open_rejected`。用户从策略检查图上看到的 BNB 信号结构时间是 `2026-05-23 01:59:59`，而周期执行时间是 `2026-05-23 19:51:49`，直观看起来“开仓信号时间”和“策略检查信号时间”不一致。

排查结果显示，两边的核心信号身份实际一致：

- `signal_id = chanlun_v2:BNBUSDT:1h:buy2:1779472799999`
- `signal_close_time = 1779472799999`
- `decision_close_time = 1779472799999`

问题不在字段串错，而在两个设计缺口：

1. 缠论 V2 反复把很早的 1h `buy2` 信号继续作为开仓候选，缺少独立的 stale signal / age gate。
2. `decision_close_time` 目前在缠论 V2 中等于结构信号 K 线时间，不能表达“本轮评估/拒绝动作发生时间”，策略检查页也没有清晰区分结构时间、信号确认时间和本轮动作时间。

这会导致用户误解信号来源，也会造成过期信号在每个周期反复进入 open gate，增加 open rejection 噪声和 `runaway_rejection_loop` 风险。

## 功能摘要

本优化要求：

- 缠论 V2 开仓候选必须经过信号新鲜度检查，过期信号不得继续进入开仓验证和 open gate。
- 决策日志、策略检查 API 和前端图表必须同时保留并展示结构时间、信号确认时间、本轮评估/动作时间。
- 策略检查页应让用户一眼判断：图上 marker 属于哪根 K 线，本轮周期何时评估/拒绝了它，二者是否相差较久。
- 不降低现有风控：BTC 多周期、ADX/DI、profile、仓位 sizing、preflight 等 gate 仍按原逻辑执行。

## 术语

- **结构时间 / Signal Close Time**：信号所属结构或 K 线的 close time，例如 `signal_close_time`。
- **决策锚点时间 / Decision Close Time**：用于在策略检查图上定位交易动作 marker 的 K 线 close time。对于 closed-kline 策略，必须来自本轮策略实际分析的最新闭合交易级别 K 线，不允许另抓一份可能不一致的行情作为锚点。
- **动作发生时间 / Action Timestamp**：本轮周期实际生成执行动作、open rejection 或 wait 记录的 wall-clock 时间，通常来自 `DecisionAction.Timestamp`。
- **信号年龄 / Signal Age**：从 `signal_close_time` 到当前评估锚点的交易级别 K 线数量或时间跨度。
- **Stale Signal**：超过配置允许寿命、目标已穿越、RR 已失效或入场条件不再成立的旧信号。
- **Trade Timeframe**：缠论 V2 主交易级别，当前线上为 `1h`。

## Requirements

### Requirement 1: 缠论 V2 开仓信号必须有新鲜度门控

**User Story:** 作为交易监督者，我希望旧的缠论 V2 信号不会在多个小时后继续触发开仓候选，以避免过期信号反复被风控拒绝。

#### Acceptance Criteria

1. WHEN `chanlun_v2` 生成 open-like 决策 THEN 系统 SHALL 计算该信号相对当前交易级别评估锚点的年龄。
2. IF 信号年龄超过硬过期阈值 THEN 系统 SHALL NOT 将该信号送入 `ValidateStrategyDecisions()` 或 `EvaluateOpenGate()`。
3. IF 信号目标价已经被当前价格穿越或剩余 RR 不满足配置 THEN 系统 SHALL 将该信号标记为 stale/invalid，不得继续作为开仓候选。
4. WHEN 信号处于软老化区间但未硬过期 THEN 系统 MAY 降低置信度或提高入场要求，并 SHALL 输出可诊断原因。
5. WHEN 信号因 stale gate 被拒绝 THEN 决策日志和策略检查 marker SHALL 记录 `stale_signal`、年龄、阈值、评估锚点和拒绝原因。

### Requirement 2: 缠论 V2 时间字段必须表达真实语义

**User Story:** 作为策略开发者，我希望从日志和 API 能明确区分结构信号时间、本轮评估时间和动作发生时间，便于对账图表与周期记录。

#### Acceptance Criteria

1. WHEN 缠论 V2 产生 `Decision` THEN `signal_close_time` SHALL 表示结构信号所属 K 线时间。
2. WHEN 缠论 V2 在当前周期评估该信号 THEN `decision_close_time` SHALL 表示本轮使用的最新闭合 trade timeframe K 线 close time，且不得早于 `signal_close_time`。
3. WHEN 当前周期已经通过 `PrepareCycleContext` 准备了 closed K 线 THEN 缠论 V2 SHALL 优先复用同一批 `ctx.MarketDataMap` K 线进行分析和评估锚点计算，避免分析、风控和策略检查时间源分裂。
4. WHEN `DecisionAction` 被写入日志 THEN `timestamp` SHALL 继续表示真实动作发生时间。
5. IF 需要兼容旧日志 THEN `close_time` 和旧含义字段 SHALL 保持可读，不应破坏已有前端和 replay。
6. WHEN `OpenRejection` 从缠论 V2 `Decision` 创建 THEN SHALL 保留 `signal_id`、`signal_close_time`、`decision_close_time`、`action_timestamp` 或等价动作时间字段。

### Requirement 3: 策略检查页必须清楚展示三类时间

**User Story:** 作为前端用户，我希望策略检查页能告诉我信号是哪根 K 线产生的，以及本轮策略何时评估/拒绝它，避免把旧结构信号误认为当前刚出现的信号。

#### Acceptance Criteria

1. WHEN marker 是交易动作、拒绝或失败 THEN 图表 SHALL 使用 `decision_close_time` 作为动作 marker 锚点；结构点仍 SHALL 使用 `signal_close_time`。
2. WHEN `signal_close_time != decision_close_time` THEN tooltip 或右侧最新信号 SHALL 同时显示“结构时间”和“评估K线时间”。
3. WHEN action timestamp 可用 THEN tooltip 或右侧最新信号 SHALL 显示“动作时间/记录时间”。
4. WHEN 信号年龄超过软阈值 THEN 前端 SHALL 显示年龄或 stale 警示，不只显示 `rejected`。
5. IF 配对结构点或动作点不在当前 K 线窗口内 THEN 前端 SHALL 明确提示配对时间不在当前图表范围。

### Requirement 4: 策略检查 API 与决策日志必须可对账

**User Story:** 作为维护者，我希望能用 `signal_id` 和时间字段把 `/api/decisions/latest` 的 open rejection 与 `/api/strategy/signals` 的 marker 一一对应。

#### Acceptance Criteria

1. WHEN `/api/decisions/latest` 返回 BNBUSDT `open_rejected` THEN 对应 `signal_id` SHALL 能在 `/api/strategy/signals?symbol=BNBUSDT` 中找到同一 marker。
2. WHEN 同一 `signal_id` 被本轮拒绝 THEN marker 状态 SHALL 更新为 `rejected`，原因 SHALL 与 `DecisionAction.error` 或 `OpenRejection.reason` 一致。
3. WHEN stale gate 在 open gate 前拒绝信号 THEN marker SHALL 进入 `invalid_rejected` 或等价可识别状态，且不会产生 open gate rejection 噪声。
4. API SHALL 保持 trader-scoped 行为，所有策略信号接口必须继续使用 `trader_id` scope。

### Requirement 5: 安全边界不得降低

**User Story:** 作为系统负责人，我希望减少旧信号噪声不等于放松开仓风控。

#### Acceptance Criteria

1. WHEN 信号通过 freshness gate THEN 仍 SHALL 经过仓位补全、`ValidateStrategyDecisions()`、open gate、最终频率/持仓限制和执行 preflight。
2. WHEN BTC 多周期转弱、ADX/DI 不一致、相关性集中、亏损模式、执行质量风险等 gate 命中 THEN 系统 SHALL 保持现有拒绝或降权行为。
3. WHEN freshness gate 拒绝信号 THEN 系统 SHALL NOT 写入真实成交、交易计划或 executed marker。
4. Tests SHALL NOT 触发真实交易所下单。

### Requirement 6: 可验证性和回归覆盖

**User Story:** 作为开发者，我希望有测试证明 BNB 这类旧信号不会重复进入开仓 gate，且前端展示的时间语义稳定。

#### Acceptance Criteria

1. Unit tests SHALL cover `chanlun_v2` fresh、soft-aged、hard-expired、target-crossed、RR-invalid open signals。
2. Unit tests SHALL cover `signal_close_time < decision_close_time` 的 rejected marker 和 DecisionAction 元数据。
3. API or manager tests SHALL cover `/api/strategy/signals` 与 `/api/decisions/latest` 能通过 `signal_id` 对账。
4. Frontend tests SHALL cover action marker 使用 `decision_close_time` 锚定，结构 marker 使用 `signal_close_time` 锚定。
5. Backend tests SHALL cover `decision_close_time` 来自本轮已分析 closed K 线，而不是独立二次抓取结果。
6. Existing backend and frontend targeted tests SHALL continue passing.
