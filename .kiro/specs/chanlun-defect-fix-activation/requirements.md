# 程序化缠论策略第二轮优化 Requirements

## 文档信息

| 字段 | 值 |
|---|---|
| 规格名称 | chanlun-defect-fix-activation |
| 前置 spec | `chanlun-live-trading-defect-fix`（代码已实现，commit `c337d88`） |
| 评估窗口 | 2026-05-21 20:17 ~ 2026-05-22 08:17（CST），部署后 12 小时 |
| 现役 trader | `aster_deepseek` (Aster DEX, decision_mode=programmatic) |
| 状态 | 进行中 |

---

## 1. 背景

commit `c337d88 Fix programmatic Chanlun live trading gates` 已实现 `chanlun-live-trading-defect-fix` spec 的全部代码（P0-P3），包括：

- ✅ `fastSkipSuppressed` — 已生效（抑制循环消除）
- ✅ `IsBornInvalid` — 已生效（58 条出生无效丢弃）
- ✅ `candidateGovernor` — 已生效（476 条非加密剔除 + 139 条价差剔除）
- ✅ `wait_reason_summary` — 部分生效（226 条 `no_trigger`、12 条 `no_signal`）
- ✅ `effectivePilotMinConfidence` — 代码存在但未激活
- ✅ `direct_structure` entry_path — 代码存在但未激活
- ✅ `loosen_mode` — 代码存在但未激活
- ✅ `accountSizeGate` — 代码存在但未激活
- ✅ `daily_summary` — 代码存在

**但 config.json 未启用 `defect_fix_pack_enabled`，且显式设置了旧阈值（pilot_min_confidence=90、min_remaining_net_rr=2.5、direct_structure_open=false），导致新功能全部未激活。**

### 1.1 部署后 12h 事实数据

| 项 | 数值 | 对比修复前 24h |
|---|---|---|
| 决策周期 | 240 | 481（比例一致） |
| `wait` | 240（100%） | 480（99.8%） |
| `open_rejected` | **0** | 6 |
| 真实开仓 | **0** | 0 |
| 持仓 | 全程 0 | 全程 0 |
| 账户余额 | 44.42 → 44.42 | 44.18 → 44.42 |
| `wait_reason_summary=no_trigger` | 226（94%） | N/A（字段不存在） |
| `wait_reason_summary=no_signal` | 12（5%） | N/A |
| pilot 跳过 | 16 条 | 954 条 |
| 出生无效丢弃 | 58 条 | 0（功能不存在） |
| 非加密剔除 | 476 条 | 0 |
| 价差过高剔除 | 139 条 | 0 |

### 1.2 当前瓶颈分析

| 瓶颈 | 原因 | 影响 |
|---|---|---|
| **94% 停在 `no_trigger`** | `direct_structure_open=false` + `require_fresh_trigger=true`，信号识别后必须等 15m fresh trigger，但 trigger 从未出现 | 所有信号无法进入 entry_zone |
| **pilot 仍用 90 阈值** | config 显式设 `pilot_min_confidence=90`，代码 `normalizeRuntimePreviewSignals` 只在字段为 0 时才用新默认值 | 实际信号 60-85 全部被卡 |
| **min_remaining_net_rr=2.5** | config 显式设 2.5，代码只在字段为 0 时才用 2.0 | 即使进入 guard 也会被 RR 拒绝 |
| **loosen_mode 未启用** | config 中 `loosen_mode={}` 空对象，`Enabled` 默认 false | 12h 无开仓也不会自动放宽 |
| **XAG/XAU 仍在候选** | `isCryptoUSDT` 黑名单已包含 XAU/XAG，governor 已剔除（476 条），但日志中仍有 `XAGUSDT sell2 pilot跳过` | governor 在 preview 层之后执行？或 preview 在 governor 之前？ |

### 1.3 代码逻辑确认

`normalizeRuntimePreviewSignals` 逻辑：
```go
if policy.PilotMinConfidence <= 0 {
    if defectFixPackEnabled { policy.PilotMinConfidence = 70 }
    else { policy.PilotMinConfidence = 90 }
}
```

config.json 已显式设 `pilot_min_confidence: 90`（非零），所以 normalize 不会覆盖。**必须在 config.json 中显式修改或删除该字段**。

同理 `MinRemainingNetRR`、`DirectStructureOpen`、`RequireFreshTrigger` 都是 config 显式设值，normalize 不会覆盖。

---

## 2. 问题定性

| 编号 | 类别 | 根因 |
|---|---|---|
| A1 | 配置未激活 | `defect_fix_pack_enabled` 未设置，默认 false |
| A2 | 显式旧值覆盖新默认 | config 显式设 `pilot_min_confidence=90`、`min_remaining_net_rr=2.5`、`direct_structure_open=false` |
| A3 | loosen_mode 未启用 | config 中 `loosen_mode={}` 空对象 |
| A4 | governor 与 preview 执行顺序 | XAG/XAU 被 governor 剔除但仍出现在 pilot 跳过日志中 |
| A5 | 代码 normalize 逻辑缺陷 | 当 `DefectFixPackEnabled=true` 时应强制覆盖关键阈值，而非仅在零值时覆盖 |

