# 主交易 K 线蜡烛图与买卖点标记 Requirements

## Background

当前程序化策略检查区已经能展示主交易频率的 K 线和策略信号，但主交易 K 线以表格形式展示，默认前端只截取最近 8 根 K 线。对于缠论程序化策略复盘，表格无法直观看到走势结构、信号所在位置、买卖点分类和级别关系。

项目已有以下基础能力：

- 程序化策略配置中已有 `programmatic_strategy.timeframes.trade`，支持 `15m`、`1h`、`4h` 作为主交易级别。
- 程序化策略配置中已有 `programmatic_strategy.history_depth`，按 `3m`、`15m`、`1h`、`4h` 配置历史 K 线深度。
- 后端已有 `/api/market/klines` 返回闭合 K 线。
- 后端已有 `/api/strategy/signals` 返回 `signals` 和 `signal_markers`，包含 `signal_type`、`direction`、`level`、`timeframe`、`close_time`、`source_layer`、`status`、`price` 等字段。
- 前端 `StrategyInspector` 当前将主交易 K 线渲染为表格，并对部分行展示 marker badge。

本规格目标是把主交易 K 线从表格升级为蜡烛图，并在蜡烛图上按缠论买卖点类型、交易意图、方向、级别和状态标记信号，K 线展示数量应来源于配置文件中的历史深度，而不是前端硬编码。

## Goals

- 用蜡烛图展示主交易级别 K 线，替代当前主交易 K 线表格。
- 在蜡烛图上标记 `buy1`、`buy2`、`buy3`、`sell1`、`sell2`、`sell3` 等分类买卖点。
- 在标记中同时体现买卖点分类、交易意图、方向、级别、状态和原因，方便策略复盘。
- 主交易 K 线展示数量使用配置文件对应主交易级别的 `history_depth` 数量。
- 保持程序化策略主交易级别可配置，支持 `15m`、`1h`、`4h`。
- 保持现有策略检查区、信号诊断和持仓管理信号展示能力。

## Non-Goals

- 本规格不修改缠论买卖点识别算法本身。
- 本规格不修改开仓、加仓、减仓、平仓执行逻辑。
- 本规格不新增回测能力。
- 本规格不要求前端实现专业交易终端级别的完整画线工具。
- 本规格不把 3m/15m 持仓管理信号强制画到主交易级别蜡烛图上，除非信号时间和级别能够明确映射。
- 本规格不提交或暴露真实生产 `config.json`、API key 或账户凭证。

## Glossary

- **主交易级别**：程序化策略配置中的 `programmatic_strategy.timeframes.trade`，用于真实开仓主信号判断。
- **主交易 K 线**：与主交易级别一致的闭合 K 线序列，例如 trade=`1h` 时展示 1h 闭合 K 线。
- **配置展示数量**：配置文件 `programmatic_strategy.history_depth` 中与主交易级别对应的数量，例如 trade=`1h` 时使用 `history_depth["1h"]`。
- **买卖点分类**：缠论信号类型，包括 `buy1`、`buy2`、`buy3`、`sell1`、`sell2`、`sell3`。
- **买卖点级别**：信号的分析级别或触发级别，例如 `15m`、`1h`、`4h`，由策略信号的 `level`、`analysis_timeframe`、`trigger_timeframe` 或 marker `timeframe` 表达。
- **交易意图**：由策略动作和持仓方向推导出的交易含义，包括开多、开空、加多、加空、减多、减空、平多、平空。
- **信号状态**：信号当前状态，例如 `detected`、`executed`、`rejected`、`failed`。

## Requirements

### 1. 主交易 K 线必须用蜡烛图展示

**User Story:** 作为量化交易员，我希望主交易 K 线以蜡烛图展示，而不是只看表格，以便直观看到走势、波动和买卖点所在位置。

#### Acceptance Criteria

