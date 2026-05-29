# 217 Chanlun V2 No-Open Diagnosis Consistency Review

## 范围

本次交叉校验覆盖：

- `requirements.md`
- `design.md`
- `tasks.md`
- 当前本地代码：`config/`、`decision/`、`strategy/chanlunv2/`、`strategy/chanlun/`、`logger/`、`cmd/replay/`、`trader/auto_trader.go`

## 结论

规格方向成立：217 不开仓的主要原因、BTC hard veto 判断、V2 未接入 loosen mode 的判断都与当前代码一致。

发现 3 个需要修正或执行时重点关注的问题，其中 2 个已同步修正到 requirements/design/tasks。

## Findings

### 1. P2 原设计漏掉 freshness guard 的二次 RR 检查

严重度：High

证据：

- `strategy/chanlunv2/entry_timing.go` 中父结构和 entry trigger 使用 `minRemainingNetRRForV2Signal()`。
- `strategy/chanlunv2/engine.go` 的 `applyChanlunV2FreshnessGuard()` 会再次计算 `remainingRR < minRR`。
- 该 `minRR` 来自 `e.minRemainingNetRR(ctx, policy)`，优先 `signal_freshness.min_remaining_net_rr`，其次 `ctx.StrategyRiskPolicy.DefaultMinNetRR`，不使用 `entry_zone.signal_type_min_rr`。

影响：

如果只把 `sell2` 从 1.5 降到 1.1，父结构可以不再直接终态，但后续 entry trigger 仍可能被 freshness guard 用更高阈值重新拒绝。这样会导致“开仓率恢复”目标落空。

处理：

已补充 Requirement 2 / Requirement 3 / Design §3.3 / Tasks P2：同一个 effective RR resolver 必须覆盖父结构、entry trigger 和 freshness guard。

### 2. P1 原任务与现有 replay 日报重复

严重度：Medium

证据：

- `cmd/replay/main.go` 已支持 `-open-rejection-daily`。
- `logger/replay.go` 已有 `BuildOpenRejectionDailyReport()`。
- `logger/replay_test.go` 已有 `TestBuildOpenRejectionDailyReport`。

影响：

原任务写成“增加 daily report”容易导致重复实现。真正缺口是现有日报还不能完整区分 Chanlun V2 的 direct terminal、suppressed terminal、trigger ready、BTC diagnostics。

处理：

已将 Tasks P1 和 Design §3.4 修正为“扩展现有 replay 日报为 Chanlun V2 no-open report”。

### 3. V2 loosen mode 需要明确状态所有权

严重度：Medium

证据：

- `strategy/chanlun/engine.go` 有 `activeMode`、`loosenMinRRDelta`、`loosenChaseBump` 和 `loosenModeController()`。
- `strategy/chanlunv2/engine.go` 当前没有对应字段或 controller。
- `trader/auto_trader.go` 已在构建 context 时注入 `FrequencyPolicy`、`FrequencyState`、`RuntimeMinutes`，并在风险快照中读取 `ctx.FrequencyPolicy.EffectiveMode`。

影响：

如果照搬 V1 `StateStore`，会把 V2 引入不必要的状态依赖；如果只在 engine 内部设置但不写回 `ctx.FrequencyPolicy.EffectiveMode`，日志中的 `risk_state.active_mode` 仍可能保持 `balanced`。

处理建议：

P2 执行时优先复用 `ctx.FrequencyState.OpenCount24h` / `LastOpenAt` 和 `ctx.RuntimeMinutes` 做 stateless 判定，进入 loosen 时写回 `ctx.FrequencyPolicy.EffectiveMode="loosen"`，并在 V2 diagnostics 中输出 effective thresholds。

## 逐项对齐

| 需求 | 代码现状 | 结论 |
|---|---|---|
| Requirement 1 no-open report | 已有 open rejection daily report，但缺 V2 no-open 分类 | 需扩展 |
| Requirement 2 V2 inactivity loosen | V1 已实现，V2 未实现 | 需实现 |
| Requirement 2 不绕过 BTC hard veto | `applyBTCMultiTimeframeGate()` 当前硬 block | 一致 |
| Requirement 3 short-side threshold tuning | 配置结构支持 `sell2` 和 short overrides | 一致，但需 freshness guard 对齐 |
| Requirement 4 BTC gate 显式 | 当前无可配置模式，默认 hard block | 一致 |

## 执行前必须保留的约束

- 不把 BTC hard veto 改为 report-only。
- 不把 confidence override 扩展到 counter-trend/BTC/high-ADX 风险类 gate。
- 不绕过 exchange preflight 或真实账户风险预算。
- 不把远端 `config.json` 密钥写入仓库。
