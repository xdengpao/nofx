# NOFX 服务最近 2 日无交易单输出诊断任务清单

## 阶段 0：边界确认

- [x] 确认本规格执行期间不降低任何 RR 阈值，最终开仓验证继续保留全局 RR 2.5 硬阈值。
- [x] 确认不新增 `strategy_risk.final_min_net_rr_by_signal_type`、pilot final RR、按场景降低 final RR 的配置或代码路径。
- [x] 确认不关闭 BTC hard veto，不绕过 `ValidateStrategyDecisions()`、position sizing、交易所 preflight 或保护单逻辑。
- [x] 确认不修改真实账户配置、API key、私钥、`data/`、`decision_logs/`、`coin_pool_cache/`。

## 阶段 1：No-order 报告增强

- [x] 复核 `logger/replay.go` 已有能力：`NoSuccessfulOpenHours`、`FreshnessCompatibility`、`ChanlunV2NoOpen`、`BTCGateDiagnostics`、`ConfidenceOverrides`，本阶段只做补缺和 bucket 细化。
- [x] 在 `logger/replay.go` 的 open rejection daily 报告中增加基于决策日志/config 可得的执行路径统计字段：`real_exchange_order_count`、`enabled_trader_count`。
- [x] 如需要统计 Docker 服务日志里的“订单追踪器创建”文案，新增显式服务日志输入，例如 `cmd/replay --service-log`；未提供服务日志输入时，仅在 notes 中保留人工交叉验证提示，不输出 `tracker_only_order_mentions` 断言。
- [x] 在 replay 报告中拆分 no-order bucket：`btc_hard_veto`、`adx_report_only`、`freshness_rr`、`final_rr`、`confidence_gate`、`waiting_for_trigger`、`terminal_suppressed`。
- [x] 在 replay 报告中新增 `final_validation_rr_rejection_count` 和 `freshness_pass_but_final_rr_fail_count`，明确 entry trigger 通过不代表最终可下单。
- [x] 在 replay 报告中把 `ASTERUSDT open_long` 的主因标记为 BTC multi-timeframe hard veto，而不是笼统归因到 ADX。
- [x] 在 replay 报告 notes 中标记 Docker 服务日志覆盖范围与决策日志覆盖范围不一致的风险。
- [x] 补充 `logger/replay_test.go`，覆盖无服务日志输入时不把订单追踪器文案计为真实交易所订单、ADX report-only 不计为硬阻断、final RR 独立归因。
- [x] 运行 `GOCACHE=/tmp/nofx-go-build-cache go test ./logger ./cmd/replay`。
- [x] 用最近 48 小时命令复核历史事实仍为 `record_count=960`、`rejected_open_count=44`，增强后 bucket 包含 `btc_hard_veto`、`adx_report_only` 和 `final_rr`。

## 阶段 2：BTC hard veto 前移与冷却

- [x] 梳理 `decision/open_gate.go` 中 `applyBTCMultiTimeframeGate()`、`isHighBetaAltcoin()`、`isConfirmedBTCBearishStructure()`、`buildBTCGateDiagnostics()` 的 hard veto 条件。
- [x] 在 `decision` 包提取或暴露共享只读 helper，例如 `EvaluateBTCHighBetaLongVeto(symbol, action, btcData)`，返回 veto 结论、reason 和 diagnostics，不改变 open gate 行为。
- [x] 改造 `applyBTCMultiTimeframeGate()` 使用共享 helper，确保原有 open gate 硬阻断语义不变。
- [x] 在缠论 V2 生成 open candidate 前增加 BTC hard veto precheck：BTC confirmed bearish 且高 beta alt `open_long` 时标记 `btc_hard_veto_precheck`。
- [x] 对连续命中同一 BTC hard veto 的 signal 增加短期 cooldown 或 terminal suppression，减少重复 `open_rejected`。
- [x] 确保 BTC regime 变化后 cooldown 可解除，避免 BTC 快速反转时长时间压制有效 long signal。
- [x] 在 `strategy_diagnostics` 中记录 `btc_hard_veto_precheck` 的 symbol、signal_id、reason、cooldown_until 或 terminal reason。
- [x] 确保 short-side near-miss 在 BTC 弱势环境下仍保留诊断输出，不被 long-side veto 噪声掩盖。
- [x] 明确统计口径：历史 replay 仍解释原始 `open_rejected=44`；新行为下 BTC precheck MAY 减少未来 `open_rejected` 并增加 `btc_hard_veto_precheck` 或 terminal suppression 计数。
- [x] 补充 `strategy/chanlunv2/engine_test.go`：BTC bearish 下 high beta long 被 pre-suppressed，重复 signal 进入冷却，BTC regime 解除后可重新评估。
- [x] 运行 `GOCACHE=/tmp/nofx-go-build-cache go test ./strategy/chanlunv2 ./decision`。已构建 `chanlun_v2/target/release/libchanlun_v2.a` 并通过 native focused 验证。

