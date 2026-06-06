# 程序化缠论交易策略 Requirements

## 背景

当前 NOFX 的开仓和平仓主要由 AI 在 `decision.GetFullDecision()` 中判断。AI 具备解释力和泛化能力，但存在输出不稳定、格式不稳定、同一行情下决策不完全一致等问题。为降低关键交易动作对 AI 的依赖，需要新增一种可配置切换的程序化交易策略。

本规格基于用户提供的《Nofx交易策略.docx》提炼。策略核心采用《缠中说禅》三类买卖点、走势中枢、趋势/盘整、MACD 面积背驰、级别联动和短差程序，覆盖开仓、加仓、减仓和平仓。实现应融入当前 NOFX 架构：仍由 `AutoTrader` 构建交易上下文，仍产出标准 `decision.Decision` 动作，仍经过现有强制风控、交易计划、保护单同步、下单前检查、交易所执行和决策日志。

## 目标

- 允许每个 trader 通过配置选择使用 AI 决策或程序化策略决策。
- 当 trader 使用程序化策略时，由程序化策略接管策略层交易能力，负责输出开仓、加仓、减仓和平仓决策。
- 程序化策略在相同行情、账户、持仓、配置和持久化状态输入下输出确定性决策。
- 程序化策略能识别并标注第一类、第二类、第三类买卖点。
- 程序化策略以现有 `3m`、`15m`、`1h`、`4h` K 线为默认数据基础，并可按策略需要配置更长历史深度。
- 标的池沿用当前动态候选池/静态合并池方案，保留每日自动更新能力，同时支持 trader 级自定义标的池补充或限制交易范围。
- 程序化策略输出仍受现有强制风控、交易计划、保护单同步、执行保护、最小下单额和日志可观测性约束。

## 非目标

- 本阶段不要求用 AI 辅助解释或改写程序化策略信号。
- 本阶段不要求实现股票市场接入；文档中的“股票代码”在当前 NOFX 中映射为加密合约交易对 symbol。
- 本阶段不要求绕过现有 `Trader` 接口新增交易所能力。
- 本阶段不要求实现行情级离线回测或 replay 扩展；完整 replay/backtest 后续独立 spec 处理。
- 本阶段不要求保证策略盈利，所有策略行为只保证可配置、可复现、可验证。

## 术语

- **决策模式**：每个 trader 的策略来源，至少包含 `ai` 和 `programmatic`。
- **程序化策略**：不调用 AI，基于行情、账户、持仓、配置和历史状态生成交易决策的确定性策略。
- **公共风控层**：不属于 AI 或程序化策略私有逻辑的安全层，包括全局熔断、账户回撤硬停、交易计划失效、保护单同步、下单前 preflight、最小名义额、相关性和总风险预算。
- **缠论级别**：策略分析使用的时间级别，例如 higher / trade / sub / micro。实际可用级别必须来自系统支持的 K 线周期或由配置明确启用。
- **包含关系处理**：在识别分型、笔和线段前，对相邻 K 线的高低点包含关系进行规范化处理。
- **分型**：经包含关系处理后的局部顶分型或底分型。
- **笔**：满足最小 K 线数量和最小波动阈值的一组相邻顶/底分型连接。
- **线段**：由多个连续笔或 swing-pivot 段组成的可计算走势段。
- **swing-pivot 模型**：用局部极值、左右确认 K 线数量、最小波动阈值和 ATR 过滤生成走势 pivot 和 segment 的简化机器模型。
- **走势中枢**：某级别走势中，由至少三个连续次级别走势类型重叠形成的价格区间。
- **ZG**：走势中枢高点中的最低点。
- **ZD**：走势中枢低点中的最高点。
- **趋势**：同级别至少两个依次同向中枢构成的上涨或下跌走势。
- **盘整**：某级别只包含一个走势中枢的走势类型。
- **背驰**：同向前后两段走势中，后一段趋势力度弱于前一段。程序化实现应支持 MACD 柱面积作为默认判定依据。
- **第一类买点**：下跌趋势末端，男上位最后一吻后出现背驰式下跌形成的买点。
- **第二类买点**：第一类买点后的第一次上 0 轴回抽确认，且回调低点不破最近一类买点低点形成的买点。
- **第三类买点**：次级别走势向上离开中枢后回试，低点不跌破 ZG 形成的买点。
- **第一类卖点**：上涨趋势末端，女上位缠绕后出现背驰形成的卖点。
- **第二类卖点**：第一类卖点后的第一次下 0 轴反抽确认，且反弹高点不破最近一类卖点高点形成的卖点。
- **第三类卖点**：次级别走势向下离开中枢后回抽，高点不升破 ZD 形成的卖点。
- **短差程序**：大级别买点介入后，在次级别第一类卖点减仓，在次级别第一类买点回补的仓位管理程序。
- **加仓动作**：显式动作 `add_long` 或 `add_short`，表示已有同向仓位下增加仓位，不得用重复 `open_long` 或 `open_short` 偷换语义。

## Requirements

### 1. 决策模式配置

**User Story:** 作为量化交易员，我希望每个 trader 可以通过配置选择 AI 决策或程序化策略决策，以便在需要稳定规则输出时停用 AI。

#### Acceptance Criteria

