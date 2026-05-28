# 缠论V2做空信号缺失修复 Requirements

## 文档信息

| 字段 | 值 |
|---|---|
| 规格名称 | chanlun-v2-short-signals |
| 评估服务器 | `43.133.65.217:/home/ubuntu/appai2/nofx`，HEAD `afee0c88` |
| 评估窗口 | 近 24h，480 条决策日志 |
| 状态 | cross-validation complete，等待执行任务 |

---

## 1. 问题

24h 内 **零条 sell/short 信号**。所有 3876 条"无买卖点信号"和 841 条终态信号全部是 buy 方向。系统完全无法做空。

### 1.1 根因

`chanlun_v2/src/signal.rs` 中 `detect_signals` 函数：

- **Sell1（一类卖点）**：已实现，但条件是 `DivergenceType::Top + trend == UpTrend`。由于 MACD 背驰检测依赖真实 MACD 数据质量，且需要上涨趋势中的顶背驰，触发条件苛刻。
- **Sell2（二类卖点）**：**未实现**。代码中只有 Buy2 的逻辑（`last_seg.low > center.zd && last_seg.low < center.zg`），没有对称的 Sell2。
- **Sell3（三类卖点）**：**未实现**。代码中只有 Buy3 的逻辑（`last_seg.low > center.zg`），没有对称的 Sell3。

### 1.2 影响

- 单边做多策略在下跌市场中完全无法盈利
- 错过所有做空机会（当前多个标的处于下跌趋势）
- 与缠论理论不符（缠论买卖点是对称的）

---

## 2. 用户故事与验收标准

### Requirement S1 — 实现 Sell2（二类卖点）

**定义**：下跌趋势中，价格反弹至中枢区间内但未突破中枢上沿（ZG），随后再次下跌。

**验收标准**：

1. WHEN 最后一个线段方向为下跌（`last_seg.direction == Direction::Down`），高点在中枢 ZD 和 ZG 之间（`last_seg.high < center.zg && last_seg.high > center.zd`），且当前价格低于该线段高点，THE Rust 库 SHALL 产出 `sell2` 信号。
2. THE `sell2` 信号 SHALL 包含：
   - `direction = "short"`
   - `stop_loss = center.zg`（中枢上沿）
   - `take_profit = max(center.low - (center.high - center.low), current_price * 0.9)`（中枢下方等距目标，最多不超过当前价下方 10%）
   - `confidence = 70`
3. THE Rust 库 SHALL NOT 在同一最后线段上同时产出对称的 `buy2` 和 `sell2`；为此 `buy2` SHALL 仅在 `last_seg.direction == Direction::Up` 时触发，`sell2` SHALL 仅在 `last_seg.direction == Direction::Down` 时触发。
4. THE Go 层 SHALL 正确路由 `sell2` 为 `open_short` 决策。

### Requirement S2 — 实现 Sell3（三类卖点）

**定义**：价格跌破中枢下沿（ZD）后不再回到中枢区间内。

**验收标准**：

1. WHEN 最后一个线段方向为下跌（`last_seg.direction == Direction::Down`），最后一个线段的高点低于中枢 ZD（`last_seg.high < center.zd`），且当前价格仍低于中枢 ZD（`current_price < center.zd`），THE Rust 库 SHALL 产出 `sell3` 信号。
2. THE `sell3` 信号 SHALL 包含：
   - `direction = "short"`
   - `stop_loss = center.zd`（中枢下沿）
   - `take_profit = current_price - (center.zg - center.zd)`（中枢区间等距下方）
   - `confidence = 65`
3. THE Rust 库 SHALL NOT 在当前价格已经回到 ZD 上方时产出新的 `sell3`，避免过期三卖变成无效父结构。
4. THE Go 层 SHALL 正确路由 `sell3` 为 `open_short` 决策。

### Requirement S3 — Sell1 条件放宽

**验收标准**：

1. WHEN `DivergenceType::Top` 被检测到，THE Rust 库 SHALL 在 `UpTrend` 和 `Consolidation` 两种走势类型下都产出 `sell1` 信号（当前仅 UpTrend）。
2. THE `sell1` 在 `Consolidation` 中的置信度 SHALL 降低 15（`strength * 100 - 15`），并 clamp 到 `[0, 100]`。

### Requirement S4 — Go 层 entry_timing 支持 sell 信号

**验收标准**：

1. THE `evaluateParentStructureEntry` SHALL 对 sell 信号使用相同的入场逻辑（当前代码已支持，但需验证 `signalActionForDirection` 对 short 的处理）。
2. THE `SignalTypeMinRR` 配置 SHALL 包含 sell1/sell2/sell3 的阈值（与 buy 对称）。
3. THE `multiLevelJudgment` 中逆势过滤 SHALL 对 sell 信号在 4h 上涨趋势中降低置信度（已实现，需验证）。

---

## 3. 实现方案

### 3.1 Rust `signal.rs` 修改

在 `detect_signals` 函数的"二类/三类买卖点"部分，新增 Sell2 和 Sell3，并引入 `Direction`：

```rust
use crate::kline::Direction;

// 二类卖点：反弹不破中枢上沿
if last_seg.direction == Direction::Down
    && last_seg.high < center.zg
    && last_seg.high > center.zd
    && current_price < last_seg.high {
    signals.push(Signal {
        signal_type: SignalType::Sell2,
        direction: "short".into(),
        price: current_price,
        stop_loss: center.zg,
        take_profit: (center.low - (center.high - center.low)).max(current_price * 0.9),
        confidence: 70,
        center_id: Some(center.id),
        divergence_strength: 0.0,
        timestamp: last_seg.end_time,
    });
}

// 三类卖点：跌破中枢不回
if last_seg.direction == Direction::Down
    && last_seg.high < center.zd
    && current_price < center.zd {
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

### 3.2 Sell1 条件放宽

```rust
crate::divergence::DivergenceType::Top if trend == TrendType::UpTrend || trend == TrendType::Consolidation => {
    let conf_penalty = if trend == TrendType::Consolidation { 15 } else { 0 };
    let sl = seg.high * 1.02;
    let tp = center.map(|c| c.zd).unwrap_or(seg.low);
    signals.push(Signal {
        signal_type: SignalType::Sell1,
        direction: "short".into(),
        confidence: ((div.strength * 100.0) as i32 - conf_penalty).max(0).min(100),
        ...
    });
}
```

---

## 4. 风险

| 风险 | 缓解 |
|---|---|
| Sell2/Sell3 在上涨市场中误触发 | Go 层 `multiLevelJudgment` 已有逆势过滤（4h 上涨时 sell 信号被丢弃） |
| SL/TP 计算不合理 | 使用中枢 ZG/ZD 作为止损，与缠论理论一致 |

---

## 5. 度量

| 指标 | 当前 | 目标 |
|---|---|---|
| 24h sell/short 信号数 | 0 | ≥5 |
| sell 信号通过 entry_timing | 0 | ≥1 |
| 成功 open_short | 0 | ≥1（在下跌标的上） |
