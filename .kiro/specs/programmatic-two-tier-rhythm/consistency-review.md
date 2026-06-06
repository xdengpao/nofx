# 程序化策略双层节奏 Consistency Review

复核日期：2026-05-17

## 结论

当前 `requirements.md`、`design.md`、`tasks.md` 的主方向一致：去掉 `15m shadow/report-only`，只保留主交易级别可配置，并把“主信号确认”和“已有持仓管理”拆成两层节奏。

但在执行 tasks 前，建议先修正下面的 P0/P1 问题。否则实现时容易出现：熔断时持仓管理仍被跳过、程序化减仓/止损动作缺少前置校验、公共层和程序化层同 symbol 重复输出风险降低动作、百分比配置单位误用。

## 修正状态

2026-05-17 已将本 review 的问题补充到 `requirements.md`、`design.md` 和 `tasks.md`：

- 已把 halt 拆成 `risk_increase_blocked` 和 `full_stop`。
- 已新增程序化风险降低动作的非 open-like 校验方案。
- 已补充公共层与程序化层非 open-like 冲突消解规则。
- 已明确止盈更新、交易计划同步和保护单同步继续归公共层负责。
- 已固定 `buffer_pct`、`drawdown_pct` 等百分比字段单位。
- 已补充持仓管理动作确定性 `SignalID` 规则。
- 已把 tasks 中“确认”类项改为实现和测试项。

## 已检查范围

- `.kiro/specs/programmatic-two-tier-rhythm/requirements.md`
- `.kiro/specs/programmatic-two-tier-rhythm/design.md`
- `.kiro/specs/programmatic-two-tier-rhythm/tasks.md`
- `strategy/chanlun/engine.go`
- `strategy/chanlun/state.go`
- `decision/decision.go`
- `decision/types.go`
- `decision/utils.go`
- `config/programmatic.go`
- `config/config.go`
- `manager/trader_manager.go`
- `trader/auto_trader.go`

已确认 spec 中没有残留 `15m shadow/report-only` 需求。

## Findings

### P0: 全局 halt 语义与“仍允许风险降低动作”冲突

证据：

- Requirements 要求全局熔断或账户回撤硬停时禁止开仓/加仓，但仍允许风险降低型持仓管理动作。
- Design 的 Target Flow 和 `GetFullDecision()` 流程写明：`PrepareCycleContext` 返回 halt 后直接返回。
- Tasks Phase 3 仍写着“公共层 halt/wait 仍可覆盖策略层”。
- 现有代码中 `decision.PrepareCycleContext()` 在已有熔断状态命中时会在拉取行情和评估已有持仓前直接返回 `HaltDecision`。
- 现有 `strategy/chanlun.Engine.GetFullDecision()` 收到 `prep.HaltDecision != nil` 后直接返回，不会运行程序化持仓管理层。

影响：

- 若全局熔断 cooldown 正在生效，已有持仓不会进入程序化保本止损、浮盈回撤、结构破坏、短差减仓判断。
- 这与本需求的核心目标相反：风险增加型动作应被 halt 阻断，风险降低型动作不应被 halt 阻断。

建议修正：

- 在 design 中把 halt 拆成两类语义：
  - `risk_increase_blocked`：禁止 open/add，但允许公共持仓决策和程序化风险降低动作继续运行。
  - `full_stop`：行情缺失、交易所不可用、上下文不可用等无法安全评估持仓时才整体停止。
- 调整 `PrepareCycleContext` 或增加选项，例如 `AllowRiskReducingOnHalt` / `ProgrammaticRiskReducingMode`：命中熔断或账户硬停时仍拉取已有持仓 symbol 行情，继续生成公共持仓决策，并把 halt reason 带回给策略层。
- 在程序化 `GetFullDecision()` 中，若处于 `risk_increase_blocked`，跳过主信号层，只运行持仓管理层，然后和公共持仓决策合并。
- 修改 tasks Phase 3/6，不再写“halt/wait 直接覆盖策略层”，改为“halt 阻断 open/add，但不得阻断风险降低型动作”。
- 增加测试：熔断/账户硬停 + 已有持仓 + 满足保本或回撤条件时，不输出 open/add，但仍可输出 `update_stop_loss`、`partial_close` 或 close。

### P1: 程序化非 open-like 动作缺少明确校验路径

证据：

