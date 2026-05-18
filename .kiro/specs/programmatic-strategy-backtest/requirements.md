# 程序化策略回测 Requirements

## 背景

当前 NOFX 已支持 `decision_mode=programmatic`，程序化缠论策略由 `strategy/chanlun.Engine` 生成开仓、加仓、减仓、平仓和等待决策，并继续经过 `decision` 公共风控、仓位 sizing、交易计划、保护单与执行前检查。现有 `cmd/replay` 主要用于复盘已经产生的 `decision_logs`，不能从历史行情开始重新驱动策略，也不能评估不同 `trade` 主交易级别、参数组合、手续费滑点和信号胜率。

为了验证程序化策略交易有效性，需要新增一个仅在开发测试环境执行的历史行情回测能力。回测应复用当前程序化策略实现和公共风控逻辑，但必须隔离真实交易所、AI、生产 `config.json`、生产状态文件和远端部署。

## 目标

- 用历史 K 线数据驱动当前程序化策略，评估策略在指定时间区间、标的池和参数下的表现。
- 增加本地开发测试环境的历史行情数据库，用于沉淀回测所需 K 线数据。
- 支持使用当前实时行情获取链路的数据源补齐历史数据，并按数据源 API 限频要求控制抓取频率。
- 明确区分历史数据抓取区间、正式回测统计区间和指标预热区间，避免隐性未来函数。
- 支持验证 `15m`、`1h`、`4h` 主交易级别和对应多时间框架组合。
- 复用程序化策略信号、状态、开平仓原因、买卖点标识、partial close 防重复、公共风控和仓位 sizing 的核心逻辑。
- 使用纸面撮合器模拟成交、手续费、滑点、止损、止盈、保本止损、浮盈回撤、结构破坏和短差减仓。
- 输出可审计报告，包括收益曲线、交易明细、信号明细、拒绝原因、买卖点表现和参数快照。
- 提供本地开发测试环境的独立回测操作页面，便于配置历史数据、启动回测、查看进度、分析报告和复盘 K 线信号。
- 保证回测不触发真实下单，不调用 AI，不部署到 161 生产环境。

## 非目标

- 本规格不要求把回测功能部署到远端生产服务器。
- 本规格不要求接入真实交易所下单或读取私有账户成交。
- 本规格不要求把历史行情数据库、历史数据获取任务或相关 API 部署到云端生产环境。
- 本规格不要求把回测操作页面部署到云端生产、开放公网访问或接入真实交易执行能力。
- 本规格不要求覆盖 AI 决策模式的历史回测。
- 本规格不保证策略盈利，只保证回测过程可复现、无未来函数、指标完整、报告可审计。
- 本规格不替代现有 `cmd/replay`；`replay` 继续用于已生成决策日志复盘和对账。

## 术语

- **行情级回测**：从历史 K 线和配置出发，按时间推进重新生成策略决策和模拟成交。
- **日志 replay**：读取已有 `decision_logs` 后做统计分析，不重新生成历史决策。
- **纸面撮合器 / paper broker**：不连接交易所，只根据历史 K 线模拟订单成交、持仓、权益、保证金和费用的模块。
- **回测时钟**：按配置的扫描周期或事件时间推进的虚拟时间，策略只能看到该时刻已经闭合的数据。
- **无未来函数**：策略、风控和撮合不得读取当前回测时刻之后的 K 线、指标、成交价或结果。
- **预热期 / warmup**：回测开始后用于累积 EMA、MACD、ADX、ATR、缠论结构和状态的样本区间，不计入最终绩效指标。
- **参数快照**：回测运行时使用的 config、strategy version、config hash、git commit、费用滑点和数据源元数据。
- **历史行情数据库**：仅在开发测试环境使用的本地数据库，用于保存回测所需 OHLCV K 线、数据源元数据和数据质量状态。
- **历史数据获取任务**：从实时行情链路可用的数据源按 symbol、timeframe、时间区间增量抓取历史 K 线，并写入历史行情数据库的本地任务。
- **data_from / data_to**：历史数据获取任务的抓取区间，用于补齐历史行情数据库。
- **backtest_from / backtest_to**：正式回测统计区间，默认采用半开区间 `[backtest_from, backtest_to)`。
- **warmup_from**：为指标和缠论结构预热自动向前扩展的历史数据读取起点。
- **position lifecycle**：同一 symbol、side 从开仓到完全退出的一段持仓生命周期，是主交易统计单位。
- **execution event**：生命周期内的每次开仓、加仓、减仓、平仓、止损移动或保护单触发事件。
- **MFE / MAE**：持仓生命周期内最大有利变动和最大不利变动，默认基于初始风险距离换算为 R。
- **独立回测操作页面**：仅在本地开发测试环境使用的 Web 操作界面，用于历史数据管理、回测运行、结果分析和信号复盘，与实盘交易监控和交易执行页面隔离。

