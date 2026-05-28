# 缠论V2做空信号缺失修复 Tasks

## 关联

- `requirements.md`

---

## Task 1 — Rust signal.rs 实现 Sell2/Sell3

- [ ] `chanlun_v2/src/signal.rs`：引入 `Direction`，或使用 `crate::kline::Direction::Down` 全限定名。
- [ ] `chanlun_v2/src/signal.rs`：在 `detect_signals` 中枢部分新增 Sell2 逻辑（`last_seg.direction == Direction::Down`，反弹不破 ZG）。
- [ ] `chanlun_v2/src/signal.rs`：Sell2 `take_profit` 使用 `(center.low - (center.high - center.low)).max(current_price * 0.9)`，避免目标过远。
- [ ] `chanlun_v2/src/signal.rs`：新增 Sell3 逻辑（`last_seg.direction == Direction::Down && last_seg.high < center.zd && current_price < center.zd`）。
- [ ] `chanlun_v2/src/signal.rs`：Sell1 条件放宽（UpTrend + Consolidation），Consolidation 置信度减 15 并 clamp 到 `[0,100]`。
- [ ] `chanlun_v2/src/signal.rs`：新增 Rust 单测覆盖 Sell2、Sell3、Sell1 consolidation、Buy2/Sell2 不同时触发、Sell3 回到 ZD 上方不触发。
- [ ] `cargo test` 通过。

## Task 2 — 验证 Go 层兼容

- [ ] 确认 `signalToDecision` 对 `sell2/sell3` 正确路由为 `open_short`。
- [ ] 确认 `entry_timing.go` 对 short 方向的 `invalidStopTakeProfit` 检查正确（SL > price > TP）。
- [ ] 确认 `SignalTypeMinRR` 配置包含 sell1/sell2/sell3。
- [ ] `go test ./config ./strategy/chanlunv2` 通过。

## Task 3 — 构建部署验证

- [ ] 同步到 217 服务器实际目录 `/home/ubuntu/appai2/nofx`。
- [ ] 部署前备份并保留 217 本地 Aster 账户配置，不从 161 复制凭证。
- [ ] 部署后确认 `capital_allocation.enabled=true`、`capital_allocation.allocated_balance=100` 保持不变。
- [ ] `docker compose build && docker compose up -d`。
- [ ] 等待 1h K 线闭合后检查日志：确认 sell 信号出现。
- [ ] 若没有实际 `open_short`，检查是否存在 sell 信号对应的 RR/open gate/entry zone 结构化拒绝原因。

---

## 验收

| 指标 | 当前 | 目标 |
|---|---|---|
| sell/short 信号 | 0 | ≥5/24h |
| open_short 决策 | 0 | ≥1 |
