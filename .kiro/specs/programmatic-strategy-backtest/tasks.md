# 程序化策略回测 Tasks

## Phase 1: 本地目录、配置与忽略规则

- [x] 更新 `.gitignore`，忽略 `backtest_data/`、`backtest_runs/`、`*.sqlite`、`*.sqlite-shm`、`*.sqlite-wal`。
- [x] 新增 `backtest/config.go`，定义 `BacktestConfig`、`CostConfig`、`ExecutionConfig`、`DataConfig`、`BatchConfig`。
- [x] 实现回测配置解析和校验：`backtest_from/backtest_to/timezone/symbols/initial_equity/scan_interval_minutes`。
- [x] 实现 `YYYY-MM-DD` 与 RFC3339 时间解析，默认 timezone 为 `Asia/Singapore`。
- [x] 实现 `[backtest_from, backtest_to)` 半开区间校验。
- [x] 实现 `warmup_from` 计算，按 `programmatic_strategy.history_depth` 和指标预热需求向前扩展。
- [ ] 实现生产配置模板脱敏读取，只保留策略和风险相关字段，不输出 API key、secret、私钥。
- [x] 新增回测配置单元测试，覆盖合法配置、非法时间段、非法 timezone、缺少历史库覆盖等情况。

## Phase 2: 历史行情数据库

- [x] 新增 `historydb` 包，封装 SQLite 或等价本地嵌入式存储的打开、关闭和路径初始化。
- [x] 实现 schema 初始化和 `schema_migrations` 版本记录。
- [x] 创建 `klines` 表，包含 source、symbol、timeframe、open/close time、OHLCV、quality、fetched_at、raw_hash。
- [x] 创建 `fetch_runs` 表，记录抓取任务摘要。
- [x] 创建 `quality_issues` 表，记录 gap、重复、异常价格和时间错位。
- [x] 实现 K 线 upsert，唯一键至少覆盖 source、symbol、timeframe、open_time。
- [x] 实现按 symbol/timeframe/time range 查询 K 线。
- [x] 实现 `LastClosedKlines(symbol,timeframe,limit,asOf)`，只返回 `close_time <= asOf` 的 K 线。
- [x] 实现数据 hash 计算，用于报告记录数据版本。
- [x] 为 schema 初始化、upsert 幂等、范围查询、close_time 过滤补单元测试。

## Phase 3: 历史数据源与抓取任务

- [x] 定义 `historydb.KlineSource` 接口，抽象数据源名称、支持周期、分页 limit 和 FetchKlines。
- [x] 实现 Binance Futures 公共 K 线数据源，支持 `symbol`、`interval`、`startTime`、`endTime`、`limit`。
- [x] 实现 `RateLimitProfile`，包含每分钟请求数、并发数、分页 limit、最大重试、退避参数。
- [x] 实现限频器和退避重试，禁止无限快速重试。
- [x] 实现抓取任务断点续抓，基于数据库已有最大 close_time 或游标继续。
- [x] 确保断点续抓不得越过用户配置的 `data_to`。
- [x] 实现抓取摘要，包含配置区间、实际覆盖、请求数、实际速率、限频等待、重试、写入、重复、失败项。
- [x] 新增 `cmd/history-data fetch` 命令。
- [x] 新增 `cmd/history-data inspect` 命令，展示数据库 source、symbol、timeframe、覆盖范围和数据量。
- [x] 新增 `cmd/history-data gaps` 命令，输出指定区间的数据缺口和质量问题。
- [x] 使用 mock source 测试分页、限频、重试、断点续抓和 upsert。

## Phase 4: 市场数据无网络构建入口

- [x] 将 `market.buildDataFromKlines()` 重构为可导出的无网络构建入口，例如 `market.BuildDataFromKlines()`。
- [x] 为构建入口增加 `BuildDataOptions`，支持 `SeriesLimit`、`IncludeMicroADX`、`EnrichmentMode`、历史 OI/funding 注入。
- [x] 保持生产 `market.Get()`、`market.GetWithHistory()` 行为不变，继续可使用实时 OI/funding。
- [x] 回测构建路径默认使用 `EnrichmentMode=disabled`，不得读取实时 OI/funding。
- [x] 确认 3m、15m、1h、4h 指标计算结果与现有实时路径一致。
- [x] 为 `market.BuildDataFromKlines()` 添加测试，覆盖无网络构建、数据不足、micro ADX、OI/funding disabled。

