# 程序化策略双层节奏 Requirements

## Background

当前 NOFX 程序化缠论策略已经支持 `decision_mode=programmatic`，并按 `programmatic_strategy.timeframes.trade` 的闭合 K 线确认主级别信号。线上配置使用 `scan_interval_minutes=3`、`trade=1h` 时，策略每 3 分钟执行一次，但同一根 1h 闭合 K 线只分析一次；在下一根 1h K 线闭合前，候选标的会输出 `1h 无新闭合K线`。

这个机制对新开仓/加仓是必要的防抖保护，但不应阻断已有持仓的 3m 周期管理。已有持仓仍需要持续评估保本止损、浮盈回撤、结构破坏、短差减仓，并继续保留公共层的交易计划同步、保护单同步和其他风险降低型动作。

本规格是 `.kiro/specs/programmatic-chanlun-strategy` 的增量需求，目标是把程序化策略拆成两层节奏：

- **主级别信号层**：仅在 `trade` 级别新闭合 K 线后确认新开仓和加仓。
- **持仓管理层**：每个 `scan_interval` 周期运行，服务已有持仓，不受 `trade` 级别无新闭合 K 线阻断。

## Goals

- 避免未闭合主级别 K 线造成缠论结构、背驰和买卖点信号闪烁。
- 避免 `无新闭合K线` 误伤已有持仓管理。
- 让 `trade` 主交易级别可配置并支持 `15m`、`1h`、`4h`。
- 保留现有公共风控、交易计划、保护单同步、执行前检查和日志审计链路。

## Non-Goals

- 本规格不引入未闭合 `15m/1h/4h` K 线确认主买卖点。
- 本规格不要求 replay 或离线回测能力改造，除非后续 design/tasks 明确补充。
- 本规格不改变交易所接口，不新增真实下单绕过路径。
- 本规格不要求引入 AI 参与程序化策略解释或二次判断。
- 本规格不评估 `15m`、`1h`、`4h` 哪个级别信号更优；不同主交易级别的信号质量、收益、假突破和成本影响 SHALL 在后续回测规格中验证。

## Glossary

- **主交易级别 / trade level**：程序化策略用于确认主买卖点、新开仓和加仓的 K 线级别，允许值为 `15m`、`1h`、`4h`。
- **新闭合 K 线**：相对程序化状态中上次已分析的 `symbol + trade_level` 闭合时间，出现新的已闭合 K 线。
- **主级别信号层**：基于 `trade_level` 闭合 K 线识别三类买卖点，并生成新开仓或加仓候选的策略层。
- **持仓管理层**：面向已有持仓的程序化风险降低和保护动作层，包括保本止损、浮盈回撤、结构破坏、短差减仓；首期只输出 `update_stop_loss`、`partial_close`、`close_long`、`close_short`，止盈更新、交易计划同步和保护单同步继续由公共层负责。
- **风险降低型动作**：不会增加同 symbol 净风险敞口的动作，例如 `close_long`、`close_short`、`partial_close`、`update_stop_loss`、必要的保护单修复。
- **风险增加型动作**：会增加净风险敞口的动作，例如 `open_long`、`open_short`、`add_long`、`add_short`。
- **风险增加阻断 / risk-increase blocked**：全局熔断、账户硬停或类似保护触发后，系统禁止 `open_long`、`open_short`、`add_long`、`add_short`，但在行情和持仓上下文可用时仍允许风险降低型动作。
- **完全停止 / full stop**：行情、交易所连接、上下文或账户数据不可用，导致系统无法安全评估持仓风险时，整个策略周期停止输出真实交易动作。

## Requirements

### 1. 双层策略节奏

**User Story:** 作为量化交易员，我希望程序化策略把新机会确认和已有持仓管理拆成两层节奏，以便主级别信号稳健，同时持仓风险可以被 3m 周期持续管理。

#### Acceptance Criteria

