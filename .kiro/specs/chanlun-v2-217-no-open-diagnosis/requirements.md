# 217 Chanlun V2 No-Open Diagnosis Requirements

## 文档信息

| 字段 | 值 |
|---|---|
| 规格名称 | `chanlun-v2-217-no-open-diagnosis` |
| 服务器 | `43.133.65.217:/home/ubuntu/appai2/nofx` |
| 远端 HEAD | `b546e4c1d` |
| Trader | `aster_chanlun_v2` |
| 取证窗口 | 2026-05-27 21:32 ~ 2026-05-29 21:32 CST，960 个决策周期 |
| 状态 | 取证完成，修复待确认 |

## 1. 实盘事实

最近 48 小时内，`aster_chanlun_v2` 服务持续运行，最近日志时间为 2026-05-29 21:28:09 CST。账户和风控状态正常：

- `position_count=0`
- `allocated_balance=100`
- `remaining_risk_budget=0.08`
- `open_count_24h=0`
- `inactivity_minutes=1728`
- `loss_mode.active=false`

决策统计：

| 指标 | 数值 |
|---|---:|
| 决策周期 | 960 |
| `wait` 决策 | 960 |
| 成功开仓 | 0 |
| 平仓 | 0 |
| `open_rejected` | 6 |
| 有 raw/parent 信号的周期 | 960 |
| 产生 fresh entry trigger 的周期 | 6 |

## 2. 未开仓原因

### R0.1 多数周期卡在父结构 RR

最近 48 小时只有 15 次新的父结构直接终止，其余大量出现的是同一终态信号的静默复述。直接终止样本：

| 信号 | 次数 | 阈值 | RR 样本 |
|---|---:|---:|---|
| `ETHUSDT sell3` | 4 | 1.20 | 0.48, 0.55, 0.54, 0.53 |
| `SOLUSDT sell2` | 4 | 1.50 | 1.12, 1.11, 1.01, 1.12 |
| `HYPEUSDT buy3` | 3 | 1.20 | 0.27, 0.57, 0.24 |
| `ADAUSDT sell2` | 3 | 1.50 | 0.92, 1.19, 1.22 |
| `DOGEUSDT sell2` | 1 | 1.50 | 1.41 |

静默复述计数显示长期主因仍是 `entry_rr_invalid`：

| 静默原因 | 48h 出现次数 |
|---|---:|
| `entry_rr_invalid` | 2766 |
| `entry_parent.watch_window_expired` | 330 |

解释：当前 `entry_timing.entry_zone.signal_type_min_rr` 已生效，日志中 2026-05-29 的直接终止阈值已经是 `sell2=1.5`、`buy3/sell3=1.2`，不是旧的全局 2.5。未开仓不是配置未加载，而是多数信号出现时剩余 RR 已低于差异化阈值。

### R0.2 唯一可执行方向是 BNB 多单，但被 BTC 高周期硬 veto

2026-05-28 16:09 ~ 16:27 CST 出现 6 次 `BNBUSDT buy2` entry trigger。配置中的 `long_base=60` 与 `range_long=60` override 已生效，日志中出现：

- `min_confidence_override_applied: {rule: "range_long", from: 82, to: 60}`
- 触发信号置信度为 62 或 70

但这 6 次仍全部被 open gate 拒绝，拒因一致：

- 标的处于震荡/波动收缩，多单需更高置信度
- `BNBUSDT 1h ADX 35.0` 但 DI 方向与开仓方向不一致，report-only
- `BTC 1h/4h 明显转弱，禁止新开高 beta 山寨多单`

代码路径在 `decision/open_gate.go`：

- `applyDirectionalConfidenceGate()` 允许配置降低 `long_base/range_long`
- `applyBTCMultiTimeframeGate()` 对所有非 BTC/ETH 多单执行 BTC 1h/4h 确认转弱硬阻断
- `isHighBetaAltcoin()` 当前定义为 symbol 不是 `BTCUSDT` 且不是 `ETHUSDT`

因此 `min_confidence_overrides` 只能解决行情类置信度 gate，不能绕过 BTC 高周期硬 veto。

### R0.3 现有 loosen mode 没有作用到 Chanlun V2

`risk_state.frequency_policy.loosen_mode.enabled=true`，且最近日志显示 `inactivity_minutes=1728`，已经远超 720 分钟窗口。但 `active_mode` 仍是 `balanced`。代码搜索显示 loosen mode 的执行逻辑在 `strategy/chanlun`，没有接入 `strategy/chanlunv2`。

