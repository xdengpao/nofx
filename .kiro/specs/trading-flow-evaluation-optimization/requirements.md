# 交易全流程评估与优化 — 需求文档

## 背景

作为量化交易员，本次目标不是只优化“开仓条件、仓位管理、平仓策略”三个局部，而是对 NOFX 的完整自动交易闭环进行系统性评估，并形成可执行优化方案。评估范围覆盖：

1. 配置加载与 trader 生命周期。
2. 行情数据、币池、候选标的筛选。
3. 账户、持仓、交易计划、风险预算上下文构建。
4. AI prompt、调用、解析、决策验证。
5. 开仓准入、仓位 sizing、杠杆、止损止盈。
6. 交易执行、订单精度、最小名义额、保护单设置。
7. 持仓管理、移动止损、分批止盈、计划失效、平仓。
8. 自动平仓检测、订单追踪、统计更新。
9. 决策日志、历史归因、滚动绩效门控、前端/API 可观测性。
10. 测试、回放、仿真与上线保护。

当前代码基线显示系统已有较完整的交易闭环：`AutoTrader.runCycle()` 负责周期编排，`decision.GetFullDecision()` 负责持仓评估、AI 新机会搜索、风险验证与决策合并，执行层通过 `trader.Trader` 接口支持 Binance Futures、Hyperliquid、Aster。已有规格 `strategy-quant-optimization` 也引入了历史归因、rolling performance gate 和 `partial_close` 小额订单保护。

本次需求在此基础上补齐全流程风险：硬风控参数需要端到端生效，AI 调用频率和状态需要跨周期持久，交易计划和熔断状态需要明确 trader 作用域，保护单失败不能让仓位裸露，自动平仓与日志归因需要 exactly-once，所有交易优化必须有可复现数据和测试验证支撑。

## 术语表

- **交易全流程**：从配置启动、数据采集、AI 决策、交易执行、持仓管理、订单追踪，到日志归因和前端观测的完整闭环。
- **硬约束**：由代码验证并强制执行的规则，不依赖 prompt 或 AI 自觉遵守。
- **软约束**：写入 prompt 或作为建议展示给 AI/用户的规则，不单独构成交易安全保证。
- **交易计划**：开仓后系统保存的计划，包含方向、入场、止损、止盈、失效条件、分批止盈和移动止损状态。
- **保护单**：止损单、止盈单，以及部分平仓后重新保护剩余仓位所需的订单。
- **滚动绩效门控**：根据最近闭合交易的 Profit Factor、PnL、胜率、方向/币种表现动态调整或禁止新开仓。
- **Exactly-once 归因**：同一次真实平仓只在统计、日志、交易计划清理中计入一次。

## 需求

### Requirement 1: 建立交易全流程评估基线

**用户故事:** 作为量化交易员，我想要系统化拆解交易闭环中每个环节的输入、输出、风险点和当前实现状态，以便判断亏损来自策略、执行、数据、风控还是可观测性缺陷。

#### 验收标准

1.1 SHALL 产出一份覆盖配置、数据、AI、风控、开仓、执行、持仓、平仓、订单追踪、日志/API/前端的全流程评估报告。

1.2 WHEN 评估每个流程节点，THEN 报告 SHALL 标明当前代码入口、关键状态、上游输入、下游影响、已有保护、缺口和优化优先级。

1.3 WHEN 本地缺少 `decision_logs/` 历史日志，THEN 评估 SHALL 明确区分“代码审计结论”和“需要线上日志验证的结论”，不得把缺失数据伪装成回测结果。

1.4 WHEN 历史日志可用，THEN 系统 SHALL 能复现总体、symbol、side、时间段、退出原因、执行失败类型的绩效归因。

1.5 SHALL 给每条优化建议标注预期收益、误伤风险、实现复杂度、验证方式和回滚策略。

### Requirement 2: 配置与运行时状态必须端到端生效

**用户故事:** 作为系统管理员，我想要配置中的风险、频率、杠杆和交易所参数真实影响运行时行为，以便避免“配置看起来生效、实际走默认值”的风险。

#### 验收标准

2.1 WHEN `max_daily_loss`、`max_drawdown`、风险预算、分析间隔或杠杆配置被设置，THEN 对应值 SHALL 从 `config` 传递到 `manager`、`AutoTrader`、`decision.Context` 和实际风控判断。

2.2 WHEN 配置使用百分数形式 `20.0` 或小数形式 `0.2`，THEN 系统 SHALL 按同一语义归一化，并在日志/API 中展示归一化后的百分比。

