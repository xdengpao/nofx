# Chanlun V2 Entry Exit Optimization Requirements

## Background

用户指出：从历史触发开仓信号看，缠论 V2 的信号触发时间总是落后于结构点时间，导致信号触发时经常已经过期。根据 161 服务 `aster_chanlun_v2` 的决策日志，当前问题不是单纯的日志展示误差，而是策略把“历史结构点”反复当作“可执行开仓信号”输出。

已检查 161 服务 `/home/ubuntu/appai3/nofx`：

- Trader: `aster_chanlun_v2`
- Exchange: `aster`
- Decision mode: `chanlun_v2`
- Scan interval: 3 minutes
- Timeframes: higher `4h`, trade `1h`, sub `15m`, micro `3m`
- Explicit V2 config only设置了 timeframes/history_depth，`signal_freshness` 使用代码默认值：soft=1 根 trade K，hard=2 根 trade K。

161 日志样本范围：

- 文件范围：`decision_20260523_145612_cycle1.json` 到 `decision_20260524_122803_cycle24.json`
- 样本数：435 个 V2 决策日志
- 决策动作统计：`wait=415`，`open_long=27`，`open_rejected=223`
- 成功开仓：0
- 27 条历史 `open_long` 全部执行失败，错误为 `仓位大小必须>0`
- `open_rejected` 主要集中在 `BNBUSDT=133`、`CLUSDT=91`、`DOGEUSDT=23`
- 有 freshness 元数据的 open/rejected 信号年龄：最小 1 根，median 2 根，最大 58 根 1h K
- Top gate reasons: BTC 1h/4h 转弱 166 次，标的下行结构逆势做多 146 次，`freshness_gate.signal_expired` 57 次

典型过期信号：

- `BNBUSDT buy2`: 结构时间 `2026-05-23 01:59 UTC+8`，首次评估 `2026-05-23 20:59 UTC+8`，首次年龄 19 根 1h，最后年龄 20 根，重复 25 次。
- `CLUSDT buy2`: 结构时间 `2026-05-23 12:59 UTC+8`，首次评估 `2026-05-23 20:59 UTC+8`，首次年龄 8 根 1h，最后年龄 9 根，重复 25 次。
- `DOGEUSDT buy3`: 结构时间 `2026-05-21 13:59 UTC+8`，评估 `2026-05-23 23:59 UTC+8`，年龄 58 根 1h。
- `HYPEUSDT buy2`: 结构时间 `2026-05-23 04:59 UTC+8`，评估 `2026-05-24 07:59` 到 `09:59 UTC+8`，年龄 27 到 29 根。
- 最新日志中 `BNBUSDT`、`HYPEUSDT` 已被前置降级为过期诊断，`raw_signal_count=2`，`signal_count=0`，说明现有 freshness/suppression 已能阻止旧信号继续进入 open gate，但仍没有提供新的鲜活入场触发。

结论：

1. 现有 V2 的结构信号是“背景结构”而不是天然可执行信号；当 Rust 分析器在长历史中返回旧 `buy2/sell2` 时，不能直接开仓。
2. 已部署的新鲜度门控只解决了“不再错误开旧信号”，没有解决“如何在结构出现后等待鲜活入场触发”。
3. 平仓侧目前主要依赖反向 1h V2 信号，缺少 15m/3m 结构破坏、保本、浮盈回撤、分批止盈等风险降低动作。
4. 早期日志中的 `仓位大小必须>0` 表明 V2 开仓执行前需要更硬的 sizing fail-safe：任何数量为 0 的 open 决策必须转为 rejected，不能进入交易所执行层。
5. 用户提出的三买/三卖 `D/N` 质量模型可作为 entry trigger 的质量层，但不能直接固化为 1h 百分比规则；需要拆分为结构边界未回中枢、突破回撤比例、回调耗时、ATR/分位数归一化和剩余净 RR。

## Glossary

