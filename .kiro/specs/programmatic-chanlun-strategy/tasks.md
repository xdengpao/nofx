# 程序化缠论交易策略 Tasks

## Phase 1: 配置与运行时策略

- [x] 在 `config.TraderConfig` 中新增 `decision_mode` 和 `programmatic_strategy` 配置结构，保持缺省 `ai` 兼容。
- [x] 实现 `config.NormalizeProgrammaticStrategies()`，输出按 `trader_id` 索引的运行时 profile，并补默认值。
- [x] 更新 `main.go/setupTraderManager`，在创建 trader 前归一化 programmatic profiles，并按 `trader_id` 传入 manager。
- [x] 调整配置校验：`decision_mode=programmatic` 时 `ai_model` 和 AI key 可为空或仅作为展示字段，不阻塞启动。
- [x] 增加配置校验：timeframe、history depth、symbol pool mode、均线周期、ADX/micro ADX、TP fallback、加仓参数。
- [x] 更新 `config.json.example`，加入带注释的 programmatic trader 示例，不写真实凭证。
- [x] 将 programmatic runtime profile 转换为 `decision.ProgrammaticStrategyPolicy`，并通过 `manager.AddTraderWithPolicies()` 注入 `trader.AutoTraderConfig`。
- [x] 更新 trader 状态和 `/api/traders`，返回 `decision_mode`。
- [x] 验证：`go test ./config ./manager`。

## Phase 2: 行情历史深度与指标契约

- [x] 在 `market` 包新增 `HistoryDepth`、`HistoryOptions`、`GetWithHistory()` 和 `GetKlines()` 只读接口。
- [x] 实现 K 线闭合过滤，确保 `4h/1h/15m` 结构确认只使用已闭合 K 线。
- [x] 让程序化模式能按 `history_depth` 拉取 `3m/15m/1h/4h`，并保留完整 OHLC 与指标序列。
- [x] 在 `market.Data` 中增加内部 `Klines map[string][]Kline` 或等价结构，避免只依赖最近 10 个摘要点。
- [x] 为 `IntradayData` 增加可选 `ADXValues`、`DIPlus`、`DIMinus`，仅在 `micro_adx_filter=true` 时计算。
- [x] 复用现有 Wilder ADX/DI 计算，数据不足时返回不可用诊断，不使用 0 伪装弱趋势。
- [x] 验证：`go test ./market`。

## Phase 3: 公共决策前置层与策略路由

- [x] 从 `decision.GetFullDecision()` 抽取 `PrepareCycleContext()`，保留初始化、行情、熔断、相关性、候选质量、持仓评估和账户硬停逻辑。
- [x] 让 `PrepareCycleContext()` 支持显式 `MarketSymbols`，用于 programmatic 自定义标的池先解析后拉取行情；当前持仓和 `BTCUSDT` 始终强制拉取。
- [x] 保持 AI 模式行为不变，`decision.GetFullDecision()` 继续走现有 AI 调用、解析和风控路径。
- [x] 在 `trader.AutoTrader` 中增加 mode-aware 路由：`ai` 调用 AI 决策，`programmatic` 调用 programmatic engine。
- [x] 保留 `syncAutoClosedOrders()`、`detectAutoClosedPositions()`、`reconcileStaleTradePlans()` 在 mode routing 之前执行，作为 AI/programmatic 公共执行前同步层。
- [x] 程序化 trader 不初始化或不使用 AI provider 进行策略决策，启动日志和周期日志显示“程序化策略周期”。
- [x] 增加 `decision_mode=programmatic` 时 AI key 非必需的验证路径。
- [x] 验证：`go test ./decision ./trader ./manager`。

## Phase 4: 公共决策校验与合并

- [x] 在 `decision/utils.go` 或相近位置增加 action helper：`IsOpenAction`、`IsAddAction`、`IsOpenLikeAction`、`DecisionDirection`。
- [x] 扩展合法 action 列表，支持 `add_long`、`add_short`。
- [x] 实现 `ValidateStrategyDecisions()`，复用 open gate、风险规范化、仓位 sizing、最小名义额、相关性、亏损模式和最终开仓限制。
- [x] 为 `add_long/add_short` 增加 open-like 风控，但跳过普通开仓“同向已有仓位拒绝”，改走加仓限制。
- [x] 实现 `MergePublicAndStrategyDecisions()`，确保公共风控和风险降低型动作优先。
- [x] 更新 `sortDecisionsByPriority()`，顺序为平/减仓、止损止盈更新、加仓、开仓、wait/hold。
- [x] 验证：`go test ./decision ./trader`。