2.3 WHEN 多个 trader 同时运行，THEN trader 级状态 SHALL 使用 `trader_id` 隔离，包括交易计划、rolling performance、AI 分析时间、订单追踪和账户风控；全局市场熔断 SHALL 明确标注为全局作用域。

2.4 WHEN `Config.Validate()` 设置默认 `Exchange` 或 `ScanIntervalMinutes`，THEN 默认值 SHALL 写回原始 trader 配置，而不是只写入 range 副本。

2.5 WHEN 风控配置没有显式设置，THEN 系统 SHALL 使用可审计默认值，并在启动日志和 `/api/status` 暴露当前有效值。

### Requirement 3: 行情数据与候选标的必须可评估、可降级、可解释

**用户故事:** 作为量化交易员，我想要知道候选标的是如何产生和排序的，以及行情数据是否可靠，以便避免 AI 基于残缺或错配数据开仓。

#### 验收标准

3.1 WHEN 构建候选币池，THEN 系统 SHALL 记录每个 symbol 的来源、排序因子、过滤原因和最终是否进入 AI prompt。

3.2 WHEN AI500、OI Top 或市场数据 API 失败，THEN 系统 SHALL 使用明确降级路径：缓存、默认币池或跳过新开仓，并记录失败原因。

3.3 WHEN 当前交易所不是 Binance，THEN 系统 SHALL 标注行情数据来源与交易执行交易所的潜在价差风险，并在设计阶段决定是否需要交易所本地行情或价差保护。

3.4 WHEN 市场数据不足以计算关键指标，THEN 系统 SHALL 禁止依赖该指标的开仓硬约束，并将该 symbol 标记为数据不足。

3.5 SHALL 为 BTC 市场状态、候选 symbol 趋势、ADX/DI、ATR、资金费率、OI、相关性生成机器可读评分，避免仅靠 prompt 文案判断。

### Requirement 4: AI 调用、解析与决策状态必须可靠

**用户故事:** 作为交易员，我想要 AI 只在必要、可观测、可恢复的情况下参与新机会搜索，以便减少成本、噪声和不可解释交易。

#### 验收标准

4.1 WHEN 距离上次 AI 新机会分析不足配置间隔，THEN 系统 SHALL 跳过 AI 调用；该 `LastAnalysisTime` SHALL 存储在 `AutoTrader` 或 trader-scoped 状态中，跨周期生效。

4.2 WHEN AI API 调用失败、余额不足或响应解析失败，THEN 系统 SHALL 保留本周期 prompt、响应摘要、错误原因，并将周期标记为失败或“新开仓不可用”，不得与主动 `wait` 混淆。

4.3 WHEN AI 输出非开仓管理动作，THEN 系统 SHALL 明确允许列表；未知 action SHALL 被拒绝并记录。

4.4 WHEN AI 输出开仓参数缺失，THEN 系统 MAY 由确定性算法补齐，但补齐后的参数仍 SHALL 经过所有硬约束验证。

4.5 WHEN prompt 中描述风险规则，THEN 对应核心规则 SHALL 有代码级硬约束；禁止只靠 prompt 保证最大持仓数、风险预算、禁交易名单、止损止盈方向和最小 RR。

### Requirement 5: 开仓准入必须从 prompt 建议升级为确定性门控

**用户故事:** 作为量化交易员，我想要每笔新开仓满足可量化的市场、绩效、风险和执行条件，以便减少低质量交易。

#### 验收标准

5.1 WHEN 新开仓决策进入验证层，THEN 系统 SHALL 校验市场数据可用性、已有同 symbol 持仓、最大持仓数、杠杆上限、仓位大小、止损止盈方向、净 RR、单笔风险、总风险预算和预开仓失效条件。

5.2 WHEN BTC 市场状态为震荡、高波动或崩盘，THEN 山寨币趋势跟随开仓 SHALL 进入降频、降仓或禁用模式，除非 symbol 具备独立强趋势与低相关确认。

5.3 WHEN symbol 或 side 的 rolling performance 处于 penalize/block 状态，THEN 新开仓 SHALL 提高置信度门槛、降低单笔风险或禁止开仓。

5.4 WHEN 候选 symbol 与现有持仓高度相关且方向风险同向，THEN 系统 SHALL 限制新增相关风险，而不仅是简单缩小该笔仓位。

5.5 WHEN 新开仓为 short，THEN 系统 SHALL 额外校验趋势强度、资金费率、反弹风险、BTC/ETH 方向和失效条件，因为历史规格显示 short 侧亏损贡献更大。

