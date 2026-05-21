# 程序化缠论策略实盘缺陷修复 Requirements

## 文档基本信息

| 字段 | 值 |
|---|---|
| 规格名称 | chanlun-live-trading-defect-fix |
| 评估对象 | 远程主机 `43.167.168.161:/home/ubuntu/appai3/nofx`，分支 `jzhbnofxdev`，commit `9656386 Implement programmatic Chanlun optimization pipeline` |
| 涉及代码 | `strategy/chanlun/{engine.go, signals.go, structure.go, divergence.go, position_management.go, state.go, lifecycle.go, types.go}`、`config.json` 中 `traders[aster_deepseek].programmatic_strategy` 与 `strategy_risk` |
| 评估窗口 | 2026-05-20 14:00 ~ 2026-05-21 14:40（CST），近 24 小时 481 条决策日志 |
| 现役 trader | `aster_deepseek` (Aster DEX, decision_mode=programmatic) |
| 关联已存 spec | `programmatic-chanlun-strategy`、`programmatic-chanlun-entry-timing`、`programmatic-chanlun-profit-optimization`、`programmatic-confidence-execution-fix`、`programmatic-signal-noise-latency-clarity`、`programmatic-signal-staleness-guard`、`programmatic-two-tier-rhythm`、`live-open-rate-tuning`、`live-loss-diagnosis-optimization` |
| 状态 | 进行中（requirements/design/tasks 已修订，待执行） |

## 1. 背景与现状证据

### 1.1 实盘 24 小时事实数据

| 项 | 数值 | 备注 |
|---|---|---|
| 决策周期总数 | 481 | scan_interval=3min，符合配置 |
| 决策动作 `wait` | 480（99.79%） | |
| 决策动作 `open_rejected` | 6（0.21%） | 全部 XAUUSDT/XRPUSDT 做空 |
| 真实成交开仓 | **0** | |
| 真实成交平仓 | 0 | 没有持仓可平 |
| 全程持仓数 | 全部 cycle = 0 | |
| 账户权益 | 44.18 → 44.42 USDT | 漂移由 unrealized 推算 |
| 解析失败日志 | 1 | 不影响结论 |

### 1.2 高频拒绝/抑制原因（按归一化模板）

| 频次 | 模板 |
|---|---|
| 1955 | `<SYM> 1h 预览层 preview_2x15m 观察到 <N> 个结构，默认仅观察并按生命周期折叠` |
| 574+380 | `<SYM> sell<N>/buy<N> pilot 跳过: 置信度 <N> 低于 90` |
| 456 | `主信号层 <N> 个无持仓候选等待 1h 新闭合 K 线` |
| 435 | `<SYM> 1h 预览层 <N> 根 15m 暂未形成买卖点` |
| 197 | `<SYM> 识别到 <N> 个信号` |
| 48 | `<SYM> sell<N> 做空止损/止盈结构不合法` |
| 46 | `<SYM> sell<N> 作为结构背景保留，direct_structure_open 关闭，等待 15m fresh entry trigger` |
| 43 | `<SYM> 无买卖点信号` |
| 37+13 | `<SYM> buy<N>/sell<N> 入场追价比例 <X> 超过阈值 <Y>` |
| 27 | `<SYM> buy<N> 做多结构已越过止盈目标` |
| 14 | `<SYM> buy<N> 做多止损/止盈结构不合法` |
| 12 | `<SYM> sell<N> 剩余净 RR <X> 低于阈值 <Y>` |
| 6 | `<SYM> open_short signal_id=<id> 已因 remaining_net_rr_too_low/invalid_stop_take_profit_structure 抑制，跳过重复开仓` |

### 1.3 单条 open_rejected 取证（cycle 489，2026-05-20 18:45:28）

```
symbol           : XAUUSDT
signal_type      : sell2
trigger_type     : preview_3x15m_pilot
entry_window     : watchlist (尚未升级 pilot)
preview_components: 3 (preview_3x15m 已成立)
preview_confirmed: false
current_price    : 4497.01
stop_loss        : 4512.00      (做空)
take_profit      : 4469.25
remaining_net_rr : 1.25         (要求 2.50, BTC/ETH/BNB 要求 2.20)
freshness_state  : fresh
age_candles      : 0
pilot_pos_size   : 49.97 USDT   (账户余额仅 44.42 USDT, 已超额)
structure_key    : c5be41a3..   (该 key 在多个 cycle 重复抑制)
最终原因         : remaining_net_rr_too_low
```

### 1.4 候选标的现状

