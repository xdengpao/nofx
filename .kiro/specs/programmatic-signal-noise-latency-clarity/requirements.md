# 程序化缠论信号去重、时效与可视化清晰度 Requirements

## Background

用户在 161 线上 `aster_deepseek` 的策略检查页观察到三个问题：

- 结构点重复严重；
- 信号滞后严重；
- 信号标记重叠，蜡烛图上难以分辨。

本次排查结合了截图、161 服务日志/API/状态文件，以及本项目代码。161 当前以 Docker 运行：

- 后端容器：`nofx-trading`
- 前端容器：`nofx-frontend`
- 决策日志：`/app/decision_logs/aster_deepseek`
- 程序化状态：`/app/data/programmatic_strategy_state.json`
- 策略配置摘要：`trade=1h`、`component=15m`、`micro=3m`、`history_depth.1h=240`、`config_hash=e0dc5de3c03b`

BTCUSDT 是截图中的代表样本，不是单币种特例。补充全币种现场统计时，161 当前策略 universe 为：

- `1000PEPEUSDT`
- `BCHUSDT`
- `BNBUSDT`
- `BTCUSDT`
- `CLUSDT`
- `ETHUSDT`
- `SOLUSDT`
- `XAGUSDT`
- `XAUUSDT`
- `XRPUSDT`

当前全币种 API 样本显示，107 条 `signal_markers` 中 99 条存在 `decision_close_time - signal_close_time > 0` 的确认/展示滞后；延迟中位数约 15.5h，平均约 14.5h，P90 约 21.6h，最大约 29h。

按 symbol 观察，重复和滞后集中在多个币种：

- `BTCUSDT`：markers 27，最大滞后约 23h。
- `CLUSDT`：markers 15，最大滞后约 29h。
- `ETHUSDT`：markers 17，最大滞后约 18h。
- `SOLUSDT`：markers 13，最大滞后约 14h。
- `XAGUSDT`：markers 11，最大滞后约 20h。
- `XRPUSDT`：markers 16，最大滞后约 17h。
- `BCHUSDT`：markers 7，最大滞后约 7h。

BTCUSDT 现场样本：

- `/api/strategy/signals?trader_id=aster_deepseek&symbol=BTCUSDT`
  - `signals=2`
  - `signal_markers=27`
  - marker 分布包括：`preview_signal confirmed sell2` 9 条、`preview_signal confirmed buy3` 7 条、`main_signal rejected sell2` 6 条、`main_signal rejected buy3` 1 条、`structure invalidated` 4 条。
- `programmatic_strategy_state.json`
  - BTCUSDT `confirmed_signals=11`
  - `executed_signals=4`
  - `suppressed_signals=7`
  - `recent_signal_markers=27`
- 当前仍在图上出现的主要结构锚点为 `2026-05-17 20:59:59`，但页面截图时间附近的预览/决策时间已经到 `2026-05-18 19:44:59` 至 `2026-05-18 19:59:59`。
- Docker 日志显示，在 `2026-05-18 19:31` 到 `19:58` 之间，BTCUSDT 每个约 3 分钟周期都报告：
  - `BTCUSDT 1h 预览层preview_2x15m识别short sell2，默认仅观察`
  - `BTCUSDT 1h 预览层preview_2x15m识别long buy3，默认仅观察`
  - 后续切到 `preview_3x15m` 后继续报告同类信号。
- 1h 主信号在整点附近重新评估，例如：
  - `2026-05-18 18:01`：BTCUSDT 识别到 2 个信号，但 sell2 因 `target_already_crossed` 不进入开仓，buy3 因止损/止盈结构不合法不进入开仓。
  - `2026-05-18 19:01`：重复同类结构拒绝。
  - `2026-05-18 20:01`：重复同类结构拒绝。

代码层面的直接原因：

- `strategy/chanlun/signals.go`
  - `StableSignalID()` 把 `configHash`、segment 起止时间等纳入 signal id。部署或配置哈希变化后，同一结构语义可能变成新的 signal id。
  - `buildSignal()` 将信号时间锚定到 segment end time；当最后一个确认线段长期未变化时，结构时间会显著早于当前决策时间。