5.6 WHEN 开仓前保护单参数无法通过交易所精度、最小名义额或价格边界校验，THEN 系统 SHALL 拒绝开仓。

### Requirement 6: 仓位 sizing 与杠杆必须以账户风险为中心

**用户故事:** 作为量化交易员，我想要仓位大小反映真实可亏金额、杠杆、波动率、流动性、相关性和账户状态，而不是只看名义仓位。

#### 验收标准

6.1 WHEN 计算仓位，THEN 系统 SHALL 区分名义仓位、保证金占用、止损亏损、手续费滑点、杠杆后 PnL 展示和强平距离。

6.2 WHEN 使用 ATR 生成止损距离，THEN 仓位 SHALL 由 `账户净值 × 有效单笔风险 / 止损百分比` 推导，并受到可用保证金、最大杠杆、最小/最大订单额限制。

6.3 WHEN rolling performance 变差、账户回撤扩大、AI API 不稳定或执行失败率升高，THEN `EffectiveMaxRiskPerTrade` SHALL 自动收缩。

6.4 WHEN 当前持仓风险估算来源不准确，THEN 总风险预算 SHALL 加安全边际，并在 prompt/API 中展示风险来源。

6.5 WHEN 仓位过小导致后续分批止盈无法满足交易所最小名义额，THEN 系统 SHALL 禁止分批方案或改为全仓退出计划。

### Requirement 7: 交易执行必须保证保护优先和失败可恢复

**用户故事:** 作为交易员，我想要任何真实下单都具备交易所级安全检查和失败补救，以便避免裸仓、重复下单和无效订单。

#### 验收标准

7.1 WHEN 执行开仓，THEN 系统 SHALL 在下单前完成精度格式化、最小名义额、最大名义额、杠杆、保证金、重复持仓和冲突挂单检查。

7.2 WHEN 开仓成交但设置止损失败，THEN 系统 SHALL 立即重试、降级设置保护、或按配置紧急平仓，并将该事件标记为高危执行失败。

7.3 WHEN 设置止损或止盈，THEN 系统 SHALL 分别使用 `CancelStopLossOrders()` 和 `CancelTakeProfitOrders()`，并保护另一类订单不被误删；若交易所 API 会联动删除，系统 SHALL 自动恢复被影响订单。

7.4 WHEN 部分平仓后剩余仓位存在，THEN 系统 SHALL 重新设置剩余数量对应的保护止损，且不得让剩余仓位裸露。

7.5 WHEN 执行失败，THEN 决策日志 SHALL 记录 action、symbol、数量、价格、错误、是否可能产生真实仓位副作用和后续补救动作。

7.6 WHEN 同一周期同时存在平仓和开仓，THEN 系统 SHALL 保持“先平后开”，并在每个执行动作后刷新必要账户/持仓状态，避免后续动作使用过期上下文。

### Requirement 8: 持仓管理与平仓策略必须覆盖止损、止盈、失效和时间风险

**用户故事:** 作为量化交易员，我想要持仓退出不只依赖固定 TP/SL，而是能根据趋势、波动、利润回撤和交易计划失效动态管理。

#### 验收标准

8.1 WHEN 持仓触及硬止损或硬止盈，THEN 系统 SHALL 优先生成全平决策。

8.2 WHEN 持仓处于最小持仓保护期，THEN 系统 SHALL 只允许硬止损、极端亏损、强平风险或交易所保护失败触发提前退出。

8.3 WHEN 触发移动止损，THEN long 新止损 SHALL 单调上移、short 新止损 SHALL 单调下移，并保持 ATR 或最小价格距离安全边界。

8.4 WHEN 触发分批止盈，THEN 系统 SHALL 检查本次平仓名义额和剩余仓位名义额；过小订单 SHALL 跳过、合并或改为全平。

8.5 WHEN 计划失效条件触发，THEN 系统 SHALL 记录是结构化条件、价格失效、趋势反转、时间衰减还是手动/未知原因。

8.6 WHEN 动态止盈被计算出来，THEN 系统 SHALL 区分“更新本地计划”和“更新交易所保护单”，避免本地 TP 与交易所 TP 长期不一致。

8.7 WHEN 持仓长期横盘且 RR 未达标，THEN 系统 SHOULD 支持时间止损或降仓逻辑，避免资金效率被低质量持仓占用。

### Requirement 9: 自动平仓、订单追踪与交易计划清理必须 exactly-once

