# 217 Chanlun V2 No-Open Diagnosis Tasks

## P0 - 取证和规格

- [x] 连接 217 服务器并确认仓库路径 `/home/ubuntu/appai2/nofx`。
- [x] 确认远端 HEAD：`b546e4c1d`。
- [x] 读取 `aster_chanlun_v2` 最近 48 小时决策日志。
- [x] 汇总 action/open rejection/terminal/suppressed/trigger ready 分类。
- [x] 交叉检查远端 `config.json` 中 V2 entry timing 配置。
- [x] 交叉检查 `strategy/chanlunv2/entry_timing.go`、`decision/open_gate.go`、`config/config.go`。
- [x] 形成 requirements/design/tasks。
- [x] 交叉校验 requirements/design/tasks 与现有代码，并记录 `consistency-review.md`。

## P1 - 只读诊断工具

- [x] 扩展现有 `cmd/replay -open-rejection-daily` / `logger.BuildOpenRejectionDailyReport` 为 Chanlun V2 no-open report：
  - action/final_action/trade_intent 分布；
  - direct terminal vs suppressed terminal；
  - RR direct terminal 的 symbol/signal_type/threshold/RR 分布；
  - trigger ready 与 open gate rejection；
  - BTC gate diagnostics 展开；
  - 样本文件名和时间。
- [x] 增加 logger/replay 单元测试，覆盖 `entry_rr_invalid`、`entry_parent.watch_window_expired`、BTC hard veto 和 confidence override diagnostics。

## P2 - Chanlun V2 loosen mode

- [x] 在 `strategy/chanlunv2` 中实现 V2 runtime mode controller，复用 `decision.FrequencyPolicy.LoosenMode` 语义，并以 `ctx.FrequencyState` / `ctx.RuntimeMinutes` 判定 inactivity，避免直接耦合 V1 `StateStore`。
- [x] 对 `minRemainingNetRRForV2Signal()` 增加 effective loosen delta，floor=1.0。
- [x] 对 V2 `EntryZone.MaxChaseRatio` 增加 loosen bump，cap=1.0。
- [x] 对 `MinTriggerConfidence` 增加 loosen confidence drop，floor=`HardFloorPilotConfidence`。
- [x] 将同一 effective RR resolver 接入 `applyChanlunV2FreshnessGuard()` 的剩余 RR 二次检查，避免 entry zone 放宽后又被更高的 freshness/global RR 阈值重拒。
- [x] 成功开仓后退出 loosen；safe/loss 模式优先级高于 loosen。
- [x] 在 `risk_state` 或 strategy diagnostics 中输出 V2 `active_mode=loosen` 与生效阈值。
- [x] 增加 `strategy/chanlunv2` 单元测试：
  - 12h+ inactivity 后进入 loosen；
  - `sell2` RR 1.12 在 `min_net_rr_delta=-0.4` 后不再直接终态；
  - BTC confirmed bearish hard veto 不被 loosen 绕过；
  - 成功 open 后退出 loosen。

## P3 - 217 灰度配置

- [x] 备份 217 `config.json`。
- [x] 灰度设置：
  - `signal_type_min_rr.sell2=1.1`
  - `min_confidence_overrides.short_base=65`
  - `min_confidence_overrides.range_short=65`
- [x] 保持 BTC hard veto 默认 `block`，不改成 report-only。
- [x] 保持 `capital_allocation.enabled=true`、`allocated_balance=100`。
- [x] 重新构建并启动 217 容器。
- [ ] 观察至少一个 1h K 线闭合，检查：
  - `sell2` 近失手信号是否进入 waiting/trigger；
  - `open_rejected` 是否从 BTC hard veto 转向更具体的后续 gate 或真实 open；
  - 是否出现成功开仓；
  - 若出现连续亏损或异常频率，回滚配置。
  - 2026-05-29 22:30 Asia/Singapore 首个重启后周期确认配置已生效：`sell2` RR 阈值显示为 `1.10`，本轮仍因剩余净 RR 低于阈值等待，未出现启动错误或开仓。
  - 2026-05-29 22:33 Asia/Singapore 第二个重启后周期无 `open_rejections`，5 个终态信号仍为 `entry_rr_invalid`，无启动错误或异常频率迹象。

## P4 - 可选 BTC gate 规格

- [ ] 如需允许 BTC bearish 下的山寨多单，另立 spec 设计 `btc_multi_timeframe_gate.mode = block|penalize|report_only`。
- [ ] 默认必须保持 `block`。
- [ ] 217 实盘切换到 `penalize/report_only` 前必须有单独确认和回滚计划。