- `strategy/chanlun/engine.go`
  - `evaluatePreviewSignals()` 用 15m 闭合组件构造预览信号，并把 `phase/synthetic.CloseTime` 纳入预览 config hash，导致同一 1h 背景结构在不同 15m 阶段产生多条可视 marker。
  - `evaluateMainSignals()` 会在主交易级别新闭合时再次分析旧结构；如果旧结构已经 target crossed 或 entry window invalid，仍可能以新的生命周期 marker 进入 recent markers。
  - `setLatestSignals()`、`LatestSignals()` 会把状态仓库中的历史 markers 与本轮 report markers 合并后返回。
- `strategy/chanlun/state.go`
  - `recent_signal_markers` 最多保留 200 条，去重 key 当前是 `signal_id|timeframe|close_time`。它能去掉完全相同 marker，但不能折叠“同一结构语义、不同 signal id 或不同 preview phase”的重复标记。
- `web/src/components/StrategyCandlestickChart.tsx`
  - 当前只对同一 K 线同一侧 marker 做纵向堆叠，没有跨相邻 K 线的碰撞避让。
  - 标签宽度按文本长度直接绘制，多个相邻 15m/1h 决策点容易互相遮挡。
  - 缺少默认降噪视图：预览、结构、拒绝、失效等状态都可能同时进入可视区域。

已有规格已经分别覆盖“entry timing”“staleness guard”“decision time marker”等局部问题。本规格的目标是把线上复发问题收敛成一个端到端需求：后端语义去重、时效解释、API 降噪、所有币种统一显示模板、前端清晰展示必须一起成立。

## Goals

- 对同一结构语义建立稳定生命周期，减少因 config hash、preview phase、重复周期造成的视觉重复。
- 保留安全语义：不能为了减少滞后而追开已经错过目标的结构。
- 明确区分结构信号、预览信号、入场触发、交易动作、拒绝/失效状态。
- 默认页面应清楚展示“当前可行动/最新关键”的信号，而不是把全部历史 marker 平铺在图上。
- 策略检查页所有币种必须使用同一套显示模板、同一套排序/过滤/聚合规则，不允许对 BTCUSDT 做特殊处理。
- 图表应支持查看完整审计信息，但默认视图不应被重复/过期/预览标记淹没。
- 决策日志、API、前端 tooltip 能解释信号为什么滞后、为什么没有开仓、哪些 marker 被折叠。

## Non-Goals

- 不放宽 open gate、止损/止盈结构校验、missed target guard、账户风控、仓位限制。
- 不把旧结构追开改造成趋势延续交易；趋势延续必须是单独策略模块。
- 不要求本规格直接改交易所执行、真实下单逻辑或资金配置。
- 不提交 161 的真实 `config.json`、运行时 `decision_logs/`、`data/` 或任何账户凭证。

## Glossary

- **结构语义键（structure key）**：表示同一缠论结构的稳定键，应由 trader、symbol、direction、signal type、analysis timeframe、center/segment 起止时间等构成，不应受 config hash、preview phase、当前周期时间影响。
- **生命周期 marker（lifecycle marker）**：后端状态中表示一个结构/触发/动作生命周期的逻辑记录。
- **视觉 marker（visual marker）**：前端在蜡烛图上渲染的图形标记，可由一个生命周期 marker 派生多个视觉点，也可被聚合成簇。
- **结构信号**：1h 或主交易级别确认后的缠论结构背景。
- **预览信号**：由 15m 等 component timeframe 闭合组件推导的未确认信号，默认观察。
- **入场触发**：在结构背景下出现的新鲜可执行事件，可产生 open/add 候选。
- **动作 marker**：已执行、失败、拒绝、跳过的交易动作或准交易动作 marker。
- **降噪视图**：默认隐藏或聚合重复、过期、仅观察预览 marker，只展示当前最有交易解释价值的信息。

## Requirements

### R1 结构语义必须可稳定去重

**User Story:** 作为交易员，我希望任意币种的同一个缠论结构不会因为重启、配置哈希变化、预览阶段变化而在图上变成一串重复结构点。

#### Acceptance Criteria

