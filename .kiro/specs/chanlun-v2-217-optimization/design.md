# 缠论V2策略217服务器优化 Design

## 关联

- `requirements.md`（同目录）

---

## 1. RR 阈值差异化（W1）

### 1.1 配置扩展

`config/config.go` `ChanlunV2EntryZoneConfig` 新增：

```go
type ChanlunV2EntryZoneConfig struct {
    Mode                 string             `json:"mode,omitempty"`
    MaxChaseRatio        float64            `json:"max_chase_ratio,omitempty"`
    MinRemainingNetRR    float64            `json:"min_remaining_net_rr,omitempty"`
    MaxChaseATRMultiplier float64           `json:"max_chase_atr_multiplier,omitempty"`
    SignalTypeMinRR      map[string]float64 `json:"signal_type_min_rr,omitempty"` // 新增
}
```

### 1.2 NormalizeChanlunV2EntryZone 修改

```go
if cfg.MinRemainingNetRR <= 0 {
    cfg.MinRemainingNetRR = 2.0  // 从 2.5 改为 2.0
}
if cfg.SignalTypeMinRR == nil {
    cfg.SignalTypeMinRR = map[string]float64{
        "buy1": 2.0, "sell1": 2.0,
        "buy2": 1.5, "sell2": 1.5,
        "buy3": 1.2, "sell3": 1.2,
    }
}
```

### 1.3 entry_timing.go 修改

`evaluateParentStructureEntry` 中 RR 检查：

```go
minRR := timing.EntryZone.MinRemainingNetRR
if v, ok := timing.EntryZone.SignalTypeMinRR[sig.SignalType]; ok {
    minRR = v
}
if rr < minRR {
    return terminal(...)
}
```

---

## 2. 观察窗口延长（W2）

### 2.1 NormalizeChanlunV2EntryTiming 修改

```go
if cfg.WatchMaxCandles <= 0 || cfg.WatchMaxCandles > 96 {
    cfg.WatchMaxCandles = 16  // 从 8 改为 16
}
```

---

## 3. open gate 置信度覆盖（W3）

### 3.1 方案

在 `validateChanlunV2Decisions` 调用 `decision.ValidateStrategyDecisions` 时，通过 `StrategyValidationOptions` 传入置信度覆盖：

```go
validOpenLike, rejections := decision.ValidateStrategyDecisions(ctx, openLike, decision.StrategyValidationOptions{
    Source:                    "chanlun_v2",
    MinConfidenceOverride:    60,  // 新增：覆盖 open gate 的 82
    BypassRangeConfidencePenalty: true,  // 新增：绕过震荡惩罚
})
```

### 3.2 decision/open_gate.go 修改

在 `EvaluateOpenGate` 中检查 `options.MinConfidenceOverride`：

```go
if opts.MinConfidenceOverride > 0 {
    effectiveMinConfidence = opts.MinConfidenceOverride
} else {
    // 现有逻辑：rangeLongMinConfidence=82 等
}
```

### 3.3 备选方案（更简单）

直接在 `signalToDecision` 中把 V2 信号的置信度提升到满足 open gate 要求：

```go
// 缠论 V2 结构确认的信号，置信度基线提升
if sig.Confidence >= 60 {
    d.Confidence = max(sig.Confidence, 85)  // 确保通过 open gate
}
```

**推荐备选方案**：改动最小，不需要修改 `decision` 包的公共接口。

---

## 4. entry trigger 去重（W4）

在 `evaluateParentStructureEntry` 中，当 trigger ready 产出 Decision 后，记录 trigger ID。下一 cycle 检查：

```go
if e.State.IsTriggerRejected(ctx.TraderID, symbol, triggerID) {
    rejectedCount := e.State.TriggerRejectedCount(ctx.TraderID, symbol, triggerID)
    if rejectedCount >= 3 {
        return terminal("gate_blocked", ...)
    }
    return skip  // 不再产出 Decision
}
```

执行后如果被 open gate 拒绝，在 `markRejectedOpenMarkers` 中标记：

```go
e.State.MarkTriggerRejected(ctx.TraderID, symbol, triggerID)
```

---

## 5. 实施优先级

| 优先级 | 修复 | 预估 | 效果 |
|---|---|---|---|
| **P0** | W1 RR 差异化 + W2 窗口延长 | 配置修改 + 2 行代码 | 解锁 73%+26% 终态信号 |
| **P0** | W3 置信度覆盖（备选方案） | 1 行代码 | 解锁 trigger ready 被拒 |
| **P1** | W4 trigger 去重 | state 扩展 | 减少噪音 |

---

## 6. 最小修改方案（仅配置 + 3 行代码）

如果不想改 `decision` 包，最小修改为：

1. **config.json** 新增 `signal_type_min_rr` + 改 `watch_max_candles=16`
2. **config/config.go** `NormalizeChanlunV2EntryZone`：默认 `MinRemainingNetRR=2.0` + 填充 `SignalTypeMinRR`
3. **entry_timing.go**：RR 检查使用 `SignalTypeMinRR[sig.SignalType]`
4. **engine.go** `signalToDecision`：`d.Confidence = max(sig.Confidence, 85)` 确保通过 open gate