## Requirements

### 1. 开发测试环境边界

**User Story:** 作为系统维护者，我希望回测只在本地开发测试环境执行，以免误触生产服务或真实账户。

#### Acceptance Criteria

1. WHEN 运行回测命令 THEN 系统 SHALL 不启动真实 `AutoTrader` 实盘循环，不调用任何 `trader.Trader` 真实下单方法。
2. WHEN 回测执行 THEN 系统 SHALL 不调用 AI API，不读取或使用 AI provider API key。
3. WHEN 回测执行 THEN 系统 SHALL 不修改生产 `config.json`、`data/`、`decision_logs/`、`coin_pool_cache/` 中的运行时文件。
4. WHEN 回测需要状态持久化 THEN 系统 SHALL 使用回测运行目录下的临时状态文件，例如 `backtest_runs/<run_id>/state/programmatic_strategy_state.json`。
5. WHEN 生成回测报告和中间产物 THEN 默认 SHALL 写入本地 git ignored 目录，例如 `backtest_runs/`。
6. WHEN 回测命令启动 THEN 系统 SHALL 在日志中明确标记 `BACKTEST`、`dry_run=true`、`live_trading=false`。
7. IF 配置或命令参数尝试启用真实交易所下单 THEN 回测 SHALL 直接失败并输出中文错误。
8. WHEN 历史数据库、回测报告或大体量中间产物落盘 THEN 默认 SHALL 使用 git ignored 路径，例如 `backtest_data/`、`backtest_runs/`、`*.sqlite`、`*.sqlite-shm`、`*.sqlite-wal`。

### 2. 历史行情数据输入

**User Story:** 作为量化交易员，我希望回测使用可审计的历史 K 线数据，以便验证策略在真实历史价格序列上的表现。

#### Acceptance Criteria

1. WHEN 回测运行 THEN 系统 SHALL 从本地历史数据源读取 `3m`、`15m`、`1h`、`4h` K 线，字段至少包含 `open_time`、`open`、`high`、`low`、`close`、`volume`、`close_time`。
2. WHEN 数据源包含多个 symbol THEN 系统 SHALL 按 symbol 和 timeframe 分区加载，并对每个分区按 `open_time` 升序校验。
3. WHEN K 线存在重复、倒序、缺失关键价格、`high < low`、价格小于等于 0 或 close_time 非法 THEN 系统 SHALL 标记数据质量问题；严重问题 SHALL 使该 symbol/timeframe 不参与回测。
4. WHEN 某 symbol 缺少策略所需 timeframe THEN 系统 SHALL 跳过该 symbol 并在报告中列出缺失 timeframe。
5. WHEN 数据源包含未闭合或未来 K 线 THEN 回测 SHALL 按虚拟时钟过滤，只允许策略看到 `close_time <= current_backtest_time` 的 K 线。
6. WHEN 回测使用外部下载数据 THEN 下载 SHALL 是独立准备步骤，策略回测阶段 SHALL 只消费本地落盘数据。
7. WHEN 输出回测报告 THEN 报告 SHALL 包含数据源路径、symbol 数量、timeframe 覆盖、起止时间、缺失数据统计和数据 hash。
8. WHEN 回测构建 `market.Data` THEN 系统 SHALL 使用历史行情数据提供者或等价接口，不得在策略周期内调用实时 `market.Get()`、`market.GetWithHistory()` 或外部网络 K 线接口。
9. WHEN 历史库未提供 OI 或 funding 历史数据 THEN 回测 SHALL 默认将 OI/funding 标记为 `unknown` 或禁用，并在报告中记录 `oi_mode=disabled`、`funding_mode=disabled`。
10. WHEN 后续启用历史 OI 或 funding THEN 这些数据 SHALL 存入历史行情数据库，并按 `current_backtest_time` 过滤，不得读取当前实时值。

### 3. 历史行情数据库与数据获取

**User Story:** 作为量化交易员，我需要给本项目增加历史数据存储数据库用于程序化策略回测，以便回测可以稳定复用已沉淀的数据，而不是每次临时拉取网络数据。