- Structure signal: 缠论 V2 在 trade timeframe 上识别的 `buy1/buy2/buy3/sell1/sell2/sell3` 结构点。
- Parent structure: 作为交易背景的结构信号，使用 `parent_signal_id` 和 `parent_signal_close_time` 表示。
- Entry trigger: 在 parent structure 后，由 15m/3m 回踩、重测、恢复、突破或失败确认产生的可执行入场触发。
- Trigger close time: entry trigger 所属 K 线 close time。可执行开仓的新鲜度必须基于该时间，而不是 parent structure 时间。
- Signal lifecycle: 同一个 parent structure 到触发、开仓、失效、平仓的完整状态机。
- Terminal open state: 结构已经过期、目标穿越、RR 无效、触发窗口关闭或已尝试开仓后不可重复尝试的状态。
- Risk-reducing action: `close_long`、`close_short`、`partial_close`、`move_stop_loss` 等降低已有仓位风险的动作。
- P0 / Structure boundary: 三买/三卖判断的中枢边界。三买为中枢上沿，三卖为中枢下沿。
- Breakout candle: 收盘价有效脱离 P0 的第一根已收 K 线；频率可为 trade/watch/trigger timeframe，不限定 1h。
- P1 / Pullback extreme: 突破后首次回调确认形成的极值。三买为回调最低价，三卖为反抽最高价。
- G / Support gap: 方向归一化后的 P1 到 P0 间隙。三买为 `(P1-P0)/P0`，三卖为 `(P0-P1)/P0`；`G > 0` 表示未回中枢，`G <= 0` 表示三买/三卖质量失效。
- R / Breakout retracement: 突破段被回撤的比例。三买可用 `(H-P1)/(H-P0)`，三卖可用 `(P1-L)/(P0-L)`，其中 H/L 是突破后回调前的方向极值。
- N / Pullback candles: 从 breakout candle 到 P1 所属 K 线的 K 数，按实际质量评估 timeframe 计算，并可换算为分钟或 trade timeframe 等效 K 数。
- G_ATR: `abs(P1-P0)/ATR(timeframe)`，用于跨币种、跨频率归一化 P1 距 P0 的远近。

## Requirements

### Requirement 1: V2 开仓必须从旧结构直开改为鲜活入场触发

**User Story:** 作为交易监督者，我希望旧的 1h 结构点只作为背景，不在数小时后直接开仓；真正开仓必须来自结构之后的新鲜 15m/3m 入场触发。

#### Acceptance Criteria

1. WHEN V2 分析得到 trade timeframe 结构信号 THEN 系统 SHALL 先将其记录为 parent structure marker，而不是默认生成 open-like 决策。
2. WHEN parent structure 的年龄超过 direct open 窗口 THEN 系统 SHALL NOT 直接产生 `open_long/open_short`。
3. WHEN parent structure 仍处于可观察窗口且 15m/3m 出现配置允许的 entry trigger THEN 系统 SHALL 生成新的 open-like 决策。
4. WHEN 生成 open-like 决策 THEN `signal_id` 或执行去重 ID SHALL 基于 entry trigger，而 parent structure SHALL 通过 `parent_signal_id` 保留。
5. WHEN 计算 open signal freshness THEN 系统 SHALL 使用 trigger close time，而不是 parent structure close time。
6. IF parent structure 已经目标穿越、结构失效或剩余 RR 不足 THEN 系统 SHALL 终止该 lifecycle，并不得再等待 entry trigger。

### Requirement 2: 入场触发必须有可配置的窗口、类型和质量阈值

**User Story:** 作为策略开发者，我希望 V2 能在结构点后等待可解释的回踩/重测/恢复触发，而不是因为信号滞后完全错过交易。

#### Acceptance Criteria

