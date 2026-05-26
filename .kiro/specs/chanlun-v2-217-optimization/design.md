# 缠论V2策略217服务器优化 Design

## 关联

- `requirements.md`（同目录）

---

## 0. 总体设计原则

1. **不污染信号本征属性**：不修改 `d.Confidence` 来"骗过" gate；所有放宽都通过显式的 gate-side 配置或 override 接口表达。
2. **风险类 gate 与行情类 gate 区分**：`counter_trend / btc_conflict / btc_volatility / high_adx` 是"控风险"类，本规格永不开放覆盖；`long_base / short_base / range_long / range_short` 是"行情适配"类，按 gate 分项可覆盖。
3. **缺省零回归**：所有新字段缺省值都退化为现有行为，仅当部署侧显式配置才生效。
4. **诊断字段先于行为变更**：override / 去重生效时必须打出诊断，便于事后归因。

---

## 1. RR 阈值差异化（W1）

### 1.1 配置扩展

`config/config.go` `ChanlunV2EntryZoneConfig` 新增 `SignalTypeMinRR`：

```go
type ChanlunV2EntryZoneConfig struct {
    Mode                  string             `json:"mode,omitempty"`
    MaxChaseRatio         float64            `json:"max_chase_ratio,omitempty"`
    MinRemainingNetRR     float64            `json:"min_remaining_net_rr,omitempty"`
    MaxChaseATRMultiplier float64            `json:"max_chase_atr_multiplier,omitempty"`
    SignalTypeMinRR       map[string]float64 `json:"signal_type_min_rr,omitempty"` // 新增
    MinConfidenceOverrides ChanlunV2MinConfidenceOverrides `json:"min_confidence_overrides,omitempty"` // §3 引入
}
```

### 1.2 NormalizeChanlunV2EntryZone 修改

```go
// 全局 RR
if cfg.MinRemainingNetRR <= 0 {
    cfg.MinRemainingNetRR = 2.0  // 从 2.5 改为 2.0
}
if cfg.MinRemainingNetRR < 1 {
    cfg.MinRemainingNetRR = 1
}

// 按信号类型 RR
if cfg.SignalTypeMinRR == nil {
    cfg.SignalTypeMinRR = map[string]float64{
        "buy1": 2.0, "sell1": 2.0,
        "buy2": 1.5, "sell2": 1.5,
        "buy3": 1.2, "sell3": 1.2,
    }
}
// **显式 clamp**：每个 per-signal 值都不得低于 1，与全局规则保持一致
for k, v := range cfg.SignalTypeMinRR {
    if v < 1 {
        cfg.SignalTypeMinRR[k] = 1
    }
}
```

> **设计决策**：本次只搬 `signal_type_min_rr`，**不**同步引入 V1 的 `tier_overrides / symbol_overrides / theoretical_rr_unreachable_skip`。理由见 §6 "长期一致性讨论"。

### 1.3 entry_timing.go 修改

`evaluateParentStructureEntry` 中 RR 检查（约 153 行）：

