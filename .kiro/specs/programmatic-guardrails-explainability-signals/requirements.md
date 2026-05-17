# 程序化策略减仓约束、原因说明与信号展示 Requirements

## Background

NOFX 已支持 `decision_mode=programmatic`，并完成程序化策略双层节奏：主交易级别 `trade` 负责新开仓/加仓，持仓管理层在每个 `scan_interval` 周期继续处理已有持仓。

线上观察到 ETHUSDT 在两个连续 3m 周期内触发两次 `partial_close`：

- 第一次来自 `short_trade`：空仓遇到 3m `buy3` 反向短差信号。
- 第二次来自 `floating_drawdown`：峰值浮盈约 2.14%，当前浮盈约 1.38%，回撤约 35.4%，超过 `drawdown_pct=35`。

这不是同一个 `signal_id` 重复执行，但从实盘交易节奏看，同一持仓跨规则连续减仓过于激进，尤其小账户会受到手续费、滑点和最小名义额影响。因此需要新增跨规则减仓冷却、减仓预算和更清晰的动作原因说明。

同时，当前前端“策略检查”已有最新信号、信号诊断和固定 `1h K线` 表格，但 K 线级别没有自动跟随程序化策略主交易级别，也没有在 K 线数据中标记三类买卖点。量化交易员需要在最新信号栏看到主交易频率 K 线，并按买卖点分类分级定位信号出现的位置。

本规格是以下规格的增量：

- `.kiro/specs/programmatic-chanlun-strategy`
- `.kiro/specs/programmatic-two-tier-rhythm`

## Goals

- 避免同一持仓在短时间内被多个程序化持仓管理规则连续 `partial_close`。
- 为每次开仓、加仓、减仓、平仓、止损更新和被拒绝动作提供可读、可审计的原因说明。
- 让前端“策略检查/最新信号”展示主交易级别 K 线，并在 K 线上分类分级标记买卖点。
- 保留公共强制风控、保护单同步、交易计划同步和交易所执行保护的最高优先级。

## Non-Goals

- 本规格不改变程序化策略的核心买卖点识别算法。
- 本规格不引入回测能力；不同主交易级别的信号质量仍由后续回测规格验证。
- 本规格不要求使用 AI 解释程序化策略动作。
- 本规格不把前端 K 线表升级为完整 TradingView 类图表；首期以可读 K 线表、标记、筛选和诊断为目标。
- 本规格不绕过现有 `decision.Decision`、交易计划、执行前检查和交易所最小名义额规则。

## Glossary

- **跨规则减仓冷却**：同一 `trader + symbol + side` 发生一次程序化 `partial_close` 后，在配置时间内限制其他程序化规则再次输出 `partial_close`。
- **减仓预算**：同一持仓生命周期内程序化策略允许累计执行的 `partial_close` 次数和累计减仓比例上限。
- **风险降低动作**：`partial_close`、`close_long`、`close_short`、`update_stop_loss` 等不会增加净风险敞口的动作。
- **强制风控动作**：公共层硬止损、账户硬停、全局熔断、交易计划硬退出、保护单修复等安全优先动作。
- **动作原因说明**：面向用户和日志的结构化解释，说明动作由哪个层、哪个规则、哪个信号、哪个价格/阈值触发，以及是否受冷却、预算或公共风控影响。
- **主交易级别 K 线**：`programmatic_strategy.timeframes.trade` 对应的 K 线级别，允许 `15m`、`1h`、`4h`。
- **买卖点分类分级标记**：在 K 线数据中标识 `buy1/buy2/buy3/sell1/sell2/sell3`，并区分方向、级别、触发 timeframe、确认状态和执行状态。

## Requirements

### 1. 跨规则 partial_close 冷却

**User Story:** 作为量化交易员，我希望同一持仓在一次程序化部分平仓后进入冷却，避免短差减仓和浮盈回撤等规则在连续扫描周期内重复砍仓。

#### Acceptance Criteria