1. WHEN trader 配置缺少决策模式字段 THEN 系统 SHALL 默认保持现有 AI 决策行为，保证向后兼容。
2. WHEN trader 配置 `decision_mode` 为 `ai` THEN 系统 SHALL 沿用当前 AI 决策流程、AI 调用频率、AI 日志和 open gate 行为。
3. WHEN trader 配置 `decision_mode` 为 `programmatic` THEN 程序化策略 SHALL 接管策略层交易能力，并输出开仓、加仓、减仓、平仓、等待或持有决策。
4. WHEN trader 配置 `decision_mode` 为 `programmatic` THEN 系统 SHALL 不调用 AI API 生成开仓、加仓、减仓或平仓决策。
5. WHEN `decision_mode` 为不支持的值 THEN 配置校验 SHALL 失败，并返回清晰中文错误。
6. WHEN 同一系统运行多个 trader THEN 每个 trader SHALL 可独立选择 `ai` 或 `programmatic`，互不影响。
7. WHEN 程序化策略运行 THEN 现有强制风控、交易计划、保护单同步和执行保护 SHALL 作为公共层保留，且可覆盖、拒绝或补充策略输出。

### 2. 程序化策略配置

**User Story:** 作为量化交易员，我希望程序化策略的时间级别、买卖点开关、仓位比例、K 线历史、均线周期和风控参数可配置，以便按不同账户和交易周期调参。

#### Acceptance Criteria

1. WHEN `decision_mode=programmatic` THEN 系统 SHALL 要求或自动补全 `programmatic_strategy` 配置。
2. IF 程序化策略配置缺失可安全默认的字段 THEN 系统 SHALL 使用保守默认值。
3. IF 程序化策略配置缺失必须字段或字段范围非法 THEN 配置校验 SHALL 失败。
4. WHEN 配置包含 unsupported timeframe THEN 系统 SHALL 在启动时失败，而不是运行时静默跳过。
5. WHEN 配置包含 `allow_long=false` 或 `allow_short=false` THEN 程序化策略 SHALL 不输出对应方向的开仓或加仓动作。
6. WHEN 配置包含买卖点启用列表 THEN 程序化策略 SHALL 只使用启用的买卖点类型产生交易动作。
7. WHEN 配置包含 `moving_average.short_period` 和 `moving_average.long_period` THEN 均线“吻”、男上位和女上位 SHALL 使用配置周期；默认使用 EMA20 和 EMA50。
8. WHEN 配置包含 `history_depth` THEN 程序化策略 SHALL 按配置为 `3m`、`15m`、`1h`、`4h` 获取或保留足够长度的 OHLC 和指标序列。
9. IF `history_depth` 缺失 THEN 默认 SHALL 使用 `3m=240`、`15m=192`、`1h=240`、`4h=180` 作为策略专用历史深度目标。
10. WHEN 配置包含 MACD 背驰、swing-pivot、笔/线段、加仓、减仓、反手或 micro ADX 参数 THEN 系统 SHALL 校验数值范围并在日志记录生效配置摘要。

### 3. 标的池来源与自定义交易池

**User Story:** 作为量化交易员，我希望程序化策略沿用当前标的池自动更新方案，同时支持自定义标的池，以便既保留每日动态选币能力，又能手工指定重点交易标的。

#### Acceptance Criteria

1. WHEN 未配置自定义标的池 THEN 程序化策略 SHALL 沿用当前动态候选池每日自动刷新方案；若动态候选池未启用或不可用，则回退当前静态合并池方案。
2. WHEN 动态候选池启用 THEN 程序化策略 SHALL 复用现有刷新时间、TTL、最小/最大池大小、AI500、OI Top、交易所成交额 Top、核心标的和当前持仓保留逻辑。
3. WHEN 配置提供自定义标的池 THEN 自定义标的池 SHALL 位于 trader 级 `programmatic_strategy.symbol_pool` 下，而动态候选池仍保持全局配置。
4. WHEN trader 自定义标的池模式为 `append` THEN 程序化策略 SHALL 将自定义标的并入动态/静态候选池，并去重。
5. WHEN trader 自定义标的池模式为 `override` THEN 程序化策略 SHALL 仅分析自定义标的、当前持仓标的和配置允许的核心标的。
6. WHEN trader 自定义标的池模式为 `filter` THEN 程序化策略 SHALL 仅从动态/静态候选池中保留同时存在于自定义标的池的标的。
7. WHEN 存在当前持仓 symbol THEN 当前持仓 symbol SHALL 永远进入分析池，不受 `filter` 或 `override` 排除，确保可平仓、减仓和风控。
8. IF 自定义标的格式无效 THEN 配置校验 SHALL 失败，并指出具体 symbol。
9. WHEN 标的池由多个来源合成 THEN 系统 SHALL 在日志和前端数据中保留每个 symbol 的来源，例如 `dynamic`、`ai500`、`oi_top`、`exchange_volume_top`、`core`、`position`、`custom`。
10. WHEN 前端展示交易标的选择器 THEN 下单或检查用 symbol SHALL 来自最终合成后的标的池下拉框，而不是自由文本输入。
11. WHEN 用户在前端点选标的池中的 symbol THEN 页面 SHALL 联动展示该 symbol 的 K 线与关键策略信号状态。

### 4. 行情数据、级别输入与 ADX/DI

**User Story:** 作为量化交易员，我希望策略使用多级别 K 线和指标来识别买卖点，避免只依赖单周期信号。

当前项目 `market.Data` 已提供四层 K 线数据：`3m`、`15m`、`1h`、`4h`。程序化缠论策略的默认级别映射 SHALL 如下：

