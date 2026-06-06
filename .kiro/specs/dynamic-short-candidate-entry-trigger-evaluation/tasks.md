# 动态候选池 Short-side 覆盖与缠论 V2 Entry Trigger 评估任务

## 执行约束

- 不降低最终开仓验证的全局 RR 2.5。
- 不关闭、不削弱 BTC hard veto。
- 不修改真实 `config.json`、API key、私钥或生产账户凭证。
- 不提交 `data/`、`decision_logs/`、`coin_pool_cache/` 运行时数据。
- 不在测试、replay、评估命令中触发真实下单。
- 默认先执行 Phase 1；Phase 2/3 需在 Phase 1 报告和交叉检查通过后再执行。

## Phase 0: 交叉检查与基线确认

- [x] 0.1 交叉检查 `requirements.md`、`design.md`、现有代码和当前运行配置摘要，确认任务范围不包含降低 RR 2.5、关闭 BTC hard veto 或实盘下单。
  - 验证：输出交叉检查结论，列出需求到设计到任务的映射和不执行项。

- [x] 0.2 复查当前动态候选池、`AutoTrader` 候选上下文、缠论 V2 entry timing、replay no-open 报告的现有字段。
  - 验证：确认涉及文件清单和当前行为基线，包括动态池显式关闭、`entry_trigger_count=0`、父结构 terminal 逻辑。

- [x] 0.3 记录测试入口和禁止触碰目录。
  - 验证：确认后续测试只使用 mock、fixture、公共只读行情或 dry-run，且不会写生产 `data/dynamic_candidate_pool.json`。

## Phase 1: 只读评估与诊断增强

- [x] 1.1 新增 `diagnostics` 包的候选池与 entry trigger 综合评估报告类型。
  - 涉及文件：`diagnostics/candidate_entry_report.go`
  - 验证：报告结构包含 runtime facts、static pool、dynamic pool preview、short-side coverage、entry trigger funnel、layered open probability。

- [x] 1.2 实现最近 48 小时日志事实聚合。
  - 涉及文件：`diagnostics/candidate_entry_report.go`
  - 验证：能输出窗口、记录数、启用 trader、最终动作分布、真实订单数、open-like 候选数、open rejection 分层。

- [x] 1.3 实现 entry trigger 漏斗的日志侧 best-effort 解析。
  - 涉及文件：`diagnostics/candidate_entry_report.go`
  - 验证：兼容已有 `raw_signal_count`、`parent_structure_count`、`entry_trigger_count`、`trigger_rejection_reasons`、CoT 文案和 marker；缺字段时报告明确标记。

- [x] 1.4 实现 Phase 1 只读 short-side 覆盖评估。
  - 涉及文件：`diagnostics/candidate_entry_report.go` 或 `pool/dynamic_candidate_pool.go`
  - 验证：不用新增生产配置字段，也能输出 BTC regime、short-side candidate count、short-side prompt preview、方向 reasons；不改变实际排序。

- [x] 1.5 为 `pool` 增加动态候选池 preview API，支持 dry-run 且不修改全局运行配置。
  - 涉及文件：`pool/dynamic_candidate_pool.go`
  - 验证：`WriteSnapshot=false` 时不写文件；`WriteSnapshot=true` 默认写 `/tmp` 或显式临时路径，不写生产 `data/`；preview 使用传入的 source config，不依赖或污染全局 `coinPoolConfig`/`oiTopConfig`。

- [x] 1.6 新增 `cmd/candidate-entry-eval` 只读评估命令。
  - 涉及文件：`cmd/candidate-entry-eval/main.go`
  - 验证：支持 `-log-dir`、`-trader`、`-config`、`-from`、`-to`、`-output`、`-snapshot-path`、`-dynamic-pool-dry-run`、`-write-snapshot=false`，默认 dry-run；从 config 读取默认币、AI500 URL、OI Top URL、`use_default_coins` 给 preview 使用。

- [x] 1.7 在评估报告中增加开仓概率分层估计。
  - 涉及文件：`diagnostics/candidate_entry_report.go`
  - 验证：输出 candidate coverage、parent signal rate、entry trigger rate、open gate pass rate、final RR 2.5 pass rate、blocking layers；不得承诺一定开仓或收益。

