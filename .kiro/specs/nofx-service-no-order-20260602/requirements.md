# NOFX 服务最近 2 日无交易单输出诊断需求

## 背景

本规格基于 `nofx` Docker 服务日志、`decision_logs/aster_chanlun_v2` 决策日志、`config.json` 的非敏感配置摘要，以及当前代码链路完成。分析时间为 2026-06-02，时区为 `Asia/Shanghai`。

近 2 日口径采用最近 48 小时：

- 开始：2026-05-31 16:30:37 +08:00
- 结束：2026-06-02 16:30:37 +08:00

服务日志覆盖说明：

- `nofx-trading` 容器于 2026-05-31 23:27:07 +08:00 启动，因此 Docker 容器日志只覆盖该时间之后。
- 本地 `decision_logs/aster_chanlun_v2` 覆盖完整 48 小时窗口。
- `aster_deepseek` 最后一条决策日志停在 2026-05-25 17:00:51 +08:00，最近 2 日没有运行输出。

当前运行配置摘要：

- 共 5 个 trader，只有 `aster_chanlun_v2` 启用。
- `aster_chanlun_v2` 使用 `decision_mode=chanlun_v2`、`exchange=aster`、扫描周期 3 分钟。
- 动态候选池未启用，静态候选池为 10 个默认币种。
- `strategy_risk.enabled=true`，`default_min_net_rr=2.5`。
- `trading_frequency.mode=balanced`，`loosen_mode.enabled=true`。

## 日志事实

最近 48 小时内，`aster_chanlun_v2` 决策日志统计如下：

| 指标 | 数值 |
| --- | ---: |
| 决策记录 | 960 |
| 成功开仓 | 0 |
| 非空持仓周期 | 0 |
| `wait` 动作 | 960 |
| `open_rejected` 动作 | 44 |
| `open_long` 意图 | 34 |
| `open_short` 意图 | 10 |
| `trigger_ready_count` | 48 |
| `waiting_for_trigger_count` | 158 |
| 近 48 小时无成功开仓时长 | 47.91 小时 |

按日期拆分：

| 日期 | 决策周期 | `wait` | `open_rejected` |
| --- | ---: | ---: | ---: |
| 2026-05-31 | 150 | 150 | 5 |
| 2026-06-01 | 480 | 480 | 0 |
| 2026-06-02 00:00-16:30 | 330 | 330 | 39 |

replay 审计的主要 no-open bucket：

| Bucket | Count |
| --- | ---: |
| `adx` | 37 |
| `rr` | 21 |

说明：这是当前 replay 的历史基线输出。交叉检查确认 `classifyRejectionBucket()` 目前先匹配 ADX 再匹配 BTC，因此 ASTERUSDT 样本里同时包含 ADX `report-only` 与 BTC hard veto 时会先落入 `adx` bucket。增强后的目标口径应把 `btc_hard_veto`、`adx_report_only`、`final_rr` 分开。

主要拒绝归因：

- `ASTERUSDT open_long` 共 34 次，均被 `open gate` 阻止。日志同时记录 `ASTERUSDT 1h ADX 15.3-16.3 低于 profile 阈值 25.0（report-only）`，以及硬阻断原因 `BTC 1h/4h 明显转弱，禁止新开高 beta 山寨多单`。
- `DOGEUSDT open_short` 共 10 次，其中 2 次被 freshness RR 旧阈值 2.5 拒绝；replay 显示若按 `sell2` 信号类型阈值 1.1 审计，这 2 次会通过 freshness RR。
- `DOGEUSDT open_short` 另有 3 次被 open gate 置信度拒绝：`62 < 65`。
- `DOGEUSDT open_short` 另有 5 次被最终开仓验证拒绝：风险回报比约 1.36-1.59，低于当前 `validateOpenDecision` 的全局硬阈值 2.5。
- 大量父结构信号在成为开仓候选前被终态静默，主要为 `entry_rr_invalid` 和 `entry_parent.watch_window_expired`。

服务日志交叉验证：

- 容器启动正常，API 与周期任务都在运行。
- 容器日志显示配置加载成功：5 个 trader，1 个启用。
- 容器日志中只出现“创建订单追踪器”，没有真实交易所下单成功、开仓成功或成交成功日志。
- 自动成交订单检查持续运行，日志反复显示“无自动成交订单”。