1. WHEN parent structure 出现 THEN 系统 SHALL 在配置的 watch window 内跟踪该结构，默认建议为 4 到 8 根 `15m` K 或 1 到 2 根 `1h` K。
2. WHEN 15m/3m 出现 `pullback_retest_resume`、`breakout_continuation` 或 `micro_reversal_confirm` 等允许触发类型 THEN 系统 SHALL 评估是否可开仓。
3. IF entry trigger 距离当前评估 K 线超过配置的 trigger age THEN 系统 SHALL 拒绝开仓并记录 `entry_trigger_expired`。
4. IF 当前价格已追高/追低超过结构风险预算 THEN 系统 SHALL 拒绝开仓并记录 `entry_zone_chased`。
5. IF 剩余净 RR 低于阈值，默认建议 altcoin 不低于 2.5、BTC/ETH 可配置为 2.0 到 2.2 THEN 系统 SHALL 拒绝开仓。
6. WHEN 市场处于 BTC 1h/4h 明显转弱状态 THEN 高 beta 山寨多单 SHALL 继续被阻断；该优化不得绕过现有 BTC/ADX/open gate。
7. WHEN 评估 `buy3/sell3` 或突破回踩型 entry trigger THEN 系统 SHALL 计算方向归一化的 `P0`、`P1`、突破极值 H/L、`G`、`R`、`N`、`G_ATR` 和 `remaining_net_rr`。
8. IF `G <= 0` THEN 系统 SHALL 将该三买/三卖质量判为失效并拒绝开仓，reason code 使用 `third_point.reentered_center` 或等价结构化原因。
9. IF `G_ATR` 或按历史分位数归一化后的 P1-P0 间隙超过配置阈值 THEN 系统 SHALL 视为距离结构边界过远，并拒绝或降级为 `entry_zone_chased`。
10. IF `R` 超过配置的最大突破回撤比例 THEN 系统 SHALL 降级或拒绝该 trigger，并记录 `third_point.deep_retracement` 或 `third_point.fake_breakout_risk`。
11. WHEN `G > 0`、`G_ATR` 未追高/追低、`R` 未过深、`N` 不超过配置上限且剩余净 RR 达标 THEN 系统 SHALL 可将其分类为 `strong_third_buy` 或 `strong_third_sell`。
12. IF `N` 超过配置的最大回调耗时但 `G` 仍为正 THEN 系统 SHALL 将其视为突破后震荡/小级别中枢，不得直接按强三买/三卖开仓；只有在二次触发、区间套确认或降风险配置允许时才可继续评估。
13. WHEN 质量评估 timeframe 不是 1h THEN 系统 SHALL 使用 ATR、历史分位数或分钟等效 K 数做归一化；`1.0%/1.5%/3.0%` 只能作为可配置 profile seed，不得作为所有币种和频率的硬编码常量。
14. WHEN 成交量、OI、taker imbalance、funding 等辅助信息可用 THEN 系统 SHOULD 将其写入 diagnostics 用于提权或降级解释，但这些信息不得绕过 `G/R/N/G_ATR/RR` 的硬门控。

### Requirement 3: V2 开仓执行前必须具备硬 sizing fail-safe

**User Story:** 作为系统负责人，我希望任何 V2 open 决策在数量、保证金、最小名义额不满足时被明确拒绝，而不是进入交易所执行层失败。

#### Acceptance Criteria

1. WHEN open-like 决策经过 freshness/entry trigger 后 THEN 系统 SHALL 在执行前完成 position sizing。
2. IF `quantity <= 0`、名义额低于交易所最小值、保证金不足或止损/止盈结构无效 THEN 系统 SHALL 生成 `open_rejected`，reason code 使用 `position_sizing.*` 或 `preflight.*`。
3. WHEN sizing 被拒绝 THEN 系统 SHALL NOT 调用真实下单接口。
4. WHEN sizing 成功 THEN 系统 SHALL 保留 `risk_amount`、`margin_required`、`notional_value`、`leverage`、`stop_loss`、`take_profit` 等元数据。
5. Tests SHALL prove 161 历史中的 `仓位大小必须>0` 场景不会再进入交易所执行。

### Requirement 4: V2 平仓必须扩展为多层风险降低管理

**User Story:** 作为交易者，我希望已有仓位不只等 1h 反向信号才处理，而是能用结构破坏、保本、浮盈回撤和分批止盈主动降低风险。

#### Acceptance Criteria

1. WHEN 有 V2 持仓 THEN 系统 SHALL 每个周期先执行 position management，再评估新开仓。
2. WHEN 持仓达到配置的 R 倍数或利润阈值 THEN 系统 SHALL 支持移动止损到保本或保本加手续费。
3. WHEN 持仓达到第一目标或 `1R/1.5R` 等配置阈值 THEN 系统 SHALL 支持分批止盈。
4. WHEN 15m 或 1h 出现反向结构破坏 THEN 系统 SHALL 支持 `partial_close` 或 `close_*`，并记录结构位、确认 K 数和动作原因。
5. WHEN 浮盈激活后回撤超过配置比例 THEN 系统 SHALL 支持部分或全部平仓。
6. WHEN 1h 出现反向 V2 信号且 sub/micro 确认 THEN 系统 SHALL 支持全平；是否反手开仓必须走新的 entry trigger 和 open gate。
7. WHEN action 属于 risk-reducing THEN 系统 SHALL NOT 因 open signal freshness gate 被阻断，但仍 SHALL 通过交易所 preflight 和仓位存在性校验。

### Requirement 5: 信号生命周期必须可去重、可恢复、可对账