1. WHEN 程序化策略周期开始 THEN 系统 SHALL 分别执行主级别信号层和持仓管理层，且两层的跳过条件 SHALL 独立判断。
2. WHEN `trade_level` 没有新闭合 K 线 THEN 主级别信号层 SHALL 跳过新开仓和加仓确认。
3. WHEN `trade_level` 没有新闭合 K 线 THEN 持仓管理层 SHALL 继续评估所有已有持仓。
4. WHEN 某 symbol 没有持仓且 `trade_level` 没有新闭合 K 线 THEN 系统 SHALL 不为该 symbol 生成 `open_long`、`open_short`、`add_long` 或 `add_short`。
5. WHEN 某 symbol 有持仓且 `trade_level` 没有新闭合 K 线 THEN 系统 MAY 为该 symbol 生成风险降低型动作。
6. WHEN 同一周期同时存在公共层持仓决策、持仓管理层决策和主级别信号层决策 THEN 合并优先级 SHALL 保持：公共强制平仓/交易计划硬止损/交易计划失效 > 公共止损止盈同步 > 程序化持仓风险降低动作 > 程序化加仓 > 程序化开仓 > wait/hold。
7. WHEN 系统处于 risk-increase blocked 状态 THEN 主级别信号层 SHALL 跳过新开仓和加仓，但持仓管理层 SHALL 在行情和持仓上下文可用时继续运行。

### 2. 持仓管理层每周期运行

**User Story:** 作为量化交易员，我希望已有持仓每个 3m 扫描周期都被评估，以便价格快速变化时系统仍能保护利润和降低风险。

#### Acceptance Criteria

1. WHEN trader 有任一已有持仓 THEN 持仓管理层 SHALL 每个 `scan_interval` 周期执行一次。
2. WHEN 持仓管理层运行 THEN 系统 SHALL 至少支持评估保本止损、浮盈回撤、结构破坏和短差减仓，并可读取公共交易计划、当前止损、当前止盈和 peak 状态作为判断依据。
3. WHEN 当前持仓 symbol 不在本周期候选池中 THEN 系统 SHALL 仍将该 symbol 纳入持仓管理层评估。
4. WHEN 持仓管理层输出动作 THEN 动作 SHALL 进入现有 `decision.Decision` 执行链路，并继续经过执行前检查、最小名义额、交易所精度和保护单处理。
5. WHEN 持仓管理层没有产生可执行动作 THEN 系统 SHALL 记录清晰诊断，而不是用 `无新闭合K线` 概括已有持仓状态。
6. WHEN 需要止盈更新、交易计划同步或保护单同步 THEN 系统 SHALL 继续使用现有公共层逻辑；程序化持仓管理层首期不得直接输出 `update_take_profit`。

### 3. 保本止损

**User Story:** 作为量化交易员，我希望持仓达到一定浮盈后，程序化策略能在 3m 周期中尝试把止损移动到保本或保本以上，以降低回吐风险。

#### Acceptance Criteria

1. WHEN 持仓浮盈达到配置阈值 THEN 持仓管理层 SHALL 可生成 `update_stop_loss` 决策，将止损移动到保本价、保本加缓冲价或更保守的结构止损价。
2. WHEN 浮盈未达到保本阈值 THEN 系统 SHALL 不生成保本止损更新，并 SHALL 记录未触发原因。
3. WHEN 新止损价格会扩大风险或低于现有保护效果 THEN 系统 SHALL 拒绝该止损更新。
4. WHEN 交易所已有止损单需要调整 THEN 系统 SHALL 继续使用 `CancelStopLossOrders()` 路径，不得混用止盈取消逻辑。
5. WHEN 保本止损更新被公共保护规则拒绝 THEN 系统 SHALL 保留拒绝原因到决策日志。

### 4. 浮盈回撤保护

**User Story:** 作为量化交易员，我希望持仓出现明显浮盈后，如果价格从峰值回撤过大，系统能减仓或平仓保护利润。

#### Acceptance Criteria