```go
minRR := timing.EntryZone.MinRemainingNetRR
if v, ok := timing.EntryZone.SignalTypeMinRR[strings.ToLower(strings.TrimSpace(sig.SignalType))]; ok && v > 0 {
    minRR = v
}
if rr < minRR {
    // 既有诊断 + 增加 effective_min_rr / signal_type
    return terminal("terminal_rr_invalid", "entry_rr_invalid", map[string]any{
        "remaining_net_rr": rr,
        "effective_min_rr": minRR,
        "signal_type":      sig.SignalType,
    })
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

### 2.2 不在本规格范围内

> **明确剔除**：先前需求草案里的"按信号类型差异化窗口"和"trigger 形成中延长 4 根"两条已从 requirements 删除。本规格在 W2 上仅做"默认值翻倍"这一个动作，避免设计/任务悬空。

### 2.3 测试同步

`config/config_test.go` 中 `TestNormalizeChanlunV2EntryTimingDefaultsAndOverrides` 用例硬编码了 `WatchMaxCandles=8` 的默认期望，本次默认改为 16 后必须同步更新断言（详见 tasks.md Task 2）。

---

## 3. open gate 多档置信度按 gate 分项覆盖（W3）

### 3.1 选型结论：B' 方案（按 gate 分项 override），淘汰备选 A

| 维度 | A. 硬抬 Confidence（淘汰） | B'. MinConfidenceOverrides 按 gate 分项（采用） |
|---|---|---|
| 作用范围 | 污染 `d.Confidence`，仓位规模/日志/风控全部被改 | 仅修改 open gate 内具名 gate 的门槛 |
| 可观测性 | 日志里 confidence=85，实际 65，复盘失真 | 日志里仍是 65，外加 `min_confidence_override_applied` 诊断 |
| 风险 gate 影响 | counterTrend/btcConflict 等会被一并放穿 | 这些 gate 排除在覆盖白名单之外，永远走硬编码 |
| 仓位侧 | confidence 抬到 85 → pilot 自动放大 | confidence 仍 65 → pilot 仍小，符合"信号弱-小试单"语义 |
| 测试影响 | `engine_test.go` 多处 `signalToDecision` 测试断言 confidence==sig.Confidence 会破 | `signalToDecision` 不动，老测试全保留 |
| 配置驱动 | 否（硬编码 85） | 是（每个 gate 一个整数旋钮） |

> **结论**：采用 B'，淘汰 A。这两点的代价是 ~50 行新代码（新增 struct + open gate 分支），收益是没有数据污染、配置可灰度、按 gate 分项可控、测试零回归。

### 3.2 数据结构

新增 `ChanlunV2MinConfidenceOverrides`（放在 `config/config.go`，作为 `ChanlunV2EntryZoneConfig` 的子字段）：

```go
type ChanlunV2MinConfidenceOverrides struct {
    LongBase   int `json:"long_base,omitempty"`   // 覆盖 longBaseMinConfidence(78)
    ShortBase  int `json:"short_base,omitempty"`  // 覆盖 shortBaseMinConfidence(82)
    RangeLong  int `json:"range_long,omitempty"`  // 覆盖 rangeLongMinConfidence(82)
    RangeShort int `json:"range_short,omitempty"` // 覆盖 rangeShortMinConfidence(85)
}
```

**显式不暴露**：`counter_trend / btc_conflict / btc_volatility / high_adx`。即使未来用户在 JSON 里加这些 key，因为不在 struct 字段里，会被 json 包静默忽略，不可绕过。

### 3.3 decision 包接口扩展

`decision/decision.go` `StrategyValidationOptions` 新增（结构体内部传递，不在公开 JSON 配置里出现）：

```go
type StrategyValidationOptions struct {
    // ...已有字段
    MinConfidenceOverrides MinConfidenceOverrides
}

type MinConfidenceOverrides struct {
    LongBase   int
    ShortBase  int
    RangeLong  int
    RangeShort int
}
```

`decision/open_gate.go` `OpenGateInput` 同步新增 `MinConfidenceOverrides`，并在 `EvaluateOpenGate` 内的 `requireMinConfidence` 调用处用以下 helper：

```go
func effectiveFloor(defaultFloor int, override int) int {
    if override > 0 && override < defaultFloor {
        return override
    }
    return defaultFloor
}

// long base
result.requireMinConfidence(
    effectiveFloor(longBaseMinConfidence, input.MinConfidenceOverrides.LongBase),
    "long base 置信度要求")