1. WHEN 程序化持仓管理层对某 `trader + symbol + side` 成功输出并执行 `partial_close` THEN 系统 SHALL 记录最近一次程序化部分平仓的时间、规则、signal id、减仓比例和成交后持仓状态。
2. WHEN 同一 `trader + symbol + side` 仍处于 partial close 冷却期 THEN `short_trade` 和 `floating_drawdown` SHALL 不再输出新的 `partial_close`。
3. WHEN `position_management.partial_close_cooldown_minutes` 未配置 THEN 系统 SHALL 使用默认值 `15` 分钟。
4. WHEN `position_management.partial_close_cooldown_minutes` 配置为 `0` THEN 系统 SHALL 关闭跨规则冷却，但仍保留同 signal id 去重和减仓预算。
5. WHEN 冷却阻断某个程序化 `partial_close` THEN 决策诊断 SHALL 记录被阻断的规则、剩余冷却时间、最近一次减仓规则和最近一次 signal id。
6. WHEN 冷却期内触发 `update_stop_loss` THEN 系统 SHALL 允许该止损更新继续评估和执行。
7. WHEN 冷却期内触发 `close_long` 或 `close_short` 且来源为强制风控、交易计划硬退出、保护单同步或结构破坏升级为全平 THEN 系统 SHALL 不得用 partial close 冷却阻断该平仓。
8. WHEN 冷却期内触发主级别 open/add THEN 仍 SHALL 按原有主信号层、开仓频率和风险增加阻断规则判断，不得把 partial close 冷却误用为全局交易暂停。

### 2. partial_close 减仓预算

**User Story:** 作为量化交易员，我希望同一持仓的程序化部分平仓次数和累计减仓比例有上限，避免一个持仓被多个规则碎片化减到过小。

#### Acceptance Criteria

1. WHEN 新持仓首次进入程序化状态 THEN 系统 SHALL 初始化该 `trader + symbol + side` 的 partial close 预算状态。
2. WHEN `position_management.max_partial_close_count_per_position` 未配置 THEN 系统 SHALL 使用默认值 `2`。
3. WHEN `position_management.max_total_partial_close_pct` 未配置 THEN 系统 SHALL 使用默认值 `50`，表示同一持仓生命周期内程序化 partial close 累计最多减掉原始可跟踪仓位的 50%。
4. WHEN 程序化 partial close 已达到次数上限 THEN `short_trade` 和 `floating_drawdown` SHALL 不再输出新的 `partial_close`，并 SHALL 记录预算耗尽原因。
5. WHEN 程序化 partial close 将导致累计减仓比例超过上限 THEN 系统 SHALL 将本次减仓比例裁剪到剩余预算；IF 裁剪后低于交易所最小可执行名义额 THEN 系统 SHALL 跳过本次部分平仓并记录原因。
6. WHEN 公共层交易计划分批止盈执行 partial close THEN 该动作 SHALL 可单独记录为公共层减仓，不得错误消耗程序化 partial close 预算，除非 design 阶段明确选择统一预算。
7. WHEN 程序化 partial close 成功执行 THEN 系统 SHALL 基于实际执行比例或实际成交数量更新预算状态；IF 无法获得实际成交数量 THEN SHALL 使用决策比例作为保守估计。
8. WHEN 持仓完全平掉、反手或方向变化 THEN 系统 SHALL 清理或重置该 `symbol + side` 的 partial close 预算状态。

### 3. 浮盈回撤二次触发条件

**User Story:** 作为量化交易员，我希望浮盈回撤在一次减仓后只有出现新的峰值或新的持仓生命周期时才再次触发，避免同一段回撤被多次处理。

#### Acceptance Criteria

1. WHEN `floating_drawdown` 输出并执行 `partial_close` THEN 系统 SHALL 记录触发时的 peak price、peak pnl、peak R、current pnl 和 signal id。
2. WHEN 已存在已处理的 `floating_drawdown` partial close 且未出现新的 peak THEN 系统 SHALL 不再为同一回撤段输出第二次 `floating_drawdown` partial close。
3. WHEN 当前价格刷新对持仓有利的新 peak 或 peak R THEN 系统 MAY 重置 `floating_drawdown` 已处理状态，并允许后续新的回撤段重新评估。
4. WHEN `floating_drawdown.action=close` THEN 系统 SHALL 可直接输出全平动作，并不受 partial close 次数预算限制，但仍 SHALL 经过公共执行前检查和持仓方向校验。
5. WHEN 浮盈回撤因“未出现新 peak”被跳过 THEN 决策诊断 SHALL 展示上次处理 signal id、上次 peak 和当前 peak。

### 4. 结构破坏与强制风控豁免

**User Story:** 作为量化交易员，我希望冷却和预算限制只抑制节奏过密的部分平仓，不阻断真正的结构破坏或强制风险退出。

#### Acceptance Criteria

1. WHEN `structure_break.action=partial_close` 且 partial close 冷却或预算已触发 THEN 系统 SHALL 根据配置决定跳过、裁剪或升级为全平候选。
2. WHEN `structure_break.action=close` THEN 系统 SHALL 不受 partial close 冷却和 partial close 预算限制。
3. WHEN 公共层输出强制平仓、交易计划硬退出、保护单修复或账户安全退出 THEN 程序化 partial close 冷却和预算 SHALL 不得阻断公共层动作。
4. WHEN 公共层 close 与程序化 partial close 同周期冲突 THEN 公共层 close SHALL 压制程序化 partial close。
5. WHEN 公共层 partial close 与程序化 partial close 同周期冲突 THEN 系统 SHALL 使用现有冲突消解规则保证同一 symbol 同一周期最多执行一个减仓类动作。