1. WHEN 持仓浮盈达到配置的激活阈值 THEN 系统 SHALL 开始跟踪该持仓的峰值价格或峰值 R 倍数。
2. WHEN 从峰值价格或峰值 R 倍数回撤达到配置阈值 THEN 持仓管理层 SHALL 可生成 `partial_close` 或 `close_long/close_short`。
3. WHEN 回撤保护触发 THEN 决策 SHALL 记录触发依据，包括峰值、当前价、回撤幅度、初始风险距离和使用的 timeframe。
4. WHEN 回撤保护未触发 THEN 系统 SHALL 保持原持仓，不得因普通波动频繁减仓。
5. WHEN 回撤保护和交易计划止盈/止损冲突 THEN 公共交易计划和强制风控 SHALL 拥有更高优先级。

### 5. 结构破坏管理

**User Story:** 作为量化交易员，我希望已有持仓在次级别或微观级别出现结构破坏时，可以被及时减仓或平仓，而不必等到下一根主交易级别 K 线闭合。

#### Acceptance Criteria

1. WHEN 持仓方向的 `15m` 或 `3m` 已闭合 K 线跌破/突破关键结构位 THEN 持仓管理层 SHALL 可判定结构破坏。
2. WHEN 使用结构破坏触发减仓或平仓 THEN 系统 SHALL 使用已闭合 `3m` 或 `15m` K 线确认，除非动作属于强制止损或交易所保护单触发。
3. WHEN 实时价格触及已有硬止损或交易计划硬退出条件 THEN 系统 MAY 使用实时价格触发风险降低动作。
4. WHEN 结构破坏仅来自未闭合 `15m/1h/4h` K 线 THEN 系统 SHALL 不确认主级别反向买卖点。
5. WHEN 结构破坏触发 THEN 决策 SHALL 记录结构位、触发 K 线级别、触发价格和确认方式。

### 6. 短差减仓

**User Story:** 作为量化交易员，我希望已有持仓在次级别出现反向短差信号时，可以按比例减仓，并保留后续回补条件。

#### Acceptance Criteria

1. WHEN 持仓已有同向主级别仓位且 `3m` 或 `15m` 出现反向短差信号 THEN 持仓管理层 MAY 生成 `partial_close`。
2. WHEN 短差减仓触发 THEN 减仓比例 SHALL 使用配置值，默认沿用程序化策略 `position.partial_close_pct`。
3. WHEN 短差减仓成功记录后 THEN 系统 SHALL 在程序化策略状态中记录 reduced signal、减仓比例、是否允许回补和更新时间。
4. WHEN 同一短差信号已处理过 THEN 系统 SHALL 不重复减仓。
5. WHEN 反向短差信号不满足配置阈值 THEN 系统 SHALL 仅记录诊断，不输出减仓动作。

### 7. 主交易级别可配置

**User Story:** 作为量化交易员，我希望主交易级别支持 `15m`、`1h` 和 `4h`，以便在不同市场和账户风险偏好下调整策略频率。

#### Acceptance Criteria

1. WHEN `programmatic_strategy.timeframes.trade` 配置为 `15m` THEN 系统 SHALL 使用已闭合 `15m` K 线确认主级别新开仓和加仓信号。
2. WHEN `programmatic_strategy.timeframes.trade` 配置为 `1h` THEN 系统 SHALL 使用已闭合 `1h` K 线确认主级别新开仓和加仓信号。
3. WHEN `programmatic_strategy.timeframes.trade` 配置为 `4h` THEN 系统 SHALL 使用已闭合 `4h` K 线确认主级别新开仓和加仓信号。
4. WHEN `trade` 为 `15m` THEN 默认组件级别 SHALL 为 `3m`。
5. WHEN `trade` 为 `1h` THEN 默认组件级别 SHALL 为 `15m`。
6. WHEN `trade` 为 `4h` THEN 默认组件级别 SHALL 为 `1h`。
7. WHEN `trade` 配置为非 `15m`、`1h`、`4h` 的值 THEN 配置校验 SHALL 失败，并返回中文错误。
8. WHEN 主交易级别切换 THEN `last_analyzed_closed_kline` SHALL 按 `symbol + timeframe` 隔离，不得复用其他 timeframe 的已分析状态。

### 8. K 线闭合规则

