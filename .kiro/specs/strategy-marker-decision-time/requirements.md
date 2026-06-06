# 策略信号标记确认时间与展示锚点 Requirements

## Background

用户在 161 线上环境观察到：`2026-05-17 22:00:37` 的周期 #9 中，BCHUSDT 出现 `open_short` 并被风控拒绝，但策略检查蜡烛图在 `2026-05-17 21:59:59` 的 1h K 线位置没有显示对应卖点/开空标识。

本次排查基于 161 服务当前日志和 API：

- `decision_logs/aster_deepseek/decision_20260517_220037_cycle9.json`
  - 周期时间：`2026-05-17 22:00:37 +08:00`
  - 摘要包含：`BCHUSDT 识别到1个信号`
  - 摘要包含：`BCHUSDT open_short 被风控过滤: open gate要求更高置信度: 75 < 82`
- `/api/strategy/signals?trader_id=aster_deepseek&symbol=BCHUSDT`
  - `open_short/S2/rejected` marker 的 `close_time` 为 `2026-05-17 18:59:59`
  - 当前没有 `close_time=2026-05-17 21:59:59` 的 `open_short` marker
- `/api/market/klines?trader_id=aster_deepseek&symbol=BCHUSDT&timeframe=1h`
  - 最新闭合 1h K 线为 `2026-05-17 21:59:59`

代码层面的根因：

- `strategy/chanlun/signals.go`
  - `buildSignal()` 将 `ChanlunSignal.TriggerCloseTime` 设置为 `segment.EndTime`。
  - 因为缠论分型/笔/线段需要右侧确认，`segment.EndTime` 可能早于当前最新闭合 K 线。
- `strategy/chanlun/engine.go`
  - `analyzeMainSignal()` 在最新闭合 K 线变化时运行，但生成的信号仍可能指向较早的 `segment.EndTime`。
  - `signalToMainDecision()` 只把 `trigger_close_time=signal.TriggerCloseTime` 写入 `StrategyMetadata`。
  - `decisionToMarker()` 只从 `trigger_close_time` 生成 marker `close_time`。
- `web/src/utils/strategyMarkers.ts`
  - `markerBelongsToKline()` 目前按 marker `close_time` 与 K 线 `close_time` 精确匹配。

因此，图上 `21:59:59` 没有标识的直接原因是：系统没有生成任何锚定在 `21:59:59` 的 BCHUSDT `open_short` marker；已有的 rejected marker 锚定在结构段结束时间 `18:59:59`。

## Goals

- 区分“结构信号发生时间”和“本周期确认/决策时间”。
- 让开仓、加仓、减仓、平仓、被拒绝动作的交易标识可以落在本周期最新闭合 K 线，例如 `21:59:59`。
- 让结构信号时间和确认/决策时间成对显示，用户能同时看到“信号原始位置”和“策略动作确认位置”。
- 保留缠论结构点原始时间，避免把确认滞后的信号误读为结构本身发生在最新 K 线。
- 前端 tooltip 同时展示结构点时间和决策确认时间。
- 让最新决策日志和策略信号 API 能对账到 `signal_id`、信号类型、结构时间和决策时间。

## Non-Goals

- 本规格不修改缠论买卖点识别算法本身。
- 本规格不修改 open gate、风控阈值、下单执行逻辑。
- 本规格不修改主交易级别配置。
- 本规格不要求在蜡烛图上画完整缠论笔、线段或中枢。
- 本规格不把运行时 `decision_logs/`、`data/` 或生产 `config.json` 提交到仓库。

## Glossary