### 5. 持仓管理配置

**User Story:** 作为系统维护者，我希望新增冷却和预算参数可配置、可校验、可审计，生产默认偏保守。

#### Acceptance Criteria

1. WHEN 配置存在 `programmatic_strategy.position_management.partial_close_cooldown_minutes` THEN 系统 SHALL 校验其范围为 `0-1440`。
2. WHEN 配置存在 `programmatic_strategy.position_management.max_partial_close_count_per_position` THEN 系统 SHALL 校验其范围为 `1-10`。
3. WHEN 配置存在 `programmatic_strategy.position_management.max_total_partial_close_pct` THEN 系统 SHALL 按人类百分数解析，并校验范围为 `1-100`。
4. WHEN 配置存在 `position_management.short_trade.partial_close_pct` THEN 系统 SHALL 继续按人类百分数解析，且不得超过剩余程序化减仓预算。
5. WHEN 新增配置缺失 THEN 系统 SHALL 保持向后兼容并使用默认值：冷却 `15` 分钟、次数上限 `2`、累计比例上限 `50%`。
6. WHEN 这些配置发生变化 THEN 程序化策略 `config_hash` SHALL 变化，决策日志 SHALL 可追溯到对应参数快照。
7. WHEN 配置非法 THEN 服务启动或 trader 初始化 SHALL 返回中文错误，不得静默降级为危险默认值。

### 6. 持仓状态持久化

**User Story:** 作为量化交易员，我希望减仓冷却和预算状态在服务重启后仍然有效，避免重启造成重复减仓。

#### Acceptance Criteria

1. WHEN 程序化 partial close 执行成功 THEN 系统 SHALL 在 `data/programmatic_strategy_state.json` 中按 `trader + symbol + side` 持久化 partial close 管理状态。
2. persisted state SHALL 至少包含最近一次 partial close 时间、规则、signal id、累计程序化 partial close 次数、累计程序化减仓比例、上次浮盈回撤 peak 和是否需要新 peak 才可再次触发。
3. WHEN 状态文件缺少新增字段 THEN 系统 SHALL 按空状态兼容读取，不得导致 trader 启动失败。
4. WHEN 持仓消失或方向变化 THEN 系统 SHALL 在后续周期清理旧方向的 partial close 管理状态。
5. WHEN 服务重启后同一持仓仍存在 THEN 系统 SHALL 继续使用持久化冷却和预算状态。
6. WHEN 状态读取失败 THEN 系统 SHALL 记录中文告警，并使用保守行为：不重复执行已能从决策日志或交易计划识别的近期待处理 partial close；若无法识别，则按空状态启动但输出诊断。

### 7. 每次开平仓原因说明

**User Story:** 作为量化交易员，我希望每一次开仓、加仓、减仓、平仓和止损更新都有明确原因说明，以便复盘策略行为，而不是只看到 action 和 symbol。

#### Acceptance Criteria

1. WHEN 系统生成任意真实交易动作 THEN `decision.Decision` SHALL 包含用户可读的 `reasoning`。
2. WHEN 程序化策略生成动作 THEN 原因说明 SHALL 包含策略层级、规则名、信号类型、使用 timeframe、关键价格、关键阈值、signal id 和 config hash。
3. WHEN AI 模式生成动作 THEN 原因说明 SHALL 保留 AI 决策摘要，同时执行日志 SHALL 补充确定性风控改写、拒绝或 sizing 调整原因。
4. WHEN 动作经过 open gate、仓位 sizing、ATR/ADX profile、相关性、频率控制或执行前检查改写 THEN 最终日志 SHALL 同时保留原始建议原因和最终执行/拒绝原因。
5. WHEN 动作被冷却、预算、最小名义额、持仓方向、风控或公共层冲突消解拒绝 THEN 系统 SHALL 记录拒绝原因、拒绝来源和可操作诊断。
6. WHEN 交易动作执行成功 THEN 执行记录 SHALL 展示“为什么执行”和“执行结果”，包括成交方向、成交价格、比例/数量、是否更新交易计划。
7. WHEN 交易动作执行失败 THEN 执行记录 SHALL 展示“为什么尝试执行”和“为什么失败”，不得只展示交易所原始错误。
8. WHEN 前端展示程序化策略周期摘要 THEN 文案 SHALL 使用“策略分析/策略说明”等中性表述，不应把程序化策略说明误标为 AI 思维链。