1. WHEN 后端生成主交易级别缠论结构信号 THEN 系统 SHALL 生成独立于 `config_hash` 的 `structure_key` 或等价稳定语义键。
2. WHEN 同一 trader/symbol/signal type/direction/segment/center 再次被识别 THEN 系统 SHALL 识别为同一结构生命周期，而不是默认追加一个全新生命周期 marker。
3. WHEN `config_hash` 变化但结构语义未变化 THEN 系统 SHALL 保留审计上的 config hash 历史，但默认去重 SHALL 以结构语义为准。
4. WHEN 旧 state 缺少 `structure_key` THEN 系统 SHALL 能从已有字段尽量补算，补算失败时保持兼容而不是丢失或报错。
5. WHEN 同一结构已经被标记为 `target_already_crossed`、`entry_window_invalid`、`signal_expired` 或等价不可开仓状态 THEN 后续周期 SHALL 更新该生命周期的最近诊断，不应每轮追加新的同语义结构点。
6. WHEN 新的 segment end time、center id、direction 或 signal type 发生变化 THEN 系统 SHALL 视为新结构生命周期。

### R2 stale/invalid 结构必须被语义抑制，而不是反复进入图表

**User Story:** 作为交易员，我希望已经被判定错过目标或止盈止损结构无效的旧信号只保留一次清晰解释，不要每个整点重复刷出。

#### Acceptance Criteria

1. WHEN 主信号因 `target_already_crossed` 被拒绝或失效 THEN 系统 SHALL 记录语义级 suppression，key 至少包含 trader、symbol、structure key、action/intent、reason code。
2. WHEN 后续周期遇到同一 structure key 和同一 reason code THEN 系统 SHALL 输出简洁诊断，例如“同一结构已抑制”，但 SHALL NOT 追加新的默认可视 marker。
3. WHEN 后续出现新的 entry trigger id THEN 系统 SHALL 允许其独立评估，不得被旧结构 suppression 误杀。
4. WHEN 后续出现新的结构 key THEN 系统 SHALL 清晰区分新旧结构，并允许新结构正常进入生命周期。
5. WHEN suppression 已存在 THEN `recent_signal_markers` SHALL 保留最新状态和最近一次检查时间，但默认图表只显示一条折叠后的生命周期 marker。
6. WHEN 用户打开完整审计模式 THEN 系统 MAY 展示重复检查历史，但必须标注为“重复检查/已抑制”，不得误导为新信号。

### R3 预览信号必须折叠为阶段摘要

**User Story:** 作为交易员，我希望 15m 预览告诉我当前 1h K 线可能形成什么结构，但不要每 3 分钟生成一排看不清的 confirmed preview 标签。

#### Acceptance Criteria

1. WHEN `preview_2x15m` 或 `preview_3x15m` 在同一 1h trade candle 内重复识别同向同类型结构 THEN 默认 signal report SHALL 折叠为该 preview phase 的最新一条摘要。
2. WHEN 预览信号被 1h 主信号确认 THEN 系统 SHALL 把 preview lifecycle 标记为 `confirmed_by_1h`，并关联到 confirmed structure key。
3. WHEN 多个 preview markers 都 confirmed 到同一 1h structure key THEN 默认 API 和默认前端 SHALL 返回/展示聚合摘要，而不是逐条平铺。
4. WHEN 用户需要审计完整预览演变 THEN API SHALL 支持显式参数或前端开关返回完整 preview history。
5. WHEN preview 默认仅观察 THEN 图表默认 SHALL 不把它绘制成与可执行开仓同等强度的标签。
6. WHEN preview pilot/open 被显式开启并产生动作 marker THEN 该动作 marker SHALL 作为可行动信息显示，但仍需与普通观察 preview 区分。

### R4 信号滞后必须可解释，且不能用追开旧结构解决

**User Story:** 作为交易员，我希望页面能告诉我“这是旧结构背景，当前等待/缺少新鲜入场触发”，而不是看起来像刚刚出现了开仓信号却一直不交易。

#### Acceptance Criteria

1. WHEN `signal_close_time` 早于 `decision_close_time` THEN API SHALL 返回结构年龄，例如 `age_candles` 和 `freshness_state`。
2. WHEN 结构年龄超过 direct open 窗口 THEN 系统 SHALL 把结构归类为 background/suppressed/waiting 状态，不得显示为当前新开仓信号。
3. WHEN 当前价格已经越过结构 target THEN 系统 SHALL 保持 `target_already_crossed` 拒绝/失效，不得自动重算 TP 后继续开仓。
4. WHEN 当前价格不满足 `long: SL < current < TP` 或 `short: SL > current > TP` THEN 系统 SHALL 返回程序化结构无效原因，而不是只依赖泛化校验错误。
5. WHEN 用户查看最新信号面板 THEN 面板 SHALL 优先展示最新可行动 entry trigger 或最新状态摘要；若只有旧结构，必须标注“旧结构背景/等待 fresh trigger/已错过目标”。
6. WHEN preview 信号存在但默认仅观察 THEN 面板 SHALL 明确标注 `preview_2x15m` 或 `preview_3x15m` 仅观察，不得看起来像已确认开仓信号。

