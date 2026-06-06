# Chanlun V1 Live Tuning Requirements

## Background

本规格用于执行 161 服务器上的缠论 V1 实盘配置调优。

| 字段 | 值 |
|---|---|
| 规格名称 | `chanlun-v1-live-tuning` |
| 目标主机 | `43.167.168.161:/home/ubuntu/appai3/nofx` |
| 当前分支 | `jzhbnofxdev` |
| 当前 commit | `e68a07f` |
| 目标 trader | `aster_deepseek` |
| 决策模式 | `programmatic` |
| 策略 | `chanlun_programmatic v1` |
| 执行交易所 | Aster |
| 最近诊断时间 | 2026-05-23 22:46 +08:00 |

本次不切换到 `chanlun_v2`，不新增算法定义，不扩大交易账户权限；只调整 V1 的线上配置并保留 1h trade timeframe。

## Current Evidence

线上日志显示 V1 并非从未开仓：`aster_deepseek` 在 2026-05-16 至 2026-05-17 期间有 4 次真实开空，最后一次成功开仓为 2026-05-17 13:12:57 +08:00 的 `ETHUSDT open_short`。此后截至 2026-05-23 22:46 +08:00 没有新的成功开仓。

最近 24 小时事实：

| 项 | 数值 |
|---|---:|
| 决策周期 | 484 |
| 成功开仓 | 0 |
| open rejected | 0 |
| wait | 484 |
| `no_trigger` | 460 |
| `no_signal` | 24 |
| 当前持仓 | 0 |
| 当前余额 | 约 44.4215 USDT |

主要诊断：

- 候选池中每轮仍出现 `XAUUSDT`、`CLUSDT` 等非加密标的，并被 `candidate_governor` 以 `non_crypto_symbol` 剔除。
- `BCHUSDT` 多次因 `quote_spread_too_high` 被剔除，说明 Aster 执行价与 Binance 行情价差保护在工作。
- 最近 24 小时主要停在 `主信号层N个无持仓候选等待1h新闭合K线` 与预览层无买卖点。
- 旧抑制状态仍显示历史瓶颈集中在 `entry_chase_ratio_too_high`、`remaining_net_rr_too_low`、`structure_background_only`、`invalid_stop_take_profit_structure`、`target_already_crossed`。

## User Requested Scope

用户确认按三步执行：

1. 按建议执行候选池治理。
2. 按建议建立 V1 调优方案，但保留 `trade=1h`，只降低结构识别门槛。
3. 按建议执行入场、追价与 RR 温和放宽。

用户要求使用 Spec 模式，因此先产出 `requirements.md`、`design.md`、`tasks.md`，确认后再执行远程配置变更。

## Requirements

### R1 Candidate Pool Governance

作为策略运维者，我希望 V1 的有效候选名额集中在 Aster 可执行的加密永续合约上，避免非加密标的和高价差标的长期占用候选池。

1. WHEN 更新 `aster_deepseek` 配置，THE system SHALL 保留 `programmatic_strategy.candidate_governor.enabled=true`。
2. WHEN 更新候选范围，THE system SHALL 使用 crypto-only 候选列表，禁止 `XAUUSDT`、`XAGUSDT`、`CLUSDT` 等非加密标的进入 V1 主评估候选。
3. WHEN 配置 `programmatic_strategy.symbol_pool`，THE system SHALL 使用 `mode=override` 或等价方式明确限定 V1 评估标的，避免动态候选池重新注入非加密标的。
4. WHEN 配置核心候选，THE system SHALL 至少保留 `BTCUSDT`、`ETHUSDT`，并优先覆盖 `SOLUSDT`、`BNBUSDT`、`XRPUSDT`、`DOGEUSDT`、`LTCUSDT`、`ADAUSDT`、`HYPEUSDT`、`ASTERUSDT`。
5. WHEN `BCHUSDT` 在最近日志中持续触发 `quote_spread_too_high > 20bps`，THE system SHOULD 暂不把 `BCHUSDT` 放入 V1 override 候选，除非后续价差稳定回落。
6. WHEN 变更完成后运行至少 3 个周期，THE validation SHALL show `candidate_coins` 中不再出现 `XAUUSDT`、`XAGUSDT`、`CLUSDT`。

### R2 Keep 1h Timeframe And Lower Structure Thresholds

作为策略调参者，我希望保留 1h 主交易周期，同时降低结构识别门槛，让 V1 更容易在当前波动环境中形成可评估信号。

1. WHEN 更新 V1 配置，THE system SHALL 保留 `programmatic_strategy.timeframes.trade="1h"`。
2. WHEN 更新 V1 配置，THE system SHALL 保留 `programmatic_strategy.timeframes.sub="15m"` 与 `micro="3m"`，避免本次调优改变多周期语义。
3. WHEN 调低结构识别门槛，THE system SHALL 将 `structure.min_swing_pct` 从当前约 `0.3` 调整到 `0.2`。
4. WHEN 调低结构识别门槛，THE system SHALL 将 `structure.atr_multiplier` 从当前约 `0.5` 调整到 `0.35`。
5. WHEN 调低结构识别门槛，THE system SHALL 将 `structure.min_stroke_bars` 从当前约 `5` 调整到 `4`。
6. WHEN 调整结构参数，THE system SHALL 保留 `left_bars=2` 与 `right_bars=2`，避免一次性放宽过多导致噪声过大。
7. WHEN 变更完成后运行至少 3 个周期，THE validation SHALL 检查 `strategy_diagnostics.messages` 中 `no_signal` 比例是否下降，或至少不再仅由候选治理导致空转。