1. WHEN trader 为 `programmatic` 模式 THEN 策略检查区 SHALL 使用蜡烛图展示主交易级别 K 线。
2. WHEN 主交易 K 线数据存在 THEN 前端 SHALL 展示每根 K 线的开盘价、最高价、最低价、收盘价。
3. WHEN K 线收盘价大于或等于开盘价 THEN 蜡烛 SHALL 使用上涨样式。
4. WHEN K 线收盘价小于开盘价 THEN 蜡烛 SHALL 使用下跌样式。
5. WHEN 用户悬停或点击某根 K 线 THEN 前端 SHOULD 能展示该 K 线的时间、OHLC 和成交量。
6. WHEN 无 K 线数据或接口失败 THEN 前端 SHALL 展示明确空状态或错误状态，不得回退成误导性的静态表格。
7. WHEN 屏幕宽度较窄 THEN 蜡烛图 SHALL 保持可读，不得与策略检查区其它内容重叠。

### 2. K 线展示数量必须来自配置文件

**User Story:** 作为量化交易员，我希望图上展示的 K 线数量与策略配置一致，避免前端硬编码数量影响复盘视角。

#### Acceptance Criteria

1. WHEN 主交易级别为 `15m` THEN 主交易 K 线展示数量 SHALL 使用 `programmatic_strategy.history_depth["15m"]`。
2. WHEN 主交易级别为 `1h` THEN 主交易 K 线展示数量 SHALL 使用 `programmatic_strategy.history_depth["1h"]`。
3. WHEN 主交易级别为 `4h` THEN 主交易 K 线展示数量 SHALL 使用 `programmatic_strategy.history_depth["4h"]`。
4. WHEN 前端请求主交易 K 线且未显式传入 `limit` THEN 后端 SHALL 使用该 trader 程序化策略配置中对应 timeframe 的 history depth。
5. WHEN 前端请求主交易 K 线 THEN 前端 SHALL 不再硬编码 `80` 或 `8` 作为主交易图展示数量。
6. WHEN 配置中的 history depth 超过行情接口或前端可稳定展示上限 THEN 系统 MAY 使用后端安全上限截断，并 SHALL 在响应中返回实际 `limit`。
7. WHEN trader 不是程序化策略或没有 programmatic 配置 THEN `/api/market/klines` SHALL 保持兼容，使用安全默认数量。
8. WHEN 配置发生调整并服务重启后 THEN 策略检查区 SHALL 按新配置数量展示 K 线。

### 3. 蜡烛图必须标记分类买卖点

**User Story:** 作为量化交易员，我希望在主交易蜡烛图上直接看到一类、二类、三类买卖点，便于判断信号质量。

#### Acceptance Criteria

1. WHEN `signal_markers` 中存在 `source_layer=main_signal` 且 `timeframe` 等于主交易级别 THEN 蜡烛图 SHALL 在对应 `close_time` 的 K 线上显示信号标记。
2. WHEN marker `signal_type=buy1` THEN 图上 SHALL 显示一类买点标记。
3. WHEN marker `signal_type=buy2` THEN 图上 SHALL 显示二类买点标记。
4. WHEN marker `signal_type=buy3` THEN 图上 SHALL 显示三类买点标记。
5. WHEN marker `signal_type=sell1` THEN 图上 SHALL 显示一类卖点标记。
6. WHEN marker `signal_type=sell2` THEN 图上 SHALL 显示二类卖点标记。
7. WHEN marker `signal_type=sell3` THEN 图上 SHALL 显示三类卖点标记。
8. WHEN 同一根 K 线上存在多个信号 THEN 前端 SHALL 能同时展示多个标记，且标记不得互相完全遮挡。
9. WHEN marker 缺少价格 THEN 前端 SHALL 使用该 K 线的高低点附近位置放置信号标记。
10. WHEN marker 有价格 THEN 前端 SHOULD 优先将标记放置在 marker 价格附近。

### 4. 买卖点标记必须体现方向、分类、级别和状态

**User Story:** 作为量化交易员，我希望买卖点标记不仅有位置，还能区分买/卖、几类买卖点、策略级别和执行状态。

