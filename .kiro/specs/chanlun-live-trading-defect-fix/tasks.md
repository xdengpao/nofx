# 程序化缠论策略实盘缺陷修复 Tasks

## 关联文档

- `requirements.md`
- `design.md`

---

## Phase 0 — 关闭"永远 wait"循环（P0，优先级最高）

### Task 0.1 — 总开关与配置 schema

- [x] `config/programmatic.go`：`ProgrammaticStrategyConfig` 新增 `DefectFixPackEnabled *bool` 字段，JSON tag `defect_fix_pack_enabled`；profile 层归一化为普通 `bool`，缺省 true，显式 false 保留。
- [x] `config/programmatic.go` / `decision/types.go`：`ProgrammaticStrategyProfile` 与 `ProgrammaticStrategyPolicy` 新增 `DefectFixPackEnabled bool`、`SuppressionPermanentThreshold int`（默认 5）。
- [x] `manager/trader_manager.go`：`decisionProgrammaticStrategyPolicy` 复制上述字段，确保 Engine 可见总开关。
- [x] `config/config_test.go`：验证缺省 true、显式 false 回退、关闭时旧默认值不被新推荐默认覆盖。

### Task 0.2 — 置信度阈值合理化（Req 2）

- [x] `decision/types.go`：`ProgrammaticPreviewSignalsPolicy` 新增 `PilotMinConfidenceBySignal map[string]int`、`PilotMinConfidenceUseP75 bool`、`P75Floor int`、`P75Ceiling int`。
- [x] `config/programmatic.go`：`ProgrammaticPreviewSignalsConfig.PilotMinConfidence` 与 `ProgrammaticEntryPilotConfig.MinConfidence` 改为 `*int` 或增加 presence tracking，以区分未配置和显式配置。
- [x] `config/programmatic.go`：解析新字段；`defect_fix_pack_enabled=true` 时缺省 `PilotMinConfidence=70`、`P75Floor=65`、`P75Ceiling=85`、`UseP75=true`；关闭时保持旧默认 90 且 `UseP75=false`。
- [x] `config/programmatic.go`：启动校验 — 仅当 `preview_signals.pilot_min_confidence` 与 `entry_timing.pilot.min_confidence` 都显式配置且不一致时报错。
- [x] `manager/trader_manager.go`：复制新增 preview policy 字段到 `decision.ProgrammaticPreviewSignalsPolicy`。
- [x] `strategy/chanlun/state.go`：新增 `ConfidenceSample{SignalType string; Confidence int; At time.Time}` 存储与 `ConfidenceWindow(traderID, signalType, duration)` 查询。
- [x] `strategy/chanlun/signals.go`：每次产出 ChanlunSignal 时写入 ConfidenceSample。
- [x] `strategy/chanlun/engine.go`：新增 `effectivePilotMinConfidence(ctx, signalType)` 方法（P75 + mode 调整，trader_id 从 ctx 取）。
- [x] `strategy/chanlun/engine.go`：`evaluatePreviewSignals` 中把硬编码 `e.Policy.PreviewSignals.PilotMinConfidence` 替换为 `effectivePilotMinConfidence(ctx, signal.SignalType)`。
- [x] `strategy/chanlun/confidence_test.go`：P75 计算、样本不足回退、safe_mode +10、loosen -10。

### Task 0.3 — 净 RR 差异化（Req 3）