### R3 Entry, Chase, And RR Relaxation

作为风控负责人，我希望在不取消确定性风控的前提下，温和放宽 V1 的追价和 RR 约束，使高质量 1h 信号能进入 open gate。

1. WHEN 更新入场配置，THE system SHALL 保留 `entry_timing.direct_structure_open=true`。
2. WHEN 更新入场配置，THE system SHALL 保留 `direct_structure_min_confidence=70`。
3. WHEN 更新追价配置，THE system SHALL 将 `entry_timing.entry_zone.max_chase_atr_multiplier` 调整到 `0.8`。
4. WHEN 更新 loosen mode，THE system SHALL 将 `trading_frequency.loosen_mode.max_chase_ratio_bump` 调整到 `0.10`。
5. WHEN 更新 RR 配置，THE system SHALL 将 `entry_timing.entry_zone.signal_type_min_rr.buy2` 与 `sell2` 调整到 `1.4`。
6. WHEN 更新 RR 配置，THE system SHALL 将 `entry_timing.entry_zone.signal_type_min_rr.buy3` 与 `sell3` 调整到 `1.2`。
7. WHEN 更新 RR 配置，THE system SHALL 将 `programmatic_strategy.take_profit.min_net_rr` 调整到 `2.0`。
8. WHEN 更新 RR 配置，THE system SHALL 将 `programmatic_strategy.signal_freshness.min_remaining_net_rr` 调整到 `2.0`，避免 freshness guard 与 entry zone 使用互相矛盾的 RR 口径。
9. WHEN 更新 RR 配置，THE system SHOULD 保留 `buy1/sell1` 相对更严格，默认不低于 `1.8`，避免反转类信号过度放宽。
10. WHEN 变更完成后运行至少 3 个周期，THE validation SHALL 检查最新日志中的 `confidence_histogram.threshold`、`entry_zone` 阈值与配置一致。

### R4 Operational Safety And Rollback

作为实盘运维者，我希望每次配置变更都有备份、验证和可回滚路径，避免调参过程中丢失生产配置。

1. BEFORE modifying remote `config.json`, THE operator SHALL create a timestamped backup under `/home/ubuntu/appai3/nofx/config.json.bak.chanlun-v1-live-tuning-YYYYMMDD_HHMMSS`.
2. BEFORE restart, THE operator SHALL validate that the edited `config.json` is strict JSON and can be parsed by Python `json` or equivalent tooling.
3. WHEN restarting service, THE operator SHALL identify the active runtime method first. Current evidence shows the backend runs as Docker/container process `./nofx`, not PM2.
4. WHEN restarting service, THE operator SHALL use the least disruptive restart method available for the existing deployment, and SHALL not start a duplicate backend on port 8080.
5. AFTER restart, THE operator SHALL verify `/health` or local API health endpoint succeeds.
6. AFTER restart, THE operator SHALL verify new `decision_logs/aster_deepseek/decision_*.json` files are being written.
7. IF the service fails to start, health check fails, or logs show config parse errors, THE operator SHALL restore the timestamped backup and restart again.

### R5 Post-Change Observation

作为策略负责人，我希望调优完成后能用短窗口确认方向正确，而不是只看服务是否启动。

1. AFTER config deployment, THE system SHALL observe at least 3 new `aster_deepseek` cycles.
2. DURING observation, THE system SHALL summarize `candidate_coins`、`wait_reason_summary`、`strategy_diagnostics.messages`、`risk_state.active_mode`、`risk_state.suppressions`。
3. IF `candidate_coins` still includes non-crypto symbols, THE deployment SHALL be considered incomplete.
4. IF every observed cycle remains `no_trigger` but messages now show valid crypto-only candidates and lower structure thresholds are active, THE deployment SHALL be considered technically applied but strategically still waiting for a 1h signal.
5. IF `open_rejected` appears, THE system SHALL capture the exact rejection reason and decide whether it is acceptable wind-control rejection or a new configuration defect.
6. IF a successful open occurs during the observation window, THE system SHALL verify protection order status fields and report any naked-position risk.

## Out Of Scope

- Changing exchange adapters or Aster signing logic.
- Switching `aster_deepseek` to `chanlun_v2`.
- Adding a new funded account or new API credentials.
- Changing leverage.
- Disabling deterministic risk validation.
- Running tests that place real orders.

## Acceptance Summary

This spec is complete when:

- Remote V1 config has timestamped backup.
- V1 candidates are crypto-only.
- V1 keeps `trade=1h`.
- Structure thresholds are lowered to `min_swing_pct=0.2`、`atr_multiplier=0.35`、`min_stroke_bars=4`.
- Entry/RR relaxations are applied as listed above.
- Service is restarted without duplicate process.
- Health check passes.
- At least 3 new V1 cycles are observed and summarized.
