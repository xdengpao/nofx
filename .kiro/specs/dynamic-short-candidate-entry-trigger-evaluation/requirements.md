# 动态候选池 Short-side 覆盖与缠论 V2 Entry Trigger 评估需求

## 背景

本规格另开用于评估并设计以下问题：

1. 是否应启用动态候选池，以替代当前以默认币种为主的静态候选覆盖。
2. 当 BTC 处于弱势或 risk-off 状态时，如何提高 short-side 候选覆盖，让策略更容易看到顺势空头机会。
3. 缠论 V2 的 entry trigger 生成条件是否过早或过严，导致父结构信号无法转换为可执行开仓候选。

近期 no-order 诊断显示，服务和账户风险状态不是主要阻断点：唯一启用 trader 为 `aster_chanlun_v2`，账户无持仓，`loss_mode.active=false`，总风险预算仍可用，执行层没有真实交易所下单记录。近 48 小时核心现象是可执行开仓候选不足，`entry_trigger_count=0`，大量信号停留在 `no_signal`、`terminal_suppressed` 或父结构 `entry_rr_invalid`。

当前代码基础事实：

- `pool/dynamic_candidate_pool.go` 已有动态候选池框架，可合并 core/default/position/AI500/OI Top/exchange volume top，并写入 `data/dynamic_candidate_pool.json`。
- 线上配置摘要显示 `dynamic_candidate_pool.enabled=false`，因此当前运行仍使用静态合并币种池。
- 动态池已有 BTC regime 检测，但 `risk_off` 当前主要降低非 forced 山寨权重，没有 short-side 专属覆盖或配额。
- 动态池当前 `scoreTrend()` 偏向上涨趋势：DI+、EMA20>EMA50、1h/4h 正涨幅会加分；这不利于 BTC 弱势时发现顺势做空标的。
- `AutoTrader` 会将动态池输出写入 `decision.Context.CandidateCoins`，但 `prompt_candidate_limit` 可能被 `trading_frequency` profile 覆盖，候选快照中存在 short-side 标的并不等于本轮上下文一定覆盖。
- 缠论 V2 entry timing 是父结构到入场触发的两层模型。父结构会先检查目标穿越、剩余净 RR、结构有效性、观察窗口；通过后才在 15m/3m 上检测 `pullback_retest_resume`、`breakout_continuation`、`micro_reversal_confirm`。
- 父结构一旦被标记为 terminal，例如 `entry_rr_invalid` 或 `entry_parent.watch_window_expired`，后续同一 signal 会被静默，不会再进入新的 entry trigger 检测。

## 目标

1. 给出启用动态候选池的只读评估与灰度方案，明确收益、风险、回滚条件和观测指标。
2. 在 BTC 弱势时提高 short-side 候选覆盖，但不强制开空、不绕过缠论信号、不绕过 open gate。
3. 审计缠论 V2 entry trigger 生成漏斗，确认 `entry_trigger_count=0` 的主要原因是候选覆盖不足、父结构终态过早、trigger 条件过严，还是下游风控过滤。
4. 建立可复现的离线评估报告，用同一最近 48 小时窗口对比当前静态池、假设动态池、BTC 弱势 short-side 覆盖和 entry trigger 漏斗。
5. 保持最终开仓 RR 2.5 硬阈值、BTC hard veto、position sizing、交易所 preflight 等硬风控不被降低。

## 非目标

- 不降低最终开仓验证的全局 RR 2.5 阈值。
- 不关闭、不削弱 BTC hard veto。
- 不把 BTC 弱势直接解释为开空信号；它只能影响候选覆盖、排序和诊断。
- 不在本规格阶段直接修改真实 `config.json`、API key、私钥或生产账户配置。
- 不提交 `data/`、`decision_logs/`、`coin_pool_cache/` 运行时数据。
- 不在测试、replay 或评估脚本中触发真实下单。
- 不绕过 `decision` 的 freshness、open gate、position sizing、最终限制和交易所 preflight。

## 术语

- **动态候选池**：由默认币、持仓、AI500、OI Top、交易所成交额 Top、历史表现和市场指标合并评分后的候选列表。
- **short-side 覆盖**：候选池和 prompt 上下文中，具备顺势做空环境特征的标的数量、排名、来源和诊断可见性。
- **BTC 弱势**：BTC 1h/4h 下跌、DI-/ADX 显著、EMA 空头或高波动 risk-off 等 regime 条件。具体阈值应在设计中沿用或扩展现有 `detectDynamicMarketRegime()` 与 BTC hard veto 诊断。
- **父结构信号**：缠论 V2 在 trade timeframe 产出的 `buy1/buy2/buy3/sell1/sell2/sell3` 等结构信号。
- **entry trigger**：父结构信号之后，在 sub/micro timeframe 上形成的 fresh 入场触发，目前包括 `pullback_retest_resume`、`breakout_continuation`、`micro_reversal_confirm`。
- **终态静默**：父结构或 trigger 被标记为 terminal 后，同一 signal 后续重复出现时只计入诊断，不再重复作为开仓候选。