固定候选 10 个：BTCUSDT、ETHUSDT、SOLUSDT、BNBUSDT、BCHUSDT、LTCUSDT、XRPUSDT、DOGEUSDT、ADAUSDT、HYPEUSDT。但日志中持续出现 **XAUUSDT、XAGUSDT、CLUSDT**（黄金、白银、原油），与 `default_coins` 不一致；同时所有候选 `warnings` 字段都标注 `行情源为 Binance，执行交易所为 Aster，需关注价差`。

### 1.5 配置侧瓶颈一览

```jsonc
{
  "scan_interval_minutes": 3,                              // OK
  "decision_mode": "programmatic",
  "programmatic_strategy": {
    "timeframes": { "higher": "4h", "trade": "1h", "sub": "15m", "micro": "3m" },

    "preview_signals": {
      "watch_after_closed_components": 2,
      "pilot_after_closed_components": 3,
      "pilot_min_confidence": 90,        // ★ 实际信号 65~86，几乎无法过线
      "pilot_risk_fraction": 0.3,
      "require_confirmed_upgrade": true
    },

    "entry_timing": {
      "direct_structure_open": false,    // ★ 强制等 15m fresh trigger
      "max_trigger_age_candles": 1,
      "entry_zone": {
        "max_chase_ratio": 0.35,         // ★ 1h 上 K 线幅度大，频繁超阈值
        "min_remaining_net_rr": 2.5,     // ★ 与 0.2% 双边费率叠加几乎不可达
        "symbol_overrides": { "BTCUSDT/ETHUSDT/BNBUSDT": { "ratio": 0.4, "rr": 2.2 } }
      },
      "pilot": { "min_confidence": 90 }  // ★ 与 preview_signals.pilot_min_confidence 重复
    },

    "structure": { "min_swing_pct": 0.3, "atr_multiplier": 0.5, "left_bars": 2, "right_bars": 2, "min_stroke_bars": 5 },
    "divergence": { "ratio": 0.8, "require_b_zero_axis": false },
    "adx": { "min_adx": 20, "micro_adx_filter": false },
    "position": { "max_add_count": 1, "partial_close_pct": 30, "allow_reversal": false },
    "take_profit": { "mode": "structure", "fallback_mode": "reject", "min_net_rr": 2.5 }
  }
}
```

```jsonc
{
  "leverage": { "btc_eth_leverage": 5, "altcoin_leverage": 5 },
  "trading_frequency": {
    "mode": "balanced",
    "daily_open_limit": 2,
    "report_only": { "high_adx": true, "rr_threshold": true, "rolling_gate": true }  // ★ 三档全开 = gate 形同虚设
  },
  "strategy_risk": { "default_min_net_rr": 2.5, "fee_slippage_pct": 0.2, "safe_mode": { "max_risk_pct": 0.5, "daily_open_limit": 1 } }
}
```

```jsonc
"initial_balance": 44.18                                    // ★ 与 pilot_position_size_usd≈49.97 严重不匹配
```

## 2. 问题定性归类

| 编号 | 类别 | 现象 | 根因 |
|---|---|---|---|
| D1 | 入场链路过深 | 99% cycle 停在 preview_2x15m / preview_3x15m | 分型→笔→线段→中枢→背驰→preview(2x→3x→pilot)→fresh trigger→entry_zone 共 8 道关，单根 1h 周期内大概率走不完 |
| D2 | 置信度阈值与实际分布脱节 | 954 条 pilot 跳过 | `pilot_min_confidence=90` 但实际识别置信度多在 65-86，没有任何样本证据支持 90 阈值 |
| D3 | 净 RR 阈值过高且未差异化 | 12 条剩余净 RR<2.5 拒绝 + 4 条结构抑制 | `min_net_rr=2.5` 在加密 1h 周期上 + `fee_slippage_pct=0.2%` 双边 = 实际需要 ≥3R 价差才能过线，而缠论 sell2/buy2 的结构幅度通常只有 1-2R |
| D4 | 追价比例僵化 | 50 条入场追价超阈值 | `max_chase_ratio=0.35` 是固定值，未按 ATR/波动状态/标的特性调整 |
| D5 | direct_structure_open=false 强约束 | 46 条结构信号"等待 15m fresh trigger" | 关闭后必须等 sub 级别新触发；preview 与 fresh trigger 相互级联，触发时 entry_zone 已闭合 |
| D6 | 同结构重复抑制无冷却 | 6 条同 structure_key 反复 reject | 抑制状态被长期保留，但每 cycle 仍重新计算 preview/pilot/guard，浪费算力且无新交易决策 |
| D7 | 信号迟到与 target 越界 | 27 条"已越过止盈" + 14 条"做多结构非法" + 48 条"做空结构非法" | `signal_close_time` 与 `decision_close_time` 错位，加上 `direct_structure_open=false` 滞后，价格已脱离结构区间 |
| D8 | 本金与仓位不匹配 | pilot_position_size 49.97 USDT > 余额 44.42 USDT | `pilot_risk_fraction=0.3` × 5x 杠杆 × `min_net_rr=2.5` 反推出的名义额度大于账户本金 |
| D9 | 候选标的治理偏差 | XAUUSDT/XAGUSDT/CLUSDT 进入候选 | 动态候选池注入了非加密标的，行情源 Binance 与执行交易所 Aster 存在价差，且这些标的的缠论结构节奏与策略假设不匹配 |
| D10 | 频率 gate 三档全开 report_only | 一天 0 开仓但 daily_open_limit=2 永远未触发 | `report_only.high_adx/rr_threshold/rolling_gate=true` 让所有自适应 gate 退化为日志 |
| D11 | 安全模式无激活路径 | safe_mode.active=false 全程不变 | 现行触发条件依赖 rolling pf/dd，但实际既无开仓也无亏损，永远不触发 |
| D12 | 数学校验异常率高 | 48+14 条止损/止盈结构非法 | `signals.go` 计算的 SL/TP 与 `entry_reference_price` 在某些时序下违反 SL<price<TP（多）或 SL>price>TP（空），未做"非法即结构作废"而是反复重试 |
| D13 | 可观测性盲区 | 缺少每候选/每信号的最终 reason_code、累计 PnL、上次成交时间 | 决策日志聚合不到 trader 维度的"24h 健康看板" |
| D14 | 持仓管理规则未触达 | 24h 0 持仓 → breakeven/floating_drawdown/structure_break 全部静默 | 策略事实上从未走完一次完整生命周期，持仓管理质量未被验证 |

