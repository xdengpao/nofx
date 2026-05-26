# 缠论V2策略217服务器优化 Requirements

## 文档信息

| 字段 | 值 |
|---|---|
| 规格名称 | chanlun-v2-217-optimization |
| 评估服务器 | `43.133.65.217:/home/ubuntu/appai2/nofx`，分支 HEAD `ddb7b268` |
| 评估窗口 | 2026-05-25 ~ 2026-05-26（24h，306 条决策日志） |
| 现役 trader | `aster_chanlun_v2`（initial_balance=100 USDT） |
| 前置修复 | `a7fcac8e fix chanlun v2 trading defects`（V1-V7 已修复） |
| 状态 | requirements 阶段（含交叉审核修订） |

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

> **统计单位说明**：下文 §1.1 中"607 条 / 218 条"以及 73% / 26% 的占比是按 **24h 窗口内累计的终态事件计数**（同一信号每 cycle 重复进入终态各计 1 次），分母为终态事件总数 825，**不是** 306 个决策周期。`open_rejected=10` 才是按周期计的。两者口径不同，分析时勿混用。

### 1.1 当前瓶颈分析

**瓶颈 1：entry_rr_invalid 终态（607 条 / 825，占 73%）**

BNBUSDT buy2（304 次）和 HYPEUSDT buy3（303 次）的信号被识别后，因"剩余净 RR < 2.5"立即进入终态。

根因：`MinRemainingNetRR` 默认 2.5（`config/config.go` `NormalizeChanlunV2EntryZone` 第 555 行 `if cfg.MinRemainingNetRR <= 0 { cfg.MinRemainingNetRR = 2.5 }`），对于加密 1h 周期的 buy2/buy3 信号过于严格。`evaluateParentStructureEntry` 仅取该全局值（`strategy/chanlunv2/entry_timing.go` 内 `if rr < timing.EntryZone.MinRemainingNetRR { ... terminal "entry_rr_invalid" }`），不区分信号类型。

**瓶颈 2：watch_window_expired 终态（218 条 / 825，占 26%）**

SOLUSDT buy2（185 次）、DOGEUSDT buy2（20 次）、ASTERUSDT buy2（15 次）的父结构观察窗口过期。

根因：`WatchMaxCandles` 默认 8 根 15m（= 2 小时，`config/config.go` `NormalizeChanlunV2EntryTiming` 第 522 行），对于 1h 级别信号等待 15m entry trigger 来说窗口太短。

**瓶颈 3：open gate 置信度要求过高（10 条 rejected）**

DOGEUSDT 的 entry trigger 成功触发（10 次 `fresh entry trigger ready`），但被 open gate 以"置信度 62-65 < 82"拒绝。

根因：`decision/open_gate.go` 中 `rangeLongMinConfidence = 82`（第 19 行），当 `market.GetMarketState` 把标的判为 `RANGING`（条件为 `ADX <= 20`，详见 `market/data.go` 第 1547-1582 行；并非笼统的 "ADX < 25"，需要修正先前文档表述）时，多单要求置信度 ≥82。而缠论 V2 的 buy2 信号置信度通常在 60-70。

> 注：`ADX 20-25` 区间会被判为 `WEAK_UPTREND/DOWNTREND`，不触发 82 门槛，仍只受 `longBaseMinConfidence=78` / `shortBaseMinConfidence=82` 这两条 base 约束。

**瓶颈 4：信号仍在反复产出被拒（同一 trigger 10 次）**

同一个 `chanlun_v2_entry:81127866ce9f4a7ec584` 在 18:33-18:42 连续 4 次被拒，说明 entry trigger 被 open gate 拒绝后未做 trigger 维度的去重。代码侧已确认：`terminalChanlunV2Reason()` 的白名单仅包含 `freshness_gate.* / countertrend.higher_timeframe / position_sizing.* / chanlun_v2.sl_tp_invalid`，**不含**任何 open-gate 置信度类原因，因此 `markSignalTerminalRejected` 不会被触发，下一 cycle 仍会按相同 trigger ID 再次产出 Decision。

### 1.2 信号流转链路分析