### 8. 结构化动作原因 schema

**User Story:** 作为系统维护者，我希望动作原因既能给人读，也能给前端和 replay 结构化消费。

#### Acceptance Criteria

1. WHEN 决策日志写入任意交易动作 THEN SHALL 包含结构化 explanation 字段或等价 `strategy_metadata/strategy_diagnostics` 字段。
2. 结构化原因 SHOULD 包含：`layer`、`rule`、`reason_code`、`timeframe`、`signal_type`、`signal_id`、`trigger_price`、`reference_price`、`threshold`、`cooldown_status`、`budget_status`、`risk_checks`。
3. WHEN 字段暂时无法获得 THEN 系统 SHALL 省略该字段或置为明确空值，不得伪造指标。
4. WHEN 前端读取旧日志或旧 API 响应缺少结构化原因 THEN SHALL 继续显示原有 `reasoning`，保持兼容。
5. WHEN replay 或离线分析读取新字段 THEN SHALL 能区分动作来源为公共层、AI 层、程序化主信号层或程序化持仓管理层。

### 9. 策略检查使用主交易级别 K 线

**User Story:** 作为量化交易员，我希望“策略检查”面板展示的 K 线级别自动跟随程序化策略主交易级别，而不是固定写死为 `1h`。

#### Acceptance Criteria

1. WHEN trader 使用程序化策略 THEN 前端 SHALL 从策略信号报告、状态或配置摘要中获取当前 `trade` 主交易级别。
2. WHEN 当前 `trade` 为 `15m` THEN 策略检查 K 线区域 SHALL 默认请求并展示 `15m` K 线。
3. WHEN 当前 `trade` 为 `1h` THEN 策略检查 K 线区域 SHALL 默认请求并展示 `1h` K 线。
4. WHEN 当前 `trade` 为 `4h` THEN 策略检查 K 线区域 SHALL 默认请求并展示 `4h` K 线。
5. WHEN 主交易级别变化 THEN SWR/cache key SHALL 随 timeframe 变化，避免继续展示旧级别 K 线。
6. WHEN 策略信号报告暂时没有 timeframe 信息 THEN 前端 SHALL fallback 到 `1h`，并在诊断区显示“未获取主交易级别，使用1h回退”。
7. WHEN K 线表格标题展示 timeframe THEN SHALL 使用动态标题，例如 `15m K线`、`1h K线`、`4h K线`。

### 10. K 线买卖点标记

**User Story:** 作为量化交易员，我希望在主交易级别 K 线数据中看到买卖点分类分级标记，以便知道 buy1/buy2/buy3/sell1/sell2/sell3 发生在哪根 K 线附近。

#### Acceptance Criteria

1. WHEN 策略识别到主交易级别买卖点信号 THEN `/api/strategy/signals` SHALL 返回可映射到 K 线的时间字段，至少包含 confirmed close time 或 trigger close time。
2. WHEN 前端展示 K 线表格 THEN 每根 K 线 SHALL 能展示该 close time 附近的信号标记。
3. 买点 SHALL 与卖点视觉区分：buy 系列使用多头/绿色系标记，sell 系列使用空头/红色系标记。
4. 一类、二类、三类买卖点 SHALL 分级展示，例如 `B1/B2/B3`、`S1/S2/S3` 或 `buy1/buy2/buy3/sell1/sell2/sell3`。
5. WHEN 同一根 K 线存在多个信号 THEN 前端 SHALL 可同时展示多个标记，不得互相覆盖导致信息丢失。
6. WHEN 信号来自主交易级别 THEN 标记 SHALL 显示为主信号；WHEN 信号来自持仓管理微观级别 THEN 标记 SHALL 显示为持仓管理/微观信号，不得混同。
7. WHEN 信号已经执行、被拒绝、仅诊断或等待新闭合 K 线 THEN 标记 SHALL 显示对应状态。
8. WHEN 用户选择不同 symbol THEN K 线和买卖点标记 SHALL 同步切换到该 symbol。

### 11. 最新信号栏增强

**User Story:** 作为量化交易员，我希望最新信号栏不仅显示一句诊断，还能把当前 symbol 的主交易级别 K 线、最近信号和买卖点位置放在同一个检查视图里。

#### Acceptance Criteria