- **结构信号时间（signal/structure close time）**：缠论信号所在走势段的结束时间，当前来源于 `segment.EndTime`。
- **确认/决策时间（decision/evaluation close time）**：程序化策略本周期用于确认信号的最新闭合主交易 K 线时间，例如 `21:59:59`。
- **展示锚点（display anchor time）**：前端将 marker 放到哪根 K 线上的时间。交易动作 marker 应优先使用确认/决策时间；纯检测结构 marker 可使用结构信号时间。
- **逻辑 marker（logical marker）**：后端持久化和 API 返回的一条 `SignalMarker`，代表一个策略信号生命周期。
- **视觉 marker（visual marker）**：前端从一条逻辑 marker 派生出来的图上标识；当结构时间和决策时间不同时，一条逻辑 marker 可以派生出结构点和决策点两个视觉 marker。
- **确认滞后**：缠论分型/笔/线段需要右侧 K 线确认，因此结构信号时间可能早于策略周期确认时间。

## Requirements

### 1. 后端必须区分结构时间和决策时间

**User Story:** 作为量化交易员，我希望系统区分卖点结构发生在哪根 K 线、以及策略在哪根最新闭合 K 线上确认并产生交易动作，避免复盘时误判信号缺失。

#### Acceptance Criteria

1. WHEN 程序化策略识别主信号 THEN 后端 SHALL 保留结构信号时间，来源为当前 `segment.EndTime` 或等价结构时间。
2. WHEN 程序化策略在某个最新闭合主交易 K 线上完成本轮分析 THEN 后端 SHALL 记录本轮确认/决策时间，来源为该 symbol 对应 trade timeframe 的 `lastClosed`。
3. WHEN `segment.EndTime` 早于 `lastClosed` THEN 后端 SHALL 同时返回两个时间，不得只返回一个 `close_time` 导致前端无法区分。
4. WHEN 旧 state 文件缺少新增时间字段 THEN 后端 SHALL 保持兼容，不得 500。
5. 系统 SHALL 保持 `close_time` 的历史兼容语义：`close_time` 表示结构信号时间，并应等同于或回退为 `signal_close_time`。
6. 系统 SHALL 以单条逻辑 `SignalMarker` 表达一个策略信号生命周期；后端不得为了成对显示而持久化两条重复 marker。
7. WHEN 前端需要同时展示 `signal_close_time` 和 `decision_close_time` THEN 前端 SHALL 从同一条逻辑 marker 派生两个视觉 marker。

### 2. SignalMarker API 必须返回可稳定展示的时间字段

**User Story:** 作为前端开发者，我需要 marker API 提供结构时间、决策时间和展示锚点，避免前端猜测 marker 应该落在哪根 K 线。

#### Acceptance Criteria

1. `/api/strategy/signals` 的 `signal_markers` SHALL 继续返回旧字段 `close_time`，保持兼容，且新生成 marker 的 `close_time` SHALL 表示结构信号时间。
2. 新生成的 `signal_markers` SHALL 新增 `signal_close_time` 或等价字段，表示结构信号时间。
3. 新生成的交易动作 `signal_markers` SHALL 新增 `decision_close_time` 或等价字段，表示确认/决策时间。
4. 新生成的 `signal_markers` SHALL 新增 `display_close_time` 或等价字段，表示前端默认展示锚点。
5. WHEN marker 对应 `action`、`final_action`、`trade_intent` 或状态为 `rejected/executed/failed` 的交易动作 THEN `display_close_time` SHALL 优先使用 `decision_close_time`。
6. WHEN marker 只是纯检测信号且没有交易动作 THEN `display_close_time` MAY 使用 `signal_close_time`。
7. WHEN 新字段缺失 THEN 前端 SHALL 回退使用旧 `close_time`，保持历史 state 可展示。
8. WHEN `decision_close_time` 缺失且 marker 是交易动作 THEN 后端/前端 SHALL 不得把 `close_time` 误解释为确认/决策时间，只能按兼容方式展示结构点并在 tooltip 中提示缺少确认时间。

### 3. 主信号决策必须把确认时间写入 marker

**User Story:** 作为量化交易员，我希望像 `2026-05-17 22:00:37` 周期产生的 BCHUSDT `open_short/rejected` 能在 `21:59:59` 最新闭合 K 线上显示交易动作标识。

