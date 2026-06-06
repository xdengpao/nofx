# 缠论V2策略交易缺陷修复 Requirements

## 文档信息

| 字段 | 值 |
|---|---|
| 规格名称 | chanlun-v2-trading-defects |
| 评估对象 | `aster_chanlun_v2` (Aster DEX, `decision_mode=chanlun_v2`) |
| 评估来源 | 161 服务器 `/home/ubuntu/appai3/nofx/decision_logs/aster_chanlun_v2` 只读交叉验证 |
| 评估窗口 | 2026-05-23 14:56:12 ~ 2026-05-25 09:47:44 |
| 实施范围 | 仅优化 `strategy/chanlunv2`、`chanlun_v2` Rust 输入、V2 配置与 V2 执行回调 |
| 明确排除 | 不修改 V2 以外的策略包、状态、配置和行为 |
| 状态 | requirements/design/tasks 已按交叉验证修订 |

---

## 1. 交叉验证后的实盘事实

| 项 | 数值 |
|---|---|
| 决策日志文件 | 861 |
| `wait` 动作 | 839 |
| `open_rejected` 动作 | 324 |
| `open_long` 动作 | 29 |
| 真正成功开仓 | 2（BNBUSDT 1 次、SOLUSDT 1 次） |
| 执行失败开仓 | 27（全部为 `仓位大小必须>0`） |
| 主动平仓 | 0 |
| 自动平仓 | 1 次 `auto_close_long` |
| 账户余额 | 44.42 -> 44.29 USDT |
| 最新持仓样本 | SOLUSDT long entry=86.36 mark=85.27 浮亏约 -0.18 |

补充口径：原文档写的 `2026-05-23 17:58 ~ 2026-05-25 09:47` 实际只有约 39h50m，该子窗口内有 800 个 decision 文件、785 个 `wait` 动作、324 个 `open_rejected` 动作、29 个 `open_long` 动作。接近 860 周期的口径应从 2026-05-23 14:56:12 起算。

### 1.1 关键证据

**D1：同一可执行信号重复进入开仓链路**

```
2026-05-23T17:58:36 CLUSDT open_long buy2 signal_id=chanlun_v2:CLUSDT:1h:buy2:1779512399999
2026-05-23T18:01:36 CLUSDT open_long 同一 signal_id
2026-05-23T18:04:36 CLUSDT open_long 同一 signal_id
... CLUSDT 重复 13 次，BNBUSDT 同一 buy2 重复 13 次
```

这 29 个 `open_long` 动作中只有 2 个成功，其余 27 个是 zero-size 执行失败；问题不是“29 次成功开仓”，而是同一 V2 signal_id 反复进入执行层。

**D2：zero-size 开仓没有在执行前转为结构化拒绝**

27 个失败 `open_long` 的错误均为 `仓位大小必须>0`。这类确定性 sizing/preflight 失败应在 V2 validation 阶段转为 `open_rejected`，不应进入交易所执行路径，也不应记录为失败开仓。

SL/TP/Confidence 交叉验证结论：`decision_json` 中 29 个 open action 均有非零 `stop_loss`、`take_profit` 和 `confidence`。因此本缺陷不再定义为 `signalToDecision` 未映射字段，而定义为 V2 zero-size validation、执行日志字段和 SL/TP 兜底语义不完整。

**D3：非加密标的进入 V2 候选与分析**

在上述窗口内：

| 标的 | 出现在 `candidate_coins` | `included_in_prompt=true` |
|---|---:|---:|
| CLUSDT | 330 | 317 |
| XAUUSDT | 283 | 270 |

这些标的不应进入 V2 新开仓分析候选。若已有仓位存在，只允许进入风险降低管理，不允许生成新开仓。

**D4：逆势信号重复进入 open gate**

`open_rejected` 中，包含“标的处于下行结构，多单属于逆势”的样本为 186 次。高级别明确逆势时，V2 应在策略层压制该 signal_id/structure，而不是每个 cycle 都交给 open gate 重复拒绝。

**D5：终态/过期结构仍产生大量重复诊断**

同窗口内 `cot_trace` 出现：

| 诊断 | 频次 |
|---|---:|
| `重复过期信号已静默` | 1199 |
| `父结构已处于终态，跳过entry trigger` | 919 |

V2 已有终态识别和静默雏形，但仍在每 cycle 反复评估和输出噪声，需要在进入信号评估前压缩终态生命周期。

**D6：主动平仓不足**

