# Chanlun V2 Entry Exit Optimization Consistency Review

## Review Scope

本轮交叉验证覆盖：

- Spec: `requirements.md`、`design.md`、`tasks.md`
- Existing code: `config/config.go`、`config/programmatic.go`、`strategy/chanlunv2/*`、`strategy/chanlun/*`、`decision/*`、`trader/*`、`logger/*`、`web/src/types*.ts`
- 161 线上日志结论：`aster_chanlun_v2` 在 435 个日志样本中 0 次成功开仓，旧结构信号年龄最高 58 根 1h，最新版本已将 BNB/HYPE stale 信号降级为诊断。

## Verdict

结论：交叉验证通过。Spec 的需求、设计、任务与当前代码边界一致，可以进入实现阶段。实现时必须保留 6 个约束：

1. V2 现在仍从 trade timeframe `Signal` 直接生成 `open_long/open_short`，必须先拆成 parent structure 和 entry trigger 两层。
2. 可执行 freshness 必须以 entry trigger close time 为准；不能让 parent structure time 抢先进入 `signal_close_time` 计算链路。
3. 三买/三卖质量模型可行，但 `P0/P1/H/L` 必须从 V2 center、closed klines 和 fractal/局部极值稳定推导，不能只套固定 1h 百分比。
4. V2 新配置应尽量镜像 V1 programmatic 的 `entry_timing.entry_zone` 和 `position_management` 结构，避免运维和归一化逻辑分叉。
5. 风险降低动作不能被 open freshness 阻断，但必须走持仓存在性、方向、数量和交易所 preflight 校验。
6. zero quantity / min notional / margin miss 必须在执行真实下单前变成结构化 `open_rejected`，不能再记录为失败的 `open_long/open_short`。

## Requirement Cross-Check

### R1/R2: Parent Structure + Entry Trigger

Status: consistent, required.

Current code:

- `strategy/chanlunv2/engine.go` 的 `GetFullDecision()` 调用 `multiLevelJudgment()` 后，直接执行 `signalToDecision()`。
- `signalToDecision()` 将 `buy*` / `sell*` 直接映射为 open-like decision，并写入 `layer=trade_action`。
- 旧信号目前依靠 `downgradeExpiredChanlunV2Signal()` 和 `applyChanlunV2FreshnessGuard()` 降级或拒绝。

Spec fit:

- R1/R2 要求“旧结构只做背景，入场来自新鲜 trigger”正好修复当前直接映射问题。
- Design 的 parent lifecycle + entry trigger detector 与现有 `strategy/chanlunv2.Engine` 所有权匹配。
- Tasks 6-13 覆盖了结构拆层、trigger ID、freshness、拒绝诊断和质量分类。

Implementation notes:

- V1 `strategy/chanlun` 已有可迁移模式：`prepareStructureEntry()`、`tryPullbackRetestEntryTrigger()`、`StableEntryTriggerID()`、`applyProgrammaticSignalGuard()`。
- V2 entry trigger 不应复用旧 parent `SignalID`；否则 stale suppression 会误压制后续新 trigger。

### R2: Third Buy/Sell Quality Model

Status: consistent after refinement.

Current code:

- V2 Rust output `Signal` 有 `CenterID`，`AnalysisResult` 有 `Centers`，center 包含 `ZG/ZD/High/Low/StartTime/EndTime`。
- V2 `Kline` 有 OHLCV，可从 closed klines 计算 breakout candle、P1 和 H/L。
- 目前没有 `G/R/N/G_ATR` 字段、分类器或 marker 输出。

Spec fit:

- Requirements 已将用户的 `D/N` 方案拆为 `G/R/N/G_ATR/RR`，并明确非 1h 频率要做 ATR、分位数或时间归一化。
- Design 已补充数据来源：三买 `P0=Center.ZG`，三卖 `P0=Center.ZD`；`P1/H/L` 来自 closed quality-timeframe K 线或确认分型。
- Tasks 3/10/13/16/23/24 覆盖配置、计算、诊断、指标分布和前后端字段。

Implementation notes:

