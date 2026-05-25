# 缠论V2策略交易缺陷修复 Design

## 关联

- `requirements.md`

## 范围

本设计只修改 V2 策略链路：

- `strategy/chanlunv2/*`
- `chanlun_v2/*`
- `config.ChanlunV2StrategyConfig`
- `trader/auto_trader.go` 中 V2 专属执行结果回调
- 必要的 V2 测试与日志字段映射

不得修改 V2 以外的策略包、状态文件、配置或执行语义。

---

## 1. V2 候选标的过滤（D3）

新增 V2 本地 helper，不引用其他策略包的私有函数：

```go
// strategy/chanlunv2/symbol_filter.go
func isChanlunV2TradableCryptoSymbol(symbol string) bool {
    normalized := market.Normalize(symbol)
    if !(strings.HasSuffix(normalized, "USDT") || strings.HasSuffix(normalized, "USDC")) {
        return false
    }
    for _, prefix := range []string{"XAU", "XAG", "CL", "COPPER", "NG", "SI"} {
        if strings.HasPrefix(normalized, prefix) {
            return false
        }
    }
    return true
}
```

在 `strategy/chanlunv2/report.go` 的 `resolveSymbolUniverse` 过滤候选：

```go
for _, coin := range ctx.CandidateCoins {
    symbol := market.Normalize(coin.Symbol)
    if symbol == "" {
        continue
    }
    if strings.TrimSpace(coin.FilterReason) != "" {
        continue
    }
    if !isChanlunV2TradableCryptoSymbol(symbol) {
        continue
    }
    // append selected V2 symbol
}
```

持仓 symbol 可继续进入 V2 持仓管理；但非加密 symbol 不允许生成新的 open-like decision。

---

## 2. V2 可执行信号生命周期（D1/D4/D5）

### 状态结构

在 `strategy/chanlunv2/state.go` 扩展 V2 自有状态：

```go
type SignalExecutionState struct {
    TraderID        string `json:"trader_id"`
    Symbol          string `json:"symbol"`
    SignalID        string `json:"signal_id"`
    SignalType      string `json:"signal_type,omitempty"`
    Action          string `json:"action,omitempty"`
    Status          string `json:"status"` // executed, terminal_rejected, suppressed
    ReasonCode      string `json:"reason_code,omitempty"`
    FirstSeenAt     int64  `json:"first_seen_at,omitempty"`
    LastSeenAt      int64  `json:"last_seen_at,omitempty"`
    SuppressedCount int    `json:"suppressed_count,omitempty"`
    UpdatedAt       int64  `json:"updated_at"`
}
```

新增方法均挂在 V2 `Engine` 上：

```go
func (e *Engine) hasTerminalSignal(traderID, symbol, signalID string) bool
func (e *Engine) markSignalExecuted(traderID string, d decision.Decision, executedAt time.Time)
func (e *Engine) markSignalTerminalRejected(traderID string, d decision.Decision, reasonCode string, now time.Time)
func (e *Engine) suppressKnownTerminalSignal(traderID string, d decision.Decision) (bool, string)
```

这些状态可以使用现有 V2 `LifecycleStatePath` 的读写方式，但不得读写 V2 以外的策略状态文件。

### 执行结果回调

扩展 V2 接口，或新增可选接口：

```go
type ChanlunV2ExecutionReporter interface {
    OnExecutionResult(result chanlunv2.ExecutionResult)
}
```

`trader/auto_trader.go` 在执行每个 action 后，如果当前 trader 是 `chanlun_v2` 且 V2 engine 实现该接口，则报告执行结果。只有 `Success=true` 且 final action 为 open-like 时，V2 才标记 executed。失败开仓不得标记 executed。

### 终态拒绝

以下 V2 reason code 可进入 terminal/suppressed 状态：

- `freshness_gate.signal_expired`
- `freshness_gate.target_crossed`
- `freshness_gate.rr_invalid`
- `countertrend.higher_timeframe`
- `position_sizing.zero_quantity`
- `position_sizing.min_notional`
- `position_sizing.margin_insufficient`
- `position_sizing.not_executable`

非终态 open gate 原因只记录诊断，不永久阻断新结构。

---

## 3. V2 open validation 与 zero-size fail-safe（D2）

在 `strategy/chanlunv2/engine.go` 的 `validateChanlunV2Decisions` 保证：

1. open-like V2 decision 先通过 SL/TP、confidence、sizing、min notional、margin、preflight 校验。
2. 不可执行结果转为 `decision.OpenRejection`。
3. `annotateChanlunV2SizingRejections` 给 rejection 写入稳定 reason code。
4. terminal sizing reason 调用 `markSignalTerminalRejected`，后续同 signal_id 静默跳过。

执行层兜底仍保留，但正常 V2 策略不得再让 `PositionSizeUSD <= 0` 的 open-like action 到达交易所调用路径。

日志映射：

- `decision_json` 保留 `stop_loss`、`take_profit`、`confidence`。
- `DecisionAction` 保留 `requested_stop_loss`、`effective_stop_loss`、`requested_take_profit`、`effective_take_profit`、`risk_normalization`、`gate_reasons`。
- zero-size 失败记录为 `open_rejected`，不是失败 `open_long/open_short`。

---