1. WHEN 有最新主信号 THEN 最新信号栏 SHALL 显示 symbol、signal type、方向、主交易级别、触发价格、止损、止盈、结构目标和确认时间。
2. WHEN 没有最新主信号但有诊断 THEN 最新信号栏 SHALL 显示“无新闭合K线”“无足够走势段”“无信号”等诊断，并显示下一根主交易级别 K 线预计闭合时间（如可计算）。
3. WHEN 当前 symbol 有持仓管理动作 THEN 最新信号栏 SHALL 能显示最近持仓管理信号，例如 `short_trade buy3 3m partial_close` 或 `floating_drawdown partial_close`。
4. WHEN 最新信号栏展示 K 线数据 THEN SHALL 使用主交易级别 K 线，并提供最近 N 根 K 线，默认 N 不低于 8。
5. WHEN K 线存在买卖点标记 THEN 最新信号栏或 K 线区域 SHALL 允许用户快速识别信号所在行。
6. WHEN 页面宽度不足 THEN K 线数据和信号标记 SHALL 保持可横向滚动或响应式换行，不得遮挡文字。
7. WHEN trader 处于 AI 模式 THEN 策略检查 SHALL 保持兼容，提示当前 trader 使用 AI 决策模式，不请求程序化信号标记。

### 12. API 契约

**User Story:** 作为前端开发者，我希望策略信号 API 能一次性返回展示 K 线标记所需的核心元数据，减少前端猜测。

#### Acceptance Criteria

1. `/api/strategy/signals` response SHALL 增加可选字段 `trade_timeframe`、`component_timeframe`、`micro_timeframe`。
2. `/api/strategy/signals` response SHALL 增加可选字段 `signal_markers` 或在 `signals[]` 中补齐 marker 所需字段。
3. marker SHALL 至少包含 `symbol`、`timeframe`、`close_time`、`signal_type`、`direction`、`level`、`source_layer`、`status`、`signal_id`。
4. WHEN 后端无法计算 marker close time THEN SHALL 返回信号本身并在 diagnostics 中说明缺少映射时间。
5. `/api/market/klines` SHALL 保持向后兼容，不要求为了标记直接改变 K 线 DTO；若需要扩展，新增字段 SHALL 为可选字段。
6. 前端 TypeScript 类型 SHALL 与后端 JSON tag 同步，并保持旧字段可选兼容。
7. WHEN API 请求的 timeframe 不是 `3m/15m/1h/4h` THEN 后端 SHALL 返回中文错误。

### 13. 决策日志与前端展示一致性

**User Story:** 作为量化交易员，我希望前端看到的动作原因、信号标记和后端决策日志一致，以便排查实盘行为。

#### Acceptance Criteria

1. WHEN 后端决策日志记录某个程序化信号 THEN `/api/strategy/signals` SHALL 能在后续周期中展示该信号或其最近状态，除非状态已被明确清理。
2. WHEN 某信号导致真实交易动作 THEN 决策日志、执行记录和前端标记 SHALL 使用同一个 `signal_id`。
3. WHEN 某信号被拒绝 THEN 前端 SHALL 能显示拒绝状态和拒绝原因，而不是只显示“暂无信号”。
4. WHEN 同一信号因去重被跳过 THEN 前端诊断 SHALL 能显示“已处理”状态。
5. WHEN 页面刷新 THEN 最新信号、K 线标记和诊断 SHALL 从后端状态恢复，不依赖前端本地状态。

### 14. 测试要求

**User Story:** 作为维护者，我希望新增约束和展示能力有测试覆盖，避免后续策略迭代重新引入连续减仓和前后端字段不一致。

#### Acceptance Criteria

1. Config tests SHALL 覆盖 partial close 冷却、次数上限、累计比例上限的默认值、合法值和非法值。
2. State tests SHALL 覆盖旧状态文件兼容、新字段持久化、服务重启后冷却仍生效、持仓消失后状态清理。
3. Strategy tests SHALL 覆盖 `short_trade` 后下一周期 `floating_drawdown` 被冷却阻断。
4. Strategy tests SHALL 覆盖出现新 peak 后 `floating_drawdown` 可重新进入候选。
5. Strategy tests SHALL 覆盖达到累计减仓比例上限后裁剪或跳过 partial close。
6. Decision tests SHALL 覆盖公共层 close 压制程序化 partial close，且不受程序化冷却阻断。
7. API tests SHALL 覆盖 `/api/strategy/signals` 返回主交易级别和 marker 元数据。
8. Frontend build SHALL pass after TypeScript 类型和策略检查 UI 更新。
9. UI tests or component-level checks SHOULD 覆盖动态 timeframe 标题、K 线 marker 渲染和无 marker fallback。
10. Deployment validation SHALL 至少检查 `/health`、`/api/traders`、`/api/status?trader_id=...`、`/api/strategy/signals` 和策略检查页面无 TypeScript/runtime error。