---

## 3. 范围

### 3.1 本规格覆盖

1. **config.json 配置更新**：启用 `defect_fix_pack_enabled=true`，调整关键阈值。
2. **代码修复**：`DefectFixPackEnabled=true` 时应强制覆盖 `PilotMinConfidence`、`MinRemainingNetRR`、`DirectStructureOpen`，而非仅在零值时覆盖。
3. **governor 执行顺序修复**：确保 `candidateGovernor` 在 `evaluatePreviewSignals` 之前过滤候选。
4. **loosen_mode 配置激活**。
5. **部署后验证**。

### 3.2 不覆盖

- 缠论算法本身的修改。
- 新增功能。

---

## 4. 用户故事与验收标准

### Requirement A1 — 配置激活与阈值强制覆盖

**用户故事**：作为运维，我希望 `defect_fix_pack_enabled=true` 时系统自动使用优化后的阈值，即使 config.json 中存在旧的显式值。

**验收标准**：

1. WHEN `defect_fix_pack_enabled=true`，THE `normalizeRuntimePreviewSignals` SHALL 把 `PilotMinConfidence` 强制设为 `min(config_value, 70)` 而非仅在零值时覆盖。
2. WHEN `defect_fix_pack_enabled=true`，THE `normalizeRuntimeEntryTiming` SHALL 把 `DirectStructureOpen` 强制设为 `true`，把 `MinRemainingNetRR` 强制设为 `min(config_value, 2.0)`。
3. WHEN `defect_fix_pack_enabled=true` 且 `PilotMinConfidenceUseP75=true`，THE `effectivePilotMinConfidence` SHALL 使用 P75 动态计算结果（已实现，只需激活）。
4. THE config.json SHALL 新增 `"defect_fix_pack_enabled": true` 并删除或注释掉 `pilot_min_confidence: 90`。

### Requirement A2 — loosen_mode 激活

**用户故事**：作为运维，我希望 12h 无开仓后系统自动进入 loosen_mode 放宽阈值。

**验收标准**：

1. THE config.json `trading_frequency.loosen_mode` SHALL 设为：
   ```json
   { "enabled": true, "inactivity_window_minutes": 720, "pilot_confidence_drop": 10, "min_net_rr_delta": -0.4, "max_chase_ratio_bump": 0.05, "max_duration_hours": 24, "hard_floor_pilot_confidence": 60 }
   ```
2. WHEN 部署后 12h 无开仓，THE 系统 SHALL 自动进入 loosen_mode 并在日志中标记 `active_mode=loosen`。

### Requirement A3 — governor 执行顺序修复

**用户故事**：作为开发者，我希望被 candidateGovernor 剔除的标的不再出现在 preview/pilot 评估中。

**验收标准**：

1. WHEN `candidateGovernor` 把某 symbol 标记为 `non_crypto_symbol` 或 `quote_spread_too_high`，THE `evaluatePreviewSignals` SHALL 跳过该 symbol，不再产生 pilot 跳过日志。
2. THE 修复后日志中不应出现 `XAGUSDT/XAUUSDT pilot跳过` 或 `XAGUSDT/XAUUSDT 预览层` 消息。

### Requirement A4 — 部署后验证

**验收标准**：

1. 部署后 6h 内 `wait_reason_summary=no_trigger` 占比 SHALL 从 94% 降到 ≤50%。
2. 部署后 12h 内 SHALL 产生 ≥1 次真实开仓。
3. 部署后 24h 内 `active_mode` SHALL 至少出现一次 `loosen`（如果前 12h 仍无开仓）。

---

## 5. 风险

| 风险 | 缓解 |
|---|---|
| 阈值放宽后产生劣质开仓 | `max_daily_loss=10%` 硬熔断不变；首次开仓后 loosen 自动退出 |
| `direct_structure_open=true` 引入噪音 | `DirectStructureMinConfidence=70` 过滤；`signal_freshness` guard 仍生效 |
| config 修改后需重启 | 确认 nofx 不支持热更新，需 kill + restart |

---

## 6. 度量

| 指标 | 当前（12h） | 目标（修复后 24h） |
|---|---|---|
| 真实开仓 / 日 | 0 | ≥1 |
| `no_trigger` 占比 | 94% | ≤30% |
| `pilot_below_threshold` 占比 | 7% | ≤10% |
| loosen_mode 激活 | 未激活 | 12h 无开仓后自动激活 |
| XAG/XAU 出现在 pilot 日志 | 是 | 否 |