## 3. 范围

### 3.1 本规格覆盖

- 程序化缠论策略实盘 24 小时空转的根因修复。
- `entry_timing`、`preview_signals`、`signal_freshness`、`entry_zone` 的阈值合理化与自适应。
- 抑制结构（`SignalSuppression`）的冷却与跳过逻辑改进。
- pilot 仓位 sizing 与小本金账户的安全约束。
- 候选标的治理与跨交易所行情/执行一致性。
- 频率 gate `report_only` 与 `safe_mode` 的实际生效路径。
- 决策日志的可观测性扩展。

### 3.2 本规格不覆盖

- 缠论分型/笔/线段/中枢/背驰算法的数学定义（参见 `programmatic-chanlun-strategy`）。
- AI 决策模式（`decision_mode=ai`）相关行为。
- 新增交易所或新增 AI provider。
- UI/前端图表展示。
- 实盘资金管理、出入金、风控停机外部接管。

## 4. 术语

- **决策周期 (cycle)**：`AutoTrader.runCycle()` 一次完整执行，对应一条 `decision_*.json`。
- **主信号层 (main signal)**：在 trade 周期闭合 K 线上识别的 1/2/3 类买卖点。
- **预览层 (preview)**：在 sub 周期（如 15m）连续闭合 N 根后提前触发 watchlist/pilot 的信号。
- **入场触发 (entry trigger)**：sub 周期上的 fresh trigger 事件（如 `pullback_retest_resume`）。
- **入场区间 (entry zone)**：以信号 SL/TP 结构计算出的可入场价格带，受 `max_chase_ratio` 与 `min_remaining_net_rr` 双约束。
- **抑制 (suppression)**：因某 reason_code 拒绝后将 `signal_id`/`structure_key` 标记并在生命周期内拦截重复开仓。
- **生命周期键 (lifecycle key)**：`preview:<structure_key>:<phase>:<expiry_close>`，标识预览结构的有效期。
- **剩余净 RR (remaining net RR)**：`(|TP - price| - cost) / (|price - SL| + cost)`，price 为当前价，cost 含费率与滑点。
- **追价比例 (chase ratio)**：`|price - 信号入场参考价| / |信号 SL - 信号入场参考价|`。
- **频率档位 (frequency mode)**：`safe / balanced / active`，影响 daily_open_limit、prompt_candidate_limit、analysis_interval。
- **报告型 gate (report_only gate)**：在配置里被标为 `report_only=true` 的 gate，仅记录不阻断。
- **safe_mode**：当滚动 PF/DD 触发条件时，自动收紧 risk_pct、daily_open_limit、min_confidence 的状态。

## 5. 用户故事与验收标准

> 验收标准统一采用 EARS 风格：`WHEN <条件> THE <主体> SHALL <行为>`。

### Requirement 1 — 解锁入场链路使其能在加密 1h 节奏内完整闭合