- [x] `decision/types.go`：`ProgrammaticEntryZonePolicy` 新增 `SignalTypeMinRR map[string]float64`、`TierOverrides map[string]ProgrammaticEntryZoneOverridePolicy`、`TheoreticalRRUnreachableSkip bool`。
- [x] `config/programmatic.go`：解析新字段；`defect_fix_pack_enabled=true` 时缺省 `MinRemainingNetRR=2.0`、`SignalTypeMinRR={buy1@1h:2.0,buy2@1h:1.6,buy3@1h:1.4,sell1@1h:2.0,sell2@1h:1.6,sell3@1h:1.4}`；关闭时保持旧默认 2.5。
- [x] `manager/trader_manager.go`：复制 entry zone 新字段到 decision policy。
- [x] `strategy/chanlun/engine.go`：新增 `minRemainingNetRRForSignal(signalType, timeframe, symbol, tier)`，按 `signal_type@timeframe → signal_type → symbol_overrides → tier_overrides → default` 查询。
- [x] `strategy/chanlun/engine.go`：`applyProgrammaticSignalGuard` 中替换 `e.minRemainingNetRRForSymbol` 为 `minRemainingNetRRForSignal`。
- [x] `strategy/chanlun/engine.go`：`gate_diagnostics` 新增 `gross_rr / fee_slippage_pct / structure_rr / theoretical_max_rr`。
- [x] `strategy/chanlun/engine.go`：新增 `shouldSkipBeforeEntry(signal, data)` — 理论 RR 不可达时直接 reject + terminate。
- [x] `strategy/chanlun/structure_rr_test.go`：理论 RR 计算、预过滤、tier 覆盖。

### Task 0.4 — 抑制 fast-skip + lifecycle 终结（Req 5 + Req 6）

- [x] `strategy/chanlun/state.go`：`SignalSuppression` 新增 `Severity int`、`PermanentSkip bool`。
- [x] `strategy/chanlun/state.go`：新增 `LifecycleTermination` 类型与 `TerminateLifecycle / IsLifecycleTerminated / UpgradeSuppression / MarkPermanentSkip / GCExpiredSuppressions / SuppressionStats` 方法。
- [x] `strategy/chanlun/engine.go`：新增 `fastSkipSuppressed(ctx)` 返回 `map[string]string`。
- [x] `strategy/chanlun/engine.go`：`evaluateMainSignals / evaluatePreviewSignals` 入口查 skipSet，命中跳过。
- [x] `strategy/chanlun/engine.go`：`applyProgrammaticSignalGuard` 命中 `invalid_stop_take_profit_structure / target_already_crossed` 时调用 `TerminateLifecycle`。
- [x] `strategy/chanlun/signals.go`：新增 `IsBornInvalid()` 出生检查，`detectSignalsFromKlines` 中调用。
- [x] `strategy/chanlun/engine.go`：SeenCount > threshold 时 `MarkPermanentSkip`。
- [x] `strategy/chanlun/suppression_test.go`：fast-skip、升级、permanent、GC、出生检查。

### Task 0.5 — wait_reason_summary（Req 10.5）

- [x] `logger/decision_logger.go`：`DecisionRecord` 新增 `WaitReasonSummary string` JSON tag `wait_reason_summary`。
- [x] `decision/types.go`：`FullDecision` 新增 `WaitReasonSummary string` JSON tag `wait_reason_summary`。
- [x] `strategy/chanlun/engine.go`：在 `GetFullDecision()` 构建 `FullDecision` 前根据本 cycle 所有 diagnostics / open_rejections / per_candidate 终态计算 `wait_reason_summary`（按优先级取第一个命中）。
- [x] `trader/auto_trader.go`：从 `fullDecision.WaitReasonSummary` 拷贝到 `record.WaitReasonSummary`。
- [x] `strategy/chanlun/wait_reason_test.go`：优先级排序验证。

### Task 0.6 — P0 集成验证

- [x] `cmd/replay/main.go`：新增 `-defect-fix-pack` 开关，支持 P0 阶段 replay 验证。
- [x] `go build ./...` 通过。已在远程 161 临时工作树通过 `nofx-replay-builder:latest` 验证。
- [!] `go test ./config ./decision ./strategy/chanlun ./logger` 全部通过。部分通过：远程 161 临时工作树中 `go test ./config ./strategy/chanlun ./logger` 通过，`go test -run '^$' ./decision` 编译通过；阻塞：`go test ./decision` 执行既有属性测试 `TestProperty39_ReturnsSeriesLengthCap` 超时，堆栈指向 `decision/persistence.go` 自动保存测试路径问题，非本 spec 新增逻辑。
- [!] 本地用 `cmd/replay -log-dir decision_logs -defect-fix-pack` 对远程拷贝的 24h 日志重跑，确认理论开仓 ≥1。阻塞：远程临时验证具备 Go 工具链，但未提供可用于 replay 的远程 24h 决策日志/行情数据。