#### Acceptance Criteria

1. WHEN `analyzeMainSignal()` 以 `lastClosed=2026-05-17 21:59:59` 完成本轮分析并产生 `open_short` 决策 THEN 对应 marker SHALL 包含 `decision_close_time=2026-05-17 21:59:59`。
2. WHEN 同一信号的 `segment.EndTime=2026-05-17 18:59:59` THEN marker SHALL 同时保留 `signal_close_time=2026-05-17 18:59:59`。
3. WHEN `open_short` 被 open gate 拒绝 THEN rejected marker SHALL 保留 `action=open_short`、`trade_intent=open_short`、`signal_type=sell2/sell3`、`signal_id`、拒绝原因、结构时间和决策时间。
4. WHEN 同一个 `signal_id` 在后续周期重复出现且已处理 THEN 系统 SHALL 不重复生成新的交易动作 marker，但 SHOULD 保留已有 marker 的结构时间和决策时间。
5. `analyzeMainSignal()`、`ChanlunSignal` 或等价链路 SHALL 携带本轮 `evaluation_close_time` / `decision_close_time`，其值来源于该 symbol 当前 trade timeframe 的 `lastClosed`。
6. `signalToMainDecision()` SHALL 将本轮 `decision_close_time` 写入 `StrategyMetadata`，并继续写入结构信号时间。
7. `decisionToMarker()` SHALL 从 `StrategyMetadata` 读取结构信号时间和确认/决策时间，并填充 `signal_close_time`、`decision_close_time`、`display_close_time`。
8. WHEN `decision_close_time` 小于 `signal_close_time` 或与当前 trade timeframe 不一致 THEN 后端 SHOULD 记录诊断信息并回退为结构时间展示，避免错误锚定未来或错误周期。

### 4. 决策日志应保留被拒绝动作的信号元数据

**User Story:** 作为量化交易员，我希望在最新决策区看到 `open_rejected` 时，仍能追溯到对应 signal_id、买卖点分类、结构时间和决策时间。

#### Acceptance Criteria

1. WHEN 程序化开仓动作被 open gate 拒绝 THEN `decision_logs` 中的拒绝记录 SHALL 包含 `signal_id`。
2. WHEN 程序化开仓动作被 open gate 拒绝 THEN `decision_logs` 中的拒绝记录 SHALL 包含 `signal_type`。
3. WHEN 程序化开仓动作被 open gate 拒绝 THEN `decision_logs` 中的拒绝记录 SHALL 包含 `signal_timeframe`。
4. WHEN 程序化开仓动作被 open gate 拒绝 THEN `decision_logs` 中的拒绝记录 SHALL 包含 `signal_close_time` 和 `decision_close_time`。
5. WHEN 程序化开仓动作被 open gate 拒绝 THEN `decision_logs` 中的拒绝记录 SHALL 包含 `trade_intent`。
6. WHEN 前端展示最新决策中的 `open_rejected` THEN SHOULD 能显示或 tooltip 展示上述元数据。
7. WHEN 旧决策日志缺少上述字段 THEN 前端 SHALL 保持兼容，不得展示异常或阻塞决策列表。

### 5. 前端蜡烛图必须使用展示锚点匹配 K 线

**User Story:** 作为量化交易员，我希望交易动作标识显示在实际产生决策的最新闭合 K 线上，同时 tooltip 告诉我结构信号原始位置。

#### Acceptance Criteria