## 结论

最近 2 日没有交易单输出，不是服务宕机、交易所执行失败、持仓上限、亏损模式或风险预算耗尽导致。

直接原因是：当前唯一启用的 `aster_chanlun_v2` 虽然产生了少量开仓候选，但所有候选都在交易所执行前被策略/风控链路拦截，最终执行层只收到 `wait` 或 `open_rejected`，没有进入真实下单路径。

更具体地说：

1. 绝大多数周期没有可执行 entry trigger。
2. 2026-06-02 主要候选是 `ASTERUSDT open_long`，但 BTC 1h/4h 转弱时，高 beta 山寨多单被硬阻断。
3. `DOGEUSDT open_short` 是更贴近当前 BTC 弱势环境的方向，但被 freshness RR、open gate 置信度和最终 RR 2.5 硬阈值分层拒绝。
4. 当前 `loosen` 模式已经生效，但它没有绕过 BTC 硬阻断，也没有改变 `validateOpenDecision` 的全局 RR 2.5 硬检查。

## 目标

1. 明确最近 2 日无交易单输出的真实原因和责任链路。
2. 形成可执行的方案：在不关闭硬风控的前提下，提高系统把合格信号推进到下单层的能力。
3. 保留当前 no-order 诊断能力，并让后续排查能通过 replay 或服务日志快速复现。
4. 避免为了“有订单”而直接放开 BTC 硬阻断、RR、仓位 sizing 或交易所 preflight。

## 非目标

- 不直接修改真实交易账户配置、API key、私钥或钱包信息。
- 不在测试或 replay 中触发真实下单。
- 不把 `data/`、`decision_logs/`、`coin_pool_cache/` 作为功能改动提交。
- 不降低现有 RR 阈值，包括最终开仓验证的全局 RR 2.5 硬阈值。
- 不绕过 `decision` 风控直接执行缠论 V2 输出。

## 需求

### 1. 最近 2 日 no-order 诊断报告

**用户故事：** 作为 NOFX 操作者，我希望系统能从服务日志和决策日志中直接说明为什么没有交易单输出，以便区分服务故障、配置问题、策略没信号、风控阻断和交易所执行失败。

#### 验收标准

1. WHEN 分析最近 48 小时日志 THEN 报告 SHALL 输出时间窗口、服务启动时间、启用 trader、决策周期数、成功开仓数、`wait` 数和 `open_rejected` 数。
2. WHEN 只有一个 trader 启用 THEN 报告 SHALL 明确其他 trader 没有参与最近 2 日交易输出。
3. WHEN 决策日志中所有最终动作都是 `wait` 或 `open_rejected` THEN 报告 SHALL 明确没有进入交易所下单路径。
4. WHEN 服务日志出现“订单”字样 THEN 报告 SHALL 区分订单追踪器创建与真实交易所订单创建。

### 2. 分层拒绝归因

**用户故事：** 作为策略维护者，我希望每个被拒开仓都能按 freshness、entry zone、open gate、最终 RR、position sizing、exchange execution 分层归因，以便知道应该调哪个模块。

#### 验收标准

1. WHEN `ASTERUSDT open_long` 被拒 THEN 报告 SHALL 标记硬阻断层为 BTC multi-timeframe gate，而不是笼统归因到 ADX。
2. WHEN ADX 文案包含 `report-only` THEN 报告 SHALL 说明 ADX 是诊断/降权信号，不能单独解释硬阻断。
3. WHEN `DOGEUSDT open_short` 被拒 THEN 报告 SHALL 区分 freshness RR、open gate min confidence、最终 RR 2.5 三类拒绝。
4. WHEN `remaining_risk_budget>0`、无持仓且 `loss_mode.active=false` THEN 报告 SHALL 明确无订单不是账户风险预算或亏损模式导致。

### 3. BTC 弱势环境下的信号方向治理

**用户故事：** 作为策略负责人，我希望 BTC 明显转弱时，系统不要反复把高 beta 山寨多单推进到必然被硬阻断的阶段，而应更早抑制这类候选并优先暴露顺势 short near-miss。

#### 验收标准