## 阶段 3：RR 阈值语义诊断

- [x] 在 replay 报告中明确区分 `entry_timing.signal_type_min_rr` 与最终开仓验证 RR 2.5，输出 `entry_rr_threshold`、`final_rr_threshold` 和 `final_rr_kept=true`。
- [x] 对 `DOGEUSDT sell2` 类样本输出 `freshness_would_pass_signal_type_rr=true` 但 `final_rr_pass=false`，避免误判为应该下单。
- [x] 在配置或运维说明中补充：`entry_timing.signal_type_min_rr` 只控制 entry trigger，不保证最终可下单。
- [x] 补充 `decision/decision_test.go`：低于 RR 2.5 的候选即使通过 freshness/open gate，也仍被最终开仓验证拒绝。
- [x] 补充 `logger/replay_test.go`：`freshness pass but final RR fail` 被归入独立 bucket。
- [x] 运行 `GOCACHE=/tmp/nofx-go-build-cache go test ./decision ./logger ./cmd/replay`。

## 阶段 4：Loosen 模式可解释性

- [x] 在 `risk_state.frequency_state` 或 `strategy_diagnostics` 中增加 loosen 解释字段：`loosen_applied_rules`、`remaining_hard_blocks`、`loosen_started_at`、`loosen_expires_at`。
- [x] 在 open gate 置信度 override 诊断中保留 `from`、`to`、`actual_confidence`，并在 replay 中汇总 top override rules。
- [x] 当 loosen 后仍无开仓时，报告剩余硬阻断：BTC hard veto、final RR 2.5、执行质量硬停、亏损模式硬停。
- [x] 记录 `max_duration_hours` 当前为配置/诊断值且 `loosenModeController()` 尚未执行持续时间退出；本规格默认不新增实际回到 balanced/safe 的行为。
- [x] 如要让 `max_duration_hours` 触发实际退出，先补充独立设计说明、状态来源、回滚条件和测试，再实施该交易行为变更。
- [x] 补充 `decision`、`strategy/chanlunv2` 或 `logger` 测试，覆盖 loosen 降低置信度但不覆盖 BTC hard veto 和 final RR 2.5。
- [x] 运行 `GOCACHE=/tmp/nofx-go-build-cache go test ./decision ./strategy/chanlunv2 ./logger`。已构建 native lib 并通过 focused native 验证。

## 阶段 5：候选池与 OI Top 后续评估

- [x] 只读检查当前 `coin_pool` 与 `dynamic_candidate_pool` 配置，确认 `未配置OI Top API URL` 对最近 48 小时无订单不是直接原因。
- [x] 形成候选池后续建议：是否配置 OI Top API、是否启用动态候选池、BTC 弱势时是否提高 short-side 候选覆盖。
- [x] 不在本规格内直接启用动态候选池或扩大实盘交易范围，除非另开规格确认风险边界。

## 阶段 6：综合验证与交付

- [x] 运行 focused backend validation：`GOCACHE=/tmp/nofx-go-build-cache go test ./decision ./strategy/chanlunv2 ./logger ./cmd/replay`。已通过 native focused 验证。
- [x] 如修改共享后端契约、JSON schema 或 API 输出，运行 `GOCACHE=/tmp/nofx-go-build-cache go test ./...`。已在 sandbox 外使用本地 native lib 与 1200 秒 timeout 完成全量验证。
- [x] 重新运行最近 48 小时只读 replay，确认报告能清晰输出：历史 `record_count=960`、历史 `rejected_open_count=44`、唯一启用 trader、无真实交易所订单、BTC hard veto、final RR 2.5、loosen hard blocks。
- [x] 自查 `.kiro/hooks/go-write-review.kiro.hook`、`.kiro/hooks/test-file-convention.kiro.hook`、`.kiro/hooks/run-tests-after-task.kiro.hook` 的相关要求。
- [x] 确认 git diff 不包含真实密钥、真实账户敏感信息、运行态日志数据或 RR 阈值降低。
- [x] 将本文件中已完成任务从 `- [ ]` 标记为 `- [x]`，未通过复核的任务标记为 `- [!]` 并写明原因。
