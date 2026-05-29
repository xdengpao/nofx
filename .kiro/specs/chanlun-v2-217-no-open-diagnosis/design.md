# 217 Chanlun V2 No-Open Diagnosis Design

## 1. 结论

217 最近两天没有开仓不是服务停止、交易所执行失败、余额不足或密钥问题。核心原因是：

1. 短侧和 buy3/sell3 信号出现时剩余 RR 太低，父结构阶段直接终止；
2. 唯一进入 trigger ready 的 `BNBUSDT buy2` 是多单，遇到 BTC 1h/4h confirmed bearish，被 `applyBTCMultiTimeframeGate()` 硬阻断；
3. 现有 `loosen_mode` 只在 Chanlun V1 路径实现，Chanlun V2 长时间不开仓不会自动放宽。

## 2. 代码依据

### 2.1 V2 RR 检查

`strategy/chanlunv2/entry_timing.go`：

- `minRemainingNetRRForV2Signal()` 优先读取 `EntryZone.SignalTypeMinRR[signalType]`；
- `evaluateParentStructureEntry()` 在父结构阶段先计算剩余净 RR；
- 若 `rr < minRR`，则标记 `entry_rr_invalid` 终态。

217 当前配置：

```json
"signal_type_min_rr": {
  "buy1": 2.0,
  "sell1": 2.0,
  "buy2": 1.5,
  "sell2": 1.5,
  "buy3": 1.2,
  "sell3": 1.2
}
```

日志里的直接终止阈值已经显示 `sell2=1.5`、`buy3/sell3=1.2`，说明配置和代码生效。

### 2.2 Confidence override 只覆盖行情类 gate

`decision/open_gate.go`：

- `applyDirectionalConfidenceGate()` 对 `long_base/short_base/range_long/range_short` 支持 `MinConfidenceOverrides`；
- `counter_trend/btc_conflict/btc_volatility/high_adx` 不被 override 覆盖。

217 当前 `long_base=60`、`range_long=60` 已生效；BNB open rejection 中有 `min_confidence_override_applied` 诊断。

### 2.3 BTC 多周期 gate 是硬阻断

`decision/open_gate.go`：

```go
func applyBTCMultiTimeframeGate(result *OpenGateResult, d *Decision, ctx *Context) {
    if DecisionDirection(d.Action) != "long" || !isHighBetaAltcoin(d.Symbol) || ctx.MarketDataMap == nil {
        return
    }
    ...
    if isConfirmedBTCBearishStructure(btcData) {
        result.blockWithDiagnostics("BTC 1h/4h 明显转弱，禁止新开高 beta 山寨多单", "btc", diagnostics)
        return
    }
}
```

`isHighBetaAltcoin()` 当前把所有非 BTC/ETH 标的都视为高 beta alt。因此 `BNBUSDT open_long` 在 BTC 1h/4h confirmed bearish 时必定被 block。

## 3. 推荐方案

### 3.1 不建议立即绕过 BTC hard veto

BNB 的 6 次 trigger ready 都是多单，而当时 BTC 1h/4h 同时 bearish。强行把 BTC gate 改为 report-only 会直接改变全局风险边界，不适合作为本次缺口的首要修复。

本次推荐先保持 BTC hard veto，把开仓率恢复目标转向 bearish 环境中更合理的短侧机会。

### 3.2 配置侧短期灰度

217 可先灰度：

- `sell2` RR 从 1.5 降到 1.1；
- `short_base` 和 `range_short` 从 0 调到 65；
- 保持 `buy2=1.5`、`buy3/sell3=1.2`；
- 保持 `long_base=60`、`range_long=60`，但 BTC hard veto 不变。

这能覆盖最近 48h 的近失手短侧样本：

- `DOGEUSDT sell2 RR=1.41`
- `ADAUSDT sell2 RR=1.22`
- `SOLUSDT sell2 RR=1.12`

这些信号不会直接开仓；它们只是从父结构终态变为等待 fresh entry trigger，后续仍需过 open gate、risk budget 和 exchange preflight。

### 3.3 代码侧中期修复

把 trading frequency 的 `loosen_mode` 接入 Chanlun V2，而不是只停留在 risk_state 展示：

1. 在 V2 engine 周期开始时读取 `ctx.FrequencyPolicy.LoosenMode` 和 `ctx.FrequencyState` / `ctx.RuntimeMinutes` 的 inactivity 状态；
2. 进入 loosen 后记录 `risk_state.active_mode=loosen`；
3. 对 V2 entry timing 的 effective threshold 做运行态调整：
   - `SignalTypeMinRR[signalType] + MinNetRRDelta`，floor=1.0；
   - `MaxChaseRatio + MaxChaseRatioBump`，cap=1.0；
   - `MinTriggerConfidence - PilotConfidenceDrop`，floor=`HardFloorPilotConfidence`；
4. 同一个 effective RR resolver 必须覆盖：
   - `evaluateParentStructureEntry()` 父结构 RR 检查；
   - `validateEntryZoneAndRR()` entry trigger RR 检查；
   - `applyChanlunV2FreshnessGuard()` 剩余 RR 二次检查。
5. 成功开仓后退出 loosen。实现上优先复用 `ctx.FrequencyState.OpenCount24h` / `LastOpenAt`，不要把 V1 的 `StateStore` 直接耦合进 V2。
6. safe/loss 模式优先级高于 loosen。

### 3.4 可观测性

当前日志把历史终态反复静默，容易把“事件数”和“周期复述数”混在一起。仓库已有 `cmd/replay -open-rejection-daily` 和 `logger.BuildOpenRejectionDailyReport()`；本规格应扩展它们为 Chanlun V2 no-open 报告，而不是新增一套重复日报：

- `rr_direct`：本周期新终止；
- `suppressed_terminal`：历史终态静默；
- `trigger_ready`；
- `waiting_for_fresh_entry_trigger`；
- `open_gate_block`，并展开 BTC diagnostics。

## 4. 风险控制

| 风险 | 控制 |
|---|---|
| `sell2` RR 降到 1.1 后交易质量下降 | 只在 short-side 灰度，仍需 fresh trigger、open gate 和 risk budget；账户分配仍是 100 USDT |
| 震荡区间空单被放行过多 | `range_short=65` 只降低行情类 gate，不影响 counter-trend/BTC/ADX 风险类 gate |
| 误以为 confidence override 能绕过 BTC hard veto | 日志和 replay 报告显式标出 BTC veto |
| loosen mode 过度放宽 | safe/loss 模式优先；所有阈值有 floor/cap；成功开仓后退出 |

## 5. 部署策略

1. 先只合入 replay/diagnostic 与 V2 loosen 代码，不改 217 配置。
2. 跑目标测试：
   - `go test ./config`
   - `go test ./decision`
   - `go test ./strategy/chanlunv2`
   - `go test ./logger`
3. 备份 217 `config.json`。
4. 灰度配置 `sell2=1.1`、`short_base=65`、`range_short=65`。
5. 重建并启动容器。
6. 观察至少一个 1h K 线闭合：
   - V2 `active_mode` 是否进入 loosen；
   - `sell2 RR 1.1~1.5` 是否转为 waiting/trigger 而不是直接终态；
   - BTC hard veto 是否仍阻断山寨多单；
   - 是否出现真实 open 或明确的后续 gate 拒因。