**用户故事**：
作为缠论策略运维工程师，我希望主信号识别后能在 1h 周期闭合时及时进入入场流程，而不是几乎全部停在 preview_2x15m / preview_3x15m，从而让策略在合理频率下产生真实开仓。

**验收标准**：

1. WHEN 主级别 trade=1h K 线闭合且识别到 1/2/3 类买卖点，且 preview 已升级到 `preview_3x15m_pilot` 或 `preview_confirmed=true`，THE 缠论引擎 SHALL 允许跳过额外的 sub 周期 fresh trigger 等待，直接进入 `applyProgrammaticSignalGuard`。
2. WHEN `direct_structure_open` 配置为 true 并且主信号 `confidence ≥ direct_structure_min_confidence`（默认 70），THE 缠论引擎 SHALL 在不依赖 sub 周期 fresh trigger 的前提下走入 entry_zone 评估，并在决策日志中记录 `entry_path=direct_structure`。
3. WHEN 配置不变（`direct_structure_open=false`），THE 缠论引擎 SHALL 在每个候选每个 cycle 至多保留一条最高优先级的"等待 fresh trigger"诊断（同 trader+symbol+structure_key 折叠），避免日志中同一信号重复输出 ≥10 条相同提示。
4. WHEN entry trigger 在 N 个 sub 周期内未出现（默认 N=3），THE 缠论引擎 SHALL 自动把该 structure 标记为 `entry_window_missed_no_trigger` 并把 `lifecycle_key` 标为已结案，停止下一 cycle 的重复评估。
5. THE 缠论引擎 SHALL 暴露 `programmatic_strategy.entry_timing.direct_structure_open` 与 `direct_structure_min_confidence` 两个配置项，使 ops 不需重启即可通过 config reload 切换（若不支持热更新，至少 restart 即生效，并在文档显式说明）。

### Requirement 2 — 把 pilot 置信度阈值与实际信号分布对齐

**用户故事**：
作为策略维护者，我希望 pilot 的最小置信度门槛能与历史 24-72 小时内识别到的真实置信度分布相匹配，而不是把 99% 的合规信号挡在门外。

**验收标准**：

1. WHEN `programmatic_strategy.preview_signals.pilot_min_confidence` 与 `entry_timing.pilot.min_confidence` 同时存在且不一致，THE 启动校验 SHALL 拒绝启动并打印明确错误（消除"两份置信度阈值同名不同处"的歧义）。
2. THE 缠论引擎 SHALL 把当前阈值默认值由 90 调整为可由配置驱动的 P75 分位（基于最近 7 天本 trader 的同 signal_type 置信度分布）；在没有足够历史数据时回退到一个新的硬默认值 70。
3. WHEN 当前周期的候选信号置信度低于阈值，THE 决策日志 SHALL 在 `strategy_diagnostics.confidence_histogram` 中聚合输出 `{signal_type, count, p25, p50, p75, p90, threshold}`，使运维可量化阈值偏离程度。
4. WHEN 启动了 safe_mode，THE 缠论引擎 SHALL 把 pilot 阈值临时上调到 `min(default_threshold + 10, 95)`，反之退出 safe_mode 时恢复默认。
5. THE 缠论引擎 SHALL 提供分级别（buy1/sell1 vs buy2/sell2 vs buy3/sell3）独立的阈值配置覆盖入口，避免一类买点的反转信号与三类买点的趋势延续信号共享同一门槛。

### Requirement 3 — 净 RR 阈值需差异化、扣费与扣偏差化

**用户故事**：
作为风控负责人，我希望净 RR 阈值能体现不同时间周期、不同标的特性、不同信号类型的实际期望收益结构，而不是用一个固定的 2.5 把绝大多数缠论结构挡掉。

**验收标准**：

1. THE 配置 SHALL 支持按 `signal_type × timeframe` 维度配置 `min_remaining_net_rr`，例如 `{ "buy1@1h": 2.0, "buy2@1h": 1.6, "buy3@1h": 1.4, "sell*@1h": 同 }`；缺省时回退当前 `default_min_net_rr=2.5`。
2. WHEN 计算 `remaining_net_rr` 时，THE 缠论引擎 SHALL 同时输出 `gross_rr`、`fee_slippage_pct`、`structure_rr`（结构本身的 RR）三个数值到 `gate_diagnostics`，便于排查"为什么净 RR 比毛 RR 低这么多"。
3. WHEN `take_profit.mode=structure` 且结构 RR 已经 < `min_remaining_net_rr` 阈值，THE 缠论引擎 SHALL 在主信号识别阶段（而不是 entry guard 阶段）就标记 `structure_rr_below_threshold` 并 **不进入** preview 升级流程，节省 cycle 算力且避免后续的"反复抑制"。
4. WHEN 标的为 BTC/ETH/BNB（高流动性主流币），THE 缠论引擎 SHALL 适用更低的 `min_remaining_net_rr`（默认 1.6），承认这些标的的小级别背驰回报有限但胜率较高。
5. THE 缠论引擎 SHALL 在 `entry_zone` 评估之前先运行一次"理论最大可达 RR"估计（基于结构 SL/TP），若理论上不可能达到阈值则直接拒绝并标记 `theoretical_rr_unreachable`，避免每 cycle 重新算一次同样的拒绝。