窗口内无主动 `close_long/close_short`。现有 V2 持仓管理已有保本、分批止盈、结构破坏和浮盈回撤机制，但缺少明确的 ATR 硬止损、持仓超时无盈利平仓，以及可验证的 full close 策略路径。

**D7：背驰强度始终为 0**

29 个 open action 的 `divergence_strength` 均为 0。当前 V2 `buildInput` 使用收盘价差近似 MACD histogram，不能满足 Rust 背驰检测对标准 MACD(12,26,9) 柱状图的输入要求。

---

## 2. 问题定性

| 编号 | 类别 | 根因 | 严重度 |
|---|---|---|---|
| D1 | V2 可执行信号生命周期缺失 | V2 没有执行结果回调和成功开仓去重状态，同一 signal_id 可反复进入执行层 | 严重 |
| D2 | V2 zero-size/open validation 不完整 | 确定性 sizing/preflight 失败未在 V2 validation 阶段转为 `open_rejected` | 严重 |
| D3 | V2 候选过滤缺失 | `resolveSymbolUniverse` 未跳过非加密标的和已带 `FilterReason` 的候选 | 中 |
| D4 | V2 逆势压制缺失 | 高级别逆势只依赖后置 open gate 拒绝，缺少 V2 结构级压制 | 中 |
| D5 | V2 终态结构清理不足 | 终态/过期结构仍每 cycle 重新评估并输出重复诊断 | 中 |
| D6 | V2 主动平仓不足 | 持仓管理缺少 ATR 硬止损、超时无盈利 full close 等可验证路径 | 严重 |
| D7 | V2 MACD 输入错误 | Rust 输入 `macd_hist` 不是标准 MACD histogram | 中 |

---

## 3. 用户故事与验收标准

### Requirement D1 - V2 可执行信号生命周期

**User Story:** 作为 V2 策略维护者，我希望同一可执行 signal_id 在成功开仓或终态拒绝后不再重复进入执行层。

**验收标准**：
1. WHEN V2 open-like 决策执行成功，THE V2 引擎 SHALL 在 V2 自有状态中记录该 `signal_id` 已执行。
2. WHEN 后续 cycle 再次遇到同一已执行 `signal_id`，THE V2 引擎 SHALL 跳过该 open-like decision。
3. WHEN V2 信号因终态原因被拒绝（如过期、目标穿越、剩余 RR 无效、V2 逆势压制、zero-size 不可执行），THE V2 引擎 SHALL 记录终态拒绝并抑制同一 `signal_id` 重复进入 open gate。
4. THE 实现 SHALL 只新增 V2 状态和 V2 执行结果回调，不修改 V2 以外的策略行为。

### Requirement D2 - V2 open validation 与日志语义

**User Story:** 作为交易系统维护者，我希望 V2 的不可执行开仓在真实下单前被确定性拒绝，并在日志中表现为 `open_rejected`。

**验收标准**：
1. WHEN V2 open-like decision 的 sizing 结果为 0、低于最小名义额、保证金不足或 preflight 不可执行，THE 系统 SHALL 记录 `open_rejected`，不得调用交易所开仓接口。
2. WHEN Rust 返回非零 SL/TP/Confidence，THE V2 decision SHALL 保留这些字段。
3. WHEN Rust 返回 SL/TP 为 0，THE V2 引擎 SHALL 在 V2 内基于 ATR 或中枢边界生成兜底 SL/TP；若仍无法形成有效结构，THE decision SHALL 被拒绝而不是进入执行层。
4. THE 决策日志 SHALL 能区分 requested/effective stop-loss、take-profit、confidence、sizing rejection reason。

### Requirement D3 - V2 候选标的过滤

**User Story:** 作为 V2 策略维护者，我希望 V2 只分析可交易的加密永续标的，不让 CL/XAU/XAG 等非加密标的生成开仓。

**验收标准**：
1. THE V2 引擎 SHALL 在 `strategy/chanlunv2` 内实现本策略专属的标的过滤 helper。
2. THE V2 引擎 SHALL 在 market data 拉取和 Rust 分析前过滤非加密标的。
3. THE V2 引擎 SHALL 跳过 `CandidateCoin.FilterReason != ""` 的候选。
4. CLUSDT/XAUUSDT/XAGUSDT SHALL 不出现在 V2 的新开仓分析候选中。
5. THE 实现 SHALL 不复用或修改 V2 以外策略包的私有 helper。

### Requirement D4 - V2 逆势信号抑制

**User Story:** 作为风控维护者，我希望高级别明确逆势的 V2 信号在策略层直接被压制，而不是每个 cycle 反复进入 open gate。

