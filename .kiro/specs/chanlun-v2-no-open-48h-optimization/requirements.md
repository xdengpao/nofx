# 缠论 V2 最近 48 小时无开仓诊断与优化需求

## 背景

本规格基于本地 `decision_logs/aster_chanlun_v2` 和当前代码完成。分析快照时间为 2026-05-31 17:25 +08:00，最近 48 小时口径为 2026-05-29 17:25:00 +08:00 到 2026-05-31 17:24:14 +08:00。

本地配置中只有 `aster_chanlun_v2` 处于启用状态；`aster_deepseek` 最后一条日志停在 2026-05-25。最近 48 小时内 `aster_chanlun_v2` 共有 961 条决策记录，成功开仓数为 0。

关键日志结论：

- `final_action=wait` 共 961 次。
- `open_rejected` 共 3 次，全部为 `freshness_gate.rr_invalid`。
- `raw_signal_count` 累计 4028，说明策略并非没有结构信号。
- `entry_trigger_count` 仅 3，且这 3 次都被 freshness RR gate 拒绝。
- 直接父结构 RR 终态 15 次，重复终态静默 3884 次。
- `waiting_for_fresh_entry_trigger` 86 次，`entry_zone_chased` 39 次。
- 最新风险状态中 `open_count_24h=0`、`remaining_risk_budget=0.08`、`loss_mode.active=false`，因此不是持仓上限、亏损模式或总风险预算导致无开仓。

核心代码归因：

- 旧运行代码在 `strategy/chanlunv2/engine.go` 的 freshness RR 门控中使用全局 `strategy_risk.default_min_net_rr=2.5`。
- 日志中 3 个被拒的 entry trigger 均带有 `parent_signal_type=sell2`，而配置里 `sell2` 的 `signal_type_min_rr` 是 1.1。
- 被拒样本的剩余净 RR 分别为 1.19、1.16、1.90，低于旧代码误用的 2.5，但高于配置中 `sell2=1.1`。
- 当前已同步到本地的 HEAD `dd023df3b` 已把 freshness RR 改为按有效信号类型读取阈值，并新增 no-open 诊断字段；现有日志缺少 `active_mode` 与 `effective_entry_timing`，表明运行进程很可能尚未重启到最新代码。

## 目标

1. 确认最近 48 小时没有成功开仓的真实原因。
2. 将旧版本 freshness RR 阈值误用风险转化为可测试、可观测的回归保护。
3. 在不放弃确定性风控的前提下，提高缠论 V2 在长时间无开仓时捕获合格 entry trigger 的能力。
4. 改善 no-open 可观测性，避免下次只能靠人工翻大量日志定位。

## 非目标

- 不绕过 `decision` 风控直接执行策略开仓。
- 不降低账户级硬风控、亏损模式、持仓上限、最小下单额和交易所 preflight。
- 不在测试或 replay 中触发真实下单。
- 不提交真实 API key、私钥或运行时日志数据。

## 需求

### 1. 日志证据与原因归因

**用户故事：** 作为 NOFX 操作者，我希望系统能从最近交易日志中明确说明无开仓原因，以便判断是市场无机会、策略过严、运行版本滞后还是风控阻断。

#### 验收标准

1. WHEN 分析最近 48 小时日志 THEN 报告 SHALL 输出启用 trader、记录范围、周期数、成功开仓数、`open_rejected` 数、主要拒绝原因和关键样本。
2. WHEN 日志里存在 `open_rejected` THEN 报告 SHALL 区分 freshness、open gate、position sizing、final limit、exchange execution 等阻断层级。
3. IF 风险状态显示 `loss_mode.active=false` 且 `remaining_risk_budget>0` THEN 报告 SHALL 明确无开仓不是亏损模式或总风险预算导致。
4. IF 日志字段与当前代码诊断字段不一致 THEN 报告 SHALL 标记可能的运行版本滞后风险。