| 项目现有 K 线 | 策略级别 | 默认用途 |
| --- | --- | --- |
| `4h` (`LongerTermContext`) | 大级别 / higher level | 大趋势过滤、市场状态过滤、大级别中枢/趋势背景、方向共振、宽波动止损地板、禁止逆大级别重仓 |
| `1h` (`MidTermSeries1h`) | 主交易级别 / trade level | 默认的一二三类买卖点识别、A+B+C 结构、主级别中枢、MACD 背驰、均线吻、ADX/DI 趋势有效性、基础 ATR 止损 |
| `15m` (`MidTermSeries15m`) | 次级别 / sub level | 1h 买卖点的次级别确认、第二类买卖点回抽确认、第三类买卖点离开/回试确认、短差减仓/回补、精细化入场过滤 |
| `3m` (`IntradaySeries`) | 执行级别 / micro level | 最终执行触发、价格突破/跌破确认、微观 MACD/RSI/ATR 辅助、止损/减仓执行细化；默认不得单独作为主开仓依据 |

ADX/DI 可以由行情 K 线自行计算，不需要外部专门接口。计算所需最小数据为同一 timeframe 的连续 OHLC K 线：`high`、`low`、`close`，并需要上一根 K 线的 `high`、`low`、`close` 计算 `+DM`、`-DM` 和 `TR`。默认周期 SHALL 使用 14；为了形成 Wilder ADX，样本数量 SHALL 至少满足 `period * 2` 根 K 线，建议保留更多样本提高稳定性。当前项目已经在 `15m`、`1h`、`4h` 上基于 K 线计算 ADX/DI；`3m` K 线也具备 `high/low/close`，技术上可以计算 ADX/DI，但需要在 `IntradayData` 中补充 `ADXValues`、`DIPlus`、`DIMinus` 后才能作为结构化数据使用。

#### Acceptance Criteria

1. WHEN 程序化策略运行 THEN 系统 SHALL 使用当前项目已有的 `3m`、`15m`、`1h`、`4h` 行情数据作为默认计算输入，不得默认依赖当前项目未提供的 K 线级别。
2. WHEN 程序化策略需要识别走势、中枢、A+B+C 或二/三类买卖点 THEN 系统 SHALL 使用策略专用历史深度下的连续 OHLC 和指标序列，不得只依赖面向 AI 摘要的最近 10 个指标点。
3. IF 任一标的缺少判定必须的 K 线或指标数据 THEN 程序化策略 SHALL 对该标的输出 `wait` 或跳过，并记录缺失的 timeframe 与指标名。
4. WHEN 识别主交易信号 THEN 系统 SHALL 默认以 `1h` 作为主交易级别，以 `15m` 作为次级别确认，以 `4h` 作为大级别方向过滤，以 `3m` 作为执行触发。
5. WHEN 计算一类买卖点的 MACD 背驰 THEN 系统 SHALL 默认在 `1h` 上比较同向 A/C 段 MACD 柱面积，并用 `15m` 的背驰或结构完成作为确认。
6. WHEN 计算二类买卖点 THEN 系统 SHALL 默认使用 `1h` 判断第一类买卖点后的 0 轴回抽结构，并使用 `15m` 判断回抽不破前低/不破前高的确认点。
7. WHEN 计算三类买卖点 THEN 系统 SHALL 默认使用 `1h` 识别中枢 ZG/ZD 和离开段，并使用 `15m` 识别回试不破 ZG 或回抽不破 ZD。
8. WHEN 执行短差程序 THEN 系统 SHALL 默认使用 `15m` 的第一类卖点/买点产生减仓或回补候选，并使用 `3m` 进行最终执行触发确认。
9. WHEN 判断大级别方向共振或禁止逆势重仓 THEN 系统 SHALL 使用 `4h` 的 EMA、MACD、自计算 ADX/DI、Bollinger、ATR 和价格位置作为过滤条件。
10. WHEN 使用 ADX/DI 判断趋势有效性 THEN 系统 SHALL 优先使用由同 timeframe OHLC K 线自行计算的 Wilder ADX、DI+、DI-，不得依赖 AI 或外部文本结果。
11. WHEN 计算 ADX/DI THEN 系统 SHALL 使用每根 K 线的 `high`、`low`、`close` 和上一根 K 线的 `high`、`low`、`close`，先计算 `TR`、`+DM`、`-DM`，再用 Wilder smoothing 计算 `DI+`、`DI-`、`DX` 和 `ADX`。
12. WHEN 某 timeframe 的 K 线数量少于 `period * 2` THEN 系统 SHALL 将该 timeframe 的 ADX/DI 标记为数据不足，并不得用 0 值伪装成有效趋势弱信号。
13. WHEN 配置未启用 `micro_adx_filter` THEN `3m` SHALL 默认只用于价格、ATR、MACD、RSI 和执行触发，不使用 ADX/DI。
14. WHEN 配置启用 `micro_adx_filter=true` THEN 系统 SHALL 先基于 3m K 线计算并写入 `IntradayData` 的 `ADXValues`、`DIPlus`、`DIMinus` 序列，再允许策略读取。
15. IF 配置启用 `micro_adx_filter=true` 但 `3m` ADX/DI 未实现或数据不足 THEN 配置校验 SHALL 失败，或运行时记录明确错误并跳过该指标。
16. WHEN 计算 ATR 止损或波动地板 THEN 系统 SHALL 默认以 `1h` ATR 为基础，允许用 `15m` ATR 做执行级收紧，用 `4h` ATR 做大波动保护地板；`3m` ATR 仅用于执行滑点或微观触发辅助。
17. WHEN 计算 RSI、价格突破或微观 MACD THEN `3m` 信号 SHALL 只作为入场/出场执行确认，不得单独生成主级别开仓、加仓、减仓或平仓决策，除非配置显式启用 micro-only 模式。
18. WHEN 策略信号形成 THEN 日志 SHALL 记录使用的 K 线 level、K 线区间、关键价格、MACD 面积、均线关系、ADX/DI、ATR、ZG/ZD 和信号类型。