- [x] 1.8 为 `strategy/chanlunv2` 新增结构化 `entry_trigger_funnel` diagnostics。
  - 涉及文件：`strategy/chanlunv2/engine.go`
  - 验证：保留旧字段，同时新增父结构终态、等待 trigger、trigger ready、trigger rejected、terminal suppression 的聚合字段。

- [x] 1.9 为 Phase 1 添加单元测试。
  - 涉及文件：`diagnostics/candidate_entry_report_test.go`、`pool/dynamic_candidate_pool_test.go`、`strategy/chanlunv2/engine_test.go`
  - 验证：覆盖 dry-run preview、source config 不污染全局状态、short-side 只读覆盖评估、缺字段兼容、entry trigger funnel 统计。

- [x] 1.10 运行 Phase 1 验证命令。
  - 验证：至少通过 `go test ./pool ./strategy/chanlunv2 ./diagnostics`；如新增命令依赖 logger，则补跑 `go test ./logger`。

- [x] 1.11 用最近 48 小时日志生成一次本地 dry-run 报告。
  - 验证：输出报告路径在 `/tmp` 或用户指定路径；报告明确动态池预览使用当前公共行情，历史日志事实来自最近 48 小时。
  - 结果：已生成 `/tmp/nofx_candidate_entry_eval_48h_20260606.json`，`write-snapshot=false`，未写生产 `data/dynamic_candidate_pool.json`。

## Phase 2: 动态候选池 Short-side 覆盖灰度

- [x] 2.1 扩展 `config.DynamicCandidatePoolConfig`，新增 `short_side_coverage` 配置。
  - 涉及文件：`config/config.go`
  - 验证：旧配置行为不变；默认 `enabled=false`、`report_only=true`。

- [x] 2.2 将 short-side 配置传入 pool runtime config。
  - 涉及文件：`main.go`、`pool/dynamic_candidate_pool.go`
  - 验证：`initializeModules()` 正确传递归一化配置；未配置时不改变候选排序。

- [x] 2.3 为动态候选池增加 BTC regime 结构化诊断。
  - 涉及文件：`pool/dynamic_candidate_pool.go`
  - 验证：snapshot 包含 BTC 1h/4h 涨跌幅、ADX、DI、EMA 和 regime reasons。

- [x] 2.4 为动态候选新增方向 profile。
  - 涉及文件：`pool/dynamic_candidate_pool.go`
  - 验证：每个候选可记录 `SideProfile`，包括 bias、short score、long score、相对 BTC 强弱和 reasons。

- [x] 2.5 实现 short-side report-only 统计。
  - 涉及文件：`pool/dynamic_candidate_pool.go`
  - 验证：`report_only=true` 时只输出 `ShortSideSummary`，不改变排序、不改变 prompt 选择。

- [x] 2.6 实现 short-side 非 report-only 排序加分与 prompt 保底。
  - 涉及文件：`pool/dynamic_candidate_pool.go`
  - 验证：仅在 BTC 弱势且显式 `report_only=false` 时生效；保底数量受 `min_prompt_count` 和 `max_prompt_ratio` 限制；低流动性/资金费率拥挤/高波动标的仍被剔除。

- [x] 2.7 扩展 `decision.CandidateCoin`、`logger.CandidateSnapshot` 并在 `AutoTrader` 中复制 side diagnostics。
  - 涉及文件：`decision/types.go`、`logger/decision_logger.go`、`trader/auto_trader.go`
  - 验证：`decision.Context` 与 `decision_logs[].candidate_details` 都包含 side bias、short score、long score、side reasons；不改变交易所接口。

- [x] 2.8 同步前端候选详情类型。
  - 涉及文件：`web/src/types/index.ts`
  - 验证：`candidate_details` 新增字段均为可选字段；前端不显示时也能通过类型检查。

- [x] 2.9 动态池启用日志增加 short-side summary。
  - 涉及文件：`trader/auto_trader.go`
  - 验证：日志能看到 `short_side_candidate_count`、`short_side_prompt_count` 和 BTC regime，不泄露敏感配置。