#### Acceptance Criteria

1. WHEN 开发测试环境需要回测历史行情 THEN 系统 SHALL 提供本地历史行情数据库，用于保存至少 `3m`、`15m`、`1h`、`4h` K 线。
2. WHEN 历史行情数据库保存 K 线 THEN 每条记录 SHALL 至少包含 symbol、timeframe、open_time、close_time、open、high、low、close、volume、数据源、抓取时间和数据质量状态。
3. WHEN 写入历史 K 线 THEN 系统 SHALL 以 `symbol + timeframe + open_time` 或等价唯一键做幂等 upsert，避免重复抓取导致重复记录。
4. WHEN 首期实现历史行情数据库 THEN 系统 SHALL 使用本地单文件数据库或等价本地嵌入式存储；设计阶段 SHOULD 优先评估 SQLite。
5. WHEN 使用默认历史数据库路径 THEN SHALL 位于 git ignored 目录，例如 `backtest_data/nofx_history.sqlite`。
6. WHEN 历史数据库 schema 变更 THEN 系统 SHALL 记录 schema version，并提供仅用于开发测试环境的迁移或初始化能力。
7. WHEN 回测读取历史行情 THEN 默认 SHALL 优先从历史行情数据库读取，不直接依赖实时网络请求。
8. WHEN 历史行情数据库缺少指定 symbol、timeframe 或时间区间的数据 THEN 系统 SHALL 明确报告缺口，并 MAY 在用户显式允许时运行历史数据获取任务补齐。
9. WHEN 历史数据获取任务运行 THEN 数据源 MAY 复用当前实时行情获取链路使用的数据源和 K 线 API，但 SHALL 通过独立开发测试命令执行。
10. WHEN 历史数据获取任务运行 THEN 用户 SHALL 可配置抓取时间段 `data_from` 和 `data_to`，并支持 RFC3339 与 `YYYY-MM-DD` 两种输入格式。
11. WHEN 用户只配置 `data_from` 未配置 `data_to` THEN 获取任务 SHALL 默认抓取到当前时间之前最后一根已闭合 K 线。
12. WHEN 用户只配置 `data_to` 未配置 `data_from` THEN 获取任务 SHALL 启动失败并提示必须指定起始时间，除非配置提供明确的默认回溯窗口。
13. WHEN 获取任务配置的 `data_from >= data_to`、时间格式非法或时间段超出数据源支持范围 THEN 系统 SHALL 在调用外部 API 前失败。
14. WHEN 历史数据获取任务调用外部 API THEN 系统 SHALL 按可配置的 rate limit profile 控制请求频率，至少包含每分钟请求数、并发数、分页 limit 和退避参数。
15. WHEN 未显式配置 rate limit profile THEN 系统 SHALL 使用保守默认值，不得使用无法调整的硬编码高速请求策略。
16. WHEN 数据源返回限频、临时失败或网络错误 THEN 获取任务 SHALL 使用退避重试、断点续抓和清晰错误报告，不得无限快速重试。
17. WHEN 获取任务中断后再次运行 THEN 系统 SHALL 能基于数据库已有最大 close_time 或抓取游标继续补齐缺失区间，但不得越过用户配置的 `data_to`。
18. WHEN 获取任务完成 THEN 系统 SHALL 输出抓取摘要，包括配置时间段、实际覆盖时间段、请求次数、实际请求速率、限频等待次数、重试次数、写入条数、跳过重复条数、缺失区间、失败 symbol/timeframe 和数据源限频统计。
19. WHEN 数据库中存在断档、重复、异常价格或时间错位 THEN 系统 SHALL 在数据质量检查中标记，并允许回测按配置跳过异常区间或失败退出。
20. WHEN 历史行情数据库、获取任务、迁移脚本或维护命令存在 THEN 它们 SHALL 仅用于本地开发测试环境，不纳入 161 云端生产部署流程。
21. WHEN 生成回测参数快照 THEN 系统 SHALL 记录历史数据库路径或连接信息的脱敏摘要、数据源、配置抓取时间段、数据版本、数据范围和数据 hash。

### 4. 回测配置与参数快照

**User Story:** 作为量化交易员，我希望回测可以配置时间区间、标的、主交易级别、资金和费用，以便比较不同策略设置。

#### Acceptance Criteria