### Requirement 4 — 追价比例自适应与极端时段容忍度

**用户故事**：
作为缠论策略使用者，我希望追价限制能识别"信号刚刚在 1h K 线收盘时确认 + 当前价格仍在结构合理回抽带"的情形，而不是因为单根 K 线动幅大就一刀切拒绝。

**验收标准**：

1. WHEN 计算 chase_ratio 时，THE 缠论引擎 SHALL 同时输出 `chase_ratio_atr`（按 ATR 归一化的追价距离），并把 `max_chase_ratio` 与 `max_chase_atr_multiplier` 双轨判断（任一通过即可），默认 `max_chase_atr_multiplier=0.6`。
2. THE 配置 SHALL 支持按 timeframe 与标的层级（core / trend / 其他）覆盖 `max_chase_ratio`：core 默认 0.5、trend 默认 0.4、其他默认 0.35。
3. WHEN 信号 `freshness_state=fresh` 且 `age_candles=0`（即与 decision_close_time 同根 K 线），THE 缠论引擎 SHALL 临时放宽 chase_ratio 的硬上限至 `max_chase_ratio + 0.1`，因为入场参考价仍在结构内是合理预期。
4. WHEN chase_ratio 拒绝触发，THE 缠论引擎 SHALL 在 `gate_diagnostics` 中输出 `entry_zone_low/entry_zone_high/distance_to_zone_low/distance_to_zone_high`，让运维可决定是否调整阈值。

### Requirement 5 — 抑制冷却与生命周期闭环

**用户故事**：
作为运维，我希望同一 structure_key 因同一 reason_code 被拒后能进入冷却态、停止下一 cycle 的重复评估，而不是每 3 分钟都把同一结构走一遍 preview→pilot→guard→reject。

**验收标准**：

1. WHEN 缠论引擎对某 `(trader, symbol, structure_key, action, reason_code)` 写入 `SignalSuppression`，THE 引擎 SHALL 在该 lifecycle_key 失效之前直接跳过该结构的 entry guard 评估（fast-skip），且每个被跳过的 cycle 在 `strategy_diagnostics` 中至多保留一条 `suppressed_fast_skip` 摘要。
2. WHEN 同一 structure_key 的 reason_code 切换为更严重等级（如 `target_already_crossed` → `signal_expired`），THE 引擎 SHALL 升级 suppression 并立即闭合该 lifecycle_key。
3. WHEN suppression 生命周期结束（默认 `max_lifetime_candles` 根 trade 周期 K 线），THE 引擎 SHALL 自动清理该 suppression 记录，并允许新 lifecycle_key 重新评估。
4. THE 引擎 SHALL 暴露 `state.suppressions` 的运行时统计 `{trader_id, total_active, by_reason, oldest_age_candles}`，便于通过 `/api/status` 或日志查看抑制堆积情况。
5. WHEN suppression 的命中次数（`SeenCount`）超过阈值（默认 5），THE 引擎 SHALL 把该结构标记为 `permanent_skip` 并停止再次进入 preview 流程，避免无谓的状态机抖动。

### Requirement 6 — 数学校验异常作为结构终结而非反复重试

**用户故事**：
作为缠论策略 owner，我希望"做多止损/止盈结构不合法"或"已越过止盈"这类硬错误能立即把对应结构作废，而不是每 cycle 重新走一遍只为了同样地拒绝。

**验收标准**：

1. WHEN `applyProgrammaticSignalGuard` 命中 `invalid_stop_take_profit_structure` 或 `target_already_crossed`，THE 引擎 SHALL 把对应 `structure_key` 立即标记为 `lifecycle_terminated` 并写入 `state.permanent_invalid_structures`，下一 cycle 不再生成同结构的任何决策。
2. WHEN 主信号在识别阶段产生不合法的 SL/TP 关系（如做多 SL>price 或 TP<price），THE 缠论引擎 SHALL 在 `signals.go` 中先做合法性自检，产生该信号时即标记 `signal_invalid_at_birth`，不进入 preview 升级，并把该次失败计入 `strategy_diagnostics.signal_quality_breakdown`。
3. WHEN 价格已越过 TP，THE 引擎 SHALL 输出 `crossed_by_pct`（越过百分比），便于评估是否需要新的"延伸延迟入场"策略（不在本规格范围内，仅做记录）。
4. WHEN `gate_diagnostics.invalid_stop_take_profit_structure` 在 24h 内累计超过阈值（默认 20），THE 引擎 SHALL 通过日志或 `risk_state.warnings` 暴露"信号生成器质量问题"告警，便于运维及时介入。

