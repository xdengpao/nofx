# 部分平仓历史成交优化 Requirements

## Background

161 线上服务已确认 `partial_close` 动作能够成功写入 `decision_logs/aster_deepseek/decision_*.json`，且 `/api/performance` 的 `execution_quality.partial_close_attempts` 能统计到部分平仓尝试和失败率。

但前端“历史成交”读取的是 `/api/performance` 的 `recent_trades`。该字段由 `logger.BuildTradeOutcomes()` 根据决策日志中的开仓和平仓动作配对生成，目前只将 `close_long`、`close_short`、`auto_close_long`、`auto_close_short` 视为完整闭合交易；`partial_close` 只被归入执行质量统计，不会进入历史成交列表。

同时，不能简单把 `partial_close` 当作普通 `close_*` 处理，因为部分平仓只关闭持仓的一部分，当前配对逻辑会按完整开仓数量计算 PnL，并删除整笔 open trace，可能造成盈亏重复、提前结束持仓生命周期或后续全平无法正确配对。

本规格需要把“完整闭合交易”和“部分平仓成交事件”区分开：完整闭合交易继续服务胜率、profit factor、rolling gate 等策略绩效；部分平仓需要作为真实可审计的成交事件进入历史成交展示，并尽可能使用交易所成交数据确认价格、数量、手续费和 realized PnL。

## Goals

- 让成功执行的 `partial_close` 出现在前端历史成交/成交事件中。
- 保持完整开平仓生命周期统计不被部分平仓错误截断或重复计入。
- 对 `partial_close` 使用实际减仓数量计算展示用 PnL，不使用原始整仓数量。
- 补强订单 ID、成交状态、成交均价、手续费、realized PnL 的交易所对账能力。
- 让前端区分“完整平仓”和“部分平仓/减仓”，并支持 CSV 导出。
- 对旧日志、无订单 ID、交易所暂未返回成交的情况保持兼容。

## Non-Goals

- 本规格不修改程序化策略的买卖点识别和减仓触发规则。
- 本规格不新增回测能力。
- 本规格不改变强制风控、交易计划同步、保护单同步和执行前检查优先级。
- 本规格不把部分平仓直接计入当前 rolling gate 的完整交易胜率，除非后续设计明确增加独立的“减仓质量”指标。
- 本规格不要求一次性重构为数据库存储；首期仍基于 JSON 日志和现有本地状态。

## Glossary

- **完整闭合交易**：由开仓/加仓和最终全平动作配对形成的完整交易生命周期，用于胜率、profit factor、rolling performance 等绩效统计。
- **成交事件**：一次真实或可审计的执行事件，包括开仓、加仓、部分平仓、完整平仓、自动平仓等。
- **部分平仓成交事件**：`partial_close` 成功执行且实际减仓数量大于 0 的成交事件。
- **跳过的部分平仓**：策略尝试 `partial_close`，但因最小名义额、剩余仓位过小、预算或冷却等原因未真实下单或实际数量为 0。
- **交易所对账**：通过 `GetOrderStatus()`、`GetTradeHistory()` 或等价接口确认订单成交数量、均价、手续费、realized PnL 和成交时间。
- **估算成交**：交易所成交明细暂不可用时，基于决策日志、持仓快照、开仓价、执行价和数量计算的展示用记录。
- **reconciled**：成交事件已经通过交易所订单或成交明细确认。

## Requirements

### 1. 部分平仓进入历史成交展示

**User Story:** 作为量化交易员，我希望成功执行的部分平仓能出现在历史成交中，以便复盘每一次减仓行为和实际收益影响。

#### Acceptance Criteria

1. WHEN `decision_logs` 中存在成功的 `partial_close` 且实际减仓数量大于 0 THEN 系统 SHALL 在历史成交/成交事件 API 中返回该部分平仓记录。
2. WHEN `partial_close` 因最小名义额或其他原因被跳过且实际减仓数量为 0 THEN 系统 SHALL 不把该记录展示为真实历史成交。
3. WHEN `partial_close` 返回到前端 THEN 记录 SHALL 明确标记为 `partial_close` 或 `is_partial=true`。
4. WHEN 前端展示历史成交 THEN 用户 SHALL 能区分“部分平仓/减仓”和“完整平仓”。
5. WHEN 同一持仓先部分平仓后最终全平 THEN 历史成交 SHALL 同时展示部分平仓事件和最终全平事件。
6. WHEN 旧日志没有 `close_quantity` 字段但 `quantity > 0` THEN 系统 SHALL 使用 `quantity` 作为部分平仓数量的兼容来源。
7. WHEN `partial_close` 缺少 `final_action` 字段 THEN 系统 SHALL 仍可根据 `action=partial_close` 识别旧日志。
8. WHEN `partial_close` 的 `final_action=partial_close_skipped` THEN 系统 SHALL 将其归类为跳过记录，不进入真实成交列表。