1. WHEN 启动回测 THEN 用户 SHALL 可配置 `backtest_from`、`backtest_to`、`timezone`、`symbols`、`initial_equity`、`scan_interval_minutes`、手续费、滑点、杠杆限制和输出目录。
2. WHEN 配置 `programmatic_strategy.timeframes.trade` 为 `15m`、`1h` 或 `4h` THEN 回测 SHALL 使用对应主交易级别驱动新开仓和加仓确认。
3. WHEN 配置中缺少程序化策略字段 THEN 回测 SHALL 复用生产代码中的程序化策略保守默认值。
4. WHEN `backtest_from` 或 `backtest_to` 使用 `YYYY-MM-DD` 格式 THEN 系统 SHALL 按配置 `timezone` 解析；默认 timezone SHALL 为 `Asia/Singapore`。
5. WHEN 回测统计区间确定 THEN 系统 SHALL 使用半开区间 `[backtest_from, backtest_to)`。
6. WHEN 配置包含非法 timeframe、history depth、手续费、滑点、初始资金、symbol、timezone、回测统计时间段或历史数据获取时间段 THEN 回测 SHALL 在启动前失败。
7. WHEN 用户只提供 `backtest_from/backtest_to` THEN 系统 SHALL 根据 `history_depth` 和指标预热要求自动计算 `warmup_from`，并校验历史库覆盖 `warmup_from` 到 `backtest_to` 的所需数据。
8. WHEN 历史库覆盖不足且未允许自动补齐 THEN 回测 SHALL 在启动前失败并报告缺口。
9. WHEN 回测运行 THEN 系统 SHALL 记录完整参数快照，包括策略名、策略版本、config hash、git commit、初始资金、费用滑点模型、symbol 池、timezone、`warmup_from` 和正式统计区间。
10. WHEN 使用生产配置作为模板 THEN 回测 SHALL 忽略真实 API key、secret、私钥和远端部署字段，并不得把敏感字段写入报告。
11. WHEN 同一份数据和参数重复运行 THEN 核心回测结果 SHALL 保持确定性一致。

### 5. 策略逻辑复用

**User Story:** 作为量化交易员，我希望回测运行的就是当前程序化策略，而不是另写一套近似策略，以便结果能指导实盘参数。

#### Acceptance Criteria

1. WHEN 回测生成策略决策 THEN 系统 SHALL 复用 `strategy/chanlun.Engine` 和 `decision.ProgrammaticStrategyPolicy`。
2. WHEN 回测构建上下文 THEN 系统 SHALL 构造与实盘兼容的 `decision.Context`，包含账户、持仓、候选币、market data、风险策略、频率策略和执行质量输入。
3. WHEN 回测计算行情指标 THEN 系统 SHALL 复用当前 `market` 包的 EMA、MACD、RSI、ADX/DI、ATR、Bollinger 计算逻辑或等价公开构建入口。
4. WHEN 程序化策略需要状态 THEN 回测 SHALL 使用隔离的 `StateStore` 路径，保留 `last_analyzed_closed_kline`、已处理 signal、买卖点 marker、峰值和 partial close guard。
5. WHEN 回测周期内触发 open/add/close/partial_close/update_stop_loss THEN 决策 SHALL 保留与实盘一致的 `reasoning`、`explanation`、`strategy_metadata`、`signal_id`、`signal_type`、`signal_close_time` 和 `decision_close_time`。
6. WHEN 公共风控拒绝策略信号 THEN 回测 SHALL 记录 `open_rejections` 和拒绝原因，不得静默丢弃。
7. WHEN 回测完成 THEN 报告 SHALL 能区分策略原始信号、风控后有效决策和纸面撮合实际成交。
8. WHEN 回测运行策略周期 THEN 系统 SHALL 使用虚拟时钟注入或等价机制，确保策略决策时间来自 `current_backtest_time`，不得使用当前真实系统时间影响交易判断。
9. WHEN 某实盘公共逻辑依赖当前真实时间但无法安全注入虚拟时钟 THEN 回测 SHALL 禁用该逻辑或标记为不参与回测，并在报告中声明。

### 6. 回测时钟与无未来函数

**User Story:** 作为量化交易员，我希望回测严格按历史时间推进，确保结果没有未来函数。

#### Acceptance Criteria

