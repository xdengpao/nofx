# 程序化缠论策略实盘缺陷修复 Design

## 文档信息

| 字段 | 值 |
|---|---|
| 规格名称 | chanlun-live-trading-defect-fix |
| 关联 | `requirements.md`（同目录） |
| 主要改动文件 | `strategy/chanlun/{engine.go, signals.go, state.go, types.go, position_management.go}`、`decision/types.go`、`config/{config.go,programmatic.go}`、`manager/trader_manager.go`、`trader/auto_trader.go`、`logger/decision_logger.go`、`cmd/replay/main.go`、`web/src/types/index.ts` |
| 设计原则 | 配置开关化、字段只增不删、行为可一键回滚 |

---

## 1. 整体架构变更

在 `Engine.GetFullDecision()` 现有 pipeline 前后插入轻量编排层，不改变交易执行链路与 `decision` 包对外契约：

```
Engine.GetFullDecision(ctx)
│
├─ ① fastSkipSuppressed(ctx)          ← Req 5/6
├─ ② accountSizeGate(ctx)             ← Req 7
├─ ③ candidateGovernor(ctx)            ← Req 8
├─ ④ loosenModeController(ctx)        ← Req 9
│
├─ evaluatePositionManagement()        （不变）
├─ evaluateMainSignals()               ← Req 1/3/4/6 改造
├─ evaluatePreviewSignals()            ← Req 2 阈值改造
│
├─ ⑤ decisionLogEnricher(ctx, decs)   ← Req 10
└─ ⑥ dailySummaryWriter (每日 cron)    ← Req 10.4
```

总开关：`programmatic_strategy.defect_fix_pack_enabled`。配置层使用 `*bool` 或等价 presence tracking，以区分"未配置"与"显式 false"：

- 未配置：归一化为 `true`，启用本规格推荐默认值。
- 显式 `false`：①②③④⑤ 全部 bypass，且恢复旧默认值：`pilot_min_confidence=90`、`entry_timing.pilot.min_confidence=90`、`min_remaining_net_rr=2.5`、`direct_structure_open=false`。
- 运行时：`ProgrammaticStrategyProfile` 与 `decision.ProgrammaticStrategyPolicy` 使用普通 `bool`，通过 `manager.decisionProgrammaticStrategyPolicy` 传入 Engine。

> **代码命名约定**：代码中类型统一带 `Policy` 后缀（如 `ProgrammaticEntryTimingPolicy`、`ProgrammaticPreviewSignalsPolicy`、`ProgrammaticEntryZonePolicy`）。本文档为简洁省略后缀，实现时以代码实际类型名为准。Engine 入口函数为 `GetFullDecision()`，非 `Run()`。

---

## 2. 模块设计

### 2.1 入场链路改造（Req 1 + Req 4）

#### 2.1.1 新增 `EntryPath` 枚举

```go
const (
    EntryPathDirectStructure    = "direct_structure"
    EntryPathPreviewThenTrigger = "preview_then_trigger"
)
```

#### 2.1.2 路由逻辑

在 `evaluateMainSignals` 识别到信号后：

```go
path := EntryPathPreviewThenTrigger
if e.Policy.EntryTiming.DirectStructureOpen &&
   signal.Confidence >= e.Policy.EntryTiming.DirectStructureMinConfidence {
    path = EntryPathDirectStructure
}
```

- `direct_structure`：跳过 preview 升级与 sub fresh trigger，直接进入 `entry_zone` → `applyProgrammaticSignalGuard`。
- `preview_then_trigger`：走现有 preview_2x→3x→pilot→fresh_trigger 链路。

#### 2.1.3 entry_window 终结条件（Req 1.4）

当 `direct_structure_open=false` 时，若连续 `max_no_trigger_sub_candles`（默认 3）个 sub 周期无 fresh trigger：

```go
if subCandlesSinceSignal >= e.Policy.EntryTiming.MaxNoTriggerSubCandles {
    e.StateStore.TerminateLifecycle(traderID, symbol, structureKey,
        "entry_window_missed_no_trigger", signalID, expiry)
}
```

终结后 fast-skip 生效，不再每 cycle 重复评估。