### 2. 完整交易生命周期不得被 partial_close 截断

**User Story:** 作为量化交易员，我希望部分平仓只减少持仓数量，不让系统误以为整笔交易已经结束。

#### Acceptance Criteria

1. WHEN `BuildTradeOutcomes()` 或等价 replay 逻辑处理 `partial_close` THEN 系统 SHALL 只扣减对应 open trace 的剩余数量，不得删除整笔持仓生命周期。
2. WHEN 剩余持仓后续发生 `close_long` 或 `close_short` THEN 系统 SHALL 只对剩余数量生成最终完整平仓结果。
3. WHEN 部分平仓数量小于当前 open trace 剩余数量 THEN 系统 SHALL 保留未关闭数量、原开仓时间、开仓原因和杠杆信息。
4. WHEN 部分平仓数量覆盖多个 add/open lot THEN 系统 SHOULD 按 FIFO 或设计阶段明确的规则扣减多个 lot。
5. WHEN 部分平仓数量大于本地可追踪剩余数量 THEN 系统 SHALL 只按可追踪数量生成记录，并输出 unmatched 或 reconciliation warning。
6. WHEN 没有可匹配 open trace 的 `partial_close` 出现 THEN 系统 SHALL 记录为 unmatched partial close，不得伪造开仓信息。
7. WHEN 生成 rolling performance、win rate、profit factor 等完整交易指标 THEN 系统 SHALL 不把部分平仓直接当作完整闭合交易重复计数。
8. WHEN 需要展示总成交事件数量 THEN 系统 MAY 单独提供 `trade_events_count`，不得覆盖 `total_trades` 的完整交易语义。

### 3. 部分平仓 PnL 计算口径

**User Story:** 作为量化交易员，我希望部分平仓的盈亏按实际减仓数量计算，而不是按原始整仓计算。

#### Acceptance Criteria

1. WHEN 部分平仓已通过交易所成交对账 THEN 系统 SHALL 优先使用交易所返回的 `realized_pnl` 和 `commission`。
2. WHEN 交易所未返回 realized PnL THEN 系统 SHALL 使用本地估算公式计算展示用 PnL。
3. FOR long partial close, estimated PnL SHALL 等于 `close_quantity * (close_price - open_price)`。
4. FOR short partial close, estimated PnL SHALL 等于 `close_quantity * (open_price - close_price)`。
5. WHEN 计算部分平仓 `position_value` THEN SHALL 使用 `close_quantity * open_price` 或设计阶段明确的 lot entry price。
6. WHEN 计算部分平仓 `margin_used` THEN SHALL 使用部分平仓对应名义价值除以杠杆。
7. WHEN 计算部分平仓 `pn_l_pct` THEN SHALL 使用部分平仓对应保证金作为分母。
8. WHEN PnL 为估算值 THEN API SHALL 返回 `pnl_source=estimated` 或等价字段；WHEN 来自交易所 THEN SHALL 返回 `pnl_source=exchange`。

### 4. 交易所成交对账

**User Story:** 作为量化交易员，我希望部分平仓记录尽可能使用交易所真实成交，避免只凭策略日志估算。

#### Acceptance Criteria

1. WHEN `partial_close` 下单成功且返回订单 ID THEN 系统 SHALL 保存订单 ID 到决策日志。
2. WHEN 交易所订单 ID 类型为 `float64`、`int64`、`string` 或其他常见 JSON 数字形式 THEN 系统 SHALL 正确解析为统一 `int64` 或字符串订单标识。
3. WHEN 当前交易所支持 `GetOrderStatus()` THEN 系统 SHALL 能按订单 ID 查询成交状态和成交数量。
4. WHEN 当前交易所支持 `GetTradeHistory()` THEN 系统 SHALL 能按 symbol 和时间窗口查询成交明细并关联订单 ID。
5. WHEN 成交明细能匹配订单 ID THEN 系统 SHALL 使用成交明细聚合成交均价、成交数量、手续费和 realized PnL。
6. WHEN 下单成功后交易所暂未返回成交明细 THEN 系统 SHALL 允许记录为 `reconciled=false`，并在后续周期补充对账。
7. WHEN 对账失败 THEN 系统 SHALL 保留估算记录，并记录中文诊断原因。
8. WHEN 交易所不支持订单或成交历史 THEN 系统 SHALL 退化为估算成交，并标记 `reconciliation_status=unsupported`。
9. WHEN 交易所返回部分成交 THEN 系统 SHALL 使用实际已成交数量，不得使用请求数量伪装为完全成交。