1. WHEN BTC 1h/4h 明显转弱 AND 候选为高 beta 山寨 `open_long` THEN 策略诊断 SHALL 明确标记 `btc_hard_veto`。
2. WHEN 同一 signal 在连续周期被同一 BTC hard veto 拒绝 THEN 系统 SHOULD 进入该 signal 的短期冷却或终态静默，减少重复 open_rejected。
3. WHEN BTC 弱势环境持续 THEN no-open 报告 SHALL 输出 short-side near-miss，而不只展示反复被 veto 的 long 信号。
4. IF 未来允许 BTC hard veto 的灰度放宽 THEN SHALL 只在显式 pilot 配置、低仓位、无亏损模式、无同向持仓、通过 RR 和 preflight 时生效。

### 4. 缠论 V2 RR 阈值一致性

**用户故事：** 作为策略开发者，我希望 freshness RR、entry timing RR 与最终开仓验证 RR 的阈值关系清晰一致，并且明确最终开仓验证继续保留全局 RR 2.5 硬阈值，避免把 entry trigger 通过误解为可直接下单。

#### 验收标准

1. WHEN `sell2` freshness RR 阈值为 1.1 THEN 系统 SHALL 说明最终开仓验证是否仍要求全局 RR 2.5。
2. WHEN 最终 RR 2.5 仍为硬阈值 THEN replay 报告 SHALL 把 `freshness pass but final RR fail` 单独归因。
3. WHEN 形成实施任务 THEN SHALL NOT 新增低于当前最终 RR 2.5 的 signal-type final RR、pilot final RR 或按场景降低 final RR 的配置。
4. WHEN 编写配置或运维说明 THEN SHALL 明确 `entry_timing.signal_type_min_rr` 只控制 entry trigger，不保证最终可下单。

### 5. Loosen 模式可解释性

**用户故事：** 作为 NOFX 操作者，我希望长时间无开仓后的 loosen 模式不仅显示已经开启，还能说明哪些 gate 被放宽、哪些 gate 仍是硬阻断。

#### 验收标准

1. WHEN `active_mode=loosen` THEN 决策日志 SHALL 包含有效 `min_trigger_confidence`、`min_remaining_net_rr`、`max_chase_ratio`。
2. WHEN loosen 降低基础置信度要求 THEN 日志 SHALL 输出 `from`、`to`、`actual_confidence`。
3. WHEN loosen 仍不能开仓 THEN no-open 报告 SHALL 标记剩余硬阻断，例如 BTC hard veto 或 final RR 2.5。
4. WHEN 配置存在 `max_duration_hours` THEN 系统 SHALL 明确记录该字段当前是否仅作为配置/诊断值；除非另行补充行为设计，本规格 SHALL NOT 默认新增 loosen 回到 balanced/safe 的实际退出逻辑。

### 6. 验证与回归保护

**用户故事：** 作为开发者，我希望任何方案在进入实盘前都能通过只读 replay 和单元测试验证，不影响真实账户。

#### 验收标准

1. WHEN 运行未增强前的历史基线 `cmd/replay -open-rejection-daily` THEN 报告 SHALL 能复现最近 48 小时 `record_count=960`、`rejected_open_count=44`、top buckets 为 `adx` 和 `rr`。
2. WHEN 增强后的 replay 重新分类同一窗口 THEN 报告 SHALL 保留 `record_count=960`、`rejected_open_count=44` 的历史事实，并把 ASTER long 主因归入 `btc_hard_veto`，把 ADX `report-only` 单独归入 `adx_report_only`，把最终 RR 2.5 拒绝归入 `final_rr`。
3. WHEN 阶段 2 将 BTC hard veto 前移到策略预检查 THEN 历史 replay SHALL 仍能解释原始 `open_rejected=44`，但新行为的未来周期 MAY 减少 `open_rejected` 并增加 `btc_hard_veto_precheck` 或 terminal suppression 计数。
4. WHEN 修改 RR 或 open gate 行为 THEN SHALL 添加 `decision` 或 `strategy/chanlunv2` 单元测试，覆盖 ASTER BTC hard veto、DOGE confidence rejection、DOGE final RR rejection，并确认最终 RR 2.5 未被降低。
5. WHEN 修改 replay 或日志诊断 THEN SHALL 添加 `logger` 或 `cmd/replay` 测试，确保 no-order bucket 稳定。
6. WHEN 完成实现 THEN SHALL 至少运行相关包测试；触及共享后端契约时 SHALL 运行更广泛的 `go test ./...` 或等价验证。