---

## Phase 1 — 入场链路解锁（P1）

### Task 1.1 — direct_structure 路径（Req 1）

- [x] `decision/types.go`：`ProgrammaticEntryTimingPolicy` 新增 `DirectStructureMinConfidence int`、`MaxNoTriggerSubCandles int`。
- [x] `config/programmatic.go`：`ProgrammaticEntryTimingConfig.DirectStructureOpen` 从 `bool` 改为 `*bool` 或 presence tracking；`defect_fix_pack_enabled=true` 时缺省 true，关闭时保持旧默认 false，显式 false 必须保留。
- [x] `config/programmatic.go`：解析 `DirectStructureMinConfidence=70`、`MaxNoTriggerSubCandles=3`，并传入 profile。
- [x] `manager/trader_manager.go`：复制 entry timing 新字段到 decision policy。
- [x] `strategy/chanlun/types.go`：`ChanlunSignal` 新增 `EntryPath string`、`Tier string`。
- [x] `strategy/chanlun/engine.go`：`evaluateMainSignals` 中新增 `decideEntryPath(signal)` 路由。
- [x] `strategy/chanlun/engine.go`：`EntryPathDirectStructure` 分支 — 跳过 `prepareStructureEntry`，直接进入 `applyProgrammaticSignalGuard`。
- [x] `strategy/chanlun/engine.go`：`EntryPathPreviewThenTrigger` 分支 — 走现有链路 + entry_window 终结条件（`MaxNoTriggerSubCandles`）。
- [x] `strategy/chanlun/entry_path_test.go`：direct 路径、preview 路径、终结条件。

### Task 1.2 — 双轨追价（Req 4）

- [x] `decision/types.go`：`ProgrammaticEntryZonePolicy` 新增 `MaxChaseATRMultiplier float64`、`FreshAgeChaseRelax float64`。
- [x] `config/programmatic.go`：解析新字段，缺省 `MaxChaseATRMultiplier=0.6`、`FreshAgeChaseRelax=0.10`，并传入 profile/policy。
- [x] `strategy/chanlun/engine.go`：`prepareStructureEntry` / `evaluateStructureEntryWindow` 中实现双轨判断（ratio OR ATR 任一通过）。
- [x] `strategy/chanlun/engine.go`：`effectiveMaxChaseRatio(symbol, tier, ageCandles)` — 合成 symbol_overrides → tier_overrides → base + fresh_age 放宽。
- [x] `strategy/chanlun/engine.go`：`gate_diagnostics` 新增 `chase_ratio_atr / entry_zone_low / entry_zone_high`。
- [x] `strategy/chanlun/chase_test.go`：双轨通过/拒绝、fresh_age 放宽、tier 覆盖。

### Task 1.3 — P1 集成验证

- [!] `go test ./...` 通过。部分通过：远程 161 临时工作树中 `go test -run '^$' ./...` 全仓测试编译通过，`go test ./config ./strategy/chanlun ./logger ./manager ./trader` 通过；阻塞：全量执行仍受 `go test ./decision` 既有属性测试超时影响。
- [!] `cmd/replay` 重跑确认 direct_structure 路径被命中。阻塞：远程临时验证具备 Go 工具链，但未提供 replay 数据。

---

## Phase 2 — 自适应与治理（P2）

### Task 2.1 — 账户尺寸 gate（Req 7）