- Design 写“调用 `decision.ValidateStrategyDecisions`”，Tasks 写“确认或等价非 open-like 校验路径”。
- 现有 `ValidateStrategyDecisions()` 只深度校验 `open_long/open_short/add_long/add_short`，对 `partial_close`、`close_long/close_short`、`update_stop_loss` 是直接放行。
- 持仓管理层会新增多种非 open-like 决策，且这些决策直接进入真实执行链路。

影响：

- `partial_close` 可能缺少持仓、方向不匹配或比例异常。
- `update_stop_loss` 可能在策略层输出一个不改善保护效果、方向错误、靠近/穿越当前价格的止损价。
- 错误会推迟到 executor 才失败，日志和策略诊断会变得不稳定；更糟时可能产生重复保护单调整。

建议修正：

- 新增或扩展一个确定性校验函数，例如 `ValidateRiskReducingStrategyDecisions(ctx, decisions, opts)`。
- 校验至少覆盖：
  - action 只允许 `close_long`、`close_short`、`partial_close`、`update_stop_loss`；如不实现程序化止盈更新，拒绝 `update_take_profit`。
  - symbol 必须存在已有持仓。
  - close action 方向必须和持仓 side 匹配。
  - `partial_close.ClosePercentage` 必须在 `(0, 100]`。
  - `update_stop_loss.NewStopLoss` 必须有效：多头不得低于已有有效止损，且应低于当前价；空头不得高于已有有效止损，且应高于当前价。
  - 同一 symbol/side 同一周期最多保留一个程序化持仓管理主动作。
  - 程序化动作必须带 `StrategyMode`、`StrategyName`、`StrategyVersion`、`ConfigHash`、`SignalID`、`StrategyMetadata.layer/rule`。
- tasks Phase 6 不应只写“确认”，应增加实现和测试该校验路径。

### P1: 公共层与程序化层的非 open-like 决策冲突规则不完整

证据：

- Requirements 要求同周期存在公共层、持仓管理层、主信号层决策时保持清晰优先级。
- Design 只说明公共层同 symbol 的 close/partial/update_stop_loss/update_take_profit 会阻断程序化 open/add。
- 现有 `MergePublicAndStrategyDecisions()` 也只阻断程序化 open-like 动作，不处理公共层和程序化层同时输出 `partial_close`、close 或 `update_stop_loss` 的冲突。

影响：

- 同一 symbol 可能同时出现公共层 `partial_close` 和程序化 `partial_close`，导致重复减仓意图。
- 公共层 `close_long` 与程序化 `partial_close` 同时存在时，虽然执行排序会先 close，但后续 partial close 可能失败或污染日志。
- 公共层 `update_stop_loss` 与程序化 `update_stop_loss` 同时存在时，最终保留哪一个止损价不确定。

建议修正：

- 在 design 中定义公共层与程序化非 open-like 动作的冲突消解规则。
- 推荐首期保守规则：
  - 公共层强制 close、交易计划硬止损/失效、账户硬停 close 永远优先，并压制同 symbol 程序化非 open-like 动作。
  - 若公共层已有 `partial_close`，压制同 symbol 程序化 `partial_close/close/update_stop_loss`，交给公共层执行后同步保护单。
  - 若仅存在多个 `update_stop_loss`，保留保护效果更强且方向合法的一个。
  - 程序化 open/add 继续被任何同 symbol 公共风险动作阻断。
- 增加 merge 测试，覆盖 public close + programmatic partial、public partial + programmatic stop、双 stop 更新等情况。

### P1: “止盈/交易计划同步”的归属不清晰

证据：

- Requirements 的持仓管理层写到“止损/止盈更新和交易计划同步相关状态”。
- Design 的程序化持仓管理输出只包含 `update_stop_loss`、`partial_close`、`close_long`、`close_short`。
- 现有公共层已经有交易计划、峰值数据、止盈止损同步相关逻辑；本 spec 又要求保留公共层。

影响：

- 实现者可能误以为程序化持仓管理层也要输出 `update_take_profit` 或直接改交易计划，扩大本次改动范围。
- 如果程序化层和公共层都同步止盈/计划，容易出现重复更新或优先级不明确。

建议修正：

- 首期明确归属：程序化持仓管理层只读交易计划和 peak 状态，用于生成风险降低动作；交易计划同步、保护单同步、止盈更新继续由公共层负责。
- 若确实要让程序化层输出 `update_take_profit`，需要新增独立规则、配置、校验、merge 优先级和 executor 测试；不建议纳入本次双层节奏首期。
- 在 requirements 中把“止损/止盈更新和交易计划同步相关状态”改成“继续保留公共层止损/止盈更新和交易计划同步；程序化层可输出保本 `update_stop_loss`”。