## Phase 5: 决策准备 provider 与虚拟时钟注入

- [x] 扩展 `decision.CyclePreparationOptions`，增加可选 `MarketDataProvider`、`DisableOITopFetch`、`Clock`。
- [x] 修改 `decision.PrepareCycleContext()`，当 `MarketDataProvider` 非空时使用 provider 获取行情，不调用 `market.Get()` 或 `market.GetWithHistory()`。
- [x] 修改 `strategy/chanlun.Engine`，将回测 runner 注入的 `MarketDataProvider`、`DisableOITopFetch` 和 `Clock` 透传给 `PrepareCycleContext()`。
- [x] 修改 OI Top 获取逻辑，回测设置 `DisableOITopFetch=true` 时不访问网络。
- [x] 为 `decision` 增加虚拟时钟入口，确保回测路径的熔断、统计时间和 FullDecision 时间可使用 `current_backtest_time`。
- [x] 为 `strategy/chanlun.StateStore` 增加 Clock 或等价时间注入，避免回测状态写入真实当前时间影响复现。
- [x] 增加 `decision` runtime 隔离 helper，用于回测 run 启动时重置统计、TradePlanManager、熔断和回撤基准。
- [x] 添加 provider 注入测试，验证回测路径不会调用实时 market 函数。
- [ ] 添加连续两个 run 的状态隔离测试。

## Phase 6: 历史行情 Provider

- [x] 新增 `backtest.HistoricalMarketDataProvider`，从 `historydb.Store` 读取历史 K 线。
- [x] Provider 按 `current_backtest_time` 和 `history_depth` 查询 `3m/15m/1h/4h` 已闭合 K 线。
- [x] Provider 调用 `market.BuildDataFromKlines()` 构造 `market.Data`。
- [x] Provider 在缺少 timeframe 或样本不足时返回明确错误和缺口信息。
- [x] Provider 默认设置 `oi_mode=disabled`、`funding_mode=disabled`。
- [x] 为 provider 添加测试，覆盖 close_time 过滤、history_depth、缺失数据和无实时网络访问。

## Phase 7: 纸面撮合器

- [x] 新增 `backtest.PaperBroker`，维护虚拟账户、持仓、保护单、pending orders 和 execution events。
- [x] 实现 `open_long/open_short`，按下一根 3m open 成交。
- [x] 实现 `add_long/add_short`，更新数量、平均价、保证金、风险敞口。
- [x] 实现 `partial_close`，按比例减少持仓，记录已实现盈亏、手续费、滑点和剩余持仓。
- [x] 实现 `close_long/close_short`，全平并结束 position lifecycle。
- [x] 实现 `update_stop_loss`，只更新虚拟保护单，不生成成交。
- [x] 实现止损/止盈触发，使用后续 3m high/low。
- [x] 实现同一 3m K 线同时触发止损和止盈的 worst-case 规则。
- [x] 实现手续费和固定 bps 滑点。
- [x] 实现 mark-to-market 权益更新。
- [x] v1 报告 `funding_mode=disabled`。
- [x] v1 报告 `liquidation_mode=not_modelled`，或实现可选估算强平检查。
- [ ] 为 broker 添加单元测试，覆盖成交、加仓、减仓、保护单、same-bar conflict、费用和滑点。

## Phase 8: 回测 Runner