结论：当前 V2 在长时间不开仓后不会自动降低 RR、触发器或置信度门槛。

## 3. 需求

### Requirement 1 - 可复现的 217 no-open 报告

THE 系统 SHALL 提供一条可重复运行的日志分析路径，输出最近 N 小时内：

- 周期数、action/final_action/trade_intent 分布；
- 成功开仓、开仓拒绝、平仓、失败执行日志数量；
- V2 父结构直接终止、终态静默、waiting、trigger ready 的分类统计；
- open gate 拒因按 symbol、reason、gate diagnostics 聚合；
- 最近样本文件名与时间。

WHEN 分析 `aster_chanlun_v2` 最近 48 小时日志，THE 报告 SHALL 能复现本 spec §1 和 §2 的核心数字。

### Requirement 2 - Chanlun V2 支持 inactivity loosen

WHEN `frequency_policy.loosen_mode.enabled=true` 且 trader 超过 `inactivity_window_minutes` 未成功开仓，THE Chanlun V2 engine SHALL 进入 `loosen` 运行态。

THE loosen 运行态 SHALL 至少支持：

- 对 `entry_zone.signal_type_min_rr` 应用 `min_net_rr_delta`，但最终阈值不得低于 1.0；
- 对 `entry_zone.max_chase_ratio` 应用 `max_chase_ratio_bump`，但不得超过既有上限；
- 对 entry trigger 置信度应用 `pilot_confidence_drop`，但不得低于 `hard_floor_pilot_confidence`。

THE loosen 运行态 SHALL 对 V2 所有 RR 检查使用一致的 effective RR 阈值，包括父结构检查、entry trigger 检查和 `applyChanlunV2FreshnessGuard()` 中的剩余 RR 二次检查，避免父结构已放行的候选又被更高的全局 freshness RR 阈值重新拒绝。

THE loosen 运行态 SHALL NOT 绕过：

- BTC 1h/4h confirmed bearish hard veto；
- stop loss / take profit 结构校验；
- target crossed；
- account/risk budget；
- same-side exposure gate；
- exchange preflight。

### Requirement 3 - V2 short-side threshold tuning

THE 217 推荐配置 SHALL 优先放宽 bearish 环境中的短侧机会，而不是在 BTC 高周期转弱时强行打开山寨多单。

推荐灰度配置：

```json
{
  "chanlun_v2_strategy": {
    "entry_timing": {
      "entry_zone": {
        "signal_type_min_rr": {
          "buy1": 2.0,
          "sell1": 2.0,
          "buy2": 1.5,
          "sell2": 1.1,
          "buy3": 1.2,
          "sell3": 1.2
        },
        "min_confidence_overrides": {
          "long_base": 60,
          "short_base": 65,
          "range_long": 60,
          "range_short": 65
        }
      }
    }
  }
}
```

WHEN 该配置生效，近期类似 `DOGEUSDT sell2 RR=1.41`、`ADAUSDT sell2 RR=1.22`、`SOLUSDT sell2 RR=1.12` 的父结构 SHALL 不再直接因 `sell2` RR 低于 1.5 终止，而应进入等待 entry trigger 或后续 open gate。

WHEN 这些候选进入 entry trigger 或 freshness guard，THE 后续 RR 检查 SHALL 使用同一个 `sell2=1.1` effective 阈值，而不是回退到 `strategy_risk` 或 `signal_freshness` 的更高全局阈值。

### Requirement 4 - BTC gate 行为必须显式

THE 解决方案 SHALL 明确保留当前 BTC 1h/4h confirmed bearish hard veto，除非另立 spec 显式要求把 BTC 多周期 gate 改成 `block|penalize|report_only` 可配置。

IF 将来引入 BTC gate 模式配置，THEN 默认值 SHALL 保持 `block`，并且 217 实盘不得在未确认前切到 `report_only`。

## 4. 验收指标

| 指标 | 当前 | 修复后目标 |
|---|---:|---:|
| 最近 48h 成功开仓 | 0 | >=1 |
| `open_rejected` 中 BTC hard veto 未识别 | 6/6 | 0/全部结构化识别 |
| V2 inactivity 后 `active_mode` | `balanced` | `loosen` |
| `sell2 RR 1.1~1.5` 直接终止 | 会终止 | 进入等待 trigger 或后续 gate |
| 山寨多单在 BTC confirmed bearish 下被放行 | 0 | 0，保持硬保护 |