## 需求

### 1. 动态候选池启用评估

**用户故事：** 作为 NOFX 操作者，我希望在启用动态候选池前先看到只读评估报告，以便判断它是否能扩大有效候选覆盖，而不是引入低流动性或高噪声标的。

#### 验收标准

1. WHEN 执行评估 THEN 报告 SHALL 对比当前静态候选池、假设动态候选池快照、本轮 prompt 候选三层列表。
2. WHEN 动态候选池评估 OI Top 或交易所成交额 Top THEN 报告 SHALL 输出数据源状态、成功数量、失败原因和回退路径。
3. WHEN 某个标的被纳入动态池 THEN 报告 SHALL 输出 symbol、sources、tier、score、reasons、OI、24h 成交额、ADX、1h/4h 涨跌幅、funding、流动性过滤结论。
4. WHEN 某个标的被剔除 THEN 报告 SHALL 输出剔除原因，例如市场数据缺失、OI 低、成交额低、资金费率拥挤、波动异常或历史表现冷却。
5. IF 动态池刷新失败 THEN 系统 SHALL 回退静态合并池，并在评估报告中明确该回退不会阻塞交易循环。
6. IF `trading_frequency` 覆盖了 `prompt_candidate_limit` THEN 报告 SHALL 显示动态池快照候选数与实际进入 prompt 的候选数差异。

### 2. BTC 弱势下 short-side 候选覆盖

**用户故事：** 作为策略负责人，我希望 BTC 弱势时系统增加顺势 short-side 标的的候选覆盖，让缠论 V2 有更多机会发现 `sell2/sell3` 等结构，而不是反复评估必然被 BTC hard veto 的高 beta 多单。

#### 验收标准

1. WHEN BTC regime 被判定为 `risk_off` 或等价弱势 THEN 动态候选池 SHALL 记录 BTC 弱势诊断，包括触发 timeframe、价格变化、DI/ADX、EMA 或 hard veto 相关依据。
2. WHEN BTC 弱势成立 THEN 候选排序 SHOULD 提高具备 short-side 环境特征的标的覆盖，例如价格弱于 BTC、DI- 优势、EMA 空头、ADX 足够、成交额/OI 充足、funding 不拥挤。
3. WHEN short-side 覆盖生效 THEN 报告 SHALL 输出 `short_side_candidate_count`、`short_side_prompt_count`、`short_side_sources`、`short_side_reasons` 和 top near-miss。
4. IF 某标的只是下跌但流动性不足、资金费率极端或波动异常 THEN 系统 SHALL 继续剔除或降权，不得因 short-side 覆盖目标强制纳入。
5. IF BTC 弱势下出现 high beta 山寨多单候选 THEN 诊断 SHALL 标记其可能被 `btc_hard_veto` 阻断，但不得关闭该硬阻断。
6. WHEN short-side 候选进入 prompt THEN 后续开仓仍 SHALL 依赖缠论 V2 父结构、entry trigger、freshness、open gate、最终 RR 2.5、position sizing 和交易所 preflight。

### 3. 缠论 V2 entry trigger 生成漏斗审计

**用户故事：** 作为策略开发者，我希望能逐层看到父结构信号为什么没有生成 entry trigger，以便判断应该优化候选覆盖、父结构生命周期、trigger 条件还是诊断指标。

#### 验收标准

1. WHEN 缠论 V2 完成一个周期 THEN 诊断 SHALL 至少包含 `raw_signal_count`、`parent_structure_count`、`entry_trigger_count`、`trigger_rejection_reasons`、`terminal_suppressed_count`。
2. WHEN 父结构被终态化 THEN 诊断 SHALL 区分 `entry_rr_invalid`、`entry_parent.target_crossed`、`entry_parent.invalid_structure`、`entry_parent.watch_window_expired`、`lifecycle.terminal`。
3. WHEN 父结构通过观察但没有 trigger THEN 诊断 SHALL 记录 `waiting_for_fresh_entry_trigger`，并标明等待 timeframe 和窗口。
4. WHEN trigger 条件被拒 THEN 诊断 SHALL 区分低置信度、trigger 过期、entry zone 追价、三买/三卖质量失效、剩余净 RR 失效。
5. IF 父结构因 `entry_rr_invalid` 被 terminal 标记 THEN 后续同一 parent signal 不应无限重复评估，但评估报告 SHALL 能显示这种静默对 `entry_trigger_count=0` 的贡献。
6. IF 发现父结构 RR 检查在 trigger 生成前过早终态化可恢复信号 THEN 设计 SHALL 提供替代方案，例如将部分 RR 失败降级为观察态、按 signal type 记录 near-miss，或延迟到 trigger entry price 后再 terminal；该方案不得降低最终 RR 2.5。

### 4. Entry trigger 条件一致性与安全边界

**用户故事：** 作为风控负责人，我希望 entry trigger 的优化不被误解为降低下单标准，所有最终开仓仍保持现有硬风控。

#### 验收标准

