# 程序化策略回测 Requirements 二次评审

## 评审结论

当前 `requirements.md` 已经覆盖了回测的核心方向：本地开发测试环境、历史行情数据库、历史数据获取限频、程序化策略复用、无未来函数、纸面撮合、报告输出和测试验收。整体可以支撑进入 design。

但从现有代码架构看，仍有若干实现关键点需要在 requirements 中进一步明确，否则 design 阶段容易出现回测结果不稳定、隐性未来函数、生产运行目录污染或不同 run 互相污染的问题。

本次评审建议：先补充 P0/P1 项，再进入 design。

## 已核对的项目事实

- 当前实时行情主要在 `market/data.go` 通过 Binance Futures 公共接口获取 `3m`、`15m`、`1h`、`4h` K 线。
- 当前 `decision.PrepareCycleContext()` 会调用 `market.GetWithHistory()` 或 `market.Get()` 获取行情，默认仍可能访问实时网络。
- 当前 `market.buildDataFromKlines()` 在构造 `market.Data` 时还会读取 OI 和 funding，这对纯历史回测存在实时数据泄漏风险。
- 当前程序化策略已有 `strategy/chanlun.Engine.Clock`，但 `decision`、`StateStore`、`TradePlanManager`、统计和熔断逻辑仍存在多处 `time.Now()` 或包级状态。
- 当前仓库没有数据库依赖，运行态主要是 JSON 文件；历史行情数据库是新的本地开发能力。

## P0 必须补充

### 1. 回测时间边界、时区和预热期仍不够明确

**问题:**  
requirements 已要求 `from/to` 可配置，但没有明确：

- `YYYY-MM-DD` 按哪个时区解释。
- `from/to` 是闭区间还是半开区间。
- 指标和缠论结构需要 `history_depth` 预热时，历史数据应从哪里开始拉取。
- 预热期信号是否落库、是否参与状态、是否参与绩效。

**影响:**  
同一份配置在不同时区机器上可能得出不同 K 线窗口；如果从 `from` 才开始加载数据，EMA/MACD/ADX/ATR/走势段会失真；如果预热信号参与成交，会污染正式回测。

**参考方案:**  
补充要求：

- 回测统计区间使用 `[from, to)`。
- `YYYY-MM-DD` 默认按配置时区解析，建议默认 `Asia/Singapore`，同时报告中记录 timezone。
- 历史数据读取区间应为 `warmup_from = from - max(history_depth 对应时间跨度, indicator_warmup)` 到 `to`。
- warmup 期间允许策略更新结构/状态，但默认不得产生真实成交和绩效；报告单独统计 warmup signal。

### 2. 必须禁止回测行情构建访问实时网络

**问题:**  
requirements 写了回测优先读历史库，但当前 `market.buildDataFromKlines()` 会尝试读取实时 OI 和 funding；`decision.PrepareCycleContext()` 也会调用 `market.GetWithHistory()`。如果直接复用，会在回测期间混入当前实时数据。

**影响:**  
这是典型未来函数/实时数据泄漏，尤其会影响 OI、funding、候选过滤、市场状态和报告可复现性。

**参考方案:**  
补充要求：

- 回测市场数据构建必须使用 `HistoricalMarketDataProvider` 或等价接口，禁止策略周期内访问实时网络。
- OI/funding 若没有历史数据，v1 默认写入 `0` 或 `unknown`，并在报告中声明 `funding_mode=disabled`、`oi_mode=disabled`。
- 如果后续启用历史 OI/funding，必须进入历史库并按同一虚拟时钟过滤。

### 3. 回测 run 之间必须隔离 decision 包级状态

**问题:**  
requirements 已要求 `StateStore` 路径隔离，但现有 `decision` 包还有统计、交易计划、熔断、回撤基准线等包级状态。批量回测如果不重置，会出现 run A 的亏损、计划、熔断状态影响 run B。

**影响:**  
批量比较 `15m/1h/4h` 会失真，且同一 run 重复执行可能不可复现。

**参考方案:**  
补充要求：

- 每个 backtest run 必须创建独立的运行上下文，重置或隔离 `decision` 统计、TradePlanManager、熔断、回撤基准和程序化策略状态。
- 批量回测每个 run 必须使用独立 state/cache/output 目录。
- 回测完成后不得改变进程外生产状态文件。

### 4. 历史数据库落地形态需要收敛

**问题:**  
requirements 只说“本地历史行情数据库”，未说明数据库类型、路径、迁移和是否 git ignored。虽然 design 可以决定技术选型，但这里直接影响依赖、命令、测试和数据目录边界。

**影响:**  
实现可能在 JSON、SQLite、Postgres、Badger 等方案间摇摆；如果路径不明确，也容易误提交大数据文件。

**参考方案:**  
在 requirements 中明确一条最小约束：

- 首期历史库 SHALL 使用本地单文件数据库或等价本地嵌入式存储；推荐 design 使用 SQLite。
- 默认路径位于 git ignored 目录，例如 `backtest_data/nofx_history.sqlite`。
- 数据库迁移、schema version 和数据质量表必须只服务开发测试环境。

## P1 建议补充

### 5. 数据源 API 限频不能只写“参考要求”

