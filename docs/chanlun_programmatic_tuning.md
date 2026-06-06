# 程序化缠论策略配置调优

`programmatic_strategy.defect_fix_pack_enabled` 是缺陷修复包总开关。默认开启；需要回滚时设为 `false`，会恢复旧版关键默认值：pilot confidence 90、entry pilot confidence 90、min remaining net RR 2.5、direct structure 关闭。

## 入场与置信度

- `preview_signals.pilot_min_confidence` 默认 70，并可用 `pilot_min_confidence_by_signal_type` 做买卖点级覆盖。
- `pilot_min_confidence_use_p75=true` 时，策略用近 7 天同类信号置信度 P75 作为动态阈值，并限制在 `pilot_min_confidence_p75_floor` 与 `pilot_min_confidence_p75_ceiling` 之间。
- `entry_timing.direct_structure_open=true` 允许高置信 1h 结构直接进入确定性风控，不再强制等待 15m fresh trigger。
- `entry_timing.max_no_trigger_sub_candles` 控制 preview 路径等待 fresh trigger 的最长 sub 周期数，超时后生命周期终结，避免同一结构反复 wait。

## RR 与追价

- `entry_zone.min_remaining_net_rr` 默认 2.0；`signal_type_min_rr` 可按 `buy2@1h`、`sell*@1h`、`*@15m` 等粒度覆盖。
- `theoretical_rr_unreachable_skip=true` 会在入场前预过滤理论 RR 不可达的结构，并终结生命周期。
- 追价使用双轨判断：`max_chase_ratio` 或 `max_chase_atr_multiplier` 任一通过即可继续。
- `fresh_age_chase_relax` 只对刚产生的结构放宽追价比例。

## 候选治理与账户尺寸

- `candidate_governor` 默认剔除非加密 USDT/USDC 标的，并可通过 `allow_non_crypto_symbols` 放行。
- `max_quote_spread_bps` 使用行情源价格与执行交易所价格做价差保护，超过阈值的候选会被标记为 `quote_spread_too_high`。
- `max_pilot_notional_pct` 与 `min_pilot_notional_usd` 限制 pilot 仓位，不会为了满足最小名义额反向放大超过账户上限。

## Loosen Mode

`trading_frequency.loosen_mode` 用于长期无开仓时的受控放宽。它与 safe/loss/account-too-small 互斥：

- 进入：无成功开仓超过 `inactivity_window_minutes`。
- 退出：成功开仓、到期、safe/loss 激活或账户过小。
- 效果：降低 pilot confidence、降低最小净 RR、放宽追价比例，且受 hard floor 限制。

## 观测字段

决策日志新增顶层 `wait_reason_summary`，并在 `strategy_diagnostics` 中输出 `per_candidate`、`confidence_histogram`、`signal_quality_breakdown` 与扩展 `risk_state`。每日汇总写入 `decision_logs/<trader>/daily_summary_YYYYMMDD.json`。