- [x] `strategy/chanlun/engine.go`：新增 `accountSizeGate(ctx)` 返回 `AccountSizeDecision`。
- [x] `strategy/chanlun/engine.go`：`GetFullDecision()` 中 `hold_only` 时跳过开仓评估，仅保留持仓管理。
- [x] `strategy/chanlun/engine.go`：pilot sizing 先计算 `max_allowed_notional`，若小于交易所最小名义额则拒绝 `pilot_size_below_min_notional`，不得用 `math.Max` 反向放大超过账户上限。
- [x] `config/programmatic.go` / `decision/types.go`：新增 `MaxPilotNotionalPct`（默认 0.6）、`MinPilotNotionalUSD`（默认 30）并传入 policy。
- [x] `config/programmatic.go`：启动校验 `preview_signals.pilot_risk_fraction > 0.4` 或 `entry_timing.pilot.risk_fraction > 0.4` 时 warn。
- [x] `strategy/chanlun/account_gate_test.go`：hold_only、pilot 计算、最小名义额。

### Task 2.2 — 候选标的治理（Req 8）

- [x] `strategy/chanlun/engine.go`：新增 `candidateGovernor(ctx)` 过滤非加密；价差判断通过 `QuoteSpreadProvider` 函数注入，不直接依赖具体交易所。
- [x] `strategy/chanlun/engine.go`：`isCryptoUSDT(symbol)` 实现。
- [x] `strategy/chanlun/engine.go`：`ensureCoreSymbols` 保证 core 至少出现。
- [x] `trader/auto_trader.go`：用行情源 mid_price 与 `trader.Trader.GetMarketPrice()` 构造执行价差 provider，并把 `quote_spread_too_high` 写入候选诊断。
- [x] `logger/decision_logger.go`：`CandidateSnapshot` 新增 `Errors []string` JSON tag `errors,omitempty`。
- [x] `web/src/types/index.ts`：候选详情类型新增可选 `errors?: string[]`。
- [x] `config/programmatic.go`：新增 `CandidateGovernor{Enabled, AllowNonCrypto, MaxQuoteSpreadBps, CoreSymbolsMustAppear}` 并传入 policy。
- [x] `strategy/chanlun/candidate_governor_test.go`：剔除、价差、core 强制。

### Task 2.3 — loosen_mode（Req 9）

- [x] `strategy/chanlun/state.go`：新增 `LoosenState` 类型。
- [x] `strategy/chanlun/engine.go`：新增 `loosenModeController(ctx)` — 触发/退出/互斥。
- [x] `strategy/chanlun/engine.go`：`effectivePilotMinConfidence / minRemainingNetRRForSignal / effectiveMaxChaseRatio` 中加 loosen 调整。
- [x] `config/config.go`：`TradingFrequencyConfig/Profile` 新增 `LoosenMode{Enabled, InactivityWindowMinutes, PilotConfidenceDrop, MinNetRRDelta, MaxChaseRatioBump, MaxDurationHours, HardFloorPilotConfidence}`。
- [x] `decision/types.go`：`FrequencyPolicy` 新增 loosen mode 运行时配置和 gate effectiveness 所需字段。
- [x] `manager/trader_manager.go`：`decisionFrequencyPolicy` 复制 loosen mode 配置。
- [x] `trader/auto_trader.go`：`buildTradingContext` 保持 `FrequencyPolicy/FrequencyState` 进入 `decision.Context`，`buildRiskStateSnapshot` 输出 `active_mode / inactivity_minutes / last_open_at / last_close_at / open_count_24h / open_rejected_24h / signal_count_24h / gate_effectiveness / warnings.runaway_rejection_loop`。
- [x] `logger/decision_logger.go`：`RiskStateSnapshot` 新增上述字段。
- [x] `strategy/chanlun/engine.go`：反事实计数 — `report_only=true` 时通过 `FullDecision.StrategyDiagnostics` 输出 `gate_effectiveness.would_reject_count`。
- [x] `strategy/chanlun/loosen_mode_test.go`：触发、退出、互斥、hard floor、反事实。

### Task 2.4 — P2 集成验证