**问题:**  
requirements 要求参考数据源 API，但没有说明限频配置从哪里来、是否允许并发、失败后如何控制全局速率。

**影响:**  
历史回填多 symbol、多 timeframe 时容易打爆 API 或因为限频导致任务不可恢复。

**参考方案:**  
补充要求：

- 数据获取任务 SHALL 支持按数据源配置 rate limit profile，包括每分钟请求数、并发数、分页 limit、退避参数。
- 默认限频参数不得硬编码为不可调整常量；应进入本地开发配置或命令参数。
- 抓取报告记录实际请求速率、限频等待次数和重试次数。

### 6. 动态候选池快照存在未来信息风险

**问题:**  
requirements 支持动态候选池快照，但没有明确快照是否是单一文件还是按时间变化。若用回测结束时的候选池跑全历史，会产生幸存者偏差和未来信息。

**影响:**  
标的选择会高估策略表现。

**参考方案:**  
补充要求：

- v1 默认使用显式 `symbols` 或静态自定义池作为回测标的。
- 若使用动态候选池，快照必须带 `effective_at`，回测只能读取 `effective_at <= current_backtest_time` 的快照。
- 单一无时间戳快照只能作为静态 symbol list 使用，并在报告标记 `candidate_pool_mode=static_snapshot`。

### 7. 期货回测缺少 funding 和爆仓/强平假设

**问题:**  
requirements 覆盖了手续费、滑点、保证金，但没有明确 funding 和 liquidation。项目交易对象是永续合约，长期回测时 funding 会影响净收益，杠杆持仓也需要处理极端穿仓。

**影响:**  
净收益可能偏乐观；极端行情下风险低估。

**参考方案:**  
补充要求：

- v1 可默认 `funding_mode=disabled`，但报告必须声明未计入资金费。
- 后续若启用 funding，必须使用历史 funding 数据并按虚拟时间结算。
- v1 至少提供保守 liquidation 检查或风险提示：当 K 线 high/low 穿过估算强平价时，按强平成交并记录 `liquidation=true`；如果不实现，也必须在报告声明 `liquidation_mode=not_modelled`。

### 8. 信号后续表现和交易胜率定义需要明确

**问题:**  
requirements 要求信号明细包含“后续表现”，报告包含胜率、平均 R，但没有定义：

- partial close 算一笔交易还是一个生命周期的一部分。
- 信号后续表现观察窗口是几根 K 线、几小时，还是直到下一反向信号。
- MFE/MAE、R multiple 按初始止损还是调整后止损计算。

**影响:**  
同一份成交可得出不同胜率、平均 R 和信号质量结论。

**参考方案:**  
补充要求：

- 以 position lifecycle 作为主交易统计单位，partial close 是生命周期内事件。
- 另外输出 execution event 级统计。
- 信号表现默认统计 `1R 是否达到`、`最大有利变动 MFE`、`最大不利变动 MAE`，观察窗口可配置，默认到该生命周期结束。

### 9. `from/to` 和历史数据获取时间段需要区分“抓取区间”和“回测统计区间”

**问题:**  
当前新增需求里历史数据获取也使用 `from/to`，回测配置也使用 `from/to`。二者语义容易混淆：抓取区间应覆盖预热，回测统计区间不应包含预热。

**影响:**  
命令参数和报告容易误读，批量回测也难复现。

**参考方案:**  
补充术语：

- `data_from/data_to`：历史数据抓取区间。
- `backtest_from/backtest_to`：正式回测统计区间。
- 如果用户只给 `backtest_from/backtest_to`，系统自动计算所需 `data_from` 并校验历史库覆盖。

## P2 可在 design 中细化

### 10. 本地数据维护命令边界

建议 design 阶段明确本地命令：

- `cmd/history-data fetch`
- `cmd/history-data inspect`
- `cmd/history-data gaps`
- `cmd/backtest run`
- `cmd/backtest batch`

这些命令不进入 Docker Compose 生产部署。

### 11. 大数据文件和报告目录应加入忽略规则

建议 requirements 或 tasks 明确：

- `backtest_runs/`
- `backtest_data/`
- `*.sqlite`
- `*.sqlite-shm`
- `*.sqlite-wal`

都不应提交。

### 12. 数据源和交易所执行源需要在报告中分开

当前实盘 trader 可跑 Aster，但行情链路主要来自 Binance Futures。回测报告应明确：

- `market_data_source`
- `execution_model`
- `instrument_metadata_source`

避免误以为 Aster 成交深度和 Binance K 线完全一致。

## 建议补充到 requirements 的最小清单

1. 明确 `[from, to)`、timezone、warmup_from 和 warmup signal 行为。
2. 明确回测行情构建不得访问实时网络；历史 OI/funding 未实现时必须禁用并报告。
3. 明确每个 backtest run 隔离并重置 `decision` 包级状态。
4. 明确历史库默认本地嵌入式数据库，推荐 SQLite，路径在 git ignored 目录。
5. 明确数据源 rate limit profile 可配置，不硬编码不可调整。
6. 明确动态候选池快照必须避免未来信息。
7. 明确 funding/liquidation v1 假设及报告字段。
8. 明确信号表现、partial close、交易生命周期的统计口径。