### R5 入场触发必须与结构背景分层

**User Story:** 作为交易员，我希望系统用旧结构提供方向背景，用新鲜触发决定是否入场，从而降低真实交易滞后。

#### Acceptance Criteria

1. WHEN 结构信号不是最新闭合主交易 K 线产生 THEN 系统 SHALL 默认要求 fresh entry trigger 才能生成 open/add 候选。
2. WHEN 15m pullback/retest/resume 或其他已配置触发出现 THEN 系统 SHALL 生成独立 `entry_trigger_id`，并关联 `parent_structure_key` / `parent_signal_id`。
3. WHEN entry trigger 生成 open/add 候选 THEN 其 `signal_id` SHALL 使用 entry trigger id，避免被旧结构 suppression 或 executed state 错误去重。
4. WHEN entry trigger 过期、剩余 RR 不足、越过 target 或被 open gate 拒绝 THEN 系统 SHALL 标记 trigger 生命周期，而不是污染原结构生命周期。
5. WHEN 没有 fresh trigger THEN 系统 SHALL 只报告结构背景和等待原因，不应生成 open/add 候选。
6. WHEN 需要趋势延续开仓 THEN 系统 SHALL 使用独立 strategy/rule/signal id/SL/TP 验证链路，不得复用 missed structure entry。

### R6 Strategy Signal API 必须支持默认降噪与完整审计

**User Story:** 作为前端和排障者，我希望默认 API 返回适合展示的 marker，同时仍能按需拉取完整审计历史。

#### Acceptance Criteria

1. `/api/strategy/signals` SHALL 支持或等价实现默认降噪模式。
2. 默认降噪模式 SHALL 返回每个 structure key / entry trigger id / action lifecycle 的最新代表 marker。
3. 默认降噪模式 SHALL 对 preview history、重复 suppressed checks、旧 rejected attempts 做聚合或隐藏。
4. API SHALL 返回折叠统计，例如 hidden preview count、suppressed repeat count、collapsed lifecycle count 或等价字段。
5. API SHALL 支持显式完整模式，例如 query 参数 `view=audit`、`include_history=true` 或等价方式。
6. API SHALL 支持按 layer/status/time range 过滤，至少能区分 structure、preview_signal、entry_trigger、main/action、position_management。
7. WHEN 旧前端不传新增参数 THEN API SHALL 保持兼容，且不得 500。
8. WHEN API 返回 marker THEN marker SHALL 包含足够字段支持前端解释：`structure_key`、`signal_id`、`parent_signal_id`、`entry_trigger_id`、`source_layer`、`status`、`trade_intent`、`signal_close_time`、`decision_close_time`、`display_close_time`、`age_candles`、`freshness_state`、`reason_code/reason`。

### R7 所有币种必须使用统一显示模板

**User Story:** 作为交易员，我希望切换任意币种时看到一致的信号表达方式，不需要重新猜测每个币种页面上标签、颜色、排序和状态含义。

#### Acceptance Criteria