- [!] `go test ./...` 通过。部分通过：远程 161 临时工作树中 `go test -run '^$' ./...` 全仓测试编译通过，`go test ./config ./strategy/chanlun ./logger ./manager ./trader` 通过；阻塞：全量执行仍受 `go test ./decision` 既有属性测试超时影响。
- [x] 本地模拟 12h 无开仓 → loosen 激活 → 阈值放宽后理论开仓。已在远程 161 临时工作树通过 `go test ./strategy/chanlun` 执行 `loosen_mode_test.go` / `e2e_test.go` 覆盖。

---

## Phase 3 — 可观测性 + 端到端验证（P3）

### Task 3.1 — per_candidate + confidence_histogram + signal_quality_breakdown（Req 10.1/2.3/6.4）

- [x] `strategy/chanlun/engine.go`：`GetFullDecision()` 构建 `FullDecision` 前构建 `per_candidate[]` 数组（每候选一条）。
- [x] `strategy/chanlun/engine.go`：构建 `confidence_histogram` 按 signal_type 聚合。
- [x] `strategy/chanlun/engine.go`：构建 `signal_quality_breakdown` 计数。
- [x] `decision/types.go` / `trader/auto_trader.go` / `logger/decision_logger.go`：通过 `FullDecision.StrategyDiagnostics` 传递并写入日志。

### Task 3.2 — risk_state 扩展（Req 10.2/10.3）

- [x] `strategy/chanlun/engine.go`：把 `suppressions / active_mode / inactivity_minutes / last_open_at / last_close_at / open_count_24h / open_rejected_24h / signal_count_24h / warnings` 写入 `FullDecision.StrategyDiagnostics`。
- [x] `trader/auto_trader.go`：`buildRiskStateSnapshot` 从 context、open_rejections 和 strategy diagnostics 合成最终 `RiskStateSnapshot`。
- [x] `logger/decision_logger.go`：`RiskStateSnapshot` 新增上述字段；`AccountSnapshot` 新增 `account_too_small / total_realized_24h`。
- [x] `web/src/types/index.ts`：同步 risk_state、account_state 新字段，全部标为可选。

### Task 3.3 — daily_summary（Req 10.4）

- [x] `logger/daily_summary.go`：新增 `DailySummary` 类型与 `WriteDailySummary(traderID, date, logDir)` 函数。
- [x] `logger/decision_logger.go`：每日 0:05 后写决策日志时触发 `WriteDailySummary`，失败仅告警不阻塞交易周期。
- [x] `logger/daily_summary_test.go`：聚合算法、容错、文件轮转。

### Task 3.4 — 前端类型同步

- [x] `web/src/types/index.ts`：所有新增 JSON 字段标为 `?:` 可选。
- [x] `web/src/lib/api.ts`：无需改动（字段自动透传）。
- [x] `cd web && npm run build` 通过。已在远程 161 临时工作树通过 `docker build --target builder -f docker/Dockerfile.frontend` 验证（builder 阶段执行 `npm ci && npm run build`）。

### Task 3.5 — 端到端测试（Req 11）

- [x] `strategy/chanlun/e2e_test.go`：mock 1h 信号 → direct_structure → 持仓 → breakeven → partial_close → close，断言日志字段齐备。
- [x] `strategy/chanlun/e2e_test.go`：余额不足 → hold_only → 无开仓。
- [x] `strategy/chanlun/e2e_test.go`：12h 无开仓 → loosen → 第 13h 产生开仓评估。
- [!] `cmd/replay`：复用 P0 已新增的 `-defect-fix-pack` 开关，补充 ≥3 天行情/≥50 条候选信号 fixture 或文档化数据来源。阻塞：远程临时验证具备 Go 工具链，但未提供 ≥3 天行情/≥50 条候选信号 fixture。

### Task 3.6 — 灰度部署