**User Story:** 作为量化交易员，我希望主级别信号只使用已闭合 K 线，但持仓风险管理可以使用更低级别闭合 K 线或实时价格，以兼顾稳定性和响应速度。

#### Acceptance Criteria

1. WHEN 确认主买卖点、新开仓或加仓 THEN 系统 SHALL 只使用已闭合 `trade_level` K 线。
2. WHEN 当前 `trade_level` K 线未闭合 THEN 系统 SHALL 不使用该 K 线确认新开仓或加仓。
3. WHEN 评估持仓保本止损、浮盈回撤或结构破坏 THEN 系统 MAY 使用已闭合 `3m`、`15m` 或 `trade_level` K 线。
4. WHEN 评估硬止损、交易所保护单触发或实时风险退出 THEN 系统 MAY 使用实时价格。
5. WHEN 某 timeframe 数据不足或缺失 THEN 系统 SHALL 跳过对应判断并记录缺失 timeframe，不得生成基于缺失数据的交易动作。

### 9. 配置与默认值

**User Story:** 作为系统维护者，我希望新增节奏配置可保守默认，以便旧配置不破坏，新配置可以逐步打开。

#### Acceptance Criteria

1. WHEN 旧配置没有新增节奏配置 THEN 系统 SHALL 保持当前程序化策略兼容行为，但不得阻断公共交易计划同步和保护单同步。
2. WHEN 配置启用双层节奏 THEN 系统 SHALL 默认开启“无新 `trade_level` 闭合时跳过新开仓/加仓，但继续持仓管理”。
3. WHEN 配置包含保本、回撤、结构破坏或短差减仓阈值 THEN 系统 SHALL 校验数值范围。
4. WHEN 配置阈值缺失 THEN 系统 SHALL 使用保守默认值，或在无法安全推断时禁用对应子功能并记录诊断。
5. WHEN 配置改变 `trade_level` 或关键持仓管理阈值 THEN 决策日志 SHALL 记录策略版本、配置摘要和 config hash。
6. WHEN 配置 `trigger_profit_pct`、`activation_profit_pct`、`buffer_pct` 或 `drawdown_pct` THEN 系统 SHALL 明确按人类百分数解析：`1.0` 表示 1%，`0.05` 表示 0.05%，`35` 表示 35%。
7. WHEN 运行时计算价格缓冲或回撤比例 THEN 系统 SHALL 使用归一化后的 ratio，不得把 `buffer_pct: 0.05` 误解释为 5%。

### 10. 状态持久化

**User Story:** 作为量化交易员，我希望策略重启后仍能识别已分析 K 线、峰值价格、短差减仓和已处理信号，避免重复动作或状态丢失。

#### Acceptance Criteria

1. WHEN 主级别信号层分析完某 symbol 的新闭合 K 线 THEN 系统 SHALL 持久化 `last_analyzed_closed_kline[symbol][trade_level]`。
2. WHEN 持仓管理层运行 THEN 系统 SHALL 不依赖 `last_analyzed_closed_kline` 判断是否跳过已有持仓评估。
3. WHEN 浮盈回撤保护启用 THEN 系统 SHALL 按 trader、symbol 和 side 持久化峰值价格或峰值 R 倍数。
4. WHEN 保本止损、浮盈回撤、结构破坏或短差减仓触发 THEN 系统 SHALL 持久化已处理 signal id 和对应规则状态。
5. WHEN 状态文件不存在或读取失败 THEN 系统 SHALL 使用空状态启动并记录中文告警，不得影响公共强制风控和交易计划同步。
6. WHEN 持仓管理层生成 signal id THEN signal id SHALL 基于 trader、symbol、side、rule、timeframe、触发 K 线 close time 或结构 hash、config hash 确定性生成，不得只使用当前时间戳。

### 11. 日志与可观测性

**User Story:** 作为量化交易员，我希望日志准确说明策略为什么跳过新开仓，以及已有持仓是否仍被扫描，避免误判系统停止工作。

#### Acceptance Criteria

