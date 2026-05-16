# ATR ADX Risk Optimization Requirements

## Background

本规格用于把 2026-05-13 至 2026-05-16 实盘订单与 NOFX 当前代码实现做交叉验证后，修正智能交易策略中的结构性风险。目标不是“增加更多 prompt 文案”，而是把止损、止盈、市场状态、品种差异、仓位 sizing 和相关性暴露升级为确定性硬约束。

交叉验证结论：

1. 当前代码已经有 ATR、ADX、DI、分批止盈、移动止损、BTC gate、相关性门控和 loss mode 的基础实现。
2. `decision.ValidateAndEnrichDecision()` 仅在 AI 缺失止损/止盈时用 ATR 补齐；如果 AI 已给出止损/止盈，系统会继续使用 AI 价格，只做方向、净 RR 和 sizing 校验。
3. `validateOpenDecision()` 已强制净 RR `(rewardPct - 0.2) / riskPct >= 2.5`，但没有强制“最小止损距离硬地板”或“最小止盈距离硬地板”。
4. `PositionSizing` 会用止损距离计算最大仓位，但 `RiskUSD` 当前主要记录价格触发风险，手续费滑点只参与上限计算，不完整反映总风险。
5. `decision/takeprofit.go` 已有 ATR trailing、分批止盈和移动止损，但固定 TP 仍在更高优先级，一旦 AI 给出过近 TP，系统可能先执行全平固定止盈，绕过趋势持有和分批退出。执行层仍必须保留整仓 TP 保护单，但 TP 价格必须由规范化算法重写到合格距离，不能直接使用 AI 微止盈价。
6. `market.Data.CurrentADX/CurrentDIPlus/CurrentDIMinus` 当前来自 4h K 线；1h 数据只有 `ADXValues`，没有 1h DI+/DI- 顶层字段。若需求要求“1h ADX 优先”，需要补齐 1h ADX/DI 上下文并明确各 gate 使用的时间框架。
7. `market.calculateADX()` 当前把最终 DX 直接作为 ADX，未做完整 ADX 平滑。作为交易 gate 前，应修正或封装为可测试的指标算法。
8. 当前 open gate 会对震荡状态提高置信度或降权，但不会在 ADX<20 时确定性禁止新开趋势单。
9. 同向高相关门控已存在，但允许一个同向高相关持仓后继续降权加仓；在亏损期和震荡期，这仍可能放大 ETH/BCH 等同向风险。
10. 本机决策日志中的 `stop_distance_pct` 是比例值，不是百分号值；例如 `0.032` 表示约 3.2%，不是 0.03%。需求仍应增加单位一致性校验，避免日志、UI 或人工复盘误读。
11. 本机近期开仓样本显示执行层记录的止损距离约为 1.59% 至 5.37%，并非全部低于 1%；但系统仍缺少硬保护，无法防止未来 AI 输出 0.03% 这类微止损通过。
12. 当前配置为 `trading_frequency.mode=balanced`，启用 `aster_deepseek`，扫描间隔 3 分钟，候选池包含加密主流、山寨和 XAG 等不同波动/流动性品种；品种级参数仍偏一刀切。

本需求不构成投资建议；它只定义交易系统的工程风控、策略门控和可验证行为。

## Glossary

- **ATR stop**: 基于 ATR 的动态止损距离，按 `max(ATR × multiplier, hard_floor)` 计算。
- **Hard floor**: 无论 AI 或 ATR 给出多小距离，都必须满足的最小止损/止盈距离。
- **Net RR**: 扣除手续费滑点后的净风险回报比，当前代码公式为 `(rewardPct - 0.2) / riskPct`。
- **Micro stop**: 过窄止损，容易被点差、滑点、短周期噪音触发。默认定义为止损距离低于品种硬地板。
- **Micro TP**: 过近止盈，导致盈利单过早全平，无法覆盖亏损单和交易成本。
- **Regime gate**: 基于 ADX、DI、EMA、MACD、波动率和 BTC 背景判断当前是否允许趋势开仓。
- **Instrument profile**: 品种级参数组，区分 BTC/ETH、主流山寨、高 beta 山寨、非加密合约如 XAG。
- **R multiple**: 以初始风险距离为单位的收益倍数，1R 表示价格达到初始止损距离的同等有利移动。