1. WHEN 回测开始 THEN 系统 SHALL 从 `warmup_from` 开始按 `scan_interval_minutes` 或已闭合 `3m` K 线事件推进虚拟时钟。
2. WHEN 构建某一周期行情数据 THEN 所有 timeframe SHALL 只截取 `close_time <= current_backtest_time` 的 K 线。
3. WHEN 主交易级别没有新闭合 K 线 THEN 回测 SHALL 与实盘一致：跳过新开仓和加仓，但继续评估已有持仓管理。
4. WHEN 历史样本不足以满足 `history_depth`、ADX、ATR、EMA、MACD 或缠论结构计算 THEN 系统 SHALL 进入预热或跳过该 symbol，不得用未来 K 线补足。
5. WHEN `current_backtest_time < backtest_from` THEN 系统处于 warmup 阶段，MAY 更新指标、走势结构和策略状态，但默认 SHALL 不生成真实成交和绩效。
6. WHEN 预热期结束前产生交易信号 THEN 默认 SHALL 只记录为 warmup signal，不计入真实回测交易，除非配置显式允许。
7. WHEN 回测进入 `[backtest_from, backtest_to)` THEN 才 SHALL 统计交易绩效和正式信号表现。
8. WHEN 回测报告输出交易时间 THEN SHALL 同时记录信号 K 线 close time、决策时间和成交时间。
9. WHEN 任一模块尝试读取当前虚拟时钟之后的数据 THEN 测试 SHALL 能失败或报告未来函数违规。

### 7. 纸面撮合与成交模型

**User Story:** 作为量化交易员，我希望回测能模拟真实交易成本和成交约束，以便评估策略净收益而不是理想收益。

#### Acceptance Criteria

1. WHEN 策略输出 `open_long`、`open_short`、`add_long`、`add_short`、`partial_close`、`close_long`、`close_short` 或 `update_stop_loss` THEN 纸面撮合器 SHALL 按动作更新虚拟账户、持仓、交易计划和保护单。
2. WHEN 策略在某周期产生市价类动作 THEN 默认 SHALL 在下一根可用 `3m` K 线开盘价成交，并应用配置滑点；不得用同一根已用于决策的未来收盘价成交。
3. WHEN 持仓存在止损或止盈 THEN 纸面撮合器 SHALL 使用后续 `3m` K 线 high/low 判断是否触发保护单。
4. IF 同一根 `3m` K 线同时触及止损和止盈 THEN 默认 SHALL 采用保守规则：先触发对当前持仓更不利的一侧，并在报告中标记 `same_bar_conflict=true`。
5. WHEN 计算手续费 THEN 系统 SHALL 支持配置 taker/maker fee bps，默认按 taker 成交计费。
6. WHEN 计算滑点 THEN 系统 SHALL 支持固定 bps、按 ATR 比例或按成交额阶梯的模型；首期 SHALL 至少支持固定 bps。
7. WHEN 加仓发生 THEN 纸面持仓 SHALL 更新平均开仓价、数量、保证金、止损、止盈和风险敞口。
8. WHEN partial close 发生 THEN 纸面撮合器 SHALL 按比例减少持仓数量，记录已实现盈亏、手续费、剩余持仓和 close reason。
9. WHEN update_stop_loss 发生 THEN 纸面撮合器 SHALL 只更新虚拟保护单，不得生成成交。
10. WHEN 同一 symbol 已有多仓 THEN 回测 SHALL 不允许再开空仓；WHEN 已有空仓 THEN 不允许再开多仓，除非先完成平仓或减仓动作。
11. WHEN 账户保证金、风险预算或最小名义额不足 THEN 纸面撮合器或公共风控 SHALL 拒绝动作并记录原因。
12. WHEN v1 未接入历史资金费 THEN 回测 SHALL 默认不计入 funding，并在报告中记录 `funding_mode=disabled`。
13. WHEN 后续启用 funding THEN 系统 SHALL 使用历史 funding 数据并按虚拟时间结算，不得读取当前实时 funding。
14. WHEN K 线 high/low 穿过估算强平价且启用 liquidation 模型 THEN 纸面撮合器 SHALL 按强平规则退出并记录 `liquidation=true`。
15. WHEN v1 不启用 liquidation 模型 THEN 报告 SHALL 明确记录 `liquidation_mode=not_modelled`，并提示杠杆风险可能被低估。

### 8. 公共风控与交易计划模拟

**User Story:** 作为系统维护者，我希望回测保留实盘公共层约束，以便回测结果不会高估策略。

#### Acceptance Criteria