- `Signal.CenterID == nil` 或 center 缺失时，不能硬判强三买/三卖；应记录 `third_point.missing_center_boundary`。
- `micro_reversal_confirm` 是 V2 新 trigger 类型；如果复用 V1 `normalizeEntryTriggerTypes()`，需要扩展 allowed list。
- Percentile 模式需要明确 lookback 和阈值，避免 `use_symbol_percentiles` 成为空开关。

### R3: Sizing Fail-Safe

Status: consistent, implementation boundary must be precise.

Current code:

- `decision.ValidateAndEnrichDecision()` 会在 `PositionSizeUSD <= 0` 时补默认仓位。
- `validateOpenDecisionWithOptions()` 仍有 `仓位大小必须>0` 校验，并调用 open gate、风险规范化和统一 sizing。
- `trader.executeOpenLikeWithRecord()` 也保留 `PositionSizeUSD <= 0` 和 `EvaluateExecutionPreflight()` 兜底，但当前兜底错误会被记录为失败 open action。

Spec fit:

- R3 和 task 14 必要，因为 161 历史日志证明 zero-size 曾进入执行层。
- Design 已要求 deterministic sizing/preflight miss 在真实交易所调用前转为 `open_rejected`。

Implementation notes:

- 首选在 V2 validation 或通用 decision validation 产出 `OpenRejection`。
- 执行层兜底仍必须保留，但日志语义应从 failed open 改为 structured rejection，或保证该路径永远不被正常策略触达。

### R4: Position Management

Status: consistent, required.

Current code:

- V2 `managePositions()` 只处理 trade timeframe 反向信号平仓。
- V1 `strategy/chanlun/position_management.go` 已有 breakeven、floating drawdown、structure break、partial close guard 和 short trade partial close。
- `trader.AutoTrader` 支持 `partial_close`、`update_stop_loss`、`update_take_profit`、`close_long/close_short`，并有排序测试保证风险降低动作先于开仓。

Spec fit:

- R4 和 tasks 17-21 与现有执行层能力一致，不需要修改 `trader.Trader` 接口。
- Task 21 已补充 V2 risk-reducing action 要调用 `decision.ValidateRiskReducingStrategyDecisions()` 或等价 wrapper。

Implementation notes:

- 调整止损只走 `CancelStopLossOrders()`，调整止盈只走 `CancelTakeProfitOrders()`。
- V2 风险降低动作 marker 应使用 `SourceLayer=position_management`，避免和 entry trigger 混在同一生命周期展示里。

### R5/R6: Lifecycle And Observability

Status: consistent, partially supported.

Current code:

- V2 只有 in-memory `staleSuppressions`，重启后会丢失。
- Shared `chanlun.SignalMarker` 已有 parent/entry/freshness/time 字段，但尚无 third-point quality 字段。
- Frontend `SignalMarker` 类型也已有 parent/entry/freshness/time 字段，但尚无 quality metrics 字段。

Spec fit:

- R5 lifecycle persistence 是现有 stale suppression 的自然升级。
- R6 已补充 third-point quality metrics 输出要求。
- Tasks 22-24 覆盖 diagnostics、marker metadata 和前端类型展示。

Implementation notes:

- 后端 `strategy/chanlun/types.go` 和前端 `web/src/types.ts`、`web/src/types/index.ts` 必须同步新增字段。
- V2 marker 当前对结构 signal 使用 `SourceLayer=trade_action`；实现 parent structure 后应改为 `structure` / `entry_trigger` / `position_management`。

### R7/R8: Regression And Safety

Status: consistent.

Current code:

- V2 tests 已覆盖 freshness、stale suppression、marker time alignment。
- V1 有 `strategy/chanlun/testdata/entry_timing_161_stale_signals.json` 可参考。
- Trader 和 logger tests 已覆盖 open rejection、partial close、排序和 preflight 的关键行为。

Spec fit:

- R7 已覆盖 BNB/CLUSDT/DOGE/HYPE stale、old parent + fresh trigger、zero quantity、position management 和 `G/R/N/G_ATR/RR` 场景。
- R8 与项目 steering 的安全边界一致：不提交 runtime data，不绕过 open gate，不触发真实下单。

## Design Cross-Check

### Config Shape