- [x] 2.10 为 Phase 2 添加单元测试。
  - 涉及文件：`config/config_test.go`、`pool/dynamic_candidate_pool_test.go`、`trader/trader_test.go`、必要时 `logger` 或前端类型检查
  - 验证：覆盖默认值、report-only 不改排序、非 report-only prompt 保底、候选上下文字段、日志快照字段。

- [x] 2.11 运行 Phase 2 验证命令。
  - 验证：通过 `go test ./config ./pool ./trader ./logger`；若同步前端类型，运行 `cd web && npm run build`；如触及共享类型导致更广影响，运行 `go test ./...`。
  - 结果：已通过 `go test ./config ./pool ./trader ./logger`、`cd web && npm run build`、`go build ./...`。

## Phase 3: 父结构 RR 终态策略评估

- [ ] 3.1 基于 Phase 1 报告判断是否进入 Phase 3。
  - 验证：只有当报告证明 `entry_rr_invalid` 过早终态化是主要原因时，才继续执行 3.2 以后任务；否则记录不执行原因。

- [ ] 3.2 新增父结构 RR policy 配置，默认保持现状。
  - 涉及文件：`config/config.go`
  - 验证：默认 `mode=terminal` 或等价现状；旧配置行为不变。

- [ ] 3.3 实现 parent RR near-miss report-only 统计。
  - 涉及文件：`strategy/chanlunv2/entry_timing.go`、`strategy/chanlunv2/engine.go`
  - 验证：`report_only=true` 时只记录如果继续观察会影响多少父结构，不改变 lifecycle。

- [ ] 3.4 实现显式配置下的 `watching_entry_rr_near_miss` 观察态。
  - 涉及文件：`strategy/chanlunv2/entry_timing.go`、`strategy/chanlunv2/state.go`
  - 验证：只影响父结构是否继续等待 fresh trigger；trigger 生成后仍经过 entry zone、freshness、open gate、最终 RR 2.5。

- [ ] 3.5 为 Phase 3 添加单元测试。
  - 涉及文件：`strategy/chanlunv2/engine_test.go`、`strategy/chanlunv2/state_test.go`
  - 验证：覆盖 terminal 现状、report-only near-miss、显式观察态、watch window 过期、terminal suppression。

- [ ] 3.6 运行 Phase 3 验证命令。
  - 验证：通过 `go test ./config ./strategy/chanlunv2`；确认最终 RR 2.5 相关测试未被降低或删除。

## Phase 4: 灰度上线文档与运维验证

- [ ] 4.1 编写只读评估运行说明。
  - 涉及文件：可在本 spec 或项目 docs 中补充。
  - 验证：包含本地/服务器命令、输出路径、dry-run 说明、不会触发实盘下单的边界。

- [ ] 4.2 编写动态池 report-only 灰度配置说明。
  - 验证：说明先启用 `dynamic_candidate_pool.enabled=true` 与 `short_side_coverage.report_only=true`，观察 24-48 小时。

- [ ] 4.3 编写 short-side 非 report-only 启用与回滚说明。
  - 验证：包含 `short_side_coverage.report_only=false` 的生效条件、观察指标、回滚到 `report_only=true` 或关闭动态池的方法。

- [ ] 4.4 编写上线后观测清单。
  - 验证：至少包含 `short_side_candidate_count`、`short_side_prompt_count`、`entry_trigger_count`、`open_rejections`、`final_rr` 拒绝比例、真实订单数、loss mode 状态。

- [ ] 4.5 执行最终交叉检查。
  - 验证：确认需求、设计、任务、代码、测试、运行说明一致；确认没有降低 RR 2.5、没有关闭 BTC hard veto、没有实盘下单路径变化。

## 推荐执行顺序

- [x] A. 先执行 Phase 0 + Phase 1，生成只读报告。
- [x] B. 根据报告决定是否执行 Phase 2。
- [ ] C. Phase 2 上线时先保持 `report_only=true`。
- [ ] D. 只有当 Phase 1 报告证明父结构 RR 终态是主要瓶颈时，才执行 Phase 3。
- [ ] E. 任一阶段上线前先执行对应测试和最终交叉检查。