### 2. Freshness RR 信号类型阈值回归保护

**用户故事：** 作为策略开发者，我希望 entry trigger 的 freshness RR 门控使用信号类型阈值，而不是全局净 RR 阈值，避免 `sell2/buy3/sell3` 这类可交易信号被错误过滤。

#### 验收标准

1. WHEN `Decision.StrategyMetadata.parent_signal_type=sell2` 且剩余净 RR 为 1.19 THEN `applyChanlunV2FreshnessGuard()` SHALL 使用 `entry_timing.entry_zone.signal_type_min_rr.sell2`。
2. WHEN `sell2` 阈值配置为 1.1 THEN 上述样本 SHALL 不因 `freshness_gate.rr_invalid` 被拒绝。
3. WHEN 信号类型缺失 THEN freshness RR SHALL 回退到 `signal_freshness.min_remaining_net_rr`、`strategy_risk.default_min_net_rr` 或默认值。
4. WHEN loosen mode 生效 THEN freshness RR SHALL 应用同一套 effective entry timing 放宽值。

### 3. No-open 诊断可观测性

**用户故事：** 作为系统维护者，我希望每个缠论 V2 周期都记录可执行的 no-open 诊断字段，以便前端、replay 和告警能直接解释为什么没有开仓。

#### 验收标准

1. WHEN 写入缠论 V2 决策日志 THEN `strategy_diagnostics` SHALL 包含 `active_mode`、`effective_entry_timing`、`raw_signal_count`、`parent_structure_count`、`entry_trigger_count`、`open_rejections`。
2. WHEN 连续 12 小时无成功开仓 THEN replay 报告 SHALL 输出 top no-open bucket、near-miss 样本和建议动作。
3. WHEN terminal suppression 反复出现 THEN 日志 SHALL 保留聚合计数和有限样本，避免每周期重复噪声掩盖真实 near-miss。
4. IF 运行日志缺少当前 HEAD 应有诊断字段 THEN 报告 SHALL 建议重启或重新部署运行进程。

### 4. 长时间无开仓的保守优化

**用户故事：** 作为交易策略负责人，我希望在长时间无开仓且风险状态健康时，系统能在既有 sizing 风控内试探符合信号类型 RR 的高质量 near-miss，而不是被全局过严阈值永久过滤。

#### 验收标准

1. WHEN 12 小时无成功开仓、`loss_mode.active=false`、`open_count_24h=0` THEN loosen mode SHALL 可进入观测或实盘候选放宽状态。
2. WHEN loosen mode 触发 THEN 只允许 lowering entry trigger confidence、放宽 chase ratio、降低 signal-type min RR 到配置允许的下限，不得绕过 freshness、open gate、position sizing 或 exchange preflight，也不得额外放大仓位。
3. WHEN 候选信号为 `sell2/buy3/sell3` THEN threshold SHALL 以 `signal_type_min_rr` 为基准，而不是 `strategy_risk.default_min_net_rr`。
4. IF 试探开仓失败、连续亏损或命中风险回滚条件 THEN 系统 SHALL 自动回到 balanced/safe 模式。

### 5. 离线验证与上线保护

**用户故事：** 作为开发者，我希望在改动或重启前后能用本地日志验证行为变化，不影响实盘账户。

#### 验收标准

1. WHEN 运行 `cmd/replay -open-rejection-daily` THEN 报告 SHALL 能复现最近 48 小时无开仓主因。
2. WHEN 使用当前 HEAD 对历史 rejected trigger 做兼容审计 THEN 报告 SHOULD 标记哪些样本在新阈值逻辑下会通过 freshness RR。
3. WHEN 完成代码改动 THEN SHALL 运行 `go test ./strategy/chanlunv2 ./logger ./decision` 或更窄但覆盖关键链路的测试。
4. WHEN 只执行部署/重启验证 THEN SHALL 不修改 `data/`、`decision_logs/`、`coin_pool_cache/` 运行时目录。