### 5. 成交事件 API 契约

**User Story:** 作为前端开发者，我希望 API 能返回完整平仓和部分平仓统一的成交事件字段，减少前端猜测。

#### Acceptance Criteria

1. `/api/performance` SHALL 保持现有 `recent_trades` 字段向后兼容。
2. 系统 SHALL 新增 `recent_trade_events` 字段或扩展 `recent_trades` 的可选字段，用于展示完整平仓和部分平仓事件。
3. 成交事件 SHALL 至少包含 `event_type`、`symbol`、`side`、`quantity`、`leverage`、`open_price`、`close_price`、`position_value`、`margin_used`、`pn_l`、`pn_l_pct`、`open_time`、`close_time`、`open_reason`、`close_reason`。
4. 部分平仓事件 SHALL 额外包含 `is_partial`、`close_quantity`、`requested_close_percentage`、`executed_close_percentage`、`remaining_quantity`、`order_id`、`signal_id`、`strategy_name`、`strategy_version`、`pnl_source`、`reconciled`。
5. WHEN 字段来自旧日志不可得 THEN API SHALL 省略可选字段或返回明确零值，不得导致前端渲染失败。
6. WHEN API 返回完整平仓 THEN `event_type` SHALL 为 `full_close` 或等价枚举。
7. WHEN API 返回部分平仓 THEN `event_type` SHALL 为 `partial_close`。
8. WHEN API 返回自动止损/止盈平仓 THEN `event_type` SHALL 能区分 `auto_close` 或在 close source 中体现。

### 6. 前端历史成交展示

**User Story:** 作为量化交易员，我希望历史成交表能看到部分平仓标签、成交数量、原因和对账状态。

#### Acceptance Criteria

1. WHEN 历史成交表展示部分平仓事件 THEN SHALL 显示“部分平仓”或“减仓”标签。
2. WHEN 历史成交表展示完整平仓事件 THEN SHALL 显示“完整平仓”或保持现有展示但不得与部分平仓混淆。
3. WHEN 部分平仓记录包含 `close_reason` THEN 前端 SHALL 可展示该原因或在详情/tooltip 中展示。
4. WHEN 部分平仓记录 `reconciled=false` THEN 前端 SHALL 显示“估算/待对账”状态。
5. WHEN 部分平仓记录 `pnl_source=exchange` THEN 前端 SHALL 显示或隐含使用真实成交 PnL。
6. WHEN 数量、价格、PnL 字段缺失 THEN 前端 SHALL 使用 `-` 或兼容展示，不得抛出运行时错误。
7. WHEN 页面宽度不足 THEN 新增标签和原因字段 SHALL 保持可横向滚动或响应式折行，不得遮挡表格内容。
8. WHEN 用户导出 CSV THEN CSV SHALL 包含事件类型、是否部分平仓、订单 ID、信号 ID、原因、对账状态和 PnL 来源。

### 7. 决策日志与执行记录补强

**User Story:** 作为系统维护者，我希望部分平仓执行记录足够完整，后续 replay、前端和对账都能复用。

#### Acceptance Criteria

1. WHEN `executePartialCloseWithRecord()` 成功下单 THEN `DecisionAction` SHALL 记录最终 action、请求比例、实际执行比例、实际或请求平仓数量、订单 ID、执行价格、原因说明。
2. WHEN 订单 ID 从交易所返回但类型不是 `int64` THEN 系统 SHALL 仍正确记录订单 ID。
3. WHEN `partial_close` 被自动修正为完整平仓 THEN `DecisionAction.final_action` SHALL 记录最终 `close_long` 或 `close_short`，并作为完整平仓进入历史成交。
4. WHEN `partial_close` 被跳过 THEN `DecisionAction.final_action` SHALL 记录 `partial_close_skipped`，并记录跳过原因。
5. WHEN 部分平仓后保护止损或止盈重建失败 THEN 执行记录 SHALL 同时保留部分平仓已执行事实和保护单失败风险。
6. WHEN 执行记录包含 `signal_id`、`strategy_metadata` 或 `explanation` THEN 成交事件 SHALL 继承这些字段用于前端复盘。
7. WHEN 旧日志缺失新字段 THEN replay SHALL 使用兼容逻辑读取，不得中断性能分析。