#### 2.1.4 双轨追价（Req 4）

```go
chaseRatio := |currentPrice - entryRef| / |SL - entryRef|
chaseATR   := |currentPrice - entryRef| / ATR_14_sub

maxRatio := effectiveMaxChaseRatio(symbol, tier, ageCandles)
maxATR   := cfg.MaxChaseATRMultiplier  // 默认 0.6

pass := chaseRatio <= maxRatio || (maxATR > 0 && chaseATR <= maxATR)
```

`effectiveMaxChaseRatio` 合成顺序：`symbol_overrides → tier_overrides → base`；当 `age_candles=0` 时 `+= fresh_age_chase_relax`（默认 0.10）。

#### 2.1.5 配置增量

```jsonc
"entry_timing": {
  "direct_structure_open": true,              // defect_fix_pack_enabled=true 时默认 true；显式 false 必须可保留
  "direct_structure_min_confidence": 70,      // 新增
  "max_no_trigger_sub_candles": 3,            // 新增
  "entry_zone": {
    "max_chase_atr_multiplier": 0.6,          // 新增
    "fresh_age_chase_relax": 0.10,            // 新增
    "tier_overrides": {                       // 新增
      "core":  { "max_chase_ratio": 0.5, "min_remaining_net_rr": 1.6 },
      "trend": { "max_chase_ratio": 0.4, "min_remaining_net_rr": 1.8 }
    },
    "theoretical_rr_unreachable_skip": true   // 新增
  }
}
```

实现注意：`ProgrammaticEntryTimingConfig.DirectStructureOpen` 需要从当前 `bool` 改为 `*bool`，否则无法区分未配置与显式 false，也无法同时满足新默认与一键回滚。

---

### 2.2 置信度阈值数据驱动（Req 2）

#### 2.2.1 P75 动态计算

```go
func (e *Engine) effectivePilotMinConfidence(ctx *decision.Context, signalType string) int {
    base := e.Policy.PreviewSignals.PilotMinConfidence // 默认改 70
    if v, ok := e.Policy.PreviewSignals.PilotMinConfidenceBySignal[signalType]; ok {
        base = v
    }
    if e.Policy.PreviewSignals.PilotMinConfidenceUseP75 {
        samples := e.StateStore.ConfidenceWindow(ctx.TraderID, signalType, 7*24*time.Hour)
        if len(samples) >= 30 {
            sort.Ints(samples)
            p75 := samples[len(samples)*3/4]
            base = clamp(p75, e.Policy.PreviewSignals.P75Floor, e.Policy.PreviewSignals.P75Ceiling)
        }
    }
    // mode 调整
    mode := e.activeMode(ctx)
    if mode == "loosen" { base = max(base-10, 60) }
    if mode == "safe"   { base = min(base+10, 95) }
    return base
}
```

样本来源：`signals.go` 每次产出 `ChanlunSignal` 时同步写入 `ConfidenceSample{SignalType, Confidence, At}` 到 state，7 天滚动窗口。

#### 2.2.2 启动校验

`config/programmatic.go` 新增：当 `preview_signals.pilot_min_confidence` 与 `entry_timing.pilot.min_confidence` 同时被用户显式配置且不一致时，返回 `fmt.Errorf("pilot_min_confidence 配置冲突")`。

#### 2.2.3 配置增量

```jsonc
"preview_signals": {
  "pilot_min_confidence": 70,                    // defect_fix_pack_enabled=true 时默认改 70；关闭时保持旧默认 90
  "pilot_min_confidence_by_signal_type": {       // 新增
    "buy1": 70, "sell1": 70, "buy3": 75, "sell3": 75
  },
  "pilot_min_confidence_use_p75": true,          // 新增
  "pilot_min_confidence_p75_floor": 65,          // 新增
  "pilot_min_confidence_p75_ceiling": 85         // 新增
}
```

配置冲突校验只对"用户显式配置的两个字段"生效。实现时 `ProgrammaticPreviewSignalsConfig.PilotMinConfidence` 与 `ProgrammaticEntryPilotConfig.MinConfidence` 需要使用 `*int` 或 presence tracking，避免把默认值误判为冲突。

---

