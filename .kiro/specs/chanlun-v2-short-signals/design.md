# 缠论V2做空信号缺失修复 Design

## 关联

- `requirements.md`
- `tasks.md`

---

## 1. Rust `signal.rs` 修改

### 1.1 当前代码结构（line 94-130）

```rust
// 二类/三类买卖点：基于中枢位置
if let Some(center) = centers.last() {
    if let Some(last_seg) = segments.last() {
        // Buy2: 回抽不入中枢（做多）
        if last_seg.low > center.zd && last_seg.low < center.zg && current_price > last_seg.low { ... }
        // Buy3: 离开中枢不回（做多）
        if last_seg.low > center.zg { ... }
    }
}
```

### 1.2 新增 Sell2/Sell3（在 Buy3 之后追加）

```rust
        // Sell2: 反弹不破中枢上沿（做空）
        // 条件：最后线段高点在中枢区间内（ZD < high < ZG），且当前价低于该高点
        if last_seg.high < center.zg && last_seg.high > center.zd && current_price < last_seg.high {
            signals.push(Signal {
                signal_type: SignalType::Sell2,
                direction: "short".into(),
                price: current_price,
                stop_loss: center.zg,
                take_profit: center.low - (center.high - center.low),
                confidence: 70,
                center_id: Some(center.id),
                divergence_strength: 0.0,
                timestamp: last_seg.end_time,
            });
        }

        // Sell3: 跌破中枢不回（做空）
        // 条件：最后线段高点低于中枢下沿 ZD
        if last_seg.high < center.zd {
            signals.push(Signal {
                signal_type: SignalType::Sell3,
                direction: "short".into(),
                price: current_price,
                stop_loss: center.zd,
                take_profit: current_price - (center.zg - center.zd),
                confidence: 65,
                center_id: Some(center.id),
                divergence_strength: 0.0,
                timestamp: last_seg.end_time,
            });
        }
```

### 1.3 Sell1 条件放宽（line 76-90）

当前：
```rust
crate::divergence::DivergenceType::Top if trend == TrendType::UpTrend => { ... }
```

修改为：
```rust
crate::divergence::DivergenceType::Top if trend == TrendType::UpTrend || trend == TrendType::Consolidation => {
    let conf_penalty = if trend == TrendType::Consolidation { 15 } else { 0 };
    ...
    confidence: ((div.strength * 100.0) as i32 - conf_penalty).max(0).min(100),
    ...
}
```

---

## 2. Go 层验证点

### 2.1 `signalToDecision`（engine.go line 544）

已有：
```go
case strings.HasPrefix(sig.SignalType, "sell") || strings.HasPrefix(sig.SignalType, "quasi_sell"):
    action = "open_short"
```
✅ 无需修改。

### 2.2 `entry_timing.go` `invalidStopTakeProfit`

已有对 short 的检查（SL > price > TP）：
```go
func invalidStopTakeProfit(action string, price, sl, tp float64) bool {
    // short: sl > price > tp
}
```
✅ 无需修改。

### 2.3 `SignalTypeMinRR` 配置

需确认 217 服务器 config.json 中 `signal_type_min_rr` 包含 sell 类型。若缺失，`NormalizeChanlunV2EntryZone` 的默认值应包含：
```go
"sell1": 2.0, "sell2": 1.5, "sell3": 1.2
```

---

## 3. 信号流转验证

```
Rust detect_signals → sell2/sell3 信号产出
  ↓
Go multiLevelJudgment:
  - 4h down_trend + sell signal → aligned → confidence +15 ✅
  - 4h up_trend + sell signal → countertrend → 丢弃 ✅
  ↓
evaluateParentStructureEntry:
  - RR 检查使用 SignalTypeMinRR["sell2"]=1.5 ✅
  - watch_window 16 根 15m ✅
  ↓
signalToDecision → open_short ✅
  ↓
validateChanlunV2Decisions → open gate:
  - confidence 85 (override) ≥ 82 (shortBaseMinConfidence) ✅
```

---

## 4. 不需要修改的文件

- `strategy/chanlunv2/engine.go` — sell 路由已存在
- `strategy/chanlunv2/entry_timing.go` — short 方向已支持
- `strategy/chanlunv2/position_management.go` — close_short 已支持
- `config/config.go` — NormalizeChanlunV2EntryZone 默认值需确认包含 sell

---

## 5. 改动量

| 文件 | 改动 |
|---|---|
| `chanlun_v2/src/signal.rs` | +20 行（Sell2/Sell3）+ 修改 2 行（Sell1 条件） |
| `config/config.go`（可选） | 确认 `SignalTypeMinRR` 默认包含 sell |
| 总计 | ~22 行 Rust |

---

## 6. 交叉验证修正

### 6.1 Sell2 方向约束

为避免 Sell2 和 Buy2 同时触发，Sell2 条件增加 `last_seg.direction == Direction::Down`：

```rust
if last_seg.direction == Direction::Down
    && last_seg.high < center.zg && last_seg.high > center.zd
    && current_price < last_seg.high {
    // Sell2
}
```

### 6.2 TP 保护

Sell2 的 `take_profit` 加下限保护：

```rust
take_profit: (center.low - (center.high - center.low)).max(current_price * 0.9),
```

### 6.3 验证通过项

- ✅ `invalidStopTakeProfit` 对 short 方向正确（`tp < price < sl`）
- ✅ `SignalTypeMinRR` 包含 sell1:2.0, sell2:1.5, sell3:1.2
- ✅ Go `signalToDecision` 路由 sell → open_short
- ✅ `multiLevelJudgment` 逆势过滤对 sell 正确
- ✅ Rust `SignalType` 枚举已有 Sell1/Sell2/Sell3
