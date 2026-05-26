# 缠论V2策略217服务器优化 Tasks

## 关联

- `requirements.md`
- `design.md`

---

## P0 — 解锁入场（立即执行）

### Task 1 — RR 阈值差异化（W1）

- [ ] `config/config.go` `ChanlunV2EntryZoneConfig`：新增 `SignalTypeMinRR map[string]float64` 字段。
- [ ] `config/config.go` `NormalizeChanlunV2EntryZone`：
  - 默认 `MinRemainingNetRR` 从 2.5 改为 2.0（原下限 `< 1 → 1` 的 clamp 保留）。
  - 缺省时填充默认 `SignalTypeMinRR`：`{buy1:2.0, sell1:2.0, buy2:1.5, sell2:1.5, buy3:1.2, sell3:1.2}`。
  - **新增 clamp**：遍历 `SignalTypeMinRR`，把所有 `< 1` 的值上调到 `1`，与全局 `MinRemainingNetRR` 下限规则一致。
- [ ] `config/config_test.go` 新增 `TestNormalizeChanlunV2EntryZoneSignalTypeMinRRClamp`：传入 `{buy2: 0.5}`，断言归一化后值为 `1`。
- [ ] `strategy/chanlunv2/entry_timing.go` `evaluateParentStructureEntry`：RR 检查改为先查 `SignalTypeMinRR[lower(sig.SignalType)]`，命中且 `>0` 则用其覆盖全局值；同时把 `effective_min_rr / signal_type` 写进终态诊断 metadata。
- [ ] `strategy/chanlunv2/entry_timing_test.go`（或就近的 engine_test）新增 case：buy2 信号在 `signal_type_min_rr.buy2=1.5` 配置下，RR=1.6 通过、RR=1.4 被拒。
- [ ] 验证（实盘）：BNBUSDT buy2 和 HYPEUSDT buy3 不再因 RR 立即终态。

### Task 2 — 观察窗口延长（W2）

- [ ] `config/config.go` `NormalizeChanlunV2EntryTiming`：`WatchMaxCandles` 默认从 8 改为 16；上限 96 不变。
- [ ] `config/config_test.go` `TestNormalizeChanlunV2EntryTimingDefaultsAndOverrides`：把对默认值的断言从 `8` 改为 `16`（不改则该测试必红）。
- [ ] 验证（实盘）：SOLUSDT buy2 不再因 2h 窗口过期终态。

> 原 W2.2 / W2.3（按信号类型差异化窗口、trigger 形成中延长 4 根）已从 requirements 删除，本规格不再承接。

### Task 3 — open gate 多档置信度按 gate 分项覆盖（W3，B' 方案）

- [ ] `config/config.go`：新增 `ChanlunV2MinConfidenceOverrides struct{ LongBase, ShortBase, RangeLong, RangeShort int }`，作为 `ChanlunV2EntryZoneConfig.MinConfidenceOverrides` 字段。归一化时**不**对各值做 clamp（`0` 表示"不覆盖"），仅当 `>0` 才参与计算。
- [ ] `decision/decision.go`：`StrategyValidationOptions` 新增 `MinConfidenceOverrides` 字段（同结构，命名为 `decision.MinConfidenceOverrides`，避免跨包耦合到 config 类型）。`validateOpenDecisionWithOptions` 把它透传到 `OpenGateInput`。
- [ ] `decision/open_gate.go`：
  - `OpenGateInput` 新增 `MinConfidenceOverrides MinConfidenceOverrides`。
  - 抽出 `effectiveFloor(defaultFloor, override int) int`（仅当 `override > 0 && override < defaultFloor` 才返回 override）。
  - 把 `requireMinConfidence(longBaseMinConfidence, ...)` / `shortBaseMinConfidence` / `rangeLongMinConfidence` / `rangeShortMinConfidence` 四处替换为 `effectiveFloor(...)` 包装。
  - **不**修改 `counterTrendMinConfidence / btcConflictMinConfidence / btcVolatilityMinConfidence / highADXMinConfidence` 这四处——它们继续走硬编码值。
  - 当 override 把"会拒"翻为"放行"（即 `actual_confidence ∈ [override, default)`）时，向 `result.Diagnostics` 写入 `min_confidence_override_applied: {rule, from, to, actual_confidence}`。
- [ ] `strategy/chanlunv2/engine.go` `validateChanlunV2Decisions`：调用 `decision.ValidateStrategyDecisions` 时，从 `e.Config.EntryTiming.EntryZone.MinConfidenceOverrides` 把四个字段透传到 `decision.StrategyValidationOptions{ MinConfidenceOverrides: ... }`。
- [ ] `strategy/chanlunv2/engine.go` `signalToDecision`：**保持不动**，`d.Confidence = sig.Confidence`。
- [ ] `decision/open_gate_test.go`：
  - 新增 `TestEvaluateOpenGate_RangeLongOverrideRelaxes`：confidence=65，state=RANGING，`MinConfidenceOverrides.RangeLong=60` → 通过；override=0 → 拒绝。
  - 新增 `TestEvaluateOpenGate_OverrideDoesNotAffectCounterTrend`：在逆势场景下，`MinConfidenceOverrides.RangeLong=60` 不应让 confidence=70 通过 counter_trend=88 门槛。
  - 新增 `TestEvaluateOpenGate_OverrideAboveDefaultIsIgnored`：`MinConfidenceOverrides.RangeLong=90` 不应"提高" gate 门槛（应被忽略，仍按 82）。