```
Rust 缠论分析 → 识别 buy2/buy3 信号
  ↓
evaluateParentStructureEntry:
  ├─ 检查 RR → 73% 信号因 RR < 2.5 立即终态 ← 瓶颈1
  ├─ 检查 watch_window → 26% 信号因窗口过期终态 ← 瓶颈2
  └─ 通过 → detectV2EntryTrigger → trigger ready
       ↓
  validateChanlunV2Decisions → open gate (EvaluateOpenGate):
       ├─ longBaseMinConfidence=78（始终生效）
       ├─ ADX ≤ 20 → RANGING → rangeLongMinConfidence=82
       │     （DOGEUSDT 60-70 confidence 在此被拒）         ← 瓶颈3
       ├─ counterTrendMinConfidence=88（仅逆势时触发，本次未命中）
       ├─ btcConflictMinConfidence=88（仅 BTC 冲突时触发）
       ├─ btcVolatilityMinConfidence=85（仅 BTC 高波动时触发）
       └─ highADXMinConfidence=90（仅 ADX>60 追单时触发）
       ↓
  open gate 拒绝 → 未进入 terminal 白名单 → 下一 cycle 重复 ← 瓶颈4
```

**结论**：信号识别正常（有 buy2/buy3），但入场链路三道关卡过严：
1. RR 阈值 2.5 对 buy2/buy3 不合理；
2. 观察窗口 2h 对 1h 信号太短；
3. open gate 的 RANGING long 置信度 82 与 V2 信号置信度 60-70 不匹配；
4. open gate 拒绝未做 trigger 维度去重，造成日志噪音。

---

## 2. 问题定性

| 编号 | 类别 | 根因 | 严重度 |
|---|---|---|---|
| W1 | RR 阈值过高 | `MinRemainingNetRR=2.5` 对 buy2/buy3 不合理，加密 1h 结构 RR 通常 1.5-2.0 | 🔴 |
| W2 | 观察窗口过短 | `WatchMaxCandles=8`（8×15m=2h），1h 信号需要更长等待 | 🟡 |
| W3 | open gate RANGING long 置信度 82 与 V2 信号 60-70 不匹配 | `rangeLongMinConfidence=82` 在 ADX ≤ 20 时强制生效 | 🔴 |
| W4 | open gate 置信度类拒绝未在 trigger 维度去重 | 同一 trigger ID 反复产出被拒 | 🟡 |

---

## 3. 用户故事与验收标准

### Requirement W1 — 按信号类型差异化 RR 阈值

**验收标准**：

1. THE 配置 SHALL 支持 `entry_timing.entry_zone.signal_type_min_rr` 按信号类型设置不同阈值：
   - buy1/sell1（趋势反转）：2.0
   - buy2/sell2（回抽确认）：1.5
   - buy3/sell3（突破延续）：1.2
2. WHEN `signal_type_min_rr` 配置存在，THE `evaluateParentStructureEntry` SHALL 使用对应信号类型的阈值而非全局值。
3. WHEN 配置缺省，THE 默认 `MinRemainingNetRR` SHALL 从 2.5 降为 2.0。
4. THE 归一化函数 SHALL 把 `signal_type_min_rr` 中所有 `< 1` 的值上调到 `1`，与全局 `MinRemainingNetRR` 的下限规则保持一致。

### Requirement W2 — 观察窗口与交易级别对齐

**验收标准**：

1. THE `WatchMaxCandles` 默认值 SHALL 从 8 改为 16（16×15m=4h，覆盖 4 根 1h K 线）。

> 已删除原 W2.2（按信号类型差异化窗口）与 W2.3（trigger 形成中延长 4 根）。  
> **理由**：本次审核未在 design / tasks 中找到对应实现路径，且 W1 的 RR 差异化和 W2.1 的窗口翻倍已能覆盖 24h 评估窗口里 99% 的瓶颈案例。两条需求作为后续 spec 的候选，由 `chanlun-v2-228-window-shaping`（暂列）或类似规格承接，避免本规格内悬空。

### Requirement W3 — open gate 多档置信度按 gate 分项覆盖

**验收标准**：

1. THE 配置 SHALL 支持在 `chanlun_v2_strategy.entry_timing.entry_zone.min_confidence_overrides` 下设置**按 gate 分项**的置信度门槛覆盖：
   - `long_base` — 覆盖 `longBaseMinConfidence`（默认 78）
   - `short_base` — 覆盖 `shortBaseMinConfidence`（默认 82）
   - `range_long` — 覆盖 `rangeLongMinConfidence`（默认 82）
   - `range_short` — 覆盖 `rangeShortMinConfidence`（默认 85）