- [!] 在远程服务器新建 `aster_deepseek_canary` 配置（≤10 USDT，独立账户）。阻塞：已有远程 SSH 访问，但未提供独立账户/资金配置，且未授权修改生产配置。
- [!] canary 跑 24h，确认 ≥1 次成功开仓。阻塞：未能创建/启动远程 canary，依赖独立账户/资金配置。
- [!] 切主 trader `defect_fix_pack_enabled=true` + `loosen_mode.enabled=false` 跑 24h。阻塞：当前只在远程临时工作树验证，未授权部署或修改生产主 trader。
- [!] 主 trader 24h ≥1 次开仓后打开 `loosen_mode.enabled=true`。阻塞：依赖前置 24h 远程灰度结果。
- [!] 任一阶段触发 `max_daily_loss` 或 `runaway_rejection_loop` → `defect_fix_pack_enabled=false` 回滚。阻塞：依赖远程灰度运行。

### Task 3.7 — 文档与配置示例

- [x] `config.json.example`：新增所有新字段及注释。
- [x] `docs/`：补充缠论策略配置调优说明。

---

## 验收度量

| 指标 | 现状 | P0 后目标 | P3 后目标 |
|---|---|---|---|
| 真实成功开仓 / 日 | 0 | ≥0（replay 理论 ≥1） | ≥1 |
| open_rejected / open_attempt | 100% | ≤80% | ≤70% |
| 同 structure_key 重复 reject 占比 | ≥50% | ≤20% | ≤10% |
| pilot_skip / signal_count | 484% | ≤100% | ≤50% |
| wait_reason_summary 单一原因 ≥80% 天数 | 100% | ≤60% | ≤30% |

---

## 交叉验证注意事项

以下为 spec 与远程代码（commit `9656386`，分支 `jzhbnofxdev`）交叉验证后的关键发现，实施时必须遵守：

1. **类型名带 `Policy` 后缀**：`ProgrammaticEntryTimingPolicy`、`ProgrammaticPreviewSignalsPolicy`、`ProgrammaticEntryZonePolicy`、`ProgrammaticEntryZoneOverridePolicy`。
2. **Engine 入口是 `GetFullDecision()`**，不是 `Run()`。所有"在 Run() 末尾"的描述应理解为"在 GetFullDecision() 构建 FullDecision 之前"。
3. **`DirectStructureOpen` 字段已存在**于 `ProgrammaticEntryTimingPolicy`，但 config 层当前是普通 `bool`。需把 `ProgrammaticEntryTimingConfig.DirectStructureOpen` 改为 `*bool` 或 presence tracking，profile/policy 层仍可为普通 `bool`。
4. **`evaluatePreviewSignals` 是按单 symbol 调用**（签名含 `symbol string`），不是批量。fast-skip 需在调用前按 symbol 过滤。
5. **config 层类型是 `ProgrammaticStrategyConfig`**（在 `TraderConfig` 中），与 decision 层 `ProgrammaticStrategyPolicy` 是两个类型，中间有转换逻辑。新增配置字段需同时改两处。
6. **`LoosenState` / `SafeMode` 状态**：Engine struct 当前无此字段；`LoosenState` 建议挂在 StateStore 上（与 suppression 同级持久化），safe/loss mode 从 `decision.Context` 读取。
7. **`RiskStateSnapshot`** 已有 `FrequencyPolicy`、`FrequencyState`、`LossMode`，新增字段追加到此 struct。
8. **`defect_fix_pack_enabled`** 放在 `ProgrammaticStrategyConfig`（config 层），使用 `*bool` 区分未配置和显式 false，解析时传入 `ProgrammaticStrategyPolicy`。
9. **`wait_reason_summary`** 必须走 `decision.FullDecision -> trader.AutoTrader -> logger.DecisionRecord` 链路。
10. **候选价差治理** 需要由 `AutoTrader` 注入执行交易所价格 provider，`strategy/chanlun` 不直接依赖具体交易所。
11. **`cmd/replay -defect-fix-pack`** 是 P0 验证前置任务，不应等到 P3 才实现。