1. WHEN 回测执行开仓或加仓 THEN 系统 SHALL 复用现有 open gate、仓位 sizing、杠杆限制、总风险预算、相关性限制、最小名义额和 profile 风控。
2. WHEN 回测执行平仓、减仓或止损移动 THEN 系统 SHALL 复用风险降低型动作校验，保证方向、symbol、数量和保护效果合法。
3. WHEN 程序化策略生成交易计划字段 THEN 回测 SHALL 在隔离内存或回测目录中模拟 TradePlan 生命周期。
4. WHEN 虚拟保护单触发 THEN 回测 SHALL 记录为自动平仓事件，并与策略主动平仓区分。
5. WHEN 熔断、日亏损或最大回撤触发 THEN 回测 SHALL 与实盘语义一致：阻断风险增加动作，并允许合法风险降低动作继续执行。
6. WHEN 回测结束仍有未平仓持仓 THEN 系统 SHALL 支持按最后一根可用 `3m` close 做 mark-to-market，并在报告中单独标记未结算持仓。
7. WHEN 每个 backtest run 启动 THEN 系统 SHALL 重置或隔离 `decision` 统计、TradePlanManager、熔断、回撤基准线、程序化策略 StateStore 和其他会跨周期累积的包级状态。
8. WHEN 批量回测多个 run THEN 每个 run SHALL 使用独立 state、cache、report 和临时数据目录，避免互相污染。
9. WHEN 回测完成 THEN 系统 SHALL 不改变进程外生产状态文件，不写入生产 `data/`、`decision_logs/` 或 `coin_pool_cache/`。

### 9. 标的池与多 symbol 回测

**User Story:** 作为量化交易员，我希望回测可以使用动态候选池快照或自定义标的池，以便验证不同标的选择方式。

#### Acceptance Criteria

1. WHEN 用户指定 `symbols` THEN 回测 SHALL 以指定 symbol 作为回测标的池。
2. WHEN 用户未指定动态候选池快照 THEN v1 回测 SHALL 默认使用显式 `symbols` 或静态自定义池，避免动态选币未来信息。
3. WHEN 用户指定动态候选池快照文件 THEN 回测 SHALL 从快照还原候选币及来源，不依赖线上自动刷新。
4. WHEN 动态候选池快照用于按时间变化的候选池 THEN 每个快照 SHALL 包含 `effective_at`，回测只能读取 `effective_at <= current_backtest_time` 的快照。
5. WHEN 动态候选池快照没有时间戳 THEN 系统 SHALL 仅将其作为静态 symbol list 使用，并在报告中标记 `candidate_pool_mode=static_snapshot`。
6. WHEN 未指定动态候选池快照且配置包含自定义 `programmatic_strategy.symbol_pool` THEN 回测 SHALL 按 `append`、`override`、`filter` 规则生成标的池。
7. WHEN 多 symbol 同时回测 THEN 系统 SHALL 按同一个虚拟账户共享资金、风险预算、持仓数量上限和相关性约束。
8. WHEN 某 symbol 当前有持仓 THEN 即使其不在下一轮候选池中，回测 SHALL 继续纳入持仓管理。
9. WHEN 报告输出 symbol 维度指标 THEN SHALL 包含交易次数、胜率、净收益、最大回撤、平均 R、手续费、滑点和信号类型分布。

### 10. 结果报告与指标

**User Story:** 作为量化交易员，我希望回测报告能判断策略是否有效，而不仅是列出成交。

#### Acceptance Criteria