#### Acceptance Criteria

1. WHEN signal 为买点 THEN 标记 SHALL 使用与多头/买入一致的视觉样式。
2. WHEN signal 为卖点 THEN 标记 SHALL 使用与空头/卖出一致的视觉样式。
3. WHEN signal 类型为 `buy1` 或 `sell1` THEN 标记文本 SHALL 能体现 `B1` 或 `S1`。
4. WHEN signal 类型为 `buy2` 或 `sell2` THEN 标记文本 SHALL 能体现 `B2` 或 `S2`。
5. WHEN signal 类型为 `buy3` 或 `sell3` THEN 标记文本 SHALL 能体现 `B3` 或 `S3`。
6. WHEN marker 包含 `level` 或 `timeframe` THEN 标记 tooltip 或详情 SHALL 展示该级别。
7. WHEN marker 状态为 `executed` THEN 标记 SHALL 与仅检测到的信号有明显区分。
8. WHEN marker 状态为 `rejected` 或 `failed` THEN 标记 SHALL 显示为被拒绝/失败状态，且不应误导为已执行信号。
9. WHEN marker 包含 `reason` THEN tooltip 或详情 SHALL 展示原因说明。
10. WHEN marker 包含 `signal_id` THEN tooltip 或详情 SHOULD 展示或保留该 ID，便于日志对账。

### 5. 主交易级别必须跟随策略配置

**User Story:** 作为量化交易员，我希望图表自动展示当前配置的主交易级别，不需要前端固定为 1h。

#### Acceptance Criteria

1. WHEN `/api/strategy/signals` 返回 `trade_timeframe` THEN 前端 SHALL 使用该值作为主交易蜡烛图 timeframe。
2. WHEN `trade_timeframe=15m` THEN 图表 SHALL 请求并展示 15m K 线。
3. WHEN `trade_timeframe=1h` THEN 图表 SHALL 请求并展示 1h K 线。
4. WHEN `trade_timeframe=4h` THEN 图表 SHALL 请求并展示 4h K 线。
5. WHEN `trade_timeframe` 缺失 THEN 前端 MAY 使用 `1h` 作为兼容回退，并 SHALL 在诊断区提示回退。
6. WHEN 用户切换策略标的 THEN 图表 SHALL 刷新为该 symbol 的主交易 K 线和信号标记。

### 6. 买卖点必须区分信号分类与交易意图

**User Story:** 作为量化交易员，我希望图上的买卖点能区分开多、开空、平多和平空，避免把反向信号误解为反手交易。

#### Acceptance Criteria