### 5. 闭合 K 线与信号确认

**User Story:** 作为量化交易员，我希望主级别信号只在 K 线闭合后确认，以降低实盘信号闪烁和回测不一致。

#### Acceptance Criteria

1. WHEN 程序化策略确认 `4h`、`1h` 或 `15m` 结构信号 THEN 系统 SHALL 只使用已闭合 K 线。
2. WHEN 当前 K 线尚未闭合 THEN 系统 SHALL 不使用该 K 线确认主级别中枢、背驰、一类、二类或三类买卖点。
3. WHEN 使用 Binance K 线数据 THEN 系统 SHALL 能识别并过滤 `CloseTime` 晚于当前时间或正在形成的最后一根 K 线。
4. WHEN 使用 `3m` 级别 THEN `3m` MAY 用于执行触发、止损执行确认或连续两根闭合 K 线确认，但不得用未闭合 `3m` 单独确认主交易信号。
5. WHEN 信号因为 K 线未闭合而等待 THEN 日志 SHALL 记录等待的 timeframe、symbol 和预计确认条件。

### 6. 走势分解、中枢识别与机器定义

**User Story:** 作为量化交易员，我希望程序化策略能按可测试的缠论机器规则识别走势中枢、趋势和盘整，以便买卖点判断有结构依据。

#### Acceptance Criteria

1. WHEN 程序化策略分解走势 THEN 系统 SHALL 使用 swing-pivot 模型作为 v1 基础模型，并同时引入包含关系处理、分型、笔和线段增强。
2. WHEN 生成 pivot THEN 系统 SHALL 使用可配置的 `left_bars`、`right_bars`、`min_swing_pct` 和 `atr_multiplier` 过滤局部高低点。
3. WHEN 识别分型、笔和线段 THEN 系统 SHALL 先进行 K 线包含关系处理，再生成顶/底分型、有效笔和线段。
4. WHEN 生成有效笔 THEN 系统 SHALL 校验最小 K 线数量、顶底分型交替、价格方向和最小波动阈值。
5. WHEN 生成线段 THEN 系统 SHALL 由连续有效笔或经 ATR 过滤的 swing segment 构成，并记录组成笔、方向、起止价格和起止时间。
6. WHEN 至少三个连续次级别走势类型存在重叠 THEN 系统 SHALL 识别该重叠区间为走势中枢。
7. WHEN 识别 `1h` 主级别中枢 THEN 默认 SHALL 使用 `15m` segments 组成；WHEN 识别 `15m` 次级别中枢 THEN 默认 SHALL 使用 `3m` segments 组成；WHEN 识别 `4h` 大级别背景 THEN 默认 SHALL 使用 `1h` segments 组成。
8. WHEN 某级别走势只包含一个中枢 THEN 系统 SHALL 将该走势标记为盘整。
9. WHEN 某级别走势包含两个或以上依次同向中枢 THEN 系统 SHALL 将该走势标记为上涨趋势或下跌趋势。
10. WHEN 中枢识别完成 THEN 系统 SHALL 输出 ZG、ZD、中枢高低区间、组成段、所属级别和使用的结构算法版本。
11. IF 中枢不足以确认趋势或盘整 THEN 策略 SHALL 不产生依赖该中枢的二类或三类买卖点。
12. WHEN swing-pivot 结果与笔/线段增强结果冲突 THEN 策略 SHALL 按配置的结构严格度处理，并记录冲突详情；默认严格度 SHALL 优先使用笔/线段增强后的结构。

### 7. 背驰识别与均线“吻”

**User Story:** 作为量化交易员，我希望程序化策略能识别 MACD 背驰和均线“吻”，以便捕捉下跌转上涨或上涨转下跌的关键拐点。

#### Acceptance Criteria

1. WHEN 走势结构为 A + B + C，且 A/C 为同向走势、B 为中枢或盘整 THEN 系统 SHALL 比较 A 段和 C 段的 MACD 柱面积。
2. WHEN 计算上涨段 MACD 面积 THEN 系统 SHALL 只累加同段内正 histogram 面积。
3. WHEN 计算下跌段 MACD 面积 THEN 系统 SHALL 只累加同段内负 histogram 的绝对值面积。
4. WHEN 下跌 C 段 MACD 绿柱面积满足 `C_area <= A_area * divergence_ratio` 且 C 段价格创新低或接近新低 THEN 系统 SHALL 标记底背驰候选；默认 `divergence_ratio=0.8`。
5. WHEN 上涨 C 段 MACD 红柱面积满足 `C_area <= A_area * divergence_ratio` 且 C 段价格创新高或接近新高 THEN 系统 SHALL 标记顶背驰候选；默认 `divergence_ratio=0.8`。
6. WHEN 判断价格创新高/新低容差 THEN 系统 SHALL 使用配置值，默认取 `0.1%` 与 `0.2 * ATR / price` 中较大者。
7. WHEN B 段未将 MACD 黄白线拉回 0 轴附近且配置要求严格模式 THEN 系统 SHALL 不确认标准背驰。
8. WHEN 计算均线“吻”关系 THEN 系统 SHALL 使用配置的短/长 EMA 周期，默认 EMA20/EMA50。
9. WHEN 短 EMA 低于长 EMA THEN 系统 SHALL 标记为男上位；WHEN 短 EMA 高于长 EMA THEN 系统 SHALL 标记为女上位。
10. WHEN 短/长 EMA 距离缩小后未触达阈值又扩张 THEN 系统 SHALL 可标记飞吻。
11. WHEN 短/长 EMA 距离进入 `kiss_distance_pct` 但未交叉 THEN 系统 SHALL 可标记唇吻；默认阈值取 `0.15%` 与 `0.15 * ATR / price` 中较大者。
12. WHEN 短/长 EMA 在 N 根 K 线内发生一次以上交叉 THEN 系统 SHALL 可标记湿吻；默认 N=5。
13. WHEN 判断“最后一吻” THEN 该吻 SHALL 发生在背驰段之前或背驰段早期，不得在信号确认后倒填。
14. WHEN 背驰确认 THEN 系统 SHALL 记录 A/B/C 段边界、面积、价格极值、级别、均线吻类型和背驰强度。