**用户故事:** 作为系统维护者，我想要止损/止盈自动成交后系统准确更新统计、计划和日志，以便避免重复计数或漏计真实交易。

#### 验收标准

9.1 WHEN 交易所止损/止盈自动成交导致持仓消失，THEN 系统 SHALL 只生成一次 `auto_close_long` 或 `auto_close_short` 归因记录。

9.2 WHEN `OrderTracker` 无法拿到保护单 order id，THEN 系统 SHALL 使用成交历史和持仓快照降级识别自动平仓，并记录置信度。

9.3 WHEN 自动平仓被确认，THEN 系统 SHALL 更新交易统计、关闭或删除对应交易计划、停止订单追踪、撤销孤儿挂单，并写入决策日志。

9.4 WHEN 主循环的 `syncAutoClosedOrders()` 和 `detectAutoClosedPositions()` 都可能发现同一事件，THEN 系统 SHALL 通过事件 ID、symbol/side/时间窗口或订单 ID 去重。

9.5 WHEN 多 trader 或同 symbol 双向持仓存在，THEN 自动平仓归因 SHALL 使用 trader、symbol、side、entry time 或 position id 区分，不得只按 symbol 覆盖。

### Requirement 10: 历史归因、绩效门控与前端观测必须形成闭环

**用户故事:** 作为量化交易员，我想要系统把历史表现实时反馈到策略门控和可视化中，以便持续发现亏损来源。

#### 验收标准

10.1 WHEN 读取决策日志，THEN 系统 SHALL 能配对 `open_*`、`close_*`、`auto_close_*`，并输出 unmatched 行为。

10.2 WHEN 计算表现，THEN 系统 SHALL 输出总体、symbol、side、退出原因、持仓时长、执行失败类型、AI 失败、保护单失败和回撤指标。

10.3 WHEN 构建 rolling performance gate，THEN 计算窗口 SHALL 以闭合交易数为单位，而不是只按最近 N 个周期近似；窗口不足时 SHALL 标记置信度不足。

10.4 WHEN 前端展示策略健康状态，THEN SHALL 展示 rolling gate 当前状态、有效单笔风险、禁交易名单、执行质量和最近高危错误。

10.5 WHEN 某类执行失败率超过阈值，例如保护单失败、partial_close 失败、AI 调用失败，THEN 系统 SHALL 自动降低开仓频率或禁用新开仓。

### Requirement 11: 风控必须分层并支持 trader 级与全局级作用域

**用户故事:** 作为风险负责人，我想要区分账户风险、策略风险、执行风险和市场风险，以便不同风险触发不同保护动作。

#### 验收标准

11.1 SHALL 将风控分为账户级、策略级、执行级、市场级四类，并明确每类触发后的动作：继续持仓管理、禁止新开仓、强制降仓、熔断冷却或全停。

11.2 WHEN 账户最大回撤达到阈值，THEN 系统 SHALL 禁止新开仓；若仍有持仓，持仓管理 SHALL 继续执行。

11.3 WHEN BTC 闪崩或市场状态为崩盘，THEN 系统 SHALL 触发全局市场保护，并明确是否影响所有 trader。

11.4 WHEN 单个 trader 连续亏损或执行失败，THEN 系统 SHALL 优先暂停该 trader，而不是无条件影响其他 trader。

11.5 WHEN 保证金使用率、强平距离或可用余额低于安全阈值，THEN 系统 SHALL 禁止新增仓位并优先减少风险。

### Requirement 12: 回放、测试与上线验证必须先于实盘启用

**用户故事:** 作为量化交易员，我想要每个优化都能通过离线回放、单元测试和灰度开关验证，以便降低上线后实盘误伤。

#### 验收标准

12.1 SHALL 为全流程评估新增任务清单，包含代码审计、历史日志归因、优化设计、测试和灰度上线。

12.2 WHEN 实现交易行为变更，THEN SHALL 添加目标单元测试或 fixture 测试，覆盖正常路径、失败路径和边界条件。

12.3 WHEN 有历史 `decision_logs/`，THEN SHALL 提供离线 replay 或 dry-run 脚本，对新旧规则的开仓次数、过滤原因、风险变化和理论 PnL 做对比。

12.4 WHEN 优化涉及实盘执行，THEN SHALL 支持 feature flag、只读评估模式或 paper/dry-run 模式。

12.5 BEFORE 启用新策略逻辑，SHALL 运行相关 Go 测试；若触及前端/API 合约，SHALL 运行前端构建或类型检查。