### 2.3 净 RR 差异化（Req 3）

#### 2.3.1 分级查询

```go
func (e *Engine) minRemainingNetRRForSignal(signalType, timeframe, symbol, tier string) float64 {
    // 优先级: signal_type@timeframe → signal_type → symbol_overrides → tier_overrides → default
    if v, ok := cfg.SignalTypeMinRR[signalType+"@"+timeframe]; ok { return v }
    if v, ok := cfg.SignalTypeMinRR[signalType]; ok { return v }
    if v, ok := cfg.SymbolOverrides[symbol]; ok { return v.MinRemainingNetRR }
    if v, ok := cfg.TierOverrides[tier]; ok { return v.MinRemainingNetRR }
    return cfg.MinRemainingNetRR
}
```

#### 2.3.2 理论 RR 预过滤（Req 3.5）

在 `evaluateMainSignals` 信号识别后、进入 entry_path 之前：

```go
theoreticalNetRR := (|TP - signalPrice| - signalPrice*fee) /
                    (|signalPrice - SL| + signalPrice*fee)
if cfg.TheoreticalRRUnreachableSkip && theoreticalNetRR < minRR {
    // 直接 reject + terminate lifecycle
    e.StateStore.TerminateLifecycle(...)
    continue
}
```

#### 2.3.3 gate_diagnostics 扩展

`applyProgrammaticSignalGuard` 在计算 `remaining_net_rr` 时同时输出：

```go
diagnostics["gross_rr"]         = grossRR
diagnostics["fee_slippage_pct"] = fee
diagnostics["structure_rr"]     = structureRR
diagnostics["theoretical_max_rr"] = theoreticalNetRR
```

#### 2.3.4 配置增量

```jsonc
"entry_zone": {
  "min_remaining_net_rr": 2.0,                   // defect_fix_pack_enabled=true 时默认从 2.5 降到 2.0；关闭时保持 2.5
  "signal_type_min_rr": {                        // 新增
    "buy1@1h": 2.0, "buy2@1h": 1.6, "buy3@1h": 1.4,
    "sell1@1h": 2.0, "sell2@1h": 1.6, "sell3@1h": 1.4,
    "buy1": 2.0, "buy2": 1.6, "buy3": 1.4,
    "sell1": 2.0, "sell2": 1.6, "sell3": 1.4
  }
}
```

---

### 2.4 抑制 fast-skip 与 lifecycle 闭环（Req 5 + Req 6）

#### 2.4.1 状态扩展

```go
// state.go 新增字段
type SignalSuppression struct {
    // ... 现有字段 ...
    Severity       int    // 0=soft, 1=hard, 2=permanent
    PermanentSkip  bool
}

type LifecycleTermination struct {
    StructureKey string
    ReasonCode   string
    TerminatedAt time.Time
    ExpiryAt     time.Time
}
```

新增 StateStore 方法：

```go
FastSkipSet(traderID string) map[string]string  // structureKey → reasonCode
TerminateLifecycle(traderID, symbol, structureKey, reason, signalID string, expiry time.Time)
IsLifecycleTerminated(traderID, symbol, structureKey string) bool
UpgradeSuppression(traderID, symbol, structureKey, newReason string)
MarkPermanentSkip(traderID, symbol, structureKey string)
GCExpiredSuppressions(now time.Time)
SuppressionStats(traderID string) SuppressionStats
```

#### 2.4.2 fast-skip 流程

```go
func (e *Engine) fastSkipSuppressed(ctx *decision.Context) map[string]string {
    set := map[string]string{}
    for _, sup := range e.StateStore.ActiveSuppressions(ctx.TraderID) {
        if sup.PermanentSkip || e.StateStore.IsLifecycleTerminated(...) {
            set[sup.StructureKey] = sup.ReasonCode
        } else {
            set[sup.StructureKey] = sup.ReasonCode  // soft skip 同样跳过
        }
    }
    return set
}
```

`evaluateMainSignals/evaluatePreviewSignals` 在处理每个 signal 前查 `skipSet[signal.StructureKey]`，命中则跳过并在 `strategy_diagnostics` 中聚合一条 `suppressed_fast_skip`。

#### 2.4.3 终结化（Req 6）