1. 策略检查页 SHALL 使用同一个 `SignalDisplayModel` 或等价统一显示模型渲染所有 symbol。
2. 统一显示模型 SHALL 不包含针对 `BTCUSDT`、`ETHUSDT` 或任意具体 symbol 的特殊分支。
3. 统一显示模板 SHALL 至少分为五类：结构背景、预览观察、入场触发、交易动作、失效/拒绝。
4. 每类模板 SHALL 有稳定的标题、短标签、状态文案、颜色语义、优先级、tooltip 字段和默认可见性。
5. 结构背景模板 SHALL 显示 signal type、方向、结构时间、年龄、当前状态，例如背景、等待触发、已失效。
6. 预览观察模板 SHALL 显示 preview phase、component timeframe、闭合组件数、是否 confirmed_by_1h，并默认弱化或聚合显示。
7. 入场触发模板 SHALL 显示 trigger type、trigger timeframe、trigger close time、parent structure、ready/rejected 状态。
8. 交易动作模板 SHALL 显示 trade intent、执行/拒绝/失败状态、决策时间、原因、关联 trigger/structure。
9. 失效/拒绝模板 SHALL 显示 reason code、结构时间、决策时间、年龄和是否已被折叠/抑制。
10. 统一模板 SHALL 用同一套排序优先级：可执行/已执行动作 > ready entry trigger > 最新 rejected/failed action > 最新结构摘要 > preview 摘要 > 历史审计。
11. 统一模板 SHALL 用同一套默认过滤策略：默认隐藏普通 preview 历史、重复 suppressed checks、过期 invalidated 历史；审计模式可展开。
12. 统一模板 SHALL 在最新信号面板、蜡烛图标签、tooltip、持仓管理信号列表中共享同一语义映射，避免同一 marker 在不同位置显示成不同含义。
13. WHEN 用户切换 symbol THEN 页面 SHALL 保持相同的 layer/status filter、聚合规则、标签格式和 tooltip 字段。
14. WHEN 某 symbol 没有当前信号但有历史 markers THEN 页面 SHALL 按统一模板显示“无当前可行动信号”和可展开历史摘要，而不是空白或误显示旧信号为最新信号。
15. WHEN API 返回新增聚合统计 THEN 模板 SHALL 显示 hidden/collapsed 数量，所有 symbol 格式一致。

### R8 前端图表默认必须清楚、可切换、可审计

**User Story:** 作为交易员，我希望默认图表看得清当前关键点，需要复盘时再打开全部 marker。

#### Acceptance Criteria

1. 默认图表 SHALL 只显示降噪后的关键 marker：最新结构摘要、entry trigger、executed/failed/rejected 动作、持仓管理动作。
2. 默认图表 SHALL 隐藏或聚合普通 preview confirmed 历史、重复 suppressed checks、过期 invalidated 历史。
3. 图表 SHALL 提供 layer/status 过滤控件，例如结构、预览、入场触发、动作、持仓管理、已拒绝/已失效。
4. WHEN 某个 K 线或相邻 K 线 marker 过多 THEN 图表 SHALL 使用簇/计数 badge 或紧凑模式，hover/click 后展示完整列表。
5. WHEN 同一 K 线同一侧存在多个 marker THEN 图表 SHALL 稳定分层，不得互相覆盖。
6. WHEN 相邻 K 线标签横向碰撞 THEN 图表 SHALL 通过横向微偏移、缩短标签、簇合并或 tooltip 展示完整信息来避免遮挡。
7. WHEN marker 是旧结构背景 THEN 标签文案 SHALL 使用“结构/背景/已失效/等待触发”等语义，不得与“开空/开多已拒绝”的动作标签混淆。
8. WHEN marker 是 action marker THEN 标签 SHALL 优先表达交易意图和状态，例如 `S2 开空 已拒绝`；结构点配对标签可简化为 `S2 结构`。
9. Tooltip SHALL 展示结构时间、决策时间、年龄、source layer、状态、拒绝原因、parent/trigger 关联和 signal id。
10. 页面 SHALL 显示当前图表隐藏/折叠了多少 marker，避免用户误以为信息丢失。

### R9 最新信号面板必须避免把旧结构当成当前信号

**User Story:** 作为交易员，我希望右侧“最新信号”优先告诉我当前可行动状态，而不是一直显示昨天的结构信号。

#### Acceptance Criteria

1. 最新信号选择逻辑 SHALL 优先级排序：可执行/ready entry trigger > 最新 action marker > 最新未失效结构摘要 > 预览观察摘要 > 空状态诊断。
2. WHEN latest structure 已 invalidated/background/suppressed THEN 面板 SHALL 显示该状态和原因，而不是仅显示 signal type/方向/价格。
3. WHEN 只有 preview 观察信号 THEN 面板 SHALL 显示 preview phase、闭合组件数、是否 confirmed_by_1h、是否允许 pilot。
4. WHEN 结构时间与当前时间相差较大 THEN 面板 SHALL 显示年龄，例如 `结构 2026-05-17 20:59:59 / 决策 2026-05-18 19:59:59 / age 23x1h`。
5. WHEN 当前无可行动信号 THEN 面板 SHALL 明确显示等待原因，例如等待 15m fresh trigger、等待 1h 新闭合、结构已错过目标。

### R10 运行时状态需要可迁移/可压缩

**User Story:** 作为维护者，我希望线上已经积累的重复 markers 能被安全压缩，否则前端即使改了也会继续被旧 state 污染。