### Requirement 7 — pilot 仓位 sizing 与账户本金匹配

**用户故事**：
作为小本金账户使用者，我希望 pilot 仓位计算结果不会超过实际可用余额，并且能给出"账户本金不足以支持 pilot 模式"的明确诊断。

**验收标准**：

1. WHEN 计算 `pilot_position_size_usd`，THE 缠论引擎 SHALL 先计算 `max_allowed_notional = available_balance × leverage × max_pilot_notional_pct`（默认 0.6），再把原始 sizing 结果裁剪到该上限。
2. WHEN `max_allowed_notional` 小于交易所最小名义额，THE 缠论引擎 SHALL 拒绝该 pilot 并输出 `pilot_size_below_min_notional`，不得为了满足最小名义额而反向放大超过账户上限；同时在每日开仓窗口给出"今日不可用 pilot 模式"汇总。
3. WHEN 当 `available_balance × leverage` 小于 `min_pilot_notional_usd × 2`（默认 30 USDT），THE 缠论引擎 SHALL 自动把 trader 切到 `decision_mode=hold_only`，仅做持仓管理，不再进入开仓评估，并在启动期/每小时心跳输出"账户余额过低，已禁用开仓"。
4. THE 缠论引擎 SHALL 在 `risk_state` 中输出 `pilot_position_size_usd`、`required_min_notional`、`available_balance`、`account_too_small=true|false` 字段。
5. WHEN 用户手动配置 `programmatic_strategy.pilot.risk_fraction` 大于 0.4，THE 启动校验 SHALL 给出告警（不阻断），提示对小本金账户而言风险过高。

### Requirement 8 — 候选标的治理与跨交易所一致性

**用户故事**：
作为缠论策略 owner，我希望候选池只包含与策略假设匹配的加密永续标的，并且行情源与执行交易所差异在可控范围内。

**验收标准**：

1. WHEN 动态候选池产出非加密标的（如 XAU/XAG/CL/COPPER 等商品/原油），THE 候选池 SHALL 自动剔除，除非这些标的被显式列入 `programmatic_strategy.allow_non_crypto_symbols`；该字段缺省为空数组。
2. WHEN 行情源与执行交易所不同，THE 候选池 SHALL 在最近 5 分钟比较两端 mid_price 偏差；偏差超过 `max_quote_spread_bps`（默认 20 bps）时把该 symbol 临时下架并标记 `quote_spread_too_high`。
3. THE 候选池 SHALL 在 `candidate_details.warnings` 中保留现有"行情源/执行交易所"提示，但当 1.2 条触发时升级为 `errors`，并阻止该 symbol 进入 prompt 候选。
4. WHEN 候选池配置 `core_symbols=["BTCUSDT","ETHUSDT"]`，THE 候选池 SHALL 保证这两个标的在任何 cycle 至少出现在 prompt 中一次（即使评分为 0），便于人工对比基准。
5. THE 候选池 SHALL 暴露 24h 候选 churn rate（新进/淘汰频次）到 `risk_state` 或 `/api/status`，便于排查不稳定的标的注入。
6. THE 实施 SHALL 由拥有交易所实例的 `AutoTrader`/`manager` 层提供执行交易所 mid_price 或价差诊断，不允许 `strategy/chanlun` 直接依赖 Binance/Aster/Hyperliquid 具体实现。

### Requirement 9 — 频率 gate `report_only` 与 safe_mode 的实际生效路径

**用户故事**：
作为风控 owner，我希望频率档位、滚动 PF/DD gate 不再永远是"只看不管"，并且在长时间 0 开仓时能自动放宽而不是僵在严格档。

**验收标准**：