- [x] 新增 `backtest.Runner`，负责 run_id、目录、配置快照、虚拟时钟和主循环。
- [ ] Runner 从 `warmup_from` 开始推进，直到 `backtest_to`。
- [x] 实现 3m 事件循环：执行上一周期 pending order、处理保护单、更新权益、构建 context、调用策略、排队新动作。
- [x] 构建 `decision.Context`，包含虚拟账户、纸面持仓、候选币、历史 provider、风险策略、频率策略。
- [x] 初始化 `chanlun.Engine`，设置隔离 StateStore 路径和虚拟 Clock。
- [x] 调用 `Engine.GetFullDecision()`，保留 open rejections、strategy diagnostics 和 signal markers。
- [ ] warmup 阶段默认只更新指标/结构/状态，不产生真实成交和正式绩效。
- [x] 正式阶段 `[backtest_from, backtest_to)` 才记录绩效。
- [x] 回测结束时对未平仓持仓按最后可用 3m close 做 mark-to-market。
- [ ] 为 runner 添加 fixture 测试，覆盖无未来函数、无新主级别 K 线但持仓管理继续运行、warmup 不计绩效。

## Phase 9: 标的池与候选池

- [x] 实现显式 `symbols` 标的池。
- [ ] 实现静态 `programmatic_strategy.symbol_pool` 的 append、override、filter 规则。
- [ ] 支持读取动态候选池快照。
- [ ] 对带 `effective_at` 的动态候选池快照按虚拟时钟过滤。
- [ ] 对无时间戳快照仅作为静态 symbol list，并在报告中写 `candidate_pool_mode=static_snapshot`。
- [x] 确保已有持仓 symbol 永远进入持仓管理。
- [ ] 添加候选池测试，覆盖静态 symbols、symbol_pool、effective_at 过滤和无时间戳快照。

## Phase 10: 报告与导出

- [x] 新增 `backtest.Report`、`SummaryStats`、`TradeLifecycle`、`ExecutionEvent`、`SignalOutcome`、`EquityPoint`。
- [x] 生成 `report.json`，包含参数快照、数据 hash、假设、funding/liquidation/OI mode、summary、symbol 维度、signal 维度、拒绝原因。
- [x] 生成 `trades.csv`，主单位为 position lifecycle。
- [x] 生成 `equity.csv`，记录 timestamp、equity、cash、unrealized、realized、drawdown。
- [x] 生成 `signals.csv`，记录 signal_id、signal_type、direction、status、成交状态、拒绝原因、MFE、MAE、1R。
- [x] 生成 `rejections.csv`，记录 open_rejections 和风险降低动作拒绝。
- [x] 导出前端蜡烛图可复用 marker JSON，包含 signal_close_time、decision_close_time、display_close_time、trade_intent、position_side。
- [x] 实现 position lifecycle 统计，partial close 只作为 execution event。
- [ ] 实现 MFE/MAE 和 R multiple 统计，默认以初始风险距离为分母。
- [x] 报告区分 `market_data_source`、`execution_model`、`instrument_metadata_source`。
- [ ] 添加报告 schema 测试和 CSV 字段测试。

## Phase 11: 批量回测

- [x] 实现 `backtest.BatchRunner`，支持参数矩阵。
- [x] 新增 `cmd/backtest batch`。
- [x] 每个 run 使用独立 state、cache、report 和临时目录。
- [x] 支持比较 `trade=15m/1h/4h`。
- [ ] 批量汇总输出净收益、最大回撤、profit factor、平均 R、交易次数、手续费占比、拒绝率、数据 hash。
- [x] 单个 run 失败时记录失败原因并继续，支持 `fail_fast=true`。
- [ ] 添加 batch 测试，覆盖独立状态、失败继续和共同数据 hash。

## Phase 12: 独立回测操作页面与本地 API