## 4. V2 SL/TP 兜底（D2）

`signalToDecision` 保留 Rust 输出。新增 V2-only fallback helper：

```go
func (e *Engine) applyV2StopTakeProfitFallback(ctx *decision.Context, d decision.Decision, sig Signal, data *market.Data, tradeTF string) decision.Decision
```

优先级：

1. Rust `Signal.StopLoss/TakeProfit`
2. V2 center 边界：long 优先 ZD/下沿，short 优先 ZG/上沿
3. ATR fallback：long `SL=current-2*ATR, TP=current+3*ATR`；short 反向
4. 仍无效则拒绝，不进入执行层

fallback 只写 V2 decision metadata，例如：

```go
"sl_tp_source": "rust|center|atr|invalid"
```

---

## 5. V2 真实 MACD histogram（D7）

不新增 V2 以外的策略依赖，不要求 Go TA-Lib。新增 V2 本地 MACD 计算：

```go
// strategy/chanlunv2/macd.go
func calculateV2MACDHistogram(closes []float64, fast, slow, signal int) []float64
```

规则：

- 使用标准 EMA 公式。
- 输出长度与 closes 一致。
- 数据不足和 EMA 预热区填 0。
- `buildInput` 只负责从 K 线提取 close 并赋值 `input.MACDHist`。

`buildInput` 修改后不再使用 `k.Close - klines[i-1].Close`。

---

## 6. V2 逆势信号抑制（D4）

在 `multiLevelJudgment` 中，明确 higher timeframe 趋势时直接过滤逆势信号：

```go
if higherResult.Trend == "down_trend" && sig.Direction == "long" {
    markCountertrendSuppressed(...)
    continue
}
if higherResult.Trend == "up_trend" && sig.Direction == "short" {
    markCountertrendSuppressed(...)
    continue
}
```

实现注意：

- `consolidation` 和 `unknown` 不压制。
- 压制必须带 V2 reason code：`countertrend.higher_timeframe`。
- 后续同 signal_id 先查 suppression，再决定是否进入 open gate。
- `StrategyDiagnostics` 输出压制计数和样例，不重复刷屏。

---

## 7. V2 终态/过期结构压缩（D5）

现有 `staleSuppressions` 和 `lifecycleStates` 保留，但进入 `evaluateParentStructureEntry` 前先查终态状态：

```go
if e.hasTerminalSignal(ctx.TraderID, symbol, parentID) {
    e.incrementSuppressedTerminalSignal(...)
    return parentEntryEvaluation{ParentSeen: true, Terminal: true}
}
```

输出行为：

- 首次终态拒绝可以写详细 marker/rejection。
- 重复终态只增加计数。
- 本周期 summary 输出 `terminal_suppressed_count` 和 top reason codes。
- 不再为每个 symbol 每个 cycle 拼接长文本诊断。

---

## 8. V2 主动平仓（D6）

扩展 `config.ChanlunV2PositionManagementConfig`，只影响 V2：

```go
HardStopATREnabled    *bool   `json:"hard_stop_atr_enabled,omitempty"`
HardStopATRMultiplier float64 `json:"hard_stop_atr_multiplier,omitempty"`
MaxHoldEnabled        *bool   `json:"max_hold_enabled,omitempty"`
MaxHoldCandles        int     `json:"max_hold_candles,omitempty"`
MaxHoldTimeframe      string  `json:"max_hold_timeframe,omitempty"`
FullCloseOnBreak      *bool   `json:"full_close_on_break,omitempty"`
```

`evaluateV2PositionManagement` 增加顺序：

1. 结构破坏 full close 或 partial close（按配置）
2. ATR 硬止损 full close
3. 持仓超时且无盈利 full close
4. 反向 fresh signal close
5. 现有 floating drawdown / partial TP / breakeven

所有 `close_long/close_short`、`partial_close`、`update_stop_loss` 都继续走 risk-reducing validation；风险降低动作优先于开仓执行。

---

## 9. 验证策略

本 spec 的测试应聚焦 V2 包和受影响的执行回调：

```bash
CGO_ENABLED=0 go test ./config ./strategy/chanlunv2
CGO_ENABLED=0 go test ./trader
CGO_ENABLED=0 go build ./...
```

如部署到 161，观察同等窗口指标：

- zero-size failed open 为 0
- CLUSDT/XAUUSDT 不在 V2 新开仓候选
- 同一 signal_id 不重复产生 open action
- countertrend open gate 重复拒绝显著下降
- terminal/stale 诊断改为汇总计数
- MACD histogram 不再等于收盘价差序列

---

## 10. 实施优先级

| 优先级 | 修复 | 说明 |
|---|---|---|
| P0 | D3 标的过滤 + D2 zero-size fail-safe | 先阻止非加密候选和不可执行开仓进入执行层 |
| P1 | D1 执行结果回调 + 终态 signal_id 抑制 | 防止同一 V2 信号重复执行 |
| P2 | D7 真实 MACD + D2 SL/TP 兜底 | 提升 Rust 背驰和风控输入质量 |
| P3 | D4 逆势抑制 + D5 终态诊断压缩 | 降低重复 open gate 和日志噪声 |
| P4 | D6 主动平仓增强 | 增加 ATR/timeout/full close 风险降低路径 |