1. WHEN `trading_frequency.report_only.high_adx/rr_threshold/rolling_gate=true`，THE `risk_state` SHALL 输出 `gate_effectiveness=report_only` 与 `若 enforce 是否会拒绝` 的反事实计数，便于评估开启 enforce 的影响。
2. WHEN trader 在最近 `inactivity_window_minutes`（默认 720，即 12 小时）内 `open_count=0` 且 `account_too_small=false` 且 `loss_mode.active=false`，THE 缠论引擎 SHALL 自动进入 `loosen_mode`：把 `pilot_min_confidence` 临时下降 10、`min_remaining_net_rr` 临时下降 0.4、`max_chase_ratio` 临时上升 0.05；该状态在出现 1 次成功开仓或 24h 后自动复位。
3. WHEN 进入 `loosen_mode`，THE 决策日志 SHALL 写入显式标记，避免被误读为正常档位决策。
4. WHEN safe_mode 当前未触发但 24h `open_rejected ≥ 10` 且 `open_count = 0`，THE 缠论引擎 SHALL 写入 `risk_state.warnings.runaway_rejection_loop=true`，并在 `/api/status` 暴露，提示运维。
5. THE 缠论引擎 SHALL 在 trader 启动 30 分钟内输出一次 `frequency_self_check`：列出所有阈值与最近 24h 统计的"距离触发的 gap"，用于上线后快速发现配置-数据脱节。
6. THE 配置归一化 SHALL 把 `trading_frequency.loosen_mode` 从 `config.TradingFrequencyConfig` 传递到 `decision.FrequencyPolicy`、`decision.Context` 和日志层 `RiskStateSnapshot`，避免引擎内状态与 AutoTrader 构建的 risk_state 脱节。

### Requirement 10 — 决策日志可观测性扩展

**用户故事**：
作为运维，我希望从单条决策日志即可读出"为什么这个 cycle 没下单"的完整链路，并能从聚合维度看到 24h 健康度。

**验收标准**：

1. THE 决策日志 SHALL 在 `strategy_diagnostics.per_candidate` 输出每个候选标的的 `{symbol, signal_type, confidence, structure_key, lifecycle_phase, gate_path[], terminal_reason_code}`，每个 cycle 至多 N 条（N=候选数）。
2. THE 决策日志 SHALL 把 `account_state` 扩展为 `{total_balance, available_balance, total_unrealized_profit, total_realized_24h, position_count, margin_used_pct, account_too_small}`，与 Requirement 7.4 字段一致。
3. THE 决策日志 SHALL 在 `risk_state` 中加 `inactivity_minutes`、`last_open_at`、`last_close_at`、`open_count_24h`、`open_rejected_24h`、`signal_count_24h`，并按 trader 分维度。
4. THE 引擎 SHALL 提供一个独立的轻量摘要文件 `decision_logs/<trader>/daily_summary_YYYYMMDD.json`（每日轮转），聚合上述维度，使运维不必读 480 条日志才能定位问题。
5. WHEN cycle 决策为 `wait` 但识别到 ≥1 个非"folded preview"信号，THE 决策日志 SHALL 在顶层增加 `wait_reason_summary` 字段（`pilot_below_threshold | structure_rr_too_low | chase_too_high | invalid_structure | suppressed | no_trigger`），便于直接 `jq '.wait_reason_summary'` 做统计。
6. THE `wait_reason_summary` SHALL 从 `Engine.GetFullDecision()` 写入 `decision.FullDecision`，并由 `trader.AutoTrader` 拷贝到 `logger.DecisionRecord` 顶层字段，不能只停留在 `strategy_diagnostics` 内部。

### Requirement 11 — 持仓管理与 lifecycle 端到端冒烟

**用户故事**：
作为策略 owner，我希望在修复以上缺陷后能快速验证"策略真的会开仓 → 真的会平仓 → 真的会触发持仓管理规则"，而不是停留在配置层面。

**验收标准**：

1. THE 实施 SHALL 包含一组离线 replay 数据集（≥3 天行情、≥50 条候选信号），通过 `cmd/replay` 验证：在 loosen_mode 不生效的默认配置下也至少能产生 ≥1 次 open + ≥1 次 close。
2. THE 实施 SHALL 在 `pipeline_test.go` 或 `strategy/chanlun/*_test.go` 增加端到端用例：模拟 1h 信号 + 15m 触发 + 持仓 + 浮盈触发 breakeven + 后续触发 partial_close，断言决策日志字段齐备。
3. WHEN 集成测试中模拟"账户余额不足以开 pilot"，THE 引擎 SHALL 进入 `hold_only` 并不再产生开仓决策。
4. WHEN 集成测试中模拟"24h 0 开仓 + 0 持仓"，THE 引擎 SHALL 进入 `loosen_mode` 并能在第 13 小时产生 ≥1 次开仓评估。
5. THE 实施 SHALL 在交付前在远程灰度环境（非 aster_deepseek 本帐号）跑 ≥48 小时回归，输出 `daily_summary` 对比修复前后。

## 6. 非功能性需求

