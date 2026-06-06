# 盈利导向策略防护优化 Requirements

## 背景

对 2026-05-06 和 2026-05-07 的 `aster_deepseek` 历史决策日志复盘后，当前策略的主要亏损来源不是交易频率不足，而是：

- 单边偏多，在市场转弱后仍继续开山寨多单。
- 将极高 ADX 当成强趋势优势，导致 HYPE、ZEC、SOL 等追高入场。
- 盈利单回撤后才触发利润保护，ETH/XRP 的峰值利润保留率偏低。
- 亏损单缺少保护期后的软止损，HYPE 从小幅浮盈转为大额亏损后才平仓。

本规格的目标是把复盘结论固化为可测试的开仓准入和出场规则，提高策略正期望，不扩大交易所执行范围。

## 目标

1. 降低弱市场和趋势末端的追多概率。
2. 降低同方向高 beta 持仓叠加风险。
3. 提前锁定浮盈，减少盈利回吐。
4. 对入场后未兑现动量的亏损单执行软止损。
5. 在近期亏损后自动降低下一笔交易风险。

## 非目标

- 不新增交易所、不修改交易所 API 签名或下单协议。
- 不改动 API key、私钥、账户配置或真实凭证。
- 不重写 AI 决策框架；AI prompt 可补充规则，但核心约束必须在本地验证层执行。
- 不追求通过增加交易频率盈利；优化重点是过滤低质量交易和控制盈亏分布。

## 术语

- **杠杆收益百分比**：系统持仓字段 `UnrealizedPnLPct`，已反映杠杆后的浮动盈亏比例。
- **高 beta 山寨币**：除 `BTCUSDT`、`ETHUSDT` 外的币种。
- **多周期趋势一致**：目标方向与 BTC 的 15m、1h、4h 指标方向没有明显冲突。
- **趋势末端追入**：ADX 极高且价格短期涨幅过大时继续同向开仓。
- **软止损**：未触发硬止损前，基于持仓时长、MFE、当前 PnL 和市场动量提前退出。
- **MFE**：持仓期间最大有利浮盈，使用 `TradePlan.PeakPnLPercent` 追踪。

## Requirements

### Requirement 1: BTC 多周期市场闸门

**User Story:** 作为量化交易员，我希望系统在 BTC 多周期转弱或不一致时阻止新开山寨多单，以避免市场转弱阶段继续加多。

#### Acceptance Criteria

1. WHEN AI 输出 `open_long` 且标的是高 beta 山寨币 THEN 系统 SHALL 检查 BTC 的 15m、1h、4h 趋势状态。
2. IF BTC 1h 或 4h 为明显空头结构 THEN 系统 SHALL 拒绝新的高 beta 山寨多单。
3. IF BTC 15m 与 1h/4h 方向冲突 THEN 系统 SHALL 至少将新高 beta 山寨多单降权，并提高最低置信度。
4. IF BTC 1h 跌幅达到强风险阈值 THEN 系统 SHALL 继续沿用现有硬阻断逻辑，拒绝所有新开仓。
5. WHEN 拒绝或降权发生 THEN `OpenGateResult.Reasons` SHALL 记录可读原因。

### Requirement 2: 同方向持仓集中度控制

**User Story:** 作为量化交易员，我希望系统在已有多个同向多单时不要继续叠加高 beta 风险，以降低组合回撤。

#### Acceptance Criteria

1. WHEN 当前账户已有 2 个或以上 `long` 持仓 AND AI 输出新的高 beta `open_long` THEN 系统 SHALL 拒绝该开仓。
2. WHEN 当前账户已有 1 个 `long` 高 beta 持仓 AND AI 输出新的高 beta `open_long` THEN 系统 SHALL 降权该开仓风险。
3. IF 任一现有同方向持仓的 `UnrealizedPnLPct <= -4%` THEN 系统 SHALL 拒绝新的同方向开仓。
4. WHEN 触发集中度控制 THEN 系统 SHALL 在开仓验证日志中体现拒绝或降权原因。

### Requirement 3: 高 ADX 追高过滤

**User Story:** 作为量化交易员，我希望系统不要把极高 ADX 简单视为优势，而是识别趋势末端追入风险。

#### Acceptance Criteria