### 8. Replay 与离线分析兼容

**User Story:** 作为量化交易员，我希望历史日志 replay 能正确还原部分平仓事件，并发现无法配对的异常。

#### Acceptance Criteria

1. WHEN replay 读取包含 `partial_close` 的旧日志 THEN SHALL 生成部分平仓成交事件或 unmatched partial close 诊断。
2. WHEN replay 读取完整开平仓日志 THEN 原有完整交易结果 SHALL 保持兼容。
3. WHEN replay 遇到部分平仓后最终全平 THEN SHALL 不重复计算已部分平仓数量。
4. WHEN replay 遇到数量为 0 的部分平仓跳过记录 THEN SHALL 不生成成交事件。
5. WHEN replay 生成 unmatched 诊断 THEN SHALL 区分 `missing_open_for_partial_close` 与现有 `missing_open`。
6. WHEN replay 输出统计报告 THEN SHALL 可展示部分平仓事件数量、已对账数量、待对账数量和估算数量。
7. WHEN 单元测试构造 open -> partial_close -> close 流程 THEN SHALL 验证最终总关闭数量不超过开仓数量。

### 9. 统计口径与风险控制

**User Story:** 作为量化交易员，我希望新增部分平仓展示不会污染现有风控统计和开仓门控。

#### Acceptance Criteria

1. `total_trades`、`winning_trades`、`losing_trades`、`win_rate`、`profit_factor` SHALL 继续基于完整闭合交易，除非后续设计明确加入部分平仓统计。
2. `execution_quality.partial_close_attempts` 和 `partial_close_failures` SHALL 保持现有语义。
3. 系统 MAY 新增 `partial_close_events`、`partial_close_realized_pnl`、`partial_close_reconciled_count` 等独立指标。
4. WHEN 部分平仓产生盈利或亏损 THEN 不得直接触发 rolling gate 的完整交易亏损计数。
5. WHEN 后续需要用部分平仓质量影响开仓门控 THEN SHALL 通过独立规格定义，不在本规格隐式改变风控行为。
6. WHEN 前端展示汇总卡片 THEN SHALL 明确区分完整交易统计和部分平仓事件统计。

### 10. 数据兼容与迁移

**User Story:** 作为系统维护者，我希望上线后旧日志和线上服务能平滑兼容，不需要清空运行数据。

#### Acceptance Criteria

1. WHEN 系统启动读取旧 `decision_logs` THEN 缺少新字段不应导致 panic 或 API 500。
2. WHEN 旧 `partial_close` 日志有 `quantity` 但无 `close_quantity` THEN 系统 SHALL 使用 `quantity` 兼容。
3. WHEN 旧日志 `order_id=0` THEN 系统 SHALL 将其视为无法按订单 ID 对账，可按时间窗口尝试成交历史匹配或标记估算。
4. WHEN 旧日志没有 `requested_close_percentage` 和 `executed_close_percentage` THEN 前端 SHALL 正常展示数量，比例字段显示为空。
5. WHEN 新版本部署后继续写日志 THEN 新字段 SHALL 出现在后续 `DecisionAction` 中。
6. WHEN API 同时返回旧完整交易和新成交事件 THEN 前端 SHALL 保持旧页面可用，不强制用户清理缓存。

### 11. 测试与验证

**User Story:** 作为系统维护者，我希望该优化有足够测试，避免历史成交、replay 和前端展示再次遗漏部分平仓。

#### Acceptance Criteria

1. SHALL 增加 logger 单元测试覆盖 open -> partial_close -> close 的配对和数量扣减。
2. SHALL 增加 logger 单元测试覆盖 partial_close quantity=0 或 skipped 不生成成交事件。
3. SHALL 增加 logger 单元测试覆盖旧日志只有 `quantity` 没有 `close_quantity` 的兼容。
4. SHALL 增加 trader 单元测试覆盖订单 ID 从 `float64`、`int64`、`string` 解析。
5. SHOULD 增加 API 测试验证 `/api/performance` 返回部分平仓事件字段。
6. SHALL 增加前端类型或构建验证，确保新增字段不破坏 `AILearning` 历史成交表。
7. WHEN 完成实现 THEN 至少运行相关 Go 包测试和前端 build；若某项无法运行，交付说明 SHALL 记录原因。