1. WHEN `trade_level` 无新闭合 K 线 THEN 日志 SHALL 明确说明“仅跳过新开仓/加仓确认”，不得让用户误解为整个策略周期停止。
2. WHEN 持仓管理层仍在运行 THEN 决策日志 SHALL 记录已评估持仓数量、触发动作数量和跳过原因。
3. WHEN 所有无持仓候选 symbol 都因为无新闭合 K 线被跳过 THEN 日志 SHALL 聚合展示，不得刷屏重复输出每个 symbol 的冗长原因。
4. WHEN 预计下一根 `trade_level` K 线闭合时间可计算 THEN 日志 SHOULD 展示下一次主级别确认时间。
5. WHEN 前端展示程序化策略诊断 THEN SHALL 能区分主级别信号层诊断和持仓管理层诊断。

### 12. 安全与执行边界

**User Story:** 作为系统维护者，我希望双层节奏优化不绕过现有风控和执行保护，避免策略频率调整引入新的实盘风险。

#### Acceptance Criteria

1. WHEN 持仓管理层输出 `partial_close`、`close_long`、`close_short` 或 `update_stop_loss` THEN 系统 SHALL 继续复用现有执行前检查和交易所执行路径。
2. WHEN 主级别信号层输出 `open_long`、`open_short`、`add_long` 或 `add_short` THEN 系统 SHALL 继续执行 open gate、风险预算、仓位 sizing、杠杆限制、相关性限制和最小名义额检查。
3. WHEN 全局熔断或账户回撤硬停触发 THEN 系统 SHALL 进入 risk-increase blocked 状态，禁止新开仓和加仓，但在行情和持仓上下文可用时仍允许风险降低型持仓管理动作。
4. WHEN 同一 symbol 已有多仓 THEN 系统 SHALL 不输出新增空头风险敞口，除非先执行风险降低型平多或减仓动作。
5. WHEN 同一 symbol 已有空仓 THEN 系统 SHALL 不输出新增多头风险敞口，除非先执行风险降低型平空或减仓动作。
6. WHEN 公共层和程序化持仓管理层对同一 symbol 同时输出风险降低型动作 THEN 系统 SHALL 使用确定性冲突消解规则，避免同一周期重复减仓、重复平仓或相互覆盖止损。
7. WHEN 程序化持仓管理层输出风险降低型动作 THEN 系统 SHALL 先验证 symbol 存在持仓、方向匹配、参数范围合法、止损不会降低保护效果，然后才能进入执行链路。

### 13. 测试要求

**User Story:** 作为维护者，我希望双层节奏有可复现测试，确保未来调整不会让无新闭合 K 线再次阻断持仓管理。

#### Acceptance Criteria

1. WHEN `trade_level` 无新闭合 K 线且无持仓 THEN 测试 SHALL 验证不输出新开仓或加仓。
2. WHEN `trade_level` 无新闭合 K 线但存在持仓 THEN 测试 SHALL 验证持仓管理层仍被调用。
3. WHEN 持仓满足保本止损条件 THEN 测试 SHALL 验证可生成 `update_stop_loss`。
4. WHEN 持仓满足浮盈回撤条件 THEN 测试 SHALL 验证可生成 `partial_close` 或平仓动作。
5. WHEN `trade` 分别配置为 `15m`、`1h`、`4h` THEN 测试 SHALL 验证配置校验、组件级别映射和闭合 K 线状态隔离。
6. WHEN 状态文件已有某 timeframe 的分析记录 THEN 测试 SHALL 验证切换到其他 timeframe 不会被旧 timeframe 状态误跳过。
7. WHEN 持仓管理层输出动作 THEN 测试 SHALL 验证动作仍进入公共决策合并和执行优先级排序。
8. WHEN 系统处于 risk-increase blocked 且已有持仓满足风险降低条件 THEN 测试 SHALL 验证不输出 open/add，但仍可输出合法风险降低动作。
9. WHEN 公共层和程序化层对同一 symbol 同时输出风险降低型动作 THEN 测试 SHALL 验证冲突消解结果唯一且优先级符合要求。
10. WHEN 配置包含 `buffer_pct: 0.05` THEN 测试 SHALL 验证运行时缓冲按 0.05% 而不是 5% 计算。
