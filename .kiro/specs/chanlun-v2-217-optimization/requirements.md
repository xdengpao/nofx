# 缠论V2策略217服务器优化 Requirements

## 文档信息

| 字段 | 值 |
|---|---|
| 规格名称 | chanlun-v2-217-optimization |
| 评估服务器 | `43.133.65.217:/home/ubuntu/appai2/nofx`，分支 HEAD `ddb7b268` |
| 评估窗口 | 2026-05-25 ~ 2026-05-26（24h，306 条决策日志） |
| 现役 trader | `aster_chanlun_v2`（initial_balance=100 USDT） |
| 前置修复 | `a7fcac8e fix chanlun v2 trading defects`（V1-V7 已修复） |
| 状态 | requirements 阶段 |

---

## 1. 24h 实盘事实数据

| 项 | 数值 |
|---|---|
| 决策周期 | 306 |
| `wait` | 305（99.7%） |
| `open_rejected` | 10（全部 DOGEUSDT） |
| 成功开仓 | **0** |
| 平仓 | 0 |
| 持仓 | 全程 0 |
| 账户余额 | 198.81 → 198.81（无变化） |

### 1.1 当前瓶颈分析

**瓶颈 1：entry_rr_invalid 终态（607 条，占 73%）**

BNBUSDT buy2（304 次）和 HYPEUSDT buy3（303 次）的信号被识别后，因"剩余净 RR < 2.5"立即进入终态。

根因：`MinRemainingNetRR` 默认 2.5（`NormalizeChanlunV2EntryZone` line 555），对于加密 1h 周期的 buy2/buy3 信号过于严格。

**瓶颈 2：watch_window_expired 终态（218 条，占 26%）**

SOLUSDT buy2（185 次）、DOGEUSDT buy2（20 次）、ASTERUSDT buy2（15 次）的父结构观察窗口过期。

根因：`WatchMaxCandles` 默认 8 根 15m（= 2 小时），对于 1h 级别信号等待 15m entry trigger 来说窗口太短。

**瓶颈 3：open gate 置信度要求过高（10 条 rejected）**

DOGEUSDT 的 entry trigger 成功触发（10 次 `fresh entry trigger ready`），但被 open gate 以"置信度 62-65 < 82"拒绝。

根因：`decision/open_gate.go` 中 `rangeLongMinConfidence=82`，当 ADX < 25 时标的被判定为"震荡"，要求置信度 ≥82。而缠论 V2 的 buy2 信号置信度通常在 60-70。

**瓶颈 4：信号仍在反复产出被拒（同一 trigger 10 次）**

同一个 `chanlun_v2_entry:81127866ce9f4a7ec584` 在 18:33-18:42 连续 4 次被拒，说明 entry trigger 被拒后未标记为已处理。

### 1.2 信号流转链路分析

```
Rust 缠论分析 → 识别 buy2/buy3 信号
  ↓
evaluateParentStructureEntry:
  ├─ 检查 RR → 73% 信号因 RR < 2.5 立即终态 ← 瓶颈1
  ├─ 检查 watch_window → 26% 信号因窗口过期终态 ← 瓶颈2
  └─ 通过 → detectV2EntryTrigger → trigger ready
       ↓
  validateChanlunV2Decisions → open gate:
       └─ ADX < 25 → 震荡 → 要求置信度 ≥82 → 拒绝 ← 瓶颈3
```

**结论**：信号识别正常（有 buy2/buy3），但入场链路三道关卡全部过严：
1. RR 阈值 2.5 对 buy2/buy3 不合理
2. 观察窗口 2h 对 1h 信号太短
3. open gate 的震荡置信度要求 82 与缠论 V2 信号的置信度范围（60-70）不匹配

---

## 2. 问题定性

| 编号 | 类别 | 根因 | 严重度 |
|---|---|---|---|
| W1 | RR 阈值过高 | `MinRemainingNetRR=2.5` 对 buy2/buy3 不合理，加密 1h 结构 RR 通常 1.5-2.0 | 🔴 |
| W2 | 观察窗口过短 | `WatchMaxCandles=8`（8×15m=2h），1h 信号需要更长等待 | 🟡 |
| W3 | open gate 置信度与 V2 信号不匹配 | `rangeLongMinConfidence=82` 但 V2 buy2 置信度 60-70 | 🔴 |
| W4 | entry trigger 被拒后未去重 | 同一 trigger ID 反复产出被拒 | 🟡 |