## Phase 5: 缠论策略核心包

- [x] 新增 `strategy/chanlun` 包和核心类型：Candle、Fractal、Stroke、Segment、Center、ChanlunSignal、SignalDiagnostics。
- [x] 实现包含关系处理 `NormalizeInclusion()`，覆盖向上/向下处理和边界样例。
- [x] 实现分型识别 `FindFractals()`，支持 `left_bars/right_bars`。
- [x] 实现笔识别 `BuildStrokes()`，校验顶底交替、最小 K 线数量、最小波动和 ATR 阈值。
- [x] 实现线段和 swing-pivot 识别 `BuildSegments()`，支持 `enhanced/pivot/confirm_both` strictness。
- [x] 实现中枢识别 `BuildCenters()`，输出 ZG、ZD、区间、组成段和级别。
- [x] 实现多级别映射：`1h<-15m`、`15m<-3m`、`4h<-1h`。
- [x] 验证：`go test ./strategy/chanlun`。

## Phase 6: 背驰、均线吻与买卖点信号

- [x] 实现 MACD 面积背驰计算，上涨段累加正柱，下跌段累加负柱绝对值。
- [x] 实现背驰阈值、价格创新高/低容差、B 段回 0 轴严格模式。
- [x] 实现 EMA 男上位/女上位、飞吻、唇吻、湿吻和最后一吻识别，周期来自配置。
- [x] 实现一类买卖点识别。
- [x] 实现二类买卖点识别，并依赖可重算或持久化的一类信号。
- [x] 实现三类买卖点识别和三买/三卖失败判定。
- [x] 实现短差减仓与回补信号。
- [x] 为所有信号生成稳定 `signal_id`、结构止损、结构止盈目标和诊断。
- [x] 验证：`go test ./strategy/chanlun`。

## Phase 7: 程序化策略状态持久化

- [x] 实现 `strategy/chanlun.StateStore`，按 `trader_id + symbol` 存储状态。
- [x] 默认使用 `data/programmatic_strategy_state.json`，使用 mutex 和临时文件 rename 原子写入。
- [x] 持久化 confirmed signals、executed signals、加仓次数、短差状态、最近结构 hash。
- [x] 持久化每个 symbol/timeframe 最近已分析闭合 K 线 `last_analyzed_closed_kline`，用于“仅在 trade level 新闭合 K 线后确认新开仓/加仓”。
- [x] 实现状态读取失败时的降级：记录中文错误，使用空状态，不 panic。
- [x] 实现 bootstrap 模式和 `state_restored/bootstrap` 诊断标记。
- [x] 实现 `signal_id` 去重，防止重复开仓、重复加仓和重复短差回补。
- [x] 验证：`go test ./strategy/chanlun`。

## Phase 8: 策略引擎与决策生成

- [x] 实现 `strategy/chanlun.Engine` 和 `GetFullDecision(ctx)`。
- [x] 按 trader 级 symbol pool 配置解析最终分析池：append、override、filter，并永远保留当前持仓 symbol。
- [x] 为每个候选 symbol 构建多级别结构、信号和诊断，跳过数据不足或 K 线未闭合的 symbol。
- [x] 将信号映射为 `open_long/open_short/add_long/add_short/partial_close/close_long/close_short/update_stop_loss/wait`。
- [x] 生成结构止损、结构止盈目标、reasoning、strategy metadata 和 diagnostics。
- [x] 调用公共 `ValidateStrategyDecisions()` 和 `MergePublicAndStrategyDecisions()` 输出最终 `decision.FullDecision`。
- [x] 确保 `AICallAttempted=false`，`UserPrompt` 为空，`CoTTrace` 写程序化策略摘要。
- [x] 验证：`go test ./strategy/chanlun ./decision ./trader`。