- [x] 新增本地回测 API 开关，默认生产环境关闭，例如 `NOFX_BACKTEST_API_ENABLED=false`。
- [x] 新增 `api/backtest_handlers.go` 或等价模块，注册 `/api/backtest/*` 本地开发测试接口。
- [x] 实现历史数据 inspect/gaps/fetch API，复用 `historydb` 和抓取任务。
- [x] 实现 backtest run/batch API，复用 `backtest.Runner` 和 `BatchRunner`。
- [x] 实现本地 job manager，支持 pending/running/completed/failed/cancelled 状态。
- [x] 实现取消 run/fetch 的 context cancel 逻辑，保留中间日志和状态。
- [x] 实现报告读取 API，支持 `report.json`、CSV 文件和 marker JSON。
- [x] 新增前端 `BacktestPage` 独立页面，入口仅在本地回测 API 启用时显示。
- [x] 新增历史数据管理面板，支持 source、symbols、timeframes、`data_from/data_to`、rate limit profile、fetch/inspect/gaps。
- [x] 新增回测配置面板，支持 `backtest_from/backtest_to/timezone`、symbol 池、trade level、资金、手续费、滑点、funding/liquidation mode。
- [x] 新增单次回测和批量回测启动控件，支持 `trade=15m/1h/4h` 参数矩阵。
- [x] 新增运行状态面板，展示 run id、虚拟时间进度、周期数、成交数、信号数、拒绝数和错误原因。
- [x] 新增报告总览、权益曲线、交易 lifecycle、execution events、signals、rejections 和聚合统计视图。
- [x] 新增 K 线复盘视图，复用蜡烛图 marker 逻辑，买点下方、卖点上方、同 K 线多信号分层。
- [x] 在 K 线复盘中成对展示 `signal_close_time` 和 `decision_close_time`。
- [ ] 新增页面导出功能，导出 K 线片段、markers、交易明细和当前筛选条件。
- [ ] 添加前端测试，验证页面不显示真实下单入口、不读取敏感密钥、不连接 161 生产部署。
- [ ] 添加前端 marker 测试，验证多信号分层和 signal/decision 成对显示。
- [x] 运行 `cd web && npm run test` 和 `cd web && npm run build`。

## Phase 13: CLI 与示例

- [x] 新增 `cmd/backtest run`，支持通过配置文件和命令行覆盖运行单次回测。
- [x] `cmd/backtest run` 输出报告目录、核心摘要和关键假设。
- [x] `cmd/backtest batch` 输出批量排名文件和每个 run 目录。
- [x] 新增本地示例配置，例如 `.kiro/specs/programmatic-strategy-backtest/examples/backtest.example.json`。
- [x] 新增本地示例批量配置，例如 `.kiro/specs/programmatic-strategy-backtest/examples/batch.example.json`。
- [x] 文档说明历史数据获取、gap 检查、单次回测、批量回测的命令顺序。
- [x] 确认这些命令和回测页面不加入 Docker Compose、`start.sh` 生产流程或 161 部署步骤。

## Phase 14: 安全与回归测试

- [ ] 测试回测路径不访问真实 AI provider。
- [ ] 测试回测路径不调用任何真实 `trader.Trader` 下单方法。
- [ ] 测试回测路径不写生产 `data/`、`decision_logs/`、`coin_pool_cache/`。
- [x] 测试 fixture 中包含未来 K 线时 provider 不返回未来数据。
- [x] 测试历史库缺 OI/funding 时不读取实时值，并报告 disabled。
- [ ] 测试连续两个 backtest run 不共享 decision 统计、TradePlan、熔断和 StateStore。
- [x] 测试 partial close 不重复计为完整交易。
- [ ] 测试动态候选池无时间戳快照标记为 static_snapshot。
- [ ] 测试回测 API 默认关闭时生产前端不显示入口，接口不可访问或返回明确禁用错误。
- [ ] 测试回测操作页面不暴露真实交易、撤单、修改生产配置入口。

## Phase 15: 验证

- [x] 运行 `go test ./historydb`。
- [x] 运行 `go test ./backtest`。
- [x] 运行 `go test ./market`。
- [x] 运行 `go test ./decision`。
- [x] 运行 `go test ./strategy/chanlun`。
- [x] 运行 `go test ./cmd/history-data ./cmd/backtest`。
- [x] 运行 `go test ./api`。
- [x] 运行 `cd web && npm run test`。
- [x] 运行 `cd web && npm run build`。
- [x] 共享契约变更后运行 `go test ./...`。
- [x] 使用 mock 或小型 fixture 执行一次本地回测，确认生成 `report.json`、`trades.csv`、`equity.csv`、`signals.csv`、`rejections.csv`。
- [x] 确认 `git status` 不包含 `backtest_data/`、`backtest_runs/` 或 SQLite 数据文件。