**User Story:** 作为维护者，我希望同一个旧结构不会在重启后无限刷屏，同时能从日志看清它是否等待触发、已触发、已过期或已失效。

#### Acceptance Criteria

1. WHEN parent structure 被发现 THEN 系统 SHALL 写入 lifecycle state，包含 trader ID、symbol、parent signal ID、结构时间、方向、状态和到期时间。
2. WHEN entry trigger 被创建、拒绝、执行或失效 THEN 系统 SHALL 更新同一 lifecycle。
3. WHEN 同一 parent structure 已处于 terminal open state THEN 系统 SHALL NOT 再生成 open-like 决策。
4. IF 系统重启 THEN lifecycle state SHOULD 从 `data/` 恢复，避免 stale suppression 只存在内存导致重复刷屏。
5. WHEN 生成日志、marker、API 响应 THEN 系统 SHALL 能通过 `parent_signal_id`、`entry_trigger_id`、`signal_id` 对账。

### Requirement 6: 日志和策略检查页必须量化“结构到触发”的延迟

**User Story:** 作为用户，我希望策略检查页直接显示结构时间、触发时间、评估时间和动作时间，从而判断信号是否已经错过。

#### Acceptance Criteria

1. WHEN V2 记录 parent structure THEN marker SHALL 显示结构时间。
2. WHEN V2 记录 entry trigger THEN marker SHALL 显示触发时间和 parent structure 时间。
3. WHEN V2 记录 open/close/rejection action THEN marker SHALL 显示评估 K 线和动作时间。
4. WHEN parent structure 到 entry trigger 的延迟超过配置阈值 THEN 日志 SHALL 输出 `structure_to_trigger_latency_candles`。
5. WHEN open 被拒绝 THEN 日志 SHALL 区分 `entry_trigger_expired`、`freshness_gate.*`、`open_gate.*`、`position_sizing.*`、`preflight.*`。
6. WHEN 日志聚合时 THEN 系统 SHOULD 能输出 open conversion、stale rate、trigger latency、close reason distribution 等指标。
7. WHEN 三买/三卖质量模型参与入场判断 THEN marker、日志或 diagnostics SHALL 输出 `third_point_quality_category`、`support_gap_pct`、`support_gap_atr`、`retracement_ratio`、`pullback_candles` 和缺失数据原因。

### Requirement 7: 回归验证必须基于 161 历史问题

**User Story:** 作为开发者，我希望这次优化用 161 的历史问题做回归基线，避免再次把旧结构误当新信号。

#### Acceptance Criteria

1. Unit tests SHALL cover BNB-like 旧 parent structure 不直接开仓。
2. Unit tests SHALL cover fresh entry trigger 使用 trigger close time 通过 freshness gate。
3. Unit tests SHALL cover old parent + fresh trigger 的 `parent_signal_id`、`entry_trigger_id`、`signal_close_time`、`trigger_close_time` 元数据。
4. Tests SHALL cover zero quantity open 被拒绝且不执行下单。
5. Position management tests SHALL cover breakeven、partial take profit、structure break、reverse signal close。
6. Replay or fixture tests SHOULD include 161 的 BNB/CLUSDT/DOGE/HYPE stale cases。
7. Tests SHALL cover `G <= 0` 的三买/三卖回中枢失效场景。
8. Tests SHALL cover `G_ATR` 或分位数距离过大导致 `entry_zone_chased` 的场景。
9. Tests SHALL cover `R` 过深、`N` 过长导致降级或拒绝的场景。
10. Tests SHALL cover `G/R/N/G_ATR/RR` 全部达标时生成 `strong_third_buy` 或 `strong_third_sell` 的场景。
11. Tests SHALL NOT place real exchange orders.

### Requirement 8: 兼容性和安全边界不得降低

**User Story:** 作为系统负责人，我希望优化开平仓质量时不破坏现有 V2 freshness、stale suppression、open gate 和交易所保护单逻辑。

#### Acceptance Criteria

1. Existing `chanlun_v2` signal freshness and stale suppression SHALL continue working.
2. Existing BTC regime、ADX/DI、correlation、loss mode、execution quality gates SHALL remain effective.
3. Existing `CancelStopLossOrders()` and `CancelTakeProfitOrders()` split SHALL be preserved.
4. New config fields SHALL be optional; old `config.json` SHALL load with conservative defaults.
5. Runtime state under `data/` and `decision_logs/` SHALL NOT be committed as feature code.