1. WHEN 回测完成 THEN 系统 SHALL 输出 JSON 总报告，至少包含净收益、净收益率、最大回撤、胜率、盈亏比、profit factor、平均 R、总手续费、总滑点、交易次数、持仓时长、最大连续亏损和最终权益。
2. WHEN 回测完成 THEN 系统 SHALL 输出权益曲线数据，包含每个周期或每个成交后的 timestamp、equity、cash、unrealized_pnl、realized_pnl、drawdown。
3. WHEN 回测完成 THEN 系统 SHALL 输出交易明细，包含 entry、exit、side、size、entry_reason、exit_reason、signal_type、fees、slippage、R multiple 和持仓时长。
4. WHEN 回测完成 THEN 系统 SHALL 输出信号明细，包含每个买卖点的 signal_id、signal_type、direction、level、signal_close_time、decision_close_time、status、最终是否成交、拒绝原因和后续表现。
5. WHEN 回测完成 THEN 系统 SHALL 按 `buy1/buy2/buy3/sell1/sell2/sell3`、`open/add/partial_close/close/update_stop_loss`、symbol、timeframe、close reason 聚合统计。
6. WHEN 回测报告包含拒绝信号 THEN SHALL 聚合展示被风控拒绝的原因，例如 ADX/DI 不满足、RR 不足、风险预算不足、相关性过高、最小名义额不足。
7. WHEN 报告评估主交易级别 THEN SHALL 标记本次使用的 `trade` level，方便横向比较 `15m`、`1h`、`4h`。
8. WHEN 统计交易胜率、平均 R、profit factor 或最大连续亏损 THEN 主统计单位 SHALL 是 position lifecycle；partial close SHALL 作为该生命周期内的 execution event。
9. WHEN 输出执行质量或成本统计 THEN 系统 SHALL 另行提供 execution event 级统计。
10. WHEN 输出信号后续表现 THEN 默认 SHALL 统计是否达到 `1R`、MFE、MAE 和最终生命周期结果；观察窗口默认到 position lifecycle 结束，并 MAY 通过配置覆盖。
11. WHEN 计算 R multiple、MFE 或 MAE THEN 默认 SHALL 以开仓时初始风险距离为分母；若初始风险缺失 THEN 报告 SHALL 标记该样本不可计算。
12. WHEN 输出报告 THEN SHALL 区分 `market_data_source`、`execution_model` 和 `instrument_metadata_source`，避免把 Binance K 线与 Aster 等执行环境混同。
13. WHEN 输出文件为 CSV THEN SHALL 至少支持 `trades.csv`、`equity.csv`、`signals.csv`、`rejections.csv`。

### 11. 参数比较与批量回测

**User Story:** 作为量化交易员，我希望可以批量比较不同主交易级别和策略参数，以便为后续上线提供证据。

#### Acceptance Criteria

1. WHEN 用户提供多个回测配置或参数矩阵 THEN 系统 SHALL 支持顺序执行批量回测并输出汇总排名。
2. WHEN 批量比较 `trade=15m/1h/4h` THEN 每个 run SHALL 使用独立状态目录和独立参数快照。
3. WHEN 批量报告生成 THEN SHALL 按净收益、最大回撤、profit factor、平均 R、交易次数、手续费占比和拒绝率输出对比表。
4. WHEN 某个 run 失败 THEN 批量任务 SHALL 记录失败原因并继续执行其他 run，除非配置要求 fail-fast。
5. WHEN 批量回测使用相同数据集 THEN 报告 SHALL 记录共同数据 hash，便于确认比较公平。

### 12. 独立回测操作页面与可视化

**User Story:** 作为量化交易员，我需要程序化策略回测有独立的操作页面，以便在本地开发测试环境完成历史数据准备、回测执行、报告分析和 K 线信号复盘。

#### Acceptance Criteria

1. WHEN 本地开发测试环境启动回测功能 THEN 系统 SHALL 提供独立的回测操作页面，并与实盘交易监控、实盘下单和 161 生产部署页面隔离。
2. WHEN 用户进入回测操作页面 THEN 页面 SHALL 清晰标记 `BACKTEST`、`dry_run=true`、`live_trading=false`，避免误认为真实交易。
3. WHEN 页面展示历史数据管理区域 THEN 用户 SHALL 能配置数据源、symbol、timeframe、`data_from/data_to`、rate limit profile，并启动本地历史数据获取任务。
4. WHEN 历史数据获取任务运行 THEN 页面 SHALL 展示任务进度、请求次数、限频等待、重试次数、写入条数、重复条数、失败 symbol/timeframe 和错误原因。
5. WHEN 页面展示历史库检查区域 THEN 用户 SHALL 能查看数据库覆盖范围、数据 hash、gap、质量问题和可回测区间。
6. WHEN 页面展示回测配置区域 THEN 用户 SHALL 能配置 `backtest_from/backtest_to`、timezone、symbol 池、主交易级别 `15m/1h/4h`、初始资金、手续费、滑点、funding/liquidation 模式和输出目录。
7. WHEN 用户启动单次回测 THEN 页面 SHALL 调用本地开发测试 API 或等价本地命令服务运行回测，并展示当前状态、虚拟时间进度、已处理周期、成交数、信号数和错误信息。
8. WHEN 用户启动批量回测 THEN 页面 SHALL 支持配置参数矩阵，至少支持比较 `trade=15m/1h/4h`，并展示每个 run 的状态和汇总排名。
9. WHEN 回测运行中 THEN 页面 SHOULD 支持取消当前本地回测任务；取消后 SHALL 保留已生成的日志和中间状态，并在报告中标记 `cancelled=true`。
10. WHEN 回测完成 THEN 页面 SHALL 展示报告总览，包括净收益、最大回撤、胜率、profit factor、平均 R、交易次数、手续费、滑点和拒绝率。
11. WHEN 页面展示报告详情 THEN SHALL 支持查看权益曲线、交易生命周期列表、execution event 列表、信号列表、拒绝原因和按 symbol/signal type/timeframe 聚合统计。
12. WHEN 页面展示 K 线复盘 THEN SHALL 使用蜡烛图展示对应 symbol/timeframe 的历史 K 线，并按买点/卖点、开多/开空、平多/平空、signal_close_time/decision_close_time 成对标识信号。
13. WHEN 买点和卖点在同一 K 线或多个信号同一时间出现 THEN 页面 SHALL 按层次显示 marker，买点向 K 线下方排列，卖点向 K 线上方排列，不得重叠遮挡。
14. WHEN 回测输出信号文件 THEN SHALL 包含足够字段供独立操作页面和现有蜡烛图标识复用，包括 symbol、timeframe、close_time、signal_close_time、decision_close_time、display_close_time、signal_type、direction、level、status、trade_intent、position_side、price、reason。
15. WHEN 用户需要人工复盘 THEN 系统 SHOULD 支持从页面导出某 symbol 的 K 线片段、交易明细、信号 marker 和当前筛选条件到本地 JSON 或 CSV。
16. WHEN 回测操作页面运行 THEN 页面 SHALL 不读取、展示或提交真实 API key、secret、私钥，不提供真实下单、真实撤单或修改生产配置的入口。
17. WHEN 本地开发服务未启用回测 API THEN 生产前端 SHALL 不显示或不可访问回测操作入口。