## Requirements

### Requirement 1: 止损距离必须由确定性 ATR 规则约束

**User story:** 作为量化交易员，我希望任何开仓都不能使用过窄止损，以便避免被点差、滑点和正常盘口波动反复扫损。

Acceptance criteria:

1. WHEN AI 输出开仓止损价 THEN 系统 SHALL 计算该止损相对当前价的 `stop_distance_ratio`，并与品种 profile 的 `min_stop_pct` 和 ATR 动态距离比较；旧字段 `stop_distance_pct` SHALL 保持 ratio 语义兼容。
2. THE effective stop distance SHALL be at least `max(atr_multiplier × ATR(timeframe) / price, min_stop_pct)` unless a stricter exchange or liquidation constraint blocks the trade.
3. IF AI stop is tighter than effective stop distance THEN 系统 SHALL either rewrite stop loss to the effective ATR stop and recompute TP/sizing, or reject the open decision with a structured reason.
4. THE default `min_stop_pct` SHALL be no lower than 1.0% for all enabled instruments.
5. THE default ATR multiplier SHALL be configurable by instrument profile, with separate defaults for BTC/ETH, major alts, high beta alts, and non-crypto instruments.
6. WHEN ATR data is missing or invalid THEN 系统 SHALL use profile fallback stop distance and mark the decision as degraded; it SHALL NOT accept an AI-supplied micro stop.
7. WHEN stop distance is displayed in logs, replay, API, or frontend THEN units SHALL be explicit (`ratio` and `percent`) to prevent `0.032` being read as `0.032%`.

### Requirement 2: 止盈必须与止损和交易成本匹配

**User story:** 作为策略设计者，我希望止盈目标覆盖止损、手续费滑点和胜率不确定性，以便避免“窄止损 + 微止盈”的负期望模型。

Acceptance criteria:

1. WHEN validating an open decision THEN 系统 SHALL enforce net RR >= 2.5 after any stop rewrite.
2. THE minimum take-profit distance SHALL be at least `stop_distance_ratio × min_net_rr + fee_slippage_pct`.
3. THE system SHALL reject or rewrite any fixed TP that is closer than the minimum TP distance, even if direction is valid.
4. THE execution layer SHALL keep a full-position exchange take-profit protection order for every successful open, but its price SHALL be the normalized algorithmic full TP, not the raw AI TP when the raw TP is too close.
5. IF fixed/full TP is used THEN it SHALL be treated as the final protective target; it SHALL NOT preempt scaled/trailing exit unless the target satisfies minimum RR, profile TP rules, and final planned R-distance.
6. WHEN price reaches 1R and exchange minimum notional permits THEN 系统 SHALL either close a configured tranche or move stop to breakeven/lock-profit.
7. WHEN price reaches 2R or higher THEN 系统 SHALL prefer scaled exit plus ATR trailing while keeping the algorithmic full TP as the final exchange-side safety target.
8. Replay reports SHALL include average win, average loss, win/loss ratio, median R multiple, and count of rejected/re-written micro TP events.

### Requirement 3: 仓位 sizing 必须以真实风险金额为核心

**User story:** 作为量化交易员，我希望每笔交易的仓位由可亏金额决定，而不是由 AI 名义仓位或杠杆直觉决定。

Acceptance criteria:

1. WHEN final stop distance is known THEN position size SHALL be computed as `account_equity × effective_risk_pct / (stop_distance_ratio + fee_slippage_pct)`.
2. THE risk recorded in decision logs SHALL include stop-loss risk, fee/slippage reserve, and final effective risk percent.
3. IF AI requested position size exceeds risk-based max size THEN 系统 SHALL shrink it and log `requested_position_size_usd`, `adjusted_position_size_usd`, `risk_cap_reason`.
4. IF the risk-based position size is below exchange minimum notional THEN 系统 SHALL reject the open rather than widening hidden risk.
5. In loss mode, effective risk per trade SHALL default to <= 0.5% account equity.
6. In range regime or when recent deduplicated PF is below recovery threshold, effective risk SHALL be capped at the stricter of loss-mode cap and regime cap.
7. Position sizing tests SHALL cover tiny stop, wide ATR stop, missing ATR, min-notional failure, fee/slippage reserve, and instrument profile overrides.