### 8. 一类买卖点策略

**User Story:** 作为量化交易员，我希望策略能在“下跌+盘整+下跌”或“上涨+盘整+上涨”后识别第一类买卖点，用于试探开仓或清仓。

#### Acceptance Criteria

1. WHEN 某标的出现下跌 + 盘整 + 下跌结构，且第二段下跌出现底背驰 THEN 系统 SHALL 识别第一类买点。
2. WHEN 第一类买点被确认且允许做多且无同 symbol 反向持仓 THEN 策略 SHALL 可按配置输出 `open_long` 或 `add_long` 候选。
3. WHEN 某标的出现上涨 + 盘整 + 上涨结构，且第二段上涨出现顶背驰 THEN 系统 SHALL 识别第一类卖点。
4. WHEN 第一类卖点被确认且当前持有多仓 THEN 策略 SHALL 输出 `close_long` 或配置比例的 `partial_close`。
5. WHEN 第一类卖点被确认且允许做空且无同 symbol 反向持仓 THEN 策略 MAY 按配置输出 `open_short` 或 `add_short` 候选。
6. IF 第一类买点介入后同级别走势转入盘整且未形成继续上行条件 THEN 策略 SHALL 退出或减仓，并记录“买后一旦盘整退出”的原因。

### 9. 二类买卖点策略

**User Story:** 作为量化交易员，我希望策略优先使用第二类买点开仓，并使用第二类卖点减仓或反向处理，以便减少第一类拐点误判。

#### Acceptance Criteria

1. WHEN 第一类买点后出现首次上 0 轴并回抽，且回抽低点不跌破最近第一类买点低点 THEN 系统 SHALL 识别第二类买点。
2. WHEN 第二类买点确认且无同向持仓且无同 symbol 反向持仓 THEN 策略 SHALL 可作为主开仓信号输出 `open_long`。
3. WHEN 第二类买点确认且已有同向持仓 THEN 策略 SHALL 可在风险预算允许时输出 `add_long`。
4. WHEN 第二类买点后出现上涨背驰 THEN 策略 SHALL 先减仓或退出，等待回调不创新低再回补。
5. WHEN 回调跌破最近低点且未形成新的底背驰 THEN 策略 SHALL 继续等待，不得盲目补仓。
6. WHEN 第一类卖点后出现首次下 0 轴并反抽，且反抽高点不升破最近第一类卖点高点 THEN 系统 SHALL 识别第二类卖点，并对多仓输出减仓/平仓候选。
7. WHEN 第二类卖点确认且已有空仓 THEN 策略 SHALL 可在风险预算允许时输出 `add_short`。

### 10. 三类买卖点策略

**User Story:** 作为量化交易员，我希望策略能在中枢离开后回试不破时识别第三类买卖点，用于趋势延续开仓或加仓。

#### Acceptance Criteria

1. WHEN 次级别走势向上离开中枢后回试，且回试低点不跌破 ZG THEN 系统 SHALL 识别第三类买点。
2. WHEN 第三类买点确认且允许做多且无同 symbol 反向持仓 THEN 策略 SHALL 可输出 `open_long` 或 `add_long`。
3. WHEN 第三类买点后 `15m close < ZG` 或连续两根 `3m close < ZG` THEN 策略 SHALL 判定三买失败，并输出减仓或平仓动作。
4. WHEN 次级别走势向下离开中枢后回抽，且回抽高点不升破 ZD THEN 系统 SHALL 识别第三类卖点。
5. WHEN 第三类卖点确认且当前持有多仓 THEN 策略 SHALL 输出减仓或平仓动作。
6. WHEN 第三类卖点确认且允许做空且无同 symbol 反向持仓 THEN 策略 MAY 输出 `open_short` 或 `add_short`。
7. WHEN 第三类卖点后 `15m close > ZD` 或连续两根 `3m close > ZD` THEN 策略 SHALL 判定三卖失败，并输出空仓减仓或平仓动作。
8. WHEN 结构失败由止损触发 THEN MAY 使用盘中价格触发；WHEN 结构失败用于三买/三卖失效确认 THEN SHALL 使用闭合 K 线确认。

### 11. 买卖点级别和顺序约束

**User Story:** 作为量化交易员，我希望策略标注买卖点的级别和顺序，避免无结构依据的三买三卖误触发。