1. WHEN marker 只有 `signal_type` 且没有 `action`、`final_action` 或可确认交易动作 THEN 前端 SHALL 只展示信号分类，例如 `B1`、`B2`、`B3`、`S1`、`S2`、`S3`，不得强行标为开仓或平仓。
2. WHEN marker 对应 `final_action` 且 `final_action` 非空 THEN 前端 SHALL 优先使用 `final_action` 推导交易意图。
3. WHEN marker 对应 `action=open_long` THEN 前端 SHALL 标记交易意图为“开多”。
4. WHEN marker 对应 `action=open_short` THEN 前端 SHALL 标记交易意图为“开空”。
5. WHEN marker 对应 `action=add_long` THEN 前端 SHALL 标记交易意图为“加多”。
6. WHEN marker 对应 `action=add_short` THEN 前端 SHALL 标记交易意图为“加空”。
7. WHEN marker 对应 `action=close_long` 或 `final_action=close_long` THEN 前端 SHALL 标记交易意图为“平多”。
8. WHEN marker 对应 `action=close_short` 或 `final_action=close_short` THEN 前端 SHALL 标记交易意图为“平空”。
9. WHEN marker 对应 `action=partial_close` 且 `position_side=long`、`direction=long` 或等价字段可确认目标持仓为多头 THEN 前端 SHALL 标记交易意图为“减多”。
10. WHEN marker 对应 `action=partial_close` 且 `position_side=short`、`direction=short` 或等价字段可确认目标持仓为空头 THEN 前端 SHALL 标记交易意图为“减空”。
11. WHEN marker 对应 `final_action=partial_close_skipped` THEN 前端 SHALL 展示为“减仓跳过”或等价状态，不得标为真实减仓成交。
12. WHEN 原始 `action=partial_close` 但 `final_action=close_long` THEN 前端 SHALL 按“平多”展示，不得按“减多”展示。
13. WHEN 原始 `action=partial_close` 但 `final_action=close_short` THEN 前端 SHALL 按“平空”展示，不得按“减空”展示。
14. WHEN 同一 symbol 已有多头持仓且出现 `sell2` 或 `sell3` 反向信号 THEN 图表 MAY 展示卖点分类，但只有该信号被持仓管理采用为 `partial_close` 或 `close_long` 时，才 SHALL 标记为“减多”或“平多”。
15. WHEN 同一 symbol 已有空头持仓且出现 `buy2` 或 `buy3` 反向信号 THEN 图表 MAY 展示买点分类，但只有该信号被持仓管理采用为 `partial_close` 或 `close_short` 时，才 SHALL 标记为“减空”或“平空”。
16. WHEN 同一 symbol 不允许双向持仓且策略未启用反手 THEN 前端 SHALL 不把持仓中的反向买卖点直接标记为“开空”或“开多”。
17. WHEN marker 同时包含信号分类和交易意图 THEN 标记 SHALL 同时保留两层信息，例如 `S2 · 减多`、`B1 · 开多`、`S1 · 开空`。
18. WHEN 后端当前 marker 缺少显式交易意图字段 THEN 设计阶段 SHALL 明确通过新增字段或前端推导规则补齐，不得依赖中文 reason 文本解析。

### 7. 信号与 K 线的时间匹配必须稳定

**User Story:** 作为量化交易员，我希望买卖点标记准确落在对应闭合 K 线上，避免因为时区或毫秒/秒单位错位导致复盘错误。

#### Acceptance Criteria

1. WHEN K 线和 marker 都使用 `close_time` THEN 系统 SHALL 使用同一时间单位进行匹配。
2. WHEN marker `close_time` 与某根 K 线 `close_time` 相等 THEN 标记 SHALL 落在该 K 线上。
3. WHEN marker 时间单位疑似为秒而 K 线为毫秒，或相反 THEN 前端/后端 SHALL 统一处理，避免标记全部丢失。
4. WHEN marker 不在当前展示的 K 线范围内 THEN 图表 SHALL 不显示该 marker，但信号诊断区 MAY 继续展示最近信号信息。
5. WHEN K 线为闭合 K 线 THEN 图表 SHALL 不混入未闭合主交易 K 线。

### 8. 保留策略检查区现有诊断能力

**User Story:** 作为量化交易员，我希望新增蜡烛图后，仍能看到最新信号、配置 hash、主/组件/微观周期和持仓管理动作。

#### Acceptance Criteria

1. WHEN 策略检查区展示蜡烛图 THEN SHALL 保留“最新信号”区域。
2. WHEN 策略检查区展示蜡烛图 THEN SHALL 保留“信号诊断”区域。
3. WHEN `signals.config_hash` 存在 THEN 前端 SHALL 继续展示配置 hash。
4. WHEN `signals.component_timeframe` 和 `signals.micro_timeframe` 存在 THEN 前端 SHALL 继续展示 component 和 micro 信息。
5. WHEN 存在 `source_layer=position_management` 的 marker THEN 前端 SHALL 继续在策略检查区展示持仓管理相关动作摘要。
6. WHEN 当前 trader 使用 AI 决策模式 THEN 策略检查区 SHALL 保持现有 AI 模式提示，不请求或展示程序化蜡烛图。

### 9. API 契约与兼容性

**User Story:** 作为前端开发者，我希望后端 K 线和信号 API 提供足够元数据，使前端能稳定画图而不猜配置。