## Phase 9: 加仓执行链路

- [x] 在 `trader.executeDecisionWithRecord()` 中支持 `add_long`、`add_short`。
- [x] 抽取 open-like 执行 helper，复用开仓和加仓的价格、数量、preflight、保护单设置逻辑。
- [x] 扩展 `ExecutionPreflightInput`，增加 `Intent` 或 `AllowSameSidePosition`，使加仓跳过同向持仓拒绝但继续禁止反向持仓和小额订单。
- [x] 为加仓 intent 校验已有同向持仓、禁止反向持仓、最大加仓次数、风险预算和最小名义额。
- [x] 加仓成交后调用 `decision.OnPositionAddedScoped()` 更新交易计划。
- [x] 扩展 `TradePlan` 字段：strategy metadata、signal id、structure target、add count、average entry、last add time。
- [x] 确保加仓后保护单同步不扩大剩余仓位风险，并保留止损/止盈取消接口分离语义。
- [x] 验证：`go test ./trader ./decision`。

## Phase 10: 日志与可观测性

- [x] 扩展 `decision.Decision` 和 `logger.DecisionAction`，记录 strategy mode/name/version、config hash、signal id、signal type、timeframe、structure target 和 diagnostics。
- [x] 扩展 `logger.DecisionRecord`，记录 decision mode、strategy params、strategy diagnostics。
- [x] 程序化模式日志兼容现有前端：action、symbol、reasoning、success/error 均正常展示。
- [x] 更新 logger/replay open-like helper，识别 `add_long/add_short`，确保加仓日志不会被误判为未知动作或破坏旧日志统计读取。
- [x] 旧日志读取保持兼容，缺少策略字段不报错。
- [x] 仅做 replay 兼容性检查，不扩展行情级 replay/backtest。
- [x] 验证：`go test ./logger ./trader`。

## Phase 11: API 与前端策略检查区

- [x] 后端新增 `GET /api/strategy/symbols?trader_id=xxx`。
- [x] 后端新增 `GET /api/strategy/signals?trader_id=xxx&symbol=BTCUSDT`。
- [x] 后端新增 `GET /api/market/klines?symbol=BTCUSDT&timeframe=1h&limit=240`。
- [x] 为 K 线 API 增加专用 DTO，输出 `open_time/close_time/open/high/low/close/volume`，不直接暴露 Go 结构字段名。
- [x] 在 `manager` 和 `trader.AutoTrader` 中暴露只读策略 symbol、latest signals 和 market klines 方法。
- [x] 更新 `web/src/lib/api.ts` 和 `web/src/types/index.ts`。
- [x] 在 trader 详情页新增策略检查区，展示 decision mode、symbol 下拉、K 线摘要和最新信号诊断。
- [x] 若完整 K 线图暂不做，至少展示最近 K 线列表、关键价位、结构目标和信号摘要。
- [x] 验证：`go test ./api ./manager`，`cd web && npm run build`。

## Phase 12: 集成回归与上线保护

- [x] 增加 programmatic trader 的端到端 fake trader 测试，确保不触发真实下单。
- [x] 覆盖 AI 模式缺省兼容，确认未配置 `decision_mode` 的行为不变。
- [x] 覆盖程序化模式不调用 AI API，AI key 缺失不影响策略运行。
- [x] 覆盖程序化模式 `ai_model` 缺失不影响启动，且状态/API 通过 `decision_mode` 展示策略类型。
- [x] 覆盖自定义标的池 `append/override/filter` 中不在全局候选池的 symbol 能被拉取行情并分析。
- [x] 覆盖熔断/账户硬停时只允许风险降低型动作。
- [x] 覆盖同一 symbol 禁止双向持仓和反向信号不在同周期反手。
- [x] 覆盖最小名义额不足、结构 TP 不满足 RR、ADX/DI 数据不足、K 线未闭合等拒绝路径。
- [x] 运行后端窄测试：`go test ./config ./market ./decision ./strategy/chanlun ./trader ./api ./manager`。
- [x] 运行前端构建：`cd web && npm run build`。
- [x] 进行 spec/code 自检：requirements、design、tasks 与实现字段/API/action 名称保持一致。