---

## 3. 用户故事与验收标准

### Requirement W1 — 按信号类型差异化 RR 阈值

**验收标准**：

1. THE 配置 SHALL 支持 `signal_type_min_rr` 按信号类型设置不同阈值：
   - buy1/sell1（趋势反转）：2.0
   - buy2/sell2（回抽确认）：1.5
   - buy3/sell3（突破延续）：1.2
2. WHEN `entry_timing.entry_zone.signal_type_min_rr` 配置存在，THE `evaluateParentStructureEntry` SHALL 使用对应信号类型的阈值而非全局 2.5。
3. WHEN 配置缺省，THE 默认 `MinRemainingNetRR` SHALL 从 2.5 降为 2.0。

### Requirement W2 — 观察窗口与交易级别对齐

**验收标准**：

1. THE `WatchMaxCandles` 默认值 SHALL 从 8 改为 16（16×15m=4h，覆盖 4 根 1h K 线）。
2. THE 配置 SHALL 支持按信号类型设置不同窗口：buy1 可更长（24），buy3 可更短（8）。
3. WHEN 窗口过期，THE 引擎 SHALL 在终态前检查是否有 entry trigger 正在形成（如 pullback 已开始但未完成），若有则延长 4 根。

### Requirement W3 — chanlun_v2 信号绕过震荡置信度惩罚

**验收标准**：

1. WHEN `decision_mode=chanlun_v2` 且信号来自缠论结构确认（非 AI 猜测），THE open gate SHALL 使用 `chanlun_v2_min_confidence`（默认 60）替代 `rangeLongMinConfidence=82`。
2. THE 配置 SHALL 支持 `strategy_risk.chanlun_v2_confidence_override` 字段，允许 V2 策略覆盖 open gate 的置信度要求。
3. WHEN ADX < 25 但缠论结构明确（有中枢+背驰确认），THE open gate SHALL 不施加震荡惩罚。

### Requirement W4 — entry trigger 被拒后标记

**验收标准**：

1. WHEN entry trigger 被 open gate 拒绝，THE 引擎 SHALL 标记该 trigger ID 为 `rejected`，下一 cycle 不再产出相同 Decision。
2. WHEN 同一 trigger 被拒 ≥3 次，THE 引擎 SHALL 把父结构标记为 `gate_blocked` 终态。
3. THE 诊断日志 SHALL 输出 `trigger_rejected_count` 而非每次重复完整拒绝原因。

---

## 4. 配置变更方案

```jsonc
"chanlun_v2_strategy": {
  "entry_timing": {
    "watch_max_candles": 16,                    // 从默认 8 改为 16
    "entry_zone": {
      "min_remaining_net_rr": 2.0,              // 从默认 2.5 改为 2.0
      "signal_type_min_rr": {                   // 新增
        "buy1": 2.0, "sell1": 2.0,
        "buy2": 1.5, "sell2": 1.5,
        "buy3": 1.2, "sell3": 1.2
      }
    }
  },
  "multi_level": {
    "min_confidence_override": 60               // 新增：覆盖 open gate 的 82
  }
}
```

---

## 5. 风险

| 风险 | 缓解 |
|---|---|
| RR 降低后开仓质量下降 | buy2/buy3 本身有中枢确认，RR 1.5 仍合理 |
| 绕过 open gate 后在震荡中亏损 | 缠论结构确认（中枢+背驰）本身就是震荡过滤 |
| 窗口延长后过期信号堆积 | 终态机制仍生效，只是延长了有效期 |

---

## 6. 度量

| 指标 | 当前 | 目标 |
|---|---|---|
| 24h 成功开仓 | 0 | ≥1 |
| entry_rr_invalid 终态占比 | 73% | ≤20% |
| watch_window_expired 占比 | 26% | ≤10% |
| open_rejected 中置信度不足 | 10/10 | 0 |
| 同一 trigger 重复被拒 | 4 次 | ≤1 |