`applyProgrammaticSignalGuard` 命中 `invalid_stop_take_profit_structure` 或 `target_already_crossed` 时：

```go
e.StateStore.TerminateLifecycle(ctx.TraderID, symbol, structureKey, reasonCode, signalID, expiry)
```

`signals.go` 新增出生检查：

```go
func (s *ChanlunSignal) IsBornInvalid() bool {
    if s.Direction == SideLong  { return !(s.StopLoss < s.Price && s.Price < s.TakeProfit) }
    if s.Direction == SideShort { return !(s.StopLoss > s.Price && s.Price > s.TakeProfit) }
    return false
}
```

`detectSignalsFromKlines` 在 append 前调用，IsBornInvalid 直接 drop 并计入 `signal_quality_breakdown`。

#### 2.4.4 SeenCount 升级为 permanent

```go
if sup.SeenCount > cfg.SuppressionPermanentThreshold { // 默认 5
    e.StateStore.MarkPermanentSkip(...)
}
```

#### 2.4.5 暴露统计

```go
type SuppressionStats struct {
    TotalActive      int            `json:"total_active"`
    ByReason         map[string]int `json:"by_reason"`
    OldestAgeCandles int            `json:"oldest_age_candles"`
    PermanentSkip    int            `json:"permanent_skip"`
}
```

写入 `risk_state.suppressions`。

---

### 2.5 pilot 仓位与账户尺寸 gate（Req 7）

```go
func (e *Engine) accountSizeGate(ctx *decision.Context) AccountSizeDecision {
    avail := ctx.Account.AvailableBalance
    lev   := leverageForContext(ctx)
    minNotional := e.exchangeMinNotional(ctx.Exchange)
    maxAllowed := avail * float64(lev) * cfg.MaxPilotNotionalPct

    if avail*float64(lev) < cfg.MinPilotNotionalUSD*2 {
        return AccountSizeDecision{HoldOnly: true, Reason: "account_too_small"}
    }
    if maxAllowed < minNotional {
        return AccountSizeDecision{RejectPilot: true, Reason: "pilot_size_below_min_notional",
            RequiredMinNotional: minNotional, MaxAllowedNotional: maxAllowed}
    }

    pilotSize := computePilotSize(avail, lev, e.Policy)
    pilotSize = math.Min(pilotSize, maxAllowed)
    if pilotSize < minNotional {
        return AccountSizeDecision{RejectPilot: true, Reason: "pilot_size_below_min_notional",
            RequiredMinNotional: minNotional, MaxAllowedNotional: maxAllowed}
    }

    return AccountSizeDecision{PilotPositionSizeUSD: pilotSize, AccountTooSmall: false}
}
```

`hold_only` 时跳过 `evaluateMainSignals/evaluatePreviewSignals`，仅执行 `evaluatePositionManagement`。

配置增量：

```jsonc
"pilot": {
  "max_pilot_notional_pct": 0.6,   // 新增
  "min_pilot_notional_usd": 30     // 新增
}
```

启动校验：`preview_signals.pilot_risk_fraction > 0.4` 或 `entry_timing.pilot.risk_fraction > 0.4` 时打印 warn。

---

### 2.6 候选标的治理（Req 8）

```go
type QuoteSpreadProvider func(symbol string) (quoteMid float64, execMid float64, err error)

func (g *CandidateGovernor) Filter(in []StrategySymbol, spread QuoteSpreadProvider) []StrategySymbol {
    var out []StrategySymbol
    for _, s := range in {
        if !isCryptoUSDT(s.Symbol) && !inAllowList(g.cfg.AllowNonCrypto, s.Symbol) {
            continue // 剔除非加密
        }
        if quote, exec, err := spread(s.Symbol); err == nil &&
           spreadBps(quote, exec) > g.cfg.MaxQuoteSpreadBps { // 默认 20
            continue // 价差过大
        }
        out = append(out, s)
    }
    return ensureCoreSymbols(out, g.cfg.CoreSymbols)
}
```

`isCryptoUSDT`：后缀 `USDT`/`USDC` + 前缀不在黑名单 `{XAU,XAG,CL,COPPER,NG,SI}`。