#### Acceptance Criteria

1. `/api/market/klines` SHALL 返回 `symbol`、`timeframe`、`limit`、`klines`。
2. `/api/market/klines` SHOULD 返回 `configured_limit` 或等价字段，说明配置来源的 K 线数量。
3. `/api/market/klines` SHOULD 返回 `limit_source` 或等价字段，区分 query limit、programmatic history depth、default fallback。
4. `/api/strategy/signals` SHALL 继续返回 `trade_timeframe`、`component_timeframe`、`micro_timeframe`。
5. `/api/strategy/signals` SHALL 继续返回 `signal_markers`。
6. WHEN 新增 API 字段 THEN 旧前端客户端 SHALL 不受影响。
7. WHEN 旧日志或旧状态文件缺少 marker 字段 THEN API SHALL 返回空数组或省略字段，不得 500。
8. WHEN marker 由策略决策或执行结果生成且存在原始策略动作 THEN `/api/strategy/signals` SHALL 在 marker 中返回 `action`，用于表达原始策略动作。
9. WHEN marker 由执行结果生成且执行层存在最终动作 THEN `/api/strategy/signals` SHALL 在 marker 中返回 `final_action`，用于区分执行后真实动作与原始策略建议。
10. WHEN marker 可推导交易意图 THEN `/api/strategy/signals` SHALL 在 marker 中返回 `trade_intent`，用于表达开多、开空、加多、加空、减多、减空、平多、平空。
11. WHEN marker 的动作是 `partial_close` THEN `/api/strategy/signals` SHALL 在 marker 中返回 `position_side` 或等价字段，用于稳定区分 `partial_close` 是减多还是减空。
12. WHEN 旧状态文件中的 marker 缺少 `trade_intent`、`final_action` 或 `position_side` THEN API SHALL 保持兼容，前端 SHALL 使用可用字段做 best effort 展示。

### 10. 前端交互与可读性

**User Story:** 作为量化交易员，我希望蜡烛图在日常监控屏幕上可读、不卡顿，并能快速识别关键信号。

#### Acceptance Criteria

1. 蜡烛图高度 SHALL 足够展示价格波动和信号标记。
2. 图表 SHALL 在桌面宽屏中优先占据策略检查区更大的展示空间。
3. 图表 SHALL 在移动或窄屏下自适应宽度。
4. WHEN K 线数量较多 THEN 前端 SHALL 避免渲染明显卡顿。
5. WHEN 价格小数位较多的标的如 `1000PEPEUSDT` THEN tooltip 和价格轴 SHALL 保持可读。
6. WHEN 标的价格较高如 `BTCUSDT` THEN tooltip 和价格轴 SHALL 保持可读。
7. 图例或标记说明 SHOULD 解释 `B1/B2/B3/S1/S2/S3`、交易意图和状态含义。

### 11. 测试与验证

**User Story:** 作为系统维护者，我希望该改动有足够验证，避免主交易级别、配置数量或信号标记再次错位。

#### Acceptance Criteria

1. SHALL 增加或更新后端测试，验证 `/api/market/klines` 在未传 `limit` 时可使用程序化配置 history depth。
2. SHALL 增加或更新后端测试，验证非法 timeframe 仍返回中文 400 错误。
3. SHALL 增加或更新前端类型，覆盖新增的 K 线 limit 元数据字段。
4. SHALL 运行前端构建，确保蜡烛图组件类型正确。
5. SHOULD 增加前端纯函数测试，验证 marker 与 K 线 close_time 匹配逻辑。
6. SHOULD 增加前端纯函数测试，验证 `action/final_action/position_side/direction` 到交易意图的映射。
7. SHOULD 通过本地页面或截图检查，验证蜡烛图、买卖点标记和 tooltip 可正常显示。
8. WHEN 完成实现后部署 161 THEN SHALL 验证线上程序化 trader 的策略检查区显示蜡烛图，且 K 线数量与配置一致。
