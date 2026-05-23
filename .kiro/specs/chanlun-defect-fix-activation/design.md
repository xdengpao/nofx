# 程序化缠论策略第二轮优化 Design

## 关联

- `requirements.md`（同目录）
- 前置 spec: `.kiro/specs/chanlun-live-trading-defect-fix/`

---

## 1. 代码修复

### 1.1 normalize 逻辑：DefectFixPackEnabled 时强制覆盖

**文件**：`strategy/chanlun/engine.go`

#### `normalizeRuntimePreviewSignals`（约 line 112）

当前逻辑：
```go
if policy.PilotMinConfidence <= 0 {
    if defectFixPackEnabled { policy.PilotMinConfidence = 70 }
    else { policy.PilotMinConfidence = 90 }
}
```

修改为：
```go
if defectFixPackEnabled && policy.PilotMinConfidence > 70 {
    policy.PilotMinConfidence = 70
}
if policy.PilotMinConfidence <= 0 {
    policy.PilotMinConfidence = 90
}
```

#### `normalizeRuntimeEntryTiming`（约 line 159）

当前逻辑只在 `uninitialized` 时设 `DirectStructureOpen = defectFixPackEnabled`。

修改为：在 `uninitialized` 判断之后追加：
```go
if defectFixPackEnabled {
    policy.DirectStructureOpen = true
}
```

当前 `MinRemainingNetRR` 逻辑：
```go
if policy.EntryZone.MinRemainingNetRR <= 0 {
    if defectFixPackEnabled { policy.EntryZone.MinRemainingNetRR = 2.0 }
    ...
}
```

修改为：
```go
if defectFixPackEnabled && policy.EntryZone.MinRemainingNetRR > 2.0 {
    policy.EntryZone.MinRemainingNetRR = 2.0
}
if policy.EntryZone.MinRemainingNetRR <= 0 {
    policy.EntryZone.MinRemainingNetRR = 2.5
}
```

### 1.2 governor 执行顺序修复

**文件**：`strategy/chanlun/engine.go`

当前 `GetFullDecision` 流程（约 line 290）：
```go
governorDiagnostics := e.applyCandidateGovernor(ctx)  // line 290
...
mainDecisions, mainDiagnostics, mainRejections := e.evaluateMainSignals(ctx, universe, now)
```

`evaluateMainSignals` 内部对每个 symbol 调用 `evaluatePreviewSignals`。问题是 `applyCandidateGovernor` 标记了 `coin.FilterReason` 但 `evaluatePreviewSignals` 可能仍在处理被标记的 symbol。

**修复**：在 `evaluateMainSignals` 中，遍历 universe 时跳过已被 governor 剔除的 symbol：

```go
for i := range universe {
    if universe[i].FilterReason != "" {
        continue  // 已被 candidateGovernor 剔除
    }
    // ... 现有逻辑
}
```

同样在 `evaluatePreviewSignals` 调用前检查。

需确认 `StrategySymbol` 是否有 `FilterReason` 字段。如果没有，改为在 `applyCandidateGovernor` 中直接从 universe slice 中移除被剔除的 symbol（修改 `ctx.CandidateCoins`）。

### 1.3 PilotMinConfidenceUseP75 默认启用

当前 `normalizeRuntimePreviewSignals` 中：
```go
if policy.P75Floor <= 0 { policy.P75Floor = 65 }
if policy.P75Ceiling <= 0 { policy.P75Ceiling = 85 }
```

但 `PilotMinConfidenceUseP75` 没有默认值设置。追加：
```go
if defectFixPackEnabled && !policy.PilotMinConfidenceUseP75 {
    policy.PilotMinConfidenceUseP75 = true
}
```

---

## 2. 配置更新

**文件**：远程 `/home/ubuntu/appai3/nofx/config.json`

需修改 `traders[3]`（`aster_deepseek`）的 `programmatic_strategy` 段：

```jsonc
"programmatic_strategy": {
  "defect_fix_pack_enabled": true,           // 新增 ← 激活全部优化
  // ... 保留现有字段 ...
  "preview_signals": {
    // 删除或改为: "pilot_min_confidence": 70,
    "pilot_min_confidence": 70,              // 从 90 改为 70
    // ... 其他保留 ...
  },
  "entry_timing": {
    "direct_structure_open": true,           // 从 false 改为 true
    "require_fresh_trigger": false,          // 从 true 改为 false（direct_structure 不需要）
    "entry_zone": {
      "min_remaining_net_rr": 2.0,           // 从 2.5 改为 2.0
      // ... 其他保留 ...
    }
  }
}
```

全局 `trading_frequency` 段新增：
```jsonc
"trading_frequency": {
  // ... 现有字段保留 ...
  "loosen_mode": {
    "enabled": true,
    "inactivity_window_minutes": 720,
    "pilot_confidence_drop": 10,
    "min_net_rr_delta": -0.4,
    "max_chase_ratio_bump": 0.05,
    "max_duration_hours": 24,
    "hard_floor_pilot_confidence": 60
  }
}
```

---

## 3. 部署步骤

1. 在远程修改 `strategy/chanlun/engine.go`（§1.1 + §1.2 + §1.3）
2. 修改 `config.json`（§2）
3. `go build -o nofx`
4. 停止当前进程：`kill <pid>`
5. 启动新进程：`nohup ./nofx &`
6. 验证：`curl localhost:8080/health`
7. 等待 3-6h 观察日志

---

## 4. 回滚

- config.json 中 `"defect_fix_pack_enabled": false` 即可一键回退
- 代码修改是向后兼容的（`defectFixPackEnabled=false` 时走旧逻辑）