#### Acceptance Criteria

1. WHEN 输出任何买卖点信号 THEN 系统 SHALL 标注方向、买卖点类型、一/二/三级别、分析 timeframe 和触发 timeframe。
2. WHEN 某标的尚未出现第一类买点 THEN 系统 SHALL 不确认第二类或第三类买点，除非配置显式允许宽松模式。
3. WHEN 某标的尚未出现第一类卖点 THEN 系统 SHALL 不确认第二类或第三类卖点，除非配置显式允许宽松模式。
4. WHEN 多个级别同时出现同方向买卖点 THEN 策略 SHALL 标记为级别共振，并允许提高信号优先级或仓位上限，但仍不得突破全局风险预算。
5. WHEN 不同级别出现冲突信号 THEN 策略 SHALL 按配置的 higher timeframe 优先级处理，并记录冲突原因。
6. WHEN 前置一类买卖点存在于持久化状态但当前 K 线窗口无法重算 THEN 策略 SHALL 按严格度配置决定是否可用于二/三类买卖点，并在日志标记 `state_restored=true`。

### 12. 开仓策略

**User Story:** 作为量化交易员，我希望程序化策略能按缠论买卖点自动开仓，并自动给出止损、止盈、杠杆和仓位。

#### Acceptance Criteria

1. WHEN 程序化策略输出 `open_long` 或 `open_short` THEN 决策 SHALL 包含 symbol、action、leverage、position_size_usd、stop_loss、take_profit、reasoning。
2. WHEN 做多开仓基于第一类买点 THEN 止损 SHALL 参考背驰低点或配置的失效低点。
3. WHEN 做多开仓基于第二类买点 THEN 止损 SHALL 参考不破新低的确认低点。
4. WHEN 做多开仓基于第三类买点 THEN 止损 SHALL 参考 ZG 或回试低点。
5. WHEN 做空开仓 THEN 止损 SHALL 按上述规则镜像参考高点、ZD 或反抽高点。
6. WHEN 生成止盈目标 THEN 策略 SHALL 优先使用结构目标，而不是纯固定百分比目标。
7. WHEN 多头开仓使用结构目标 THEN take_profit SHALL 参考前一中枢上沿、最近 swing high、离开段高点或下一结构压力位。
8. WHEN 空头开仓使用结构目标 THEN take_profit SHALL 参考前一中枢下沿、最近 swing low、离开段低点或下一结构支撑位。
9. IF 结构目标无法满足配置的最小净风险收益比 THEN 策略 SHALL 默认拒绝开仓并记录原因；只有配置显式允许 `tp_fallback_mode=rr_target` 时，才 MAY 使用 RR 目标作为兜底 take_profit。
10. WHEN 计算仓位 THEN 系统 SHALL 使用账户净值、可用余额、止损距离、手续费滑点、最小下单额、单笔风险和剩余总风险预算进行 sizing。
11. IF 计算出的开仓名义额低于交易所最小名义额 THEN 策略 SHALL 不输出可执行开仓，并记录拒绝原因。

### 13. 加仓策略

**User Story:** 作为量化交易员，我希望已有盈利或结构确认的持仓可以按二类/三类买卖点加仓，而不是重复无序开仓。

#### Acceptance Criteria

1. WHEN 已有多仓且出现同方向第二类买点或第三类买点 THEN 策略 SHALL 可输出受控加仓动作 `add_long`。
2. WHEN 已有空仓且出现同方向第二类卖点或第三类卖点 THEN 策略 SHALL 可输出受控加仓动作 `add_short`。
3. WHEN 输出加仓动作 THEN 决策 action SHALL 使用 `add_long` 或 `add_short`，不得使用重复 `open_long` 或 `open_short` 表示加仓。
4. WHEN 执行 `add_long` 或 `add_short` THEN 系统 MAY 复用交易所 `OpenLong` 或 `OpenShort` 下单能力，但 SHALL 使用加仓专用风控路径。
5. WHEN 执行 `add_long` 或 `add_short` THEN 系统 SHALL 跳过普通开仓的“同 symbol 同方向已有持仓拒绝”检查，并改为检查最大加仓次数、最大同向仓位、剩余风险预算、保证金和最小名义额。
6. IF 加仓后总风险超过配置上限 THEN 策略 SHALL 拒绝加仓或自动缩小加仓名义额，并记录 sizing reason。
7. WHEN 加仓成交成功 THEN 系统 SHALL 更新交易计划中的实际仓位、加权平均入场价或等价持仓成本、累计加仓次数、最新结构止损和总风险。
8. WHEN 排序多个决策 THEN `add_long` / `add_short` 优先级 SHALL 低于平仓、减仓、止损/止盈更新，高于或等同于普通新开仓，且不得早于风险降低型动作。

### 14. 减仓策略

**User Story:** 作为量化交易员，我希望策略在次级别背驰或短差信号出现时可以先减仓，降低回撤并保留主趋势仓位。

#### Acceptance Criteria

1. WHEN 大级别买点开仓后，次级别出现第一类卖点 THEN 策略 SHALL 按配置比例输出 `partial_close`。
2. WHEN 大级别卖点开空后，次级别出现第一类买点 THEN 策略 SHALL 按配置比例输出空仓减仓。
3. WHEN 减仓后次级别再次出现同方向第一类买点或卖点 THEN 策略 SHALL 可按短差程序回补，但必须输出 `add_long` 或 `add_short` 并经过加仓风控。
4. WHEN 减仓名义额或剩余仓位名义额低于交易所最小可执行值 THEN 策略 SHALL 跳过减仓并记录原因。
5. WHEN 减仓后需要同步止损 THEN 策略 SHALL 输出 `update_stop_loss` 或更新计划，使剩余仓位风险不扩大。
6. WHEN 减仓成功 THEN 系统 SHALL 在程序化策略状态中记录短差状态、减仓比例、减仓依据和可回补条件。