1. WHEN marker 包含 `display_close_time` THEN 前端 SHALL 使用 `display_close_time` 与 K 线匹配。
2. WHEN marker 不包含 `display_close_time` 但包含 `decision_close_time` 且 marker 是交易动作 THEN 前端 SHALL 使用 `decision_close_time` 与 K 线匹配。
3. WHEN marker 是纯检测信号 THEN 前端 SHALL 使用 `signal_close_time || close_time` 与 K 线匹配。
4. WHEN marker 的展示锚点在 `21:59:59` K 线范围内 THEN 图上 SHALL 在 `21:59:59` K 线显示标识。
5. WHEN marker 的结构时间早于展示锚点 THEN tooltip SHALL 同时展示“结构点时间”和“确认/决策时间”。
6. WHEN marker 时间与 K 线 close_time 存在秒/毫秒或轻微毫秒差异 THEN 前端 SHALL 先统一归一化到毫秒，再允许 `<=1000ms` 的 close_time 容差匹配，避免肉眼同一分钟但程序不匹配。
7. 前端 SHALL 避免仅按 K 线 `[open_time, close_time]` 范围宽松匹配跨周期 marker，以免误把其它级别或其它收盘点标到当前 K 线。

### 6. 结构时间和决策时间必须成对标识显示

**User Story:** 作为量化交易员，我需要 `signal_close_time` 和 `decision_close_time` 成对标识显示，以便复盘时看清楚信号原始结构点与策略实际确认/动作位置之间的滞后关系。

#### Acceptance Criteria

1. WHEN marker 同时包含 `signal_close_time` 和 `decision_close_time` 且两者不同 THEN 蜡烛图 SHALL 同时展示一组配对标识。
2. WHEN 展示配对标识 THEN `signal_close_time` 位置 SHALL 表示结构信号点，`decision_close_time` 位置 SHALL 表示策略确认/交易动作点。
3. WHEN marker 对应交易动作，如 `open_short/rejected` THEN `decision_close_time` 标识 SHALL 显示交易意图和状态，例如 `S2 · 开空 · 已拒绝` 或等价简洁表达。
4. WHEN marker 对应结构信号点 THEN `signal_close_time` 标识 SHALL 显示信号分类和“结构点/信号点”语义，例如 `S2 · 结构点` 或等价表达，不得误导为该位置发生了真实下单动作。
5. WHEN 同一组配对标识显示在图上 THEN 两个标识 SHALL 通过同一 `signal_id` 关联，tooltip SHALL 展示相同 `signal_id`。
6. WHEN 同一组配对标识距离较近 THEN 前端 SHALL 通过上下位置、文本、线条、虚线、轻量连接线或等价视觉方式区分两者，不得完全重叠。
7. WHEN 同一组配对标识距离较远 THEN 前端 SHOULD 使用轻量连接线或 tooltip 互相引用，帮助用户理解两者属于同一个信号。
8. WHEN `signal_close_time == decision_close_time` THEN 前端 MAY 合并为一个标识，但 tooltip SHALL 同时显示结构时间和确认/决策时间。
9. WHEN marker 缺少 `decision_close_time` THEN 前端 SHALL 退化为只显示结构信号点。
10. WHEN marker 缺少 `signal_close_time` THEN 前端 SHALL 退化为只显示决策/动作点。
11. WHEN marker 是纯检测信号且尚未形成交易动作 THEN 前端 MAY 只显示结构信号点，不强制生成决策点。
12. WHEN marker 是 rejected/executed/failed 交易动作 THEN 前端 SHALL 尽量显示成对标识，除非对应时间字段缺失。
13. WHEN 结构点不在当前展示 K 线范围内但决策点在范围内 THEN 前端 SHALL 显示决策点，并在 tooltip 中提示结构点时间在当前图表范围外。
14. WHEN 决策点不在当前展示 K 线范围内但结构点在范围内 THEN 前端 SHALL 显示结构点，并在 tooltip 中提示决策点时间在当前图表范围外。
15. WHEN 结构点和决策点都不在当前展示 K 线范围内 THEN 前端 SHALL 不在图上显示该 marker，但最新信号/诊断摘要 MAY 继续展示该 marker 信息。
16. WHEN 前端派生成对视觉 marker THEN 两个视觉 marker SHALL 共享同一逻辑 marker 的状态、原因、`trade_intent`、`signal_id` 和 tooltip 对账信息，但结构点视觉 marker SHALL 明确标注为结构点，避免误读为交易动作。

