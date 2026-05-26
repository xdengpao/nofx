# 缠论V2策略217服务器优化 Tasks

## 关联

- `requirements.md`
- `design.md`

---

## P0 — 解锁入场（立即执行）

### Task 1 — RR 阈值差异化（W1）

- [ ] `config/config.go` `ChanlunV2EntryZoneConfig`：新增 `SignalTypeMinRR map[string]float64` 字段。
- [ ] `config/config.go` `NormalizeChanlunV2EntryZone`：默认 `MinRemainingNetRR` 从 2.5 改为 2.0；填充默认 `SignalTypeMinRR`（buy1:2.0, buy2:1.5, buy3:1.2）。
- [ ] `strategy/chanlunv2/entry_timing.go` `evaluateParentStructureEntry`：RR 检查使用 `SignalTypeMinRR[sig.SignalType]` 替代全局阈值。
- [ ] 验证：BNBUSDT buy2 和 HYPEUSDT buy3 不再因 RR 立即终态。

### Task 2 — 观察窗口延长（W2）

- [ ] `config/config.go` `NormalizeChanlunV2EntryTiming`：`WatchMaxCandles` 默认从 8 改为 16。
- [ ] 验证：SOLUSDT buy2 不再因 2h 窗口过期终态。

### Task 3 — 置信度覆盖（W3）

- [ ] `strategy/chanlunv2/engine.go` `signalToDecision`：当 `sig.Confidence >= 60` 时，`d.Confidence = max(sig.Confidence, 85)`。
- [ ] 验证：DOGEUSDT entry trigger 不再因置信度 < 82 被 open gate 拒绝。

### Task 4 — 部署验证

- [ ] 同步代码到 217 服务器。
- [ ] `docker compose build && docker compose up -d`。
- [ ] 等待 1h K 线闭合后检查日志：entry_rr_invalid 终态消失、trigger ready 通过 open gate。

---

## P1 — 去重优化

### Task 5 — entry trigger 被拒去重（W4）

- [ ] `strategy/chanlunv2/state.go`：新增 `MarkTriggerRejected / IsTriggerRejected / TriggerRejectedCount`。
- [ ] `strategy/chanlunv2/engine.go`：open gate 拒绝后调用 `MarkTriggerRejected`；下一 cycle 检查跳过。
- [ ] 被拒 ≥3 次 → 父结构标记 `gate_blocked` 终态。
- [ ] 验证：同一 trigger 不再重复产出被拒。

---

## 验收度量

| 指标 | 当前 | P0 后目标 |
|---|---|---|
| 24h 成功开仓 | 0 | ≥1 |
| entry_rr_invalid 终态 | 73% | ≤20% |
| watch_window_expired | 26% | ≤10% |
| open gate 置信度拒绝 | 10 | 0 |
| 同一 trigger 重复被拒 | 4 | ≤1 |