### 15. 平仓、多空与反手规则

**User Story:** 作为量化交易员，我希望策略在反向买卖点、结构失效或风险条件触发时自动平仓，并避免同一 symbol 双向持仓。

#### Acceptance Criteria

1. WHEN 持有多仓且同级别第一类卖点确认 THEN 策略 SHALL 输出 `close_long`，除非配置为先减仓。
2. WHEN 持有多仓且第三类买点失败并跌回中枢 THEN 策略 SHALL 输出 `close_long` 或配置比例的 `partial_close`。
3. WHEN 持有多仓且价格跌破该买点的失效低点 THEN 策略 SHALL 输出 `close_long`。
4. WHEN 持有空仓且同级别第一类买点确认 THEN 策略 SHALL 输出 `close_short`，除非配置为先减仓。
5. WHEN 持有空仓且第三类卖点失败并升回中枢 THEN 策略 SHALL 输出 `close_short` 或配置比例的 `partial_close`。
6. WHEN 现有交易计划的止损/止盈保护单已由交易所自动触发 THEN 程序化策略 SHALL 不重复平仓，并复用现有自动平仓去重逻辑。
7. WHEN 同一 symbol 已有多仓 THEN 程序化策略 SHALL 不输出 `open_short` 或 `add_short`，除非该周期只输出风险降低型平多动作。
8. WHEN 同一 symbol 已有空仓 THEN 程序化策略 SHALL 不输出 `open_long` 或 `add_long`，除非该周期只输出风险降低型平空动作。
9. WHEN 出现反向信号 THEN 默认 SHALL 只触发平仓或减仓，不得在同一周期反手开仓。
10. IF 配置显式启用 `allow_reversal=true` THEN 反手开仓 SHALL 发生在平仓成交确认后的后续周期，或在设计中明确成交确认后再开仓的顺序约束。

### 16. 公共风控、执行链路与优先级

**User Story:** 作为系统维护者，我希望程序化策略输出的动作仍经过现有风控和执行保护，避免新增策略绕过安全边界。

#### Acceptance Criteria

1. WHEN 程序化策略产生开仓或加仓决策 THEN 系统 SHALL 继续执行现有 open gate、仓位 sizing、杠杆限制、相关性限制、亏损模式、最小名义额和 preflight 检查。
2. WHEN 程序化策略产生平仓或减仓决策 THEN 系统 SHALL 保持“先平仓/减仓，后加仓/开仓”的执行优先级。
3. WHEN 公共风控与程序化策略信号冲突 THEN 公共风控 SHALL 拥有更高优先级。
4. WHEN 程序化策略需要更新止损或止盈 THEN 系统 SHALL 分别调用 `CancelStopLossOrders()` 或 `CancelTakeProfitOrders()`，不得使用废弃的混合取消逻辑。
5. IF 保护单设置失败 THEN 系统 SHALL 按现有执行保护规则记录高风险状态，并遵守是否紧急平仓的配置。
6. WHEN 全局熔断或账户回撤硬停触发 THEN 程序化策略 SHALL 停止新开仓和加仓，但仍允许风险降低型平仓或减仓。
7. WHEN 公共交易计划已经产生硬止损、失效平仓或保护单修复决策 THEN 程序化策略 SHALL 不输出相同 symbol 的冲突开仓或加仓决策。
8. WHEN 合并策略决策与公共层决策 THEN 优先级 SHALL 为：自动平仓/熔断/账户硬停 > 交易计划硬止损/失效 > 程序化平仓/减仓 > 止损/止盈更新 > 程序化加仓 > 程序化开仓 > wait/hold。

### 17. 状态持久化与频率控制

**User Story:** 作为量化交易员，我希望程序化策略在重启后仍能识别前置买卖点和短差状态，并避免每个扫描周期重复加仓或重复开仓。

#### Acceptance Criteria

1. WHEN 程序化策略运行 THEN 系统 SHALL 每周期从历史 K 线重算最近结构，并将已确认信号、交易计划关联状态、加仓次数和短差减仓状态持久化。
2. WHEN 写入程序化策略状态 THEN 状态 SHALL 按 trader 和 symbol 隔离，默认存储在 `data/programmatic_strategy_state.json` 或等价 JSON 状态文件。
3. WHEN 二类或三类买卖点依赖最近一类买卖点、中枢或回抽状态 THEN 系统 SHALL 优先使用可重算状态；如需使用持久化状态，日志 SHALL 标记来源。
4. WHEN 严格模式下既无法重算也没有持久化前置状态 THEN 策略 SHALL 不确认二类或三类买卖点。
5. WHEN 启用 bootstrap 模式 THEN 系统 MAY 从历史窗口内最近完整中枢初始化状态，但日志 SHALL 标记 `bootstrap=true`。
6. WHEN 程序化策略每个 `scan_interval` 运行 THEN 风险降低型动作 SHALL 每个周期都可评估。
7. WHEN 评估新开仓或加仓信号 THEN 默认 SHALL 只在 `trade_level` 新 K 线闭合后重新确认。
8. WHEN 同一 signal_id 已被执行或拒绝且仍未失效 THEN 系统 SHALL 防止重复开仓、重复加仓或重复短差回补。
9. WHEN 程序化策略使用频率控制 THEN SHALL 复用每日开仓次数、亏损模式、总风险预算等确定性风控，但不使用 AI backoff 作为程序化信号节流依据。