### Requirement 4: ADX 市场状态过滤必须成为开仓硬门控

**User story:** 作为交易员，我希望系统在无趋势或方向不清时停止新开趋势单，以便避免震荡市中双向反复止损。

Acceptance criteria:

1. THE system SHALL compute and expose the ADX/DI timeframe used by each gate; 1h ADX gate SHALL use 1h ADX and 1h DI, not silently reuse 4h fields.
2. WHEN 1h ADX < 20 THEN 系统 SHALL block new trend-following opens and allow only hold/close/stop adjustment actions.
3. WHEN 1h ADX is between 20 and 25 THEN 系统 SHALL require directional confirmation from DI, EMA alignment, and higher confidence before opening.
4. WHEN 1h ADX > 25 THEN 系统 SHALL allow only directionally aligned trades: long requires DI+ > DI- and trend confirmation; short requires DI- > DI+ and trend confirmation.
5. WHEN ADX is extreme and price is extended THEN existing high-ADX chase protection SHALL continue to block or penalize entries without pullback confirmation.
6. THE ADX implementation SHALL be corrected or wrapped so tests can verify Wilder-style smoothing semantics, instead of using final DX as ADX without explicit labeling.
7. The prompt SHALL reflect the same ADX gate, but the code-level gate SHALL be authoritative.

### Requirement 5: 品种 profile 必须区分加密主流、山寨和非加密合约

**User story:** 作为量化交易员，我希望不同波动、流动性和交易时段的品种使用不同参数，以便避免 XAG、DOGE、HYPE、BTC/ETH 共享一套不合理阈值。

Acceptance criteria:

1. THE config SHALL support instrument profiles by symbol pattern or explicit symbol list.
2. Each profile SHALL define at least `min_stop_pct`, `atr_multiplier`, `min_tp_rr`, `max_risk_pct`, `min_adx`, `allow_long`, `allow_short`, `max_same_side_positions`, and `min_order_value_usdt` override.
3. BTC/ETH, major alts, high beta alts, and non-crypto instruments SHALL have separate defaults.
4. WHEN a symbol has no explicit profile THEN 系统 SHALL fall back to the safest applicable default and log the selected profile.
5. XAG-like non-crypto instruments SHALL NOT inherit high beta crypto behavior automatically; their profile SHALL be explicit or disabled.
6. Replay SHALL group performance by instrument profile to show whether a profile is profitable before enabling wider trading.

### Requirement 6: 同向和相关性暴露必须在亏损期更严格

**User story:** 作为风控负责人，我希望系统避免在同一市场背景下叠加高度相关的同向仓位，以便减少 ETH/BCH 这类同步止损。

Acceptance criteria:

1. WHEN an existing position is same-side and high-correlation with a proposed trade THEN 系统 SHALL apply profile-specific same-side limits.
2. In loss mode or range regime, same-side high-correlation opens SHALL default to max 1 concurrent position.
3. Outside loss mode, same-side high-correlation opens SHALL default to max 2 concurrent positions, with the second trade risk reduced.
4. IF any same-side position is floating loss beyond configured threshold THEN 系统 SHALL block additional same-side opens.
5. Correlation diagnostics SHALL be written to open rejection logs, including target correlation, existing symbols, side, state, and final risk multiplier.
6. The gate SHALL apply to both longs and shorts, not only high beta altcoin longs.

### Requirement 7: 出场策略必须从固定全平升级为 R 倍数和 ATR trailing

**User story:** 作为量化交易员，我希望盈利单能覆盖亏损单，并在趋势行情中保留尾部收益。

Acceptance criteria:

1. WHEN a plan is opened THEN it SHALL store initial risk distance, initial ATR, effective stop, effective TP, and profile name.
2. WHEN MFE reaches 1R THEN stop SHALL move to breakeven or a small locked profit if exchange constraints allow.
3. WHEN MFE reaches configured tranche levels, e.g. 1R/2R/3R, THEN 系统 SHALL issue scaled exits only if Aster/Binance/Hyperliquid min notional constraints can be satisfied.
4. WHEN scaled exit is not executable due to small account size THEN 系统 SHALL use full-position trailing or stop tightening instead of emitting invalid partial orders.
5. WHEN ATR trailing is active THEN trailing distance SHALL use the plan profile multiplier and current or entry ATR according to config.
6. WHEN MFE giveback exceeds configured threshold with weak momentum THEN 系统 SHALL tighten stop or close before a profitable trade turns materially negative.
7. Fixed full TP SHALL be retained for exchange-side protection, but for strict trend profiles it SHALL be algorithmically repriced to at least the final planned R target and SHALL NOT use raw AI micro TP.