### P1: 百分比字段单位存在误用风险

证据：

- Design 示例中 `breakeven.buffer_pct` 默认写 `0.05`。
- Design 又说明“允许写 `1.0` 表示 1%，写 `0.01` 也可归一化为 1%”。
- 现有 `normalizePercentRatio()` 规则是：`value >= 0.1` 才除以 100；因此 `0.05` 会保留为 `0.05`，即运行时 ratio 5%，不是 0.05%。
- 现有 `PositionInfo.UnrealizedPnLPct` 和 `TradePlan.PeakPnLPercent` 使用的是人类百分数单位，例如 `1.0` 表示 1%。

影响：

- 保本缓冲如果按现有 ratio 规则实现，`0.05` 会把多头保本止损设置到入场价上方 5%，非常激进，可能被当前价约束频繁拒绝或产生错误保护意图。
- `activation_profit_pct`、`trigger_profit_pct`、`drawdown_pct`、`buffer_pct` 如果混用“百分数”和“比例”单位，后续排查会很困难。

建议修正：

- 在 design 中明确每个字段的运行时单位：
  - 与 `UnrealizedPnLPct` / `PeakPnLPercent` 比较的字段使用人类百分数：`trigger_profit_pct=1.0` 表示 1%，`activation_profit_pct=2.0` 表示 2%。
  - 回撤计算内部是 ratio，配置 `drawdown_pct=35` 表示 35%，归一化后用于比较 `0.35`。
  - 价格缓冲建议二选一：
    - 改名为 `buffer_ratio`，默认 `0.0005` 表示 0.05%。
    - 或保留 `buffer_pct`，默认 `0.05` 表示 0.05%，但必须使用专门的人类百分数归一化函数，不能直接复用 `normalizePercentRatio()`。
- 增加 config 测试，明确 `buffer_pct=0.05` 的归一化结果，避免上线后误读。

### P2: 持仓管理动作的 `SignalID` 生成规则未定义

证据：

- Tasks 要求所有持仓管理动作写入 `SignalID`。
- State 设计要求记录 `LastBreakevenSignalID`、`LastDrawdownSignalID`、`LastStructureSignalID`、`LastShortTradeSignalID`。
- Design 只为短差减仓提到 `StableSignalID()`，没有定义保本、浮盈回撤、结构破坏的 deterministic id。

影响：

- 重启后可能重复触发同一结构破坏或同一回撤保护。
- 不同实现者可能使用时间戳生成 id，导致去重失效。

建议修正：

- 在 design 中新增统一 id 规则，例如：
  - `pm:{trader_id}:{symbol}:{side}:{rule}:{timeframe}:{trigger_close_time}:{level_or_hash}:{config_hash}`
  - 保本止损可使用 `entry_price/current_stop_loss/target_stop_loss` 的结构化 hash。
  - 浮盈回撤可使用 peak 更新时间、peak 值、触发 close time、阈值 hash。
  - 结构破坏可使用 structure timeframe、关键结构位、触发 K 线 close time。
- 状态去重基于 `rule + signal_id`，而不是纯时间戳。

### P2: 默认组件级别推导需要区分“未配置”和“显式配置”

证据：

- Requirements 要求 `trade=15m/1h/4h` 时默认组件级别分别为 `3m/15m/1h`。
- Design 已写默认推导。
- 现有 `normalizeProgrammaticTimeframes()` 一开始就把 `sub` 默认成 `15m`，因此如果用户只配置 `trade=15m` 而不写 `sub`，现有形态无法推导出 `sub=3m`。

影响：

- `trade=15m` 配置会意外使用 `15m` 作为 sub，和 requirements 不一致。

建议修正：

- 实现时先规范化 `trade`，再判断 `cfg.Sub == ""` 时按 trade 推导默认值。
- 只有用户显式配置了 `sub` / `micro` 时才覆盖默认推导。
- 增加测试：只配置 `trade=15m` 时 `sub=3m`；只配置 `trade=4h` 时 `sub=1h`。

## 建议的 spec 修正顺序

1. 先修正 halt 语义，明确“禁止风险增加”不等于“停止持仓风险降低”。
2. 再补充非 open-like 校验和公共/程序化非 open-like 冲突消解。
3. 明确止盈/交易计划同步归属，避免扩大实现范围。
4. 固化百分比单位和 SignalID 规则。
5. 最后更新 tasks，把“确认”类任务改成可验收的实现/测试任务。