### 13. 测试与验收

**User Story:** 作为维护者，我希望回测模块有可重复测试，防止未来改动引入未来函数或实盘副作用。

#### Acceptance Criteria

1. WHEN 使用固定 fixture K 线和固定配置运行回测 THEN 测试 SHALL 验证输出核心指标稳定。
2. WHEN fixture 中故意放入未来 K 线 THEN 测试 SHALL 验证策略不会读取未来数据。
3. WHEN 主交易级别无新闭合 K 线但已有持仓 THEN 测试 SHALL 验证持仓管理层仍运行。
4. WHEN 同一根 `3m` K 线同时触发止损和止盈 THEN 测试 SHALL 验证采用保守成交规则。
5. WHEN partial close 连续触发 THEN 测试 SHALL 验证冷却、次数和累计比例预算生效。
6. WHEN 配置 `trade=15m`、`1h`、`4h` THEN 测试 SHALL 分别验证主级别切换、组件级别映射和状态隔离。
7. WHEN 运行回测测试 THEN 测试 SHALL 不访问网络、不读取真实密钥、不写生产运行目录、不触发真实交易所方法。
8. WHEN 回测报告生成 THEN 测试 SHALL 验证 JSON schema、CSV 文件和参数快照字段完整。
9. WHEN 历史数据获取任务测试运行 THEN 测试 SHALL 使用 mock 数据源或 fixture，不访问真实外部 API。
10. WHEN 历史行情数据库写入重复 K 线 THEN 测试 SHALL 验证 upsert 幂等且不会产生重复记录。
11. WHEN 历史行情数据库存在数据断档 THEN 测试 SHALL 验证数据质量检查能识别缺口并阻止不安全回测。
12. WHEN 回测 fixture 包含 OI/funding 实时可用但历史库缺失 THEN 测试 SHALL 验证回测不会访问实时 OI/funding，并在报告中标记 disabled。
13. WHEN 连续执行两个 backtest run THEN 测试 SHALL 验证 `decision` 包级状态、TradePlan、熔断和 StateStore 不会跨 run 污染。
14. WHEN 使用无时间戳动态候选池快照 THEN 测试 SHALL 验证其只作为静态 symbol list，且报告标记 `candidate_pool_mode=static_snapshot`。
15. WHEN 生成 lifecycle 统计 THEN 测试 SHALL 验证 partial close 计为 execution event，不重复计为独立完整交易。
16. WHEN 回测操作页面运行 THEN 测试 SHALL 验证页面不暴露真实下单入口、不读取敏感密钥、不连接 161 生产部署。
17. WHEN 回测操作页面展示信号 marker THEN 测试 SHALL 验证买卖点分层显示、signal_close_time/decision_close_time 成对显示和同 K 线多信号不重叠。