// RANGING long
if state == "RANGING" || state == "SQUEEZE" {
    result.requireMinConfidence(
        effectiveFloor(rangeLongMinConfidence, input.MinConfidenceOverrides.RangeLong),
        "震荡区间多单置信度要求")
}
// short / range_short 同理
// counterTrend / btcConflict / btcVolatility / highADX 不读 override
```

> **关键不变量**：`override` 永远只能**降低**门槛（且不能降到 0 以下），永远不会**升高**——升高的需求由 `requireMinConfidence` 自身的"取较大值"语义满足，不需 override 配合。

### 3.4 strategy/chanlunv2/engine.go 接入

`validateChanlunV2Decisions`（约 1007 行）调用 `ValidateStrategyDecisions` 时把配置里的 overrides 透传：

```go
ovr := e.Config.EntryTiming.EntryZone.MinConfidenceOverrides
validOpenLike, rejections := decision.ValidateStrategyDecisions(ctx, precheckedOpenLike, decision.StrategyValidationOptions{
    Source: "chanlun_v2",
    MinConfidenceOverrides: decision.MinConfidenceOverrides{
        LongBase:   ovr.LongBase,
        ShortBase:  ovr.ShortBase,
        RangeLong:  ovr.RangeLong,
        RangeShort: ovr.RangeShort,
    },
})
```

`signalToDecision` **不动**，`d.Confidence = sig.Confidence` 维持原状。

### 3.5 诊断字段

在 `EvaluateOpenGate` 中，每当某个 gate 因 override 把门槛从 default 降低，且当时 confidence 落在 `[override, default)` 区间（即 override 把"会拒"翻转为"放行"），向 `result.Diagnostics` 写入：

```go
result.Diagnostics["min_confidence_override_applied"] = map[string]any{
    "rule":              "range_long",  // 或 long_base / short_base / range_short
    "from":              82,
    "to":                60,
    "actual_confidence": 65,
}
```

下游 `Decision.StrategyMetadata` 透传时使用同名 key，便于在日志/落库里检索。

---

## 4. open gate 置信度类拒绝在 trigger 维度去重（W4）

### 4.1 数据结构选型

复用现有 `signalExecutionStates`（`strategy/chanlunv2/state.go:142-`），**不另起独立 map**。理由：
- 已经按 `traderID` 维度分片、已经有 JSON 持久化通路、已经被 `markSignalTerminalRejected / suppressKnownTerminalSignal` 等多处使用；
- trigger 维度本质是 SignalID 的下钻，复用比新建一致性更好。

在 `signalExecutionState` 上扩展：

```go
type signalExecutionState struct {
    // ...已有字段
    TriggerRejections map[string]triggerRejectionRecord `json:"trigger_rejections,omitempty"`
}