1. WHEN 评估 `entry_timing.entry_zone.min_remaining_net_rr` 或 `signal_type_min_rr` THEN 文档 SHALL 明确这些阈值只控制父结构/trigger 生成，不等于最终可下单。
2. WHEN loosen mode 生效 THEN 报告 SHALL 显示 effective entry timing 的实际值，包括 `min_trigger_confidence`、`max_chase_ratio`、`min_remaining_net_rr`、`signal_type_min_rr`。
3. IF entry trigger 在 loosen mode 下通过 THEN 仍 SHALL 进入 `validateOpenDecision()` 的最终 RR 2.5 校验。
4. IF 未来任务涉及调整父结构 terminal 逻辑 THEN 测试 SHALL 覆盖 `entry_rr_invalid`、`watch_window_expired`、`waiting_for_fresh_entry_trigger` 和 trigger ready 四类路径。
5. WHEN open gate 或最终校验拒绝 entry trigger THEN 系统 SHALL 保留 `open_rejections` 和 marker 更新，便于区分“trigger 没生成”和“trigger 生成后被风控拒绝”。

### 5. 离线评估工具与报告

**用户故事：** 作为维护者，我希望不用改生产配置即可复盘最近 48 小时，估算启用动态候选池和 short-side 覆盖后的候选变化与开仓概率。

#### 验收标准

1. WHEN 使用最近 48 小时决策日志评估 THEN 报告 SHALL 输出时间窗口、记录数、启用 trader、最终动作分布、真实订单数、open-like 候选数。
2. WHEN 评估动态候选池 THEN 报告 SHALL 支持 dry-run，不写真实 `data/dynamic_candidate_pool.json`，或写入明确的临时路径。
3. WHEN 评估 short-side 覆盖 THEN 报告 SHALL 对比静态池与动态池下可被缠论 V2 分析的 short-side symbol 数量。
4. WHEN 评估 entry trigger 漏斗 THEN 报告 SHALL 输出 raw signal、父结构、直接终态、等待 trigger、trigger ready、trigger rejection、open rejection、final wait 的分层计数。
5. IF 历史日志缺少某些新诊断字段 THEN 报告 SHALL 明确字段缺失，并从 CoT 文案或 marker 中做 best-effort 解析。
6. WHEN 报告估算新部署后开仓概率 THEN SHALL 分层表达为候选覆盖概率、父结构出现概率、entry trigger 生成概率、open gate 通过概率、最终 RR 2.5 通过概率，不得给出保证开仓或收益承诺。

### 6. 灰度上线与回滚

**用户故事：** 作为系统运维者，我希望动态候选池和 short-side 覆盖可以小步上线，出现数据源异常或噪声扩大时能快速回滚。

#### 验收标准

1. WHEN 准备启用动态候选池 THEN 方案 SHALL 先提供 report-only/dry-run 验证步骤，再提供生产配置变更步骤。
2. WHEN 生产启用动态候选池 THEN 配置 SHALL 保留 core symbols、最小 OI、最小成交额、资金费率、波动异常过滤和快照 TTL。
3. WHEN OI Top API 未配置或失败 THEN 动态池 SHALL 继续可用交易所成交额 Top、默认币和持仓回退，不得导致交易循环失败。
4. WHEN short-side 覆盖导致候选噪声显著上升 THEN 运维者 SHALL 能通过单一配置开关或回滚 commit 恢复原排序/静态池行为。
5. IF 启用后出现连续亏损、loss mode 激活、执行质量恶化或异常 open rejection 激增 THEN 系统 SHALL 保持既有风控并建议回滚动态池/short-side 覆盖，而不是降低 RR 或关闭 BTC hard veto。

### 7. 测试与回归保护

**用户故事：** 作为开发者，我希望实现后的行为有单元测试和 replay 验证，避免候选池或 trigger 优化破坏交易安全边界。

#### 验收标准

1. WHEN 修改动态候选池评分或选择逻辑 THEN SHALL 添加 `pool` 测试覆盖 BTC 弱势 short-side 排序、流动性过滤、source status、prompt candidate limit。
2. WHEN 修改候选池到 `decision.Context` 的字段 THEN SHALL 添加 `trader` 或相关测试，确认 `CandidateCoins` 保留 tier、score、reasons 和 short-side 诊断。
3. WHEN 修改缠论 V2 entry trigger 逻辑 THEN SHALL 添加 `strategy/chanlunv2` 测试覆盖父结构终态、等待 trigger、trigger ready、trigger rejected 和 terminal suppression。
4. WHEN 修改 replay 或 no-open 报告 THEN SHALL 添加 `logger` 或 `cmd/replay` 测试覆盖分层计数和缺字段兼容。
5. WHEN 完成实现 THEN SHALL 至少运行相关包测试；若触及共享配置或交易循环，SHALL 运行 `go test ./config ./pool ./strategy/chanlunv2 ./trader ./logger` 或更广验证。
6. IF 测试或评估需要市场数据 THEN SHALL 使用 mock、fixture、只读公共数据或 dry-run，不得触发真实交易所下单。