#### Acceptance Criteria

1. StateStore 加载旧 `programmatic_strategy_state.json` 时 SHALL 能兼容缺失的新字段。
2. 系统 SHALL 提供自动或命令式 state compaction 能力，对同一 structure key 的重复 markers 做折叠。
3. Compaction SHALL 保留最近诊断、最终状态、首次结构时间、最近决策时间、执行/拒绝原因和审计计数。
4. Compaction SHALL 不删除 executed action 的关键审计信息。
5. Compaction SHALL 不把不同结构 key、不同 entry trigger id 或不同真实动作误合并。
6. Compaction 后 `/api/strategy/signals` 默认返回 marker 数量 SHOULD 显著下降；以当前全币种样本为例，默认视图不应继续展示 BTCUSDT 27 条、ETHUSDT 17 条、XRPUSDT 16 条、CLUSDT 15 条这类同语义拥挤 marker。

### R11 日志与诊断必须能直接定位噪声来源

**User Story:** 作为排障者，我希望看到日志时可以立即知道 marker 是新结构、重复旧结构、预览观察、还是已折叠历史。

#### Acceptance Criteria

1. 决策日志 SHALL 包含每周期 marker 统计：created、updated、suppressed、collapsed、hidden_by_default。
2. 预览层诊断 SHALL 说明 preview phase、component close time、parent structure key、是否折叠。
3. 主信号诊断 SHALL 说明 structure key、是否新结构、是否重复旧结构、是否语义抑制。
4. API 最新诊断 SHALL 包含面向用户的简短信息和面向排障的结构化字段。
5. Docker/service 日志 SHALL 避免每周期打印过长的重复 preview 列表；重复信息 SHOULD 摘要化，例如“SYMBOL preview_3x15m 同结构观察已更新，折叠计数 +1”。

### R12 安全与交易语义必须保持

**User Story:** 作为系统所有者，我希望 UI 变清楚和信号变及时，但不能引入追涨杀跌或绕过风控。

#### Acceptance Criteria

1. 所有 open/add 候选仍 SHALL 经过现有 open gate、ADX/DI、BTC 环境、相关性、风险预算、仓位限制、止损/止盈结构和最小 RR 检查。
2. 已经 `target_already_crossed` 的结构 SHALL 不被自动转化为可开仓 continuation。
3. Preview 默认 SHALL 只观察；pilot 或 full open 必须显式配置开启，并有更严格风险上限。
4. UI 降噪 SHALL 不删除或隐藏真实已执行/失败交易动作的审计信息。
5. 新增配置默认值 SHALL 保守，不改变当前实盘下单风险。

### R13 测试与验证

**User Story:** 作为维护者，我希望这次线上复发场景有回归测试，后续改图表或状态逻辑时不会再退化。

#### Acceptance Criteria

1. SHALL 增加后端单元测试，覆盖同一 structure key 在不同 config hash 下折叠为同一生命周期。
2. SHALL 增加后端单元测试，覆盖 `target_already_crossed` 后同结构重复周期不追加默认 marker。
3. SHALL 增加后端单元测试，覆盖 preview_2x15m/preview_3x15m 同一 1h candle 内默认折叠。
4. SHALL 增加 StateStore 测试，覆盖旧 state migration/compaction 不丢 executed action。
5. SHALL 增加 API 测试，覆盖默认降噪模式、完整审计模式、layer/status/time range 过滤。
6. SHALL 增加前端 utility 测试，覆盖 marker 聚合、排序、过滤、隐藏计数、tooltip 字段。
7. SHALL 增加前端图表组件测试或截图验证，覆盖同 K 线多 marker、相邻 K 线碰撞、簇 badge 展开。
8. SHOULD 使用 161 全币种样本构造脱敏 fixture，至少覆盖 BTCUSDT、ETHUSDT、SOLUSDT、XAGUSDT、XRPUSDT、CLUSDT 的重复/滞后 marker，验证默认展示 marker 数量和语义符合预期。
9. SHALL 运行 `go test ./strategy/chanlun ./api ./manager`。
10. SHALL 运行 `cd web && npm run test`。
11. SHALL 运行 `cd web && npm run build`。
12. SHOULD 在本地或 161 测试环境复核多个 symbol 的策略检查页：默认视图清楚、审计模式可展开、右侧最新信号不再误导为新开仓信号，且所有 symbol 显示模板一致。