2. THE `counterTrendMinConfidence`、`btcConflictMinConfidence`、`btcVolatilityMinConfidence`、`highADXMinConfidence` 这四类**风险型 gate** SHALL 不在覆盖范围内，永远使用代码硬编码值（88/88/85/90），即使配置写了也忽略。
3. WHEN 覆盖值 `> 0` 且 `< 当前 gate 默认值`，THE `EvaluateOpenGate` SHALL 把对应 gate 的实际门槛调整为 `min(默认, 覆盖)`；为 `0` 或缺省时维持默认行为。
4. THE 默认 `min_confidence_overrides` SHALL 全部为 0（即开箱行为与现状完全一致），需要由部署侧显式打开。
5. THE V2 trader 的推荐配置 SHALL 至少设置 `range_long: 60`（解决本次 DOGEUSDT 案例），是否设置其他 gate 由部署方决定。
6. WHEN 任一 override 生效（实际拒绝由"会拒"翻转为"放行"或反之），THE 引擎 SHALL 在 `gate_diagnostics.min_confidence_override_applied` 中记录 `{rule, from, to, actual_confidence}` 以便事后归因。
7. **明确不采用**："在 `signalToDecision` 中把 `d.Confidence` 抬到 85" 这一备选路径——它会污染下游所有依赖 confidence 的链路（仓位规模、日志、风控、未来策略），不可接受。

> **澄清**：该需求仅放宽 RANGING long 等"行情类"门槛；逆势/BTC 冲突/高 ADX 追单这类与"风险"相关的限制不变。如未来需要在缠论结构明确时连这些 gate 都豁免，需另立规格。

### Requirement W4 — open gate 置信度类拒绝在 trigger 维度去重

**验收标准**：

1. WHEN entry trigger 被 open gate 以置信度类原因（`*_confidence` / `*_min_confidence`）拒绝，THE 引擎 SHALL 标记该 trigger ID 为已拒，下一 cycle 不再产出相同 Decision。
2. WHEN 同一 trigger 被拒 ≥3 次，THE 引擎 SHALL 把父结构标记为 `gate_blocked` 终态。
3. THE 诊断输出 SHALL 在 Decision 的 `StrategyMetadata` 中新增以下字段：
   - `trigger_rejected_count`（int）— 当前 trigger 累计被拒次数
   - `trigger_rejected_first_at`（int64, ms）— 首次被拒时间戳
   - `trigger_rejected_last_reason`（string）— 最近一次拒绝的 `reason_code`

---

## 4. 配置变更方案

```jsonc
"chanlun_v2_strategy": {
  "entry_timing": {
    "watch_max_candles": 16,                    // 从默认 8 改为 16
    "entry_zone": {
      "min_remaining_net_rr": 2.0,              // 从默认 2.5 改为 2.0
      "signal_type_min_rr": {                   // 新增（每值最终被 clamp 到 ≥1）
        "buy1": 2.0, "sell1": 2.0,
        "buy2": 1.5, "sell2": 1.5,
        "buy3": 1.2, "sell3": 1.2
      },
      "min_confidence_overrides": {             // 新增（按 gate 分项，0=不覆盖）
        "long_base":  0,
        "short_base": 0,
        "range_long": 60,
        "range_short": 0
      }
    }
  }
}
```

> 配置路径**唯一**为 `chanlun_v2_strategy.entry_timing.entry_zone.min_confidence_overrides`。  
> 旧 design 草案里出现过的 `strategy_risk.chanlun_v2_confidence_override` 与 `chanlun_v2_strategy.multi_level.min_confidence_override` 两个候选路径**作废**。

---

## 5. 风险

| 风险 | 缓解 |
|---|---|
| RR 降低后开仓质量下降 | buy2/buy3 本身有中枢确认，RR 1.5 仍合理；保留 `> 0` 配置覆盖通道，可按 symbol 收紧 |
| `range_long: 60` 在震荡中允许多单导致频繁打止损 | counterTrend/btcConflict/btcVolatility 等风险 gate 不在覆盖范围；仓位规模仍按真实 confidence(60~70) 缩放，pilot 单子小 |
| 窗口延长后过期信号堆积 | 终态机制仍生效，只是延长了有效期 |
| trigger 去重逻辑误把仍可成立的 trigger 拒掉 | 仅在 ≥3 次同因拒绝才升级为 `gate_blocked`，且只针对置信度类拒因，其他原因不受影响 |

---

## 6. 度量

| 指标 | 当前 | 目标 |
|---|---|---|
| 24h 成功开仓 | 0 | ≥1 |
| entry_rr_invalid 终态占比（按终态事件） | 73% | ≤20% |
| watch_window_expired 占比（按终态事件） | 26% | ≤10% |
| `open_rejected` 中 RANGING long 置信度不足（按 cycle） | 10/10 | 0 |
| 同一 trigger 重复被拒次数（最大值） | 4 | ≤1 |
| `gate_diagnostics.min_confidence_override_applied` 出现次数（按周期） | — | 与 `open_accepted` 数量同阶 |