### Requirement 8: 订单历史与 replay 必须验证策略病因

**User story:** 作为量化交易员，我希望 replay 能从真实订单、决策日志和交易计划中验证亏损原因，而不是只看最终盈亏。

Acceptance criteria:

1. Replay SHALL report requested stop distance, exchange filled entry, exchange stop order price, effective stop distance, requested TP distance, effective TP distance, and exchange full TP distance.
2. Replay SHALL detect and bucket micro stop, micro TP, low ADX entry, counter-DI entry, same-side correlation stacking, profile mismatch, premature full TP, and raw-AI-TP-to-algorithmic-TP rewrites.
3. Replay SHALL compare AI requested parameters with post-validation final parameters.
4. Replay SHALL compute R multiple for every closed trade when entry/exit/stop are available.
5. Replay SHALL distinguish actual exchange stop-loss exits from system active closes and from inferred snapshot closes.
6. The 2026-05-13 to 2026-05-16 samples SHALL be usable as regression fixtures after secrets and account identifiers are stripped.
7. Report-only mode SHALL remain read-only and SHALL NOT mutate `data/` or `decision_logs/`.

### Requirement 9: Prompt 必须跟随硬约束，但不得替代硬约束

**User story:** 作为系统维护者，我希望 AI 接收到清晰、可执行的策略边界，但所有关键风控仍由代码强制。

Acceptance criteria:

1. Prompt SHALL show profile-specific ATR stop, minimum stop floor, minimum TP, net RR, ADX regime gate, and same-side correlation limits.
2. Candidate rows SHALL include profile name, ATR timeframe, ATR value, minimum stop distance, minimum TP distance, ADX/DI state, and whether the symbol is executable.
3. WHEN BTC or ADX gate makes a candidate non-executable THEN prompt SHALL exclude it or label it as blocked; AI SHOULD not be asked to rank blocked symbols as active opportunities.
4. IF AI output violates a hard constraint THEN validation SHALL reject/rewrite it and record a structured open rejection.
5. Prompt and validation tests SHALL verify that hard constraints are expressed consistently and that changing prompt text cannot bypass validation.

### Requirement 10: 灰度上线必须先保护账户再扩大交易

**User story:** 作为账户负责人，我希望结构性策略修正先在保守模式中验证，再决定是否恢复 balanced/active。

Acceptance criteria:

1. The first deployment SHALL run in safe or loss-mode-compatible settings with max risk per trade <= 0.5% and max concurrent positions <= 1 until 24h replay passes.
2. Balanced/active mode SHALL remain disabled unless deduplicated 24h PF, win/loss ratio, MFE giveback, and rejection buckets pass configured thresholds.
3. Deployment logs SHALL record active profile defaults, ADX gate thresholds, risk caps, full TP mode, and the algorithmic full TP distance rule.
4. A rollback switch SHALL restore previous validation behavior without requiring code edits.
5. The system SHALL not reset or rewrite historical statistics automatically; any repaired statistics SHALL be generated as explicit replay output or migration artifact.

### Requirement 11: 保留现有交易安全语义

**User story:** 作为工程负责人，我希望策略优化不破坏已有执行安全、trader 隔离和保护单语义。

Acceptance criteria:

1. Stop-loss and take-profit cancellation SHALL remain split between `CancelStopLossOrders()` and `CancelTakeProfitOrders()`.
2. Trader-scoped state SHALL remain isolated by `trader_id`.
3. No tests SHALL place real orders or require live exchange credentials.
4. Runtime credentials, API keys, private keys, account addresses, and passwords SHALL NOT be written to spec docs, replay fixtures, or logs.
5. Successful opens SHALL keep both stop-loss and full-position take-profit protection semantics unless an exchange rejects one side; if TP is rejected, the action log SHALL record protection risk and the plan SHALL still use local exit evaluation.
6. Any code that changes live trading behavior SHALL include targeted unit tests and replay validation before deployment.