- [ ] 验证（实盘）：DOGEUSDT entry trigger 不再因 RANGING long 置信度 < 82 被 open gate 拒绝；逆势/BTC 冲突场景下置信度 < 88 仍被拒（手工构造测试不在实盘验证范畴，由单测覆盖）。

> **明确淘汰**：原 Task 3 备选方案 "在 `signalToDecision` 中 `d.Confidence = max(sig.Confidence, 85)`" 不予采用。理由见 design.md §3.1 对照表。

### Task 4 — 部署验证

- [ ] 同步代码到 217 服务器。
- [ ] 在该 trader 配置中显式设置 `chanlun_v2_strategy.entry_timing.entry_zone.min_confidence_overrides.range_long = 60`（其余三项保持 0），确认配置生效路径。
- [ ] `docker compose build && docker compose up -d`。
- [ ] 等待 1h K 线闭合后检查日志：
  - `entry_rr_invalid` 终态比例下降；
  - `trigger ready` 通过 RANGING long open gate；
  - 出现 `gate_diagnostics.min_confidence_override_applied` 字段；
  - `d.Confidence` 在日志里仍是 60-70（**不**是 85）——确认未污染。

---

## P1 — 去重优化

### Task 5 — entry trigger 被拒去重（W4）

- [ ] `strategy/chanlunv2/state.go`：
  - `signalExecutionState` 扩展 `TriggerRejections map[string]triggerRejectionRecord` 字段。
  - 新增类型 `triggerRejectionRecord{ Count int; FirstAt, LastAt int64; LastReason, LastGateRule string }`，并在 JSON 持久化中带上。
  - 新增方法 `markTriggerRejected(traderID, signalID, triggerID, reasonCode, gateRule string, now time.Time)`、`triggerRejectionRecord(traderID, signalID, triggerID string) (triggerRejectionRecord, bool)`、`isTriggerBlocked(traderID, signalID, triggerID string) bool`（`Count >= 3` 即 true）。
- [ ] `strategy/chanlunv2/engine.go` `markTerminalChanlunV2OpenRejections`（约 1067 行）：
  - 新增分支：当 `rej.ReasonCode` ∈ `{gate.long_base_confidence, gate.short_base_confidence, gate.range_long_confidence, gate.range_short_confidence}` 时调 `markTriggerRejected`。
  - 当对应 trigger `Count >= 3` 时再调 `markSignalTerminalRejected(... "gate_blocked" ...)`，把父结构升级为终态。
  - **不**记入：`counter_trend / btc_conflict / btc_volatility / high_adx` 类拒因，以及所有非 confidence 拒因。
- [ ] `strategy/chanlunv2/entry_timing.go` `evaluateParentStructureEntry`：产出 trigger Decision 之前先查 `isTriggerBlocked`；若已 block 则跳过本周期产出，并在返回的 Diagnostics 标 `trigger_skipped: gate_blocked`。
- [ ] Decision 的 `StrategyMetadata` 在每次涉及该 trigger 的产出时写入：
  - `trigger_rejected_count`
  - `trigger_rejected_first_at`
  - `trigger_rejected_last_reason`
  字段名与 requirements W4.3 一致。
- [ ] `strategy/chanlunv2/state_test.go` 新增用例：
  - 同一 triggerID 调 3 次 `markTriggerRejected` 后 `isTriggerBlocked` 返回 true。
  - 不同 reasonCode 命中后 `LastReason` 正确更新；非 confidence 拒因不参与计数。
- [ ] 验证（实盘）：同一 trigger 不再连续 ≥4 次重复产出被拒；日志里出现 `trigger_skipped: gate_blocked` 或 `gate_blocked` 父结构终态。

---

## 验收度量

| 指标 | 当前 | P0 后目标 |
|---|---|---|
| 24h 成功开仓 | 0 | ≥1 |
| `entry_rr_invalid` 终态（按终态事件） | 73% | ≤20% |
| `watch_window_expired` 终态（按终态事件） | 26% | ≤10% |
| RANGING long open gate 置信度拒绝（按周期） | 10/10 | 0 |
| 同一 trigger 重复被拒次数最大值 | 4 | ≤1（P1 完成后） |
| `gate_diagnostics.min_confidence_override_applied` 出现 | — | 与 `open_accepted` 数量同阶 |
| `d.Confidence` 在被 override 通过的开仓中是否被污染 | — | 否（仍等于 sig.Confidence） |