### 7. 买点和卖点的上下位置规则必须继续保持

**User Story:** 作为量化交易员，我希望解决时间锚点问题后，买点仍在 K 线下方、卖点仍在 K 线上方，不再重叠。

#### Acceptance Criteria

1. WHEN marker 为 `buy1/buy2/buy3` THEN 标识 SHALL 位于对应 K 线下方。
2. WHEN marker 为 `sell1/sell2/sell3` THEN 标识 SHALL 位于对应 K 线上方。
3. WHEN 同一 K 线同时有买点和卖点 THEN 买点和卖点 SHALL 分别显示在下方和上方。
4. WHEN 同一 K 线同一侧存在多个卖点 marker THEN 前端 SHALL 按层次依次向上排列，离 K 线越远层级越高，不得重叠在一起。
5. WHEN 同一 K 线同一侧存在多个买点 marker THEN 前端 SHALL 按层次依次向下排列，离 K 线越远层级越高，不得重叠在一起。
6. WHEN 同一 K 线同时存在结构点 marker 和决策/动作 marker 且同为卖点 THEN 两个标识 SHALL 向上分层排列。
7. WHEN 同一 K 线同时存在结构点 marker 和决策/动作 marker 且同为买点 THEN 两个标识 SHALL 向下分层排列。
8. WHEN 相邻 K 线的 marker 横向距离过近导致文本框相互遮挡 THEN 前端 SHOULD 通过横向微偏移、缩短文本、tooltip 展示完整信息、或等价方式降低重叠。
9. WHEN marker 数量过多导致可视空间不足 THEN 前端 MAY 合并同层摘要或显示更紧凑标签，但 SHALL 保留 tooltip 可查看完整 marker 列表。
10. WHEN 同一侧有多个 marker THEN 层次排序 SHOULD 稳定，优先按 `decision_close_time/display_close_time`、`signal_type`、`trade_intent`、`signal_id` 或等价稳定键排序，避免刷新后跳动。

### 8. 测试与验证

**User Story:** 作为系统维护者，我希望这类“周期确认时间与结构信号时间错位”的问题有测试覆盖，避免再次误判。

#### Acceptance Criteria

1. SHALL 增加后端测试，覆盖 `segment.EndTime < lastClosed` 时 marker 同时包含结构时间和决策时间。
2. SHALL 增加后端测试，覆盖 rejected open marker 保留 `signal_id/signal_type/trade_intent/reason` 和两类时间。
3. SHALL 增加前端纯函数测试，覆盖 action marker 优先用 `display_close_time/decision_close_time` 匹配 K 线。
4. SHALL 增加前端纯函数测试，覆盖纯检测 marker 使用 `signal_close_time/close_time` 匹配 K 线。
5. SHALL 增加前端测试或组件逻辑测试，覆盖 `signal_close_time != decision_close_time` 时生成成对显示模型。
6. SHALL 增加前端测试或组件逻辑测试，覆盖 `signal_close_time == decision_close_time` 时可合并显示但 tooltip 保留两类时间。
7. SHALL 增加前端测试或组件逻辑测试，覆盖同一 K 线多个卖点依次向上分层。
8. SHALL 增加前端测试或组件逻辑测试，覆盖同一 K 线多个买点依次向下分层。
9. SHALL 运行 `go test ./strategy/chanlun ./api`。
10. SHALL 运行 `cd web && npm run test`。
11. SHALL 运行 `cd web && npm run build`。
12. SHOULD 在线上或本地页面复核 BCHUSDT 类似场景：结构点出现在原始 K 线，决策/动作点出现在最新闭合 K 线，两个标识可通过 `signal_id` 成对识别。