模块边界：`strategy/chanlun` 只依赖 `QuoteSpreadProvider` 函数，不直接依赖具体交易所。`trader.AutoTrader` 持有 `trader.Trader` 实例，负责用行情源 mid_price 与执行交易所 `GetMarketPrice()` 构造该 provider，并把 `quote_spread_too_high` 写入 `CandidateSnapshot.Errors`。

配置增量：

```jsonc
"candidate_governor": {
  "enabled": true,
  "allow_non_crypto_symbols": [],
  "max_quote_spread_bps": 20,
  "core_symbols_must_appear": ["BTCUSDT", "ETHUSDT"]
}
```

---

### 2.7 loosen_mode 状态机（Req 9）

#### 状态

```go
type LoosenState struct {
    Active    bool
    EnteredAt time.Time
    ExpiresAt time.Time
}
```

#### 触发

```
进入: now - last_open_at >= inactivity_window (默认 720min)
      AND account_too_small == false
      AND safe_mode.active == false
      AND loss_mode.active == false

退出: 产生 ≥1 次成功开仓
      OR 到达 expires_at (entered_at + 24h)
      OR safe_mode/loss_mode 激活
```

#### 效果

| 参数 | 正常 | loosen |
|---|---|---|
| pilot_min_confidence | 计算值 | -10（floor 60） |
| min_remaining_net_rr | 计算值 | -0.4 |
| max_chase_ratio | 计算值 | +0.05 |

#### 互斥

```go
func activeMode() string {
    if safeModeActive  { return "safe" }
    if lossModeActive  { return "loss" }
    if loosenActive    { return "loosen" }
    return "normal"
}
```

#### 配置增量

```jsonc
"trading_frequency": {
  "report_only": { "high_adx": false, "rr_threshold": false, "rolling_gate": false },
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

配置流转：

1. `config.TradingFrequencyConfig` 新增 `LoosenMode TradingFrequencyLoosenModeConfig`。
2. `config.TradingFrequencyProfile` 新增 `LoosenMode TradingFrequencyLoosenModeProfile`。
3. `manager.decisionFrequencyPolicy()` 把 profile 转成 `decision.FrequencyPolicy.LoosenMode`。
4. `trader.AutoTrader.buildTradingContext()` 已把 `FrequencyPolicy/FrequencyState` 放入 `decision.Context`，Engine 只从 ctx 读取 loosen 配置与 24h 统计。
5. `logger.RiskStateSnapshot` 与 `AutoTrader.buildRiskStateSnapshot()` 输出 active_mode、inactivity、反事实计数和 warnings。

#### 反事实计数（Req 9.1）

当 `report_only=true` 时，gate 仍计算结果但不阻断，在 `risk_state.gate_effectiveness` 输出 `{would_reject_count, report_only: true}`。

#### 空转告警（Req 9.4）

```go
if openRejected24h >= 10 && openCount24h == 0 {
    riskState.Warnings.RunawayRejectionLoop = true
}
```

---

### 2.8 决策日志可观测性（Req 10）

#### 2.8.1 cycle 级新增字段

```jsonc
{
  "wait_reason_summary": "pilot_below_threshold",
  "account_state": {
    "account_too_small": false,
    "total_realized_24h": 0.0
  },
  "risk_state": {
    "active_mode": "normal",
    "inactivity_minutes": 1440,
    "last_open_at": null,
    "last_close_at": null,
    "open_count_24h": 0,
    "open_rejected_24h": 6,
    "signal_count_24h": 197,
    "suppressions": { "total_active": 4, "by_reason": {...}, "permanent_skip": 0 },
    "warnings": { "runaway_rejection_loop": false }
  },
  "strategy_diagnostics": {
    "per_candidate": [
      { "symbol": "ETHUSDT", "signal_type": "sell2", "confidence": 72,
        "structure_key": "c5be41...", "terminal_reason_code": "pilot_below_threshold" }
    ],
    "confidence_histogram": {
      "sell2": { "count": 18, "p25": 68, "p50": 73, "p75": 78, "threshold": 70 }
    },
    "signal_quality_breakdown": { "born_invalid_long": 3, "born_invalid_short": 5 }
  }
}
```

数据链路：

```go
// decision/types.go
type FullDecision struct {
    // ...
    WaitReasonSummary string `json:"wait_reason_summary,omitempty"`
}