Status: aligned after refinement.

- Current V2 config 只有 `timeframes/history_depth/signal_freshness` typed fields，其余为 map-based escape hatches。
- Design 已新增 typed `entry_timing`、`entry_zone`、`third_point_quality` 和 `position_management`。
- `entry_zone` 现在镜像 V1 programmatic 的关键字段，减少归一化和运维分叉。

Required implementation:

- 新字段必须 optional，老 `config.json` 加载后使用保守默认值。
- `hashChanlunV2Config()` 依赖 normalized config；新增默认值应进入 config hash。

### Freshness Time Semantics

Status: explicitly covered.

Current V2 helper:

```go
signalClose := firstPositiveInt64(
    metadataInt64(d.StrategyMetadata, "signal_close_time"),
    metadataInt64(d.StrategyMetadata, "trigger_close_time"),
    metadataInt64(d.StrategyMetadata, "segment_end_time"),
)
```

This is correct for current direct structure signals, but unsafe once parent and trigger coexist.

Required implementation:

- Entry-trigger open decision should set executable `signal_close_time` to trigger close time and preserve parent separately as `parent_signal_close_time`.
- V2 freshness helper should prefer `entry_trigger_close_time` / `trigger_close_time` when `layer=entry_trigger`.
- `decision.NewOpenRejectionFromDecision()` and V2 rejection marker helpers must preserve the executable trigger time in rejections.

### Validation Order

Status: aligned after refinement.

Current code behavior:

- `decision.ValidateStrategyDecisions()` wraps `ValidateAndEnrichDecision()` and `validateOpenDecisionWithOptions()`.
- `validateOpenDecisionWithOptions()` performs open gate, invalidation, position checks, leverage, risk/reward and sizing.
- `trader.executeOpenLikeWithRecord()` performs final preflight before exchange call.

Spec implication:

- Do not duplicate or bypass `ValidateStrategyDecisions()` open gate/sizing path.
- Add V2-specific trigger quality checks before generic validation.
- Add V2 deterministic fail-safe for zero quantity/min notional if anything still reaches execution incorrectly.

### Data Availability For G/R/N/G_ATR

Status: feasible with explicit fallbacks.

- `P0` can come from `AnalysisResult.Centers` using `Signal.CenterID`.
- `P1/H/L/N` can come from closed K lines on `quality_timeframe`; confirmed fractals are preferred when available.
- `G_ATR` can use existing market ATR helpers from market data; if ATR is unavailable, fallback to percent/percentile diagnostics rather than silently passing.

## Task Cross-Check

Tasks are ordered correctly and now cover the identified implementation gaps:

- Phase 1 captures 161 stale fixtures and log helpers.
- Phase 2 adds typed config and lifecycle state before behavior changes.
- Phase 3 prevents old structures from opening directly.
- Phase 4 adds entry triggers, third-point quality, freshness precedence and diagnostics.
- Phase 5 hardens open validation and sizing fail-safe.
- Phase 6 adds multi-layer position management and risk-reducing validation.
- Phase 7 updates diagnostics, marker metadata and frontend types.
- Phase 8/9 handle validation and staged rollout.

No task blocker remains in the Spec documents.

## Residual Risks

- If lifecycle persistence keys only by parent structure, valid later triggers can be suppressed. Trigger-ready/open states must key by `entry_trigger_id`.
- If `CenterID` is absent for some Rust V2 signals, strong third-point classification will be unavailable for those signals until the analyzer exposes enough structure metadata.
- Symbol percentile quality checks can overfit if the lookback window is too short; report-only rollout should compare quality distribution before enabling execution.
- If entry trigger rules are enabled without report-only validation, candidate count may rise during BTC bearish regimes. Existing BTC/ADX/open gate must remain authoritative.
- `CGO_ENABLED=0` tests should use pure-Go fixtures around `Signal`、`AnalysisResult` and K lines rather than relying on live Rust analysis.

## Final Verdict

Spec is internally consistent and consistent with the existing codebase after the refinements made in this cross-check. It is ready for implementation, with special attention to freshness time precedence, V2 typed config normalization, third-point data derivation, risk-reducing validation and structured sizing rejection.
