# 缠论 V2 最近 48 小时无开仓规格一致性审查

## 审查结论

结论：规格的核心事实和主因归因与当前日志、配置、代码一致；执行前发现的 3 个轻微不一致/澄清点已在 requirements/design/tasks 中收敛，可以进入任务执行阶段。

## 审查范围

- 规格文档：
  - `.kiro/specs/chanlun-v2-no-open-48h-optimization/requirements.md`
  - `.kiro/specs/chanlun-v2-no-open-48h-optimization/design.md`
  - `.kiro/specs/chanlun-v2-no-open-48h-optimization/tasks.md`
- 相关代码：
  - `strategy/chanlunv2/engine.go`
  - `strategy/chanlunv2/entry_timing.go`
  - `strategy/chanlunv2/loosen_mode.go`
  - `strategy/chanlunv2/engine_test.go`
  - `logger/replay.go`
  - `logger/replay_test.go`
  - `config/config.go`
  - `config.json` 的非敏感字段

## 已确认一致

### 1. 启用 trader 与配置事实一致

本地 `config.json` 只有 `aster_chanlun_v2` 启用，且 `decision_mode=chanlun_v2`、`exchange=aster`、`scan_interval_minutes=3`。`entry_timing.entry_zone.signal_type_min_rr` 中 `sell2=1.1`、`buy3=1.2`、`sell3=1.2`，同时 `strategy_risk.default_min_net_rr=2.5`。

这与 requirements/design 中“旧运行逻辑误用全局 2.5，而 sell2 配置阈值为 1.1”的归因一致。

### 2. 当前 HEAD 已具备信号类型 freshness RR 逻辑

当前 `strategy/chanlunv2/engine.go` 已包含：

- `strategy_diagnostics.active_mode` 和 `strategy_diagnostics.effective_entry_timing`。
- `enrichChanlunV2FreshnessMetadata()` 调用 `effectiveChanlunV2DecisionSignalType(d)`。
- `minRemainingNetRR(ctx, policy, signalType)` 按信号类型读取 `effectiveEntryTiming()`。

当前 `strategy/chanlunv2/loosen_mode.go` 已包含：

- `effectiveEntryTiming(ctx)`。
- loosen mode 对 `EntryZone.MinRemainingNetRR`、`SignalTypeMinRR`、`MaxChaseRatio`、`MinTriggerConfidence` 的调整。

因此 design 中“第一优先级是重启/部署当前 HEAD 并验证日志字段”的结论成立。

### 3. Replay no-open 报表已有基础能力

`logger.BuildOpenRejectionDailyReport()` 已输出：

- `by_reason`、`by_symbol`、`by_bucket`。
- `near_misses`。
- `chanlun_v2_no_open.action_distribution`。
- `waiting_for_trigger_count`、`trigger_ready_count`、`rr_direct_terminal_count`、`suppressed_terminal_count` 等。

因此 Phase 3 不应从零实现 no-open 报表，而应在现有结构上补缺失字段和历史兼容审计。

### 4. 风控边界与代码一致

`validateChanlunV2Decisions()` 仍调用 `decision.ValidateStrategyDecisions()`，后者会经过参数补充、open gate、position sizing 和最终限制。loosen mode 当前只调整 entry timing/阈值，不绕过后续风控。

这与 requirements 的非目标和 design 的风险控制一致。

## 已处理的澄清点

### A. Phase 2 部分测试任务已被当前代码覆盖

`tasks.md` Phase 2 写的是“添加 loosen mode 下 freshness RR 应用 effective threshold 的测试”。当前 `strategy/chanlunv2/engine_test.go` 已有：

- `TestChanlunV2LoosenModeEntersAndAdjustsThresholds`
- `TestChanlunV2LoosenRRAllowsSell2NearMissAcrossEntryAndFreshness`
- `TestV2EntryRRUsesSignalTypeThreshold`
- `TestChanlunV2LoosenDoesNotBypassBTCHardVeto`

处理结果：Phase 2 已改成只补缺口测试：

- 已有测试保持不动。
- 新增一个非 loosen 的 `sell2` freshness RR 回归测试，复现 `remaining_net_rr=1.19`、`sell2 threshold=1.1`、`default_min_net_rr=2.5` 的日志样本。
- 新增缺失信号类型时回退全局/default RR 的测试。

### B. “小仓位试探”与当前 loosen 实现不完全等价

requirements 里原先写到“以小仓位试探”，但当前缠论 V2 loosen mode 主要降低 confidence、signal-type RR 和 chase ratio；`PilotRiskFraction` 只在配置结构里出现，未在 `strategy/chanlunv2` 执行链路里实际缩小仓位。

处理结果：文档已改成“在既有 sizing 风控内试探”，并明确本规格不接入 `PilotRiskFraction` 真实缩仓。

### C. Replay 历史兼容审计需要配置来源

design 写到“从配置或日志推断 signal-type 阈值”。当前 `cmd/replay` 只读日志，`logger/replay.go` 也不应直接读 `config.json`。

处理结果：文档已明确由 `cmd/replay -config` 可选读取运行配置，再把 signal-type 阈值作为参数传入 `logger`；未提供配置时使用日志/默认阈值并写入 notes。

## 验证结果

已执行只读代码与配置核对，未发现规格中主因归因的事实错误。

尝试运行：

```bash
GOCACHE=/tmp/nofx-go-build-cache go test ./strategy/chanlunv2 ./logger ./decision
```

结果：

- `nofx/logger` 通过。
- `nofx/decision` 通过。
- `nofx/strategy/chanlunv2` 构建失败，原因是本地链接器找不到 `-lchanlun_v2`。
- 这属于本地 native 依赖缺失/未配置，不是规格文档或 Go 代码逻辑不一致。

## 建议进入执行前的最小修订

1. 调整 Phase 2 文案，避免重复实现已存在的 loosen/freshness 测试。
2. 明确“小仓位试探”是否需要真正接入仓位缩放。
3. 明确 replay 历史兼容审计是否读取配置，以及配置从哪里进入 `logger`/`cmd/replay`。

完成以上三点后，可以进入 `tasks.md` 的执行阶段。