// trader/auto_trader.go
record.WaitReasonSummary = fullDecision.WaitReasonSummary
```

`wait_reason_summary` 由 `Engine.GetFullDecision()` 在构建 `FullDecision` 前根据本 cycle diagnostics、open_rejections 和 per_candidate 终态计算；`AutoTrader` 只负责拷贝，不重新解释策略细节。

`wait_reason_summary` 优先级（取第一个命中）：

```
suppressed > permanent_skip > theoretical_rr_unreachable > invalid_structure
> target_already_crossed > chase_too_high > structure_rr_too_low
> pilot_below_threshold > no_trigger > no_signal
```

#### 2.8.2 daily_summary

路径：`decision_logs/<trader>/daily_summary_YYYYMMDD.json`

```go
type DailySummary struct {
    Date             string         `json:"date"`
    TraderID         string         `json:"trader_id"`
    CycleCount       int            `json:"cycle_count"`
    OpenCount        int            `json:"open_count"`
    OpenRejected     int            `json:"open_rejected"`
    SignalCount       int            `json:"signal_count"`
    WaitReasonHist   map[string]int `json:"wait_reason_histogram"`
    SuppressionFinal SuppressionStats `json:"suppressions_final"`
    AccountStartEnd  [2]float64     `json:"account_start_end_balance"`
    LoosenModeEnters int            `json:"loosen_mode_enters"`
}
```

每日 0:05 触发，遍历当日 cycle 日志聚合。失败仅 warn，不阻塞主循环。

---

### 2.9 持仓管理端到端验证（Req 11）

不改动 `position_management.go` 逻辑，通过测试覆盖验证：

1. P0 阶段先为 `cmd/replay` 增加 `-defect-fix-pack` 开关，对历史日志重跑，确认修复后理论开仓 ≥1。
2. `strategy/chanlun/e2e_test.go` 新增：mock 1h 信号 → direct_structure → 持仓 → breakeven → partial_close → close。
3. `strategy/chanlun/account_gate_test.go`：余额不足 → `hold_only`。
4. `strategy/chanlun/loosen_mode_test.go`：12h 0 开仓 → loosen → 第 13h 产生开仓评估。

---

## 3. 数据契约变更汇总

### 3.1 Go 类型新增/扩展

| 类型 | 新增字段 | 文件 |
|---|---|---|
| `ProgrammaticStrategyConfig/Profile/Policy` | `DefectFixPackEnabled`、`SuppressionPermanentThreshold` | `config/programmatic.go`、`decision/types.go` |
| `ProgrammaticEntryTimingConfig` | `DirectStructureOpen *bool`、`DirectStructureMinConfidence`、`MaxNoTriggerSubCandles` | `config/programmatic.go` |
| `ProgrammaticEntryTimingPolicy` | `DirectStructureMinConfidence int`、`MaxNoTriggerSubCandles int` | `decision/types.go` |
| `ProgrammaticEntryZoneConfig/Profile/Policy` | `MaxChaseATRMultiplier`、`TierOverrides`、`SignalTypeMinRR`、`FreshAgeChaseRelax`、`TheoreticalRRUnreachableSkip` | `config/programmatic.go`、`decision/types.go` |
| `ProgrammaticPreviewSignalsConfig` / `ProgrammaticEntryPilotConfig` | pilot confidence 使用 pointer/presence tracking | `config/programmatic.go` |
| `ProgrammaticPreviewSignalsPolicy` | `PilotMinConfidenceBySignal`、`PilotMinConfidenceUseP75`、`P75Floor`、`P75Ceiling` | `decision/types.go` |
| `FrequencyPolicy` | `LoosenMode`、`GateEffectiveness` 所需字段 | `decision/types.go` |
| `FullDecision` | `WaitReasonSummary string` | `decision/types.go` |
| `DecisionRecord` | `WaitReasonSummary string` | `logger/decision_logger.go` |
| `RiskStateSnapshot` / `AccountSnapshot` / `CandidateSnapshot` | `active_mode`、`account_too_small`、`errors` 等新增日志字段 | `logger/decision_logger.go` |
| `SignalSuppression` | `Severity`、`PermanentSkip` | `strategy/chanlun/state.go` |
| `ChanlunSignal` | `EntryPath`、`Tier` | `strategy/chanlun/types.go` |

### 3.2 前端

`web/src/types/index.ts` 所有新增字段标为 `?:`（可选），保证旧后端兼容。

### 3.3 配置兼容

所有新增字段有默认值。缺省时 `defect_fix_pack_enabled=true`，行为 = 新推荐默认（阈值已放宽）。`defect_fix_pack_enabled=false` 时必须恢复旧默认和旧 pipeline，不能只关闭部分新编排层。

---

## 4. 回滚策略

| 维度 | 方案 |
|---|---|
| 配置 | `defect_fix_pack_enabled: false` → 所有新编排层 bypass，且 pilot/RR/direct_structure 等默认值恢复旧值 |
| 日志 schema | 只增不删，旧字段语义不变 |
| state.json | 新字段缺失时按"未终结/未抑制"处理 |
| 前端 | 新字段 `?:` 可选，缺失时 UI 不崩 |

---

## 5. 实施分期

| 阶段 | 内容 | 预估 |
|---|---|---|
| **P0** | Req 2/3 阈值合理化 + Req 5/6 fast-skip + 终结化 + Req 10 wait_reason_summary | 1.5 周 |
| **P1** | Req 1 direct_structure 路径 + Req 4 双轨追价 + 理论 RR 预过滤 | 1 周 |
| **P2** | Req 7 account_size_gate + Req 8 candidate_governor + Req 9 loosen_mode | 1 周 |
| **P3** | Req 10.4 daily_summary + Req 11 端到端测试 + 灰度 | 1 周 |

---

## 6. 灰度策略

1. 离线 replay 通过 → 新建 `aster_deepseek_canary`（≤10 USDT）跑 24h
2. canary 24h ≥1 次开仓 → 切主 trader，先关 loosen_mode 跑 24h
3. 主 trader 24h ≥1 次开仓 → 打开 loosen_mode
4. 任一阶段触发 `max_daily_loss` 或 `runaway_rejection_loop` → 立即 `defect_fix_pack_enabled=false`

---

## 7. 待确认

1. `aster_deepseek` 本金 44 USDT 是否为长期预期？影响 Req 7 `hold_only` 阈值。
2. `direct_structure_open=false` 的历史动机？决定默认值是否改 true。
3. XAU/XAG/CL 是 Aster 真实可交易品种还是误注入？决定 `allow_non_crypto_symbols` 默认值。
4. 行情源/执行交易所价差监控是否已有 metrics 采集路径？

---

## 附录 A：交叉验证勘误表

以下为 spec 与远程代码（commit `9656386`）交叉验证后发现的命名/结构差异，实施时以此为准：

### A.1 类型名映射

| 本文档简写 | 代码实际类型名 | 所在文件 |
|---|---|---|
| `ProgrammaticEntryTiming` | `ProgrammaticEntryTimingPolicy` | `decision/types.go:582` |
| `ProgrammaticPreviewSignals` | `ProgrammaticPreviewSignalsPolicy` | `decision/types.go:570` |
| `ProgrammaticEntryZone` | `ProgrammaticEntryZonePolicy` | `decision/types.go:596` |
| `ProgrammaticEntryZoneOverride` | `ProgrammaticEntryZoneOverridePolicy` | `decision/types.go:603` |
| `ProgrammaticStrategy`（config） | `ProgrammaticStrategyConfig` | `config/programmatic.go:24`（TraderConfig 字段类型） |

### A.2 函数名映射

| 本文档引用 | 代码实际 | 说明 |
|---|---|---|
| `Engine.Run()` | `Engine.GetFullDecision(ctx *decision.Context)` | 唯一入口，返回 `*decision.FullDecision` |
| `Engine.evaluatePreviewSignals` | 签名 `(ctx, symbol string, data *market.Data, now time.Time)` | 按单 symbol 调用，非批量 |

### A.3 Engine struct 现有字段

```go
type Engine struct {
    Policy             decision.ProgrammaticStrategyPolicy
    StateStore         *StateStore
    Clock              func() time.Time
    MarketDataProvider func(symbol string, opts decision.CyclePreparationOptions) (*market.Data, error)
    DisableOITopFetch  bool
    mu                 sync.RWMutex
    latestSignals      map[string]*SignalReport
    symbolUniverse     map[string][]StrategySymbol
}
```

新增字段需求（实施时添加）：
- `QuoteSpreadProvider QuoteSpreadProvider` — 由 AutoTrader 注入执行交易所价格，不直接依赖具体交易所。

不建议新增 `TraderID` 字段；`ConfidenceWindow` 查询从 `decision.Context.TraderID` 传入，避免 Engine 成为 trader 绑定对象。

### A.4 现有 ProgrammaticEntryTimingPolicy 字段

```go
type ProgrammaticEntryTimingPolicy struct {
    Enabled                        bool
    DirectStructureOpen            bool      // ← 已存在！
    DirectOpenMaxAgeCandles        int       // ← 已存在
    RequireFreshTrigger            bool
    TriggerTimeframe               string
    AllowedTriggerTypes            []string
    EntryZone                      ProgrammaticEntryZonePolicy
    MaxTriggerAgeCandles           int
    MinTriggerConfidence           int
    Pilot                          ProgrammaticEntryPilotPolicy
    ContinuationAfterTargetCrossed string
}
```

**注意**：`DirectStructureOpen` 已存在但当前 config 层无法区分未配置与显式 false。`ProgrammaticEntryTimingConfig.DirectStructureOpen` 需改为 `*bool`，profile/policy 仍可为普通 `bool`；新增字段 `DirectStructureMinConfidence` 和 `MaxNoTriggerSubCandles` 追加到 profile/policy。

### A.5 现有 ProgrammaticEntryZonePolicy 字段

```go
type ProgrammaticEntryZonePolicy struct {
    Mode              string
    MaxChaseRatio     float64
    MinRemainingNetRR float64
    SymbolOverrides   map[string]ProgrammaticEntryZoneOverridePolicy
}
```

新增字段：`MaxChaseATRMultiplier`、`TierOverrides`、`SignalTypeMinRR`、`FreshAgeChaseRelax`、`TheoreticalRRUnreachableSkip`。

### A.6 现有 ProgrammaticPreviewSignalsPolicy 字段

```go
type ProgrammaticPreviewSignalsPolicy struct {
    Enabled                    bool
    ComponentTimeframe         string
    TradeTimeframe             string
    WatchAfterClosedComponents int
    PilotAfterClosedComponents int
    AllowPilotOpen             bool
    PilotRiskFraction          float64
    PilotMinConfidence         int
    RequireConfirmedUpgrade    bool
}
```

新增字段：`PilotMinConfidenceBySignal`、`PilotMinConfidenceUseP75`、`P75Floor`、`P75Ceiling`。

### A.7 RiskStateSnapshot 现有字段

已有 `FrequencyPolicy`、`FrequencyState`、`LossMode`。新增 `Suppressions`、`ActiveMode`、`InactivityMinutes`、`LastOpenAt`、`LastCloseAt`、`OpenCount24h`、`OpenRejected24h`、`SignalCount24h`、`Warnings` 需追加到此 struct。

### A.8 待实施确认项

1. `decision.Context` 是否有 `Account.AvailableBalance` 和 `Leverage()` — 需查 `decision/types.go` 中 Context 定义。
2. `safe_mode` 状态存储位置 — 可能在 `decision` 包的 `CyclePreparation` 或 `risk.go` 中，非 Engine 字段。
3. `defect_fix_pack_enabled` 放在 `ProgrammaticStrategyConfig`（config 层）并使用 `*bool`；解析后传入 `ProgrammaticStrategyProfile` 和 `ProgrammaticStrategyPolicy`。
4. `wait_reason_summary` 必须经过 `decision.FullDecision -> logger.DecisionRecord`，不能只写入 `strategy_diagnostics`。
5. `cmd/replay -defect-fix-pack` 属于 P0 验证前置能力，不能放到 P3 才实现。