1. WHEN 高 beta `open_long` 的目标币种 `CurrentADX > 60` THEN 系统 SHALL 将其视为高追涨风险。
2. IF `CurrentADX > 60` AND 1h 涨幅或 4h 涨幅超过阈值 AND 当前价格没有回踩确认 THEN 系统 SHALL 拒绝该开仓。
3. IF `CurrentADX > 50` 但未达到拒绝条件 THEN 系统 SHALL 将仓位风险至少减半，并提高最低置信度。
4. WHEN AI 输出理由包含“强趋势”但本地指标触发追高过滤 THEN 本地过滤 SHALL 优先于 AI 理由。

### Requirement 4: 盈利保护收紧

**User Story:** 作为量化交易员，我希望盈利单更早锁定利润，避免 ETH/XRP 这类峰值收益大幅回吐。

#### Acceptance Criteria

1. WHEN 持仓浮盈达到 `+6%` 杠杆收益 THEN 系统 SHALL 允许移动止损到保本或小幅盈利位置。
2. WHEN 持仓峰值盈利达到 `+10%` 杠杆收益 THEN 系统 SHALL 触发至少一档分批止盈或更紧的移动止损。
3. WHEN 峰值盈利达到 `+12%` 杠杆收益 THEN 利润保护线 SHALL 不低于峰值盈利的 `65%`。
4. WHEN 峰值盈利达到 `+8%` 但低于 `+12%` THEN 利润保护线 SHALL 不低于峰值盈利的 `60%`。
5. IF 当前盈利跌破利润保护线 THEN 系统 SHALL 输出平仓决策并保留“峰值、当前、保护线”原因。

### Requirement 5: 保护期后的软止损

**User Story:** 作为量化交易员，我希望入场后没有兑现动量的亏损单能提前退出，避免 HYPE 这类亏损扩大。

#### Acceptance Criteria

1. WHEN 持仓超过最小持仓保护期 AND `UnrealizedPnLPct <= -5%` THEN 系统 SHALL 平仓。
2. WHEN 持仓超过 60 分钟 AND MFE 未达到 `+3%` AND 当前 `UnrealizedPnLPct <= -3%` THEN 系统 SHALL 平仓。
3. WHEN 持仓曾经盈利 AND 当前跌回开仓价以下 AND BTC 或目标币短周期动量转弱 THEN 系统 SHALL 平仓或至少输出可执行的减仓决策。
4. Soft stop SHALL 不影响保护期内 `-3%` 极端亏损硬平仓的现有优先级。

### Requirement 6: 近期亏损后的风险降档

**User Story:** 作为量化交易员，我希望连续亏损后下一笔交易自动减仓，而不是马上用同等风险继续交易。

#### Acceptance Criteria

1. WHEN 最近两笔已闭合交易均亏损 THEN 系统 SHALL 将下一笔开仓风险至少减半。
2. WHEN 最近三笔已闭合交易中亏损不少于两笔 AND 总 PnL 为负 THEN 系统 SHALL 提高最低开仓置信度。
3. WHEN 滚动表现缺少足够闭合交易样本 THEN 系统 SHALL 不因样本不足错误阻断交易。
4. Risk downgrade SHALL 通过已有 `RollingPerformanceSnapshot` / `OpenGateResult` 机制传递，不新增运行时全局状态。

### Requirement 7: Prompt 与本地规则一致

**User Story:** 作为量化交易员，我希望 AI 收到的开仓说明与本地硬约束一致，减少无效开仓建议。

#### Acceptance Criteria

1. WHEN 系统构建开仓 prompt THEN SHALL 明确说明 BTC 多周期闸门、极高 ADX 追高风险、同向持仓限制和亏损后减仓规则。
2. IF AI 仍输出违反本地规则的开仓 THEN 本地验证 SHALL 拒绝或降权。
3. Prompt 文案 SHALL 保持中文，JSON 字段 SHALL 保持现有英文结构。

### Requirement 8: 可验证性与回归安全

**User Story:** 作为维护者，我希望策略规则有聚焦测试，避免后续改动破坏风控。

#### Acceptance Criteria

1. SHALL 为开仓闸门新增单元测试，覆盖 BTC 转弱、同向集中、浮亏持仓、高 ADX 追高和降权场景。
2. SHALL 为出场规则新增单元测试，覆盖 6% 保本、10% 分批/移动、12% 利润保护线、-5% 软止损和 MFE 失败退出。
3. SHALL 保留现有保护期极端亏损、硬止损、固定止盈和移动止损单调性的测试语义。
4. Validation SHALL 至少运行 `go test ./decision`；如修改 logger 滚动绩效逻辑，还 SHALL 运行 `go test ./logger ./decision`。