**验收标准**：
1. WHEN 4h 明确下跌且 V2 信号为做多，THE V2 引擎 SHALL 丢弃该 open candidate。
2. WHEN 4h 明确上涨且 V2 信号为做空，THE V2 引擎 SHALL 丢弃该 open candidate。
3. WHEN V2 信号被逆势压制，THE V2 引擎 SHALL 记录 V2 终态/冷却状态，后续同一结构不再重复进入 open gate。
4. THE 诊断 SHALL 输出汇总计数，不应每 cycle 重复刷屏。

### Requirement D5 - V2 过期与终态结构清理

**User Story:** 作为策略观察者，我希望过期或终态父结构被压缩成生命周期状态，而不是每个 cycle 产生重复诊断。

**验收标准**：
1. WHEN V2 父结构进入终态，THE V2 引擎 SHALL 在进入 entry trigger 检测前识别并跳过。
2. THE V2 引擎 SHALL 对重复终态信号只保留计数和最后出现时间。
3. THE `StrategyDiagnostics` SHALL 输出 suppressed/rejected 汇总指标。
4. THE 日志 SHALL 避免大量重复 `重复过期信号已静默` 和 `父结构已处于终态` 文本。

### Requirement D6 - V2 主动平仓

**User Story:** 作为实盘风控维护者，我希望 V2 持仓在结构失效或风险扩大时主动降低风险。

**验收标准**：
1. THE V2 引擎 SHALL 保留现有保本、分批止盈、结构破坏和浮盈回撤管理。
2. THE V2 引擎 SHALL 增加可配置的 ATR 硬止损 full close。
3. THE V2 引擎 SHALL 增加可配置的持仓超时且无盈利 full close。
4. WHEN 新增 full close 条件满足，THE V2 引擎 SHALL 产出 `close_long/close_short` risk-reducing decision。
5. THE risk-reducing decision SHALL 经过 `decision.ValidateRiskReducingStrategyDecisions()` 或等价 V2 wrapper 校验。

### Requirement D7 - V2 真实 MACD histogram

**User Story:** 作为 Rust 缠论库使用者，我希望传给 Rust 的 `macd_hist` 是标准 MACD(12,26,9) 柱状图，以便背驰检测有效。

**验收标准**：
1. THE V2 `buildInput` SHALL 使用 V2 本地标准 EMA 算法计算 MACD(12,26,9) histogram。
2. THE `macd_hist` 长度 SHALL 与输入 K 线长度一致，数据不足或预热区使用 0 对齐。
3. THE 实现 SHALL 不新增 V2 以外策略链路依赖，也不要求修改其他策略指标链路。
4. THE 单元测试 SHALL 使用固定 K 线 fixture 验证 histogram 非收盘价差近似，并覆盖长度对齐。
5. `divergence_strength > 0` 的比例作为部署后观察指标，不作为市场无关单元测试的硬断言。

---

## 4. 风险

| 风险 | 缓解 |
|---|---|
| 信号去重过严导致错过新入场 | 以 V2 `signal_id`/entry trigger ID 和结构终态区分，不按 symbol 粗暴去重 |
| 逆势压制错杀盘整信号 | 仅在 higher timeframe 明确 `up_trend/down_trend` 时压制，`consolidation/unknown` 不压制 |
| SL/TP 兜底不准 | 优先使用 Rust 输出；兜底无效时拒绝，不强行开仓 |
| MACD 算法口径引入差异 | V2 内固定 EMA 公式并用 fixture 测试，避免跨包副作用 |
| 主动平仓过度 | 默认保守配置，先用单元测试和部署后观察验证 |

---

## 5. 度量

| 指标 | 当前 | 目标 |
|---|---:|---:|
| 同一 signal_id 重复 open action | CLUSDT 13 次、BNBUSDT 13 次 | 0 |
| zero-size 失败开仓 | 27 | 0 |
| 非加密标的进入 V2 新开仓候选 | CLUSDT 330、XAUUSDT 283 | 0 |
| 逆势 open gate 重复拒绝 | 186 | <=10 |
| 大量重复终态诊断 | `重复过期信号已静默` 1199、`父结构已处于终态` 919 | 输出汇总，不刷屏 |
| 主动 full close 路径 | 0 | fixture 可触发；实盘观察 |
| `macd_hist` 输入 | 收盘价差近似 | 标准 MACD histogram |
| 背驰强度 > 0 信号占比 | 0% | 部署后观察提升 |