### 6.1 兼容性
- 现有 `decision_logs/*.json` schema 保持向后兼容：本规格只新增字段不删除。
- 现有 `programmatic_strategy.*` 配置缺省时行为不变（除明确标记为新默认值的字段）。
- 对于默认值从旧值改为新推荐值、但仍需要支持显式 false/0/未配置区分的字段（如 `defect_fix_pack_enabled`、`direct_structure_open`、pilot confidence），配置层 SHALL 使用 pointer 或等价 presence tracking；profile/policy 层再落成普通运行时值。

### 6.2 性能
- 抑制 fast-skip 路径应使每 cycle 对已抑制结构的处理时间从 O(N candidates × M phases) 降到 O(N) 内。
- `daily_summary` 文件每日生成 1 次，单次生成 < 30 秒，文件大小 < 5MB。

### 6.3 安全
- 配置文件不得暴露真实密钥；本规格不修改密钥处理。
- `loosen_mode` 不得绕过 `max_daily_loss`、`max_drawdown`、`stop_trading_minutes` 等账户级硬熔断。

### 6.4 可观测性
- 所有新增字段在 `web/src/types/index.ts` 与 `web/src/lib/api.ts` 中同步类型，避免前后端契约脱节。

### 6.5 可回滚
- 所有新增行为受配置开关控制（默认开启），运维可通过单一开关 `programmatic_strategy.defect_fix_pack_enabled=false` 一键回退到当前行为，便于 A/B。
- WHEN `defect_fix_pack_enabled=false`，THE 系统 SHALL 恢复旧版默认值与旧版 pipeline：`pilot_min_confidence=90`、`entry_timing.pilot.min_confidence=90`、`min_remaining_net_rr=2.5`、`direct_structure_open=false`，并跳过 P75、loosen、candidate governor、account size gate、fast-skip 新逻辑。

## 7. 依赖与不确定性

### 7.1 依赖

- 真实 24h 历史置信度分布数据（用于 Requirement 2.2 的 P75 默认值）。
- Aster DEX 的最小下单名义额（用于 Requirement 7.2 的 `min_notional_usd`）。
- 行情源 vs 执行交易所价差监控（用于 Requirement 8.2）。

### 7.2 不确定性

- 当前 `aster_deepseek` 的 `initial_balance=44.18 USDT` 是否为预期生产配置？若期望本金 ≥ 200 USDT，则 Requirement 7 的优先级降低。
- `XAUUSDT/XAGUSDT/CLUSDT` 是 Aster 实际可交易的标的，还是动态候选池侧错误注入？需运维确认是否要保留 `allow_non_crypto_symbols` 通道。
- `direct_structure_open=false` 是出于"规避主信号闪烁"的有意决策，还是默认未调整？需要业务侧确认是否允许默认改为 true。

## 8. 风险与权衡

| 风险 | 影响 | 缓解 |
|---|---|---|
| `loosen_mode` 自动放宽阈值后产生劣质开仓 | 真实亏损 | 与 `safe_mode` 互斥；放宽幅度受 hard cap；进入后开仓数 ≥1 即立刻退出 |
| `direct_structure_open=true` 引入 1h 主信号噪音 | 假突破亏损 | 设最小置信度门槛 70；继续保留 `signal_freshness` guard |
| 候选标的剔除后样本变少 | 错过部分机会 | 暴露 `allow_non_crypto_symbols` 白名单显式接管 |
| 持仓管理路径在生产中从未验证 | 一旦开仓后规则可能异常 | Requirement 11 强制 replay + 集成测试覆盖 |
| 抑制 fast-skip 可能掩盖真实信号变化 | 错过同结构第二次机会 | 当 `signal_id` 变更或 lifecycle_key 重生成时强制重新评估 |

## 9. 度量与验收

发布后两周内通过 `daily_summary` + 决策日志 quantitative 度量以下指标：

| 指标 | 现状（24h） | 目标（修复后 7 日均值） |
|---|---|---|
| 真实成功开仓数 / 日 | 0 | ≥ 1 |
| `open_rejected` / `open_attempt` 比 | 6/6 = 100% | ≤ 70% |
| 同 structure_key 重复 reject 占比 | ≥ 50%（6/12 净 RR 拒绝中至少一半） | ≤ 10% |
| `pilot_skip_below_threshold` / `signal_count` | 954/197 ≈ 484% (一个信号被多次评估) | ≤ 50% |
| `wait_reason_summary` 单一原因占比 ≥ 80% 的天数 | 100% | ≤ 30% |
| `loosen_mode` 在 12h 静默后激活 | N/A | 按 spec 自动激活 |
| `account_too_small=true` 时未开仓 | N/A | 100% |

---

> 本规格由远程实盘 24 小时日志取证驱动。当前 `requirements.md`、`design.md`、`tasks.md` 已对齐到实现前检查意见；后续可从 `tasks.md` 的 P0 阶段开始执行。
