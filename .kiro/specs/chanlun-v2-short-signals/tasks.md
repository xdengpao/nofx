# 缠论V2做空信号缺失修复 Tasks

## 关联

- `requirements.md`

---

## Task 1 — Rust signal.rs 实现 Sell2/Sell3

- [ ] `chanlun_v2/src/signal.rs`：在 `detect_signals` 中枢部分新增 Sell2 逻辑（反弹不破 ZG）。
- [ ] `chanlun_v2/src/signal.rs`：新增 Sell3 逻辑（跌破 ZD 不回）。
- [ ] `chanlun_v2/src/signal.rs`：Sell1 条件放宽（UpTrend + Consolidation）。
- [ ] `cargo test` 通过。

## Task 2 — 验证 Go 层兼容

- [ ] 确认 `signalToDecision` 对 `sell2/sell3` 正确路由为 `open_short`。
- [ ] 确认 `entry_timing.go` 对 short 方向的 `invalidStopTakeProfit` 检查正确（SL > price > TP）。
- [ ] 确认 `SignalTypeMinRR` 配置包含 sell1/sell2/sell3。

## Task 3 — 构建部署验证

- [ ] 同步到 217 服务器。
- [ ] `docker compose build && docker compose up -d`。
- [ ] 等待 1h K 线闭合后检查日志：确认 sell 信号出现。

---

## 验收

| 指标 | 当前 | 目标 |
|---|---|---|
| sell/short 信号 | 0 | ≥5/24h |
| open_short 决策 | 0 | ≥1 |