type triggerRejectionRecord struct {
    Count        int    `json:"count"`
    FirstAt      int64  `json:"first_at_ms"`
    LastAt       int64  `json:"last_at_ms"`
    LastReason   string `json:"last_reason"`
    LastGateRule string `json:"last_gate_rule,omitempty"` // long_base / range_long...
}
```

### 4.2 引擎接入

新增三个 state 方法：

```go
func (e *Engine) markTriggerRejected(traderID string, d decision.Decision, reasonCode, gateRule string, now time.Time)
func (e *Engine) triggerRejectionRecord(traderID, signalID, triggerID string) (triggerRejectionRecord, bool)
func (e *Engine) isTriggerBlocked(traderID, signalID, triggerID string) bool // count >= 3
```

接入点：
1. **拒绝时记录**：`markTerminalChanlunV2OpenRejections`（engine.go:1067）现仅处理白名单内的终态原因；新增分支：当 `rej.ReasonCode` 匹配 `*_confidence` / `*_min_confidence` 这类 open-gate 置信度拒因时，调 `markTriggerRejected`；当 `Count >= 3` 时升级为终态（写 `gate_blocked` 到现有 `markSignalTerminalRejected` 通路）。
2. **下一 cycle 跳过**：`evaluateParentStructureEntry` 在产出 trigger Decision 前，先查 `isTriggerBlocked`：若已 block 则直接 `return skip`，并在 Diagnostics 标 `trigger_skipped: gate_blocked`。
3. **诊断字段**：每条 Decision 的 `StrategyMetadata` 写入：
   - `trigger_rejected_count`
   - `trigger_rejected_first_at`
   - `trigger_rejected_last_reason`
   字段名与 requirements W4.3 完全一致。

### 4.3 拒因匹配规则

属于"置信度类"的拒因（命中即记入 trigger rejection）：
- `gate.long_base_confidence`
- `gate.short_base_confidence`
- `gate.range_long_confidence`
- `gate.range_short_confidence`

**不**记入的拒因（避免误把风险拒绝当成噪音去重掉）：
- `gate.counter_trend_confidence`
- `gate.btc_conflict_confidence`
- `gate.btc_volatility_confidence`
- `gate.high_adx_confidence`
- 所有非 confidence 类拒因（流动性、相关性、风控等）

> 这与 §3 的"行情类 vs 风险类" gate 划分完全对齐。

---

## 5. 实施优先级

| 优先级 | 修复 | 改动量 | 效果 |
|---|---|---|---|
| **P0** | W1 RR 差异化 + 默认 2.5→2.0 | 配置 + ~30 行 | 解锁 73% 终态信号 |
| **P0** | W2 窗口默认 8→16 + 测试同步 | ~5 行 | 解锁 26% 终态信号 |
| **P0** | W3 MinConfidenceOverrides（B' 方案） | 新 struct + 4 处分支 + 透传，~60 行 | 解锁 RANGING long 拒绝 |
| **P1** | W4 trigger 维度去重 + 诊断字段 | state 扩展 + 拒因匹配，~80 行 | 减少日志噪音、限制重试上限 |

---

## 6. 长期一致性讨论：要不要把 V1 的全套 RR 控制迁到 V2？

V1（`strategy/chanlun/engine.go`）的 entry zone 已演化为：

- `signal_type_min_rr`（含 `buy1@1h` 时间帧后缀变体 + 通配 `buy*` / `sell*` / `*`）
- `tier_overrides`（按 trader tier，例如 core/standard）
- `symbol_overrides`（按交易对）
- `theoretical_rr_unreachable_skip`（理论 RR 不可达直接终态）

本规格**只迁移** `signal_type_min_rr`，并采用 V2 自然命名（`buy1` / `buy2` / `buy3`，**不**带 `@1h` 后缀）。原因：
- 24h 评估窗口尚未出现按 tier / symbol / 时间帧通配的实际诉求；
- V2 当前只有一个 trader 在跑（`aster_chanlun_v2`），引入 tier 维度提前过度设计；
- `theoretical_rr_unreachable_skip` 在 V2 流程里已由 `evaluateParentStructureEntry` 的 RR 检查覆盖（不通过即立即 terminal），与 V1 的"理论 RR" 概念不完全等价，需要单独立题再设计；
- 命名风格选择 V2 自然命名（短 key），是因为 V2 当前只跑 1h trade timeframe，加 `@1h` 后缀属于装饰性噪音。

**后续规格留白**：未来若 V2 引入多 timeframe / 多 tier，应另立 spec 把 V1 的 `@<tf>` 后缀语义和 tier/symbol overrides 一次性迁过来；届时 `signal_type_min_rr` 的查询顺序应统一为 `signal_type@tf → signal_type → tier → symbol → default`，与 V1 对齐。本规格不做向前兼容承诺。

---

## 7. 测试与回归

新增/调整测试（详见 tasks.md）：
1. `config/config_test.go` 新增 `TestNormalizeChanlunV2EntryZoneSignalTypeMinRRClamp` — 校验 `<1` 被上调到 1。
2. `config/config_test.go` 修改 `TestNormalizeChanlunV2EntryTimingDefaultsAndOverrides` — `WatchMaxCandles` 默认期望从 8 改到 16。
3. `decision/open_gate_test.go` 新增 `TestEvaluateOpenGate_RangeLongOverrideRelaxes` 与 `TestEvaluateOpenGate_OverrideDoesNotAffectCounterTrend` — 覆盖 §3.3 的核心不变量。
4. `strategy/chanlunv2/entry_timing_test.go`（或就近的 engine_test）新增按信号类型 RR 阈值生效的用例。
5. `strategy/chanlunv2/state_test.go` 新增 trigger 维度 reject 计数与 ≥3 升级 `gate_blocked` 用例。
6. 既有 `strategy/chanlunv2/engine_test.go` 中所有 `signalToDecision` 用例**保持不变**（这是 B' 方案优于 A 的关键回归收益）。

---

## 8. 关于先前文档表述的修正

- 先前 requirements / design 中 "ADX < 25 → 震荡" 的说法不准确。`market.GetMarketState`（`market/data.go:1547-1582`）实际：`adx > 25` → STRONG_*，`20 < adx ≤ 25` → WEAK_*，`adx ≤ 20` → RANGING，`adx ≤ 20 && bollingerWidth < 2.0` → SQUEEZE。本规格统一改为 "ADX ≤ 20 → RANGING/SQUEEZE → 触发 82 门槛"。
- 先前出现过的两个候选配置路径 `strategy_risk.chanlun_v2_confidence_override` 与 `chanlun_v2_strategy.multi_level.min_confidence_override` 均**作废**，唯一规范路径为 `chanlun_v2_strategy.entry_timing.entry_zone.min_confidence_overrides`。
- §1.1 中 607 / 218 / 73% / 26% 的统计单位为"24h 内累计的终态事件计数"，分母为 825 终态事件，**不是** 306 决策周期。请勿与 `open_rejected=10` 这类按 cycle 计的指标混读。