### 18. 决策日志与可观测性

**User Story:** 作为量化交易员，我希望程序化策略的每个信号都有可追溯诊断，方便复盘为什么开仓、加仓、减仓或平仓。

#### Acceptance Criteria

1. WHEN `decision_mode=programmatic` THEN 决策日志 SHALL 标记策略模式、策略名称和策略版本。
2. WHEN `decision_mode=programmatic` THEN 每条策略决策记录 SHALL 写入 `strategy_name`、`strategy_version`、`config_hash` 和关键参数摘要。
3. WHEN 输出买卖点信号 THEN 决策日志 SHALL 包含买卖点类型、级别、走势结构、背驰指标、中枢边界、止损依据、止盈结构目标和仓位依据。
4. WHEN 某候选标的被跳过 THEN 日志 SHALL 记录跳过原因，例如数据不足、无中枢、无背驰、级别冲突、风险预算不足、K 线未闭合或已执行过相同 signal_id。
5. WHEN 前端展示最新决策 THEN 程序化策略决策 SHALL 能与 AI 决策一样显示 action、symbol、reasoning、success/error。
6. WHEN 程序化策略使用持久化状态、bootstrap 状态或恢复状态 THEN 日志 SHALL 标记状态来源、状态版本和相关 signal_id。

### 19. 前端策略检查界面与 API

**User Story:** 作为量化交易员，我希望在前端查看自定义标的池、K 线和程序化策略信号，以便人工确认策略状态。

#### Acceptance Criteria

1. WHEN 进入 trader 详情页或策略检查区 THEN 前端 SHALL 能展示当前 trader 的决策模式。
2. WHEN 当前 trader 使用程序化策略 THEN 前端 SHALL 展示自定义标的池或候选池列表。
3. WHEN 用户选择某个标的 THEN 前端 SHALL 在页面下方或主要详情区域展示该标的 K 线。
4. WHEN 选中标的存在程序化信号 THEN 前端 SHALL 展示最近一次信号类型、级别、方向、关键价格、结构目标和触发原因。
5. IF K 线或信号数据加载失败 THEN 前端 SHALL 显示错误状态，不得影响后端交易循环。
6. WHEN 后端提供策略标的列表 THEN SHALL 新增或扩展只读 API `GET /api/strategy/symbols?trader_id=xxx`。
7. WHEN 后端提供策略信号诊断 THEN SHALL 新增或扩展只读 API `GET /api/strategy/signals?trader_id=xxx&symbol=BTCUSDT`。
8. WHEN 后端提供 K 线展示数据 THEN SHALL 新增或扩展只读 API `GET /api/market/klines?symbol=BTCUSDT&timeframe=1h`。
9. WHEN 新增 API 字段 THEN 后端 JSON tag、`web/src/lib/api.ts` 和 `web/src/types/index.ts` SHALL 同步更新。
10. IF 本期前端暂不实现完整 K 线图 THEN 页面 SHALL 至少展示最新 strategy diagnostics、关键价位和信号摘要。

### 20. 测试和验证

**User Story:** 作为维护者，我希望程序化策略可单元测试、可诊断、可配置校验，确保后续调整不会破坏交易安全。

#### Acceptance Criteria

1. WHEN 新增程序化策略核心识别逻辑 THEN SHALL 提供不依赖真实交易所的单元测试。
2. WHEN 测试走势结构 THEN SHALL 覆盖包含关系处理、分型、笔、线段、swing-pivot、中枢识别、趋势和盘整识别。
3. WHEN 测试买卖点识别 THEN SHALL 覆盖第一类、第二类、第三类买点和卖点的正例与反例。
4. WHEN 测试 MACD 背驰 THEN SHALL 覆盖面积阈值、价格容差、B 段回 0 轴严格模式和非背驰反例。
5. WHEN 测试均线“吻” THEN SHALL 覆盖飞吻、唇吻、湿吻、最后一吻和配置短/长周期。
6. WHEN 测试 ADX/DI THEN SHALL 覆盖 `15m`、`1h`、`4h` 自计算数据，以及 `3m` micro ADX 启用和数据不足路径。
7. WHEN 测试闭合 K 线 THEN SHALL 覆盖未闭合主级别 K 线不确认信号、`3m` 执行触发和连续闭合确认。
8. WHEN 测试加仓/减仓 THEN SHALL 覆盖 `add_long`、`add_short`、风险预算不足、最小名义额不足、最大加仓次数限制和短差回补。
9. WHEN 测试多空与反手 THEN SHALL 覆盖同一 symbol 禁止双向持仓、反向信号只平仓、`allow_reversal` 后续周期约束。
10. WHEN 测试状态持久化 THEN SHALL 覆盖重启恢复、signal_id 去重、bootstrap 标记和 trader/symbol scope。
11. WHEN 测试配置 THEN SHALL 覆盖 `ai` 默认兼容、`programmatic` 合法配置和非法配置失败。
12. WHEN 测试执行集成 THEN SHALL 使用 fake trader 或 mock，不得触发真实下单。
13. WHEN 前端新增策略展示 THEN SHALL 至少通过 TypeScript build，并为关键工具函数补充测试。
