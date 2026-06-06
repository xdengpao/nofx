# NOFX Project Spec

本文档基于当前仓库代码生成，作为后续开发的项目事实基线。若实现发生变化，优先更新本文档和 `skills/nofx-development/`。

## 1. 项目定位

NOFX 是一个面向加密货币永续合约市场的 agentic trading OS。系统运行多个 AI trader，每个 trader 使用独立账户、AI 模型和交易所配置，在固定扫描周期内完成：

1. 读取账户状态、持仓、候选币池和多时间框架市场数据。
2. 用交易计划和风控规则评估已有持仓。
3. 在允许时调用 AI 寻找新机会。
4. 按“先平仓、后开仓”的优先级执行交易动作。
5. 将 prompt、AI 输出、执行动作、账户快照写入决策日志。

当前支持的 AI 提供方是 DeepSeek、Qwen 和 OpenAI 兼容自定义 API。当前支持的交易所是 Binance Futures、Hyperliquid 和 Aster。

## 2. 技术栈

后端：

- Go module: `nofx`
- Go 版本：`go.mod` 声明 `go 1.25.0`
- HTTP: Gin
- 交易所依赖：`go-binance/v2`、`go-hyperliquid`、`go-ethereum`
- 测试：标准 `testing` + `gopter` 属性基测试
- 配置：JSON 文件，默认 `config.json`
- 持久化：本地 JSON 文件，主要在 `data/` 和 `decision_logs/{trader_id}/`

前端：

- 位置：`web/`
- React 18、TypeScript 5、Vite 6
- Tailwind CSS、Recharts、SWR、Zustand
- Vite dev server 默认 `:3000`，代理 `/api` 到后端 `:8080`

## 3. 运行与验证命令

常用后端命令：

```bash
go build -o nofx
go run main.go
go test ./...
go test ./decision/...
go test ./config ./decision ./market ./pool ./logger
```

常用前端命令：

```bash
cd web && npm run build
cd web && npm run test
cd web && npm run dev
```

部署相关：

```bash
docker compose up -d --build
./start.sh start --build
```

注意：`go test ./...` 可能触达含网络或真实交易所依赖的路径；开发前应优先阅读相关测试是否使用 mock/testutil。

## 4. 目录职责

| 路径 | 职责 |
| --- | --- |
| `main.go` | 程序入口：加载配置、初始化模块、创建 `TraderManager`、启动 API server 和所有 trader、优雅退出 |
| `config/` | JSON 配置结构、默认值和校验 |
| `api/` | Gin HTTP API，前端仪表盘读取数据的唯一后端入口 |
| `manager/` | 多 trader 生命周期管理，统一启动/停止，订单追踪服务 |
| `trader/` | 交易执行层，含统一 `Trader` 接口和各交易所实现 |
| `decision/` | AI 决策、交易计划、风控、熔断、失效条件解析、统计持久化 |
| `market/` | Binance 市场数据抓取、指标计算、30 秒缓存 |
| `pool/` | 默认币池、AI500 API、OI Top API、合并候选币池 |
| `mcp/` | AI chat completions 客户端，支持 DeepSeek/Qwen/custom |
| `logger/` | 决策日志 JSON 写入、读取、统计和表现分析 |
| `web/` | React 监控界面 |
| `.kiro/steering/` | 既有项目知识：product/tech/structure |
| `.kiro/skills/` | 既有任务级操作指南，例如添加交易所、API、AI 模型、技术指标 |
| `skills/nofx-development/` | Codex 项目专用 skill |

## 5. 后端启动生命周期

`main()` 的关键流程：

1. 解析配置文件路径，默认 `config.json`。
2. `loadAndValidateConfig()` 调用 `config.LoadConfig()`，并要求至少一个 trader `Enabled=true`。
3. `ensureDataDir("./data")` 确保数据目录存在。
4. `initializeModules()`：
   - 设置默认币池、AI500 API、OI Top API。
   - 初始化 `decision` 全局模块，传入风险预算、杠杆和数据目录。
5. `setupTraderManager()`：
   - 创建 `manager.TraderManager`。
   - 注册自动平仓回调 `handleAutoClose()`。
   - 对每个启用 trader 调用 `TraderManager.AddTrader()`。
6. 创建 `api.NewServer(traderManager, cfg.APIServerPort)` 并异步启动。
7. 调用 `traderManager.StartAll()` 启动所有 trader 和订单追踪服务。
8. 收到信号后 `gracefulShutdown()`：停止 trader、保存 decision 数据、打印统计。

## 6. 配置模型

核心配置结构在 `config/config.go`：

- `Config.Traders`: 多 trader 配置。
- `TraderConfig`: trader ID、显示名、启用状态、AI 模型、交易所和凭证、初始余额、扫描间隔。
- `LeverageConfig`: BTC/ETH 杠杆和 altcoin 杠杆。
- `CoinPoolAPIURL` / `OITopAPIURL`: 候选币池数据源。
- `MaxDailyLoss` / `MaxDrawdown` / `StopTradingMinutes`: 传入 trader 的风控提示和暂停参数。

校验规则：

- `ai_model` 必须是 `qwen`、`deepseek` 或 `custom`。
- `exchange` 必须是 `binance`、`hyperliquid` 或 `aster`。
- Binance 必须有 `binance_api_key` 和 `binance_secret_key`。
- Hyperliquid 必须有 `hyperliquid_private_key`。
- Aster 必须有 `aster_user`、`aster_signer`、`aster_private_key`。
- custom AI 必须有 `custom_api_url`、`custom_api_key`、`custom_model_name`。
- 全局杠杆小于等于 0 时默认设为 5。

当前实现约束：

- `Config.Validate()` 中对 `trader.Exchange` 和 `trader.ScanIntervalMinutes` 的默认赋值发生在 `for _, trader := range c.Traders` 的副本上，不会写回 `c.Traders[i]`。如果后续依赖这些默认值，应改为按索引写回并补测试。
- `config.json.example` 包含注释，不是严格 JSON；文档可读，但直接用标准 JSON parser 读取会失败。实际 `config.json` 不应包含注释。
- 不要提交真实 API key、私钥或账户地址。

## 7. 交易执行抽象

所有交易所必须实现 `trader.Trader` 接口：

- 账户和持仓：`GetBalance()`、`GetPositions()`
- 开平仓：`OpenLong()`、`OpenShort()`、`CloseLong()`、`CloseShort()`
- 风控订单：`SetStopLoss()`、`SetTakeProfit()`、`CancelStopLossOrders()`、`CancelTakeProfitOrders()`、`CancelAllOrders()`
- 工具：`SetLeverage()`、`GetMarketPrice()`、`FormatQuantity()`
- 历史和状态：`GetOrderHistory()`、`GetTradeHistory()`、`GetOrderStatus()`

新增交易所时，至少修改：

1. `trader/interface.go` 不应随意变更；若必须变更，所有交易所实现都要同步。
2. 新增 `trader/{exchange}_trader.go`。
3. `config.TraderConfig` 增加凭证字段。
4. `config.Validate()` 增加合法值和必填校验。
5. `manager.TraderManager.AddTrader()` 把配置映射到 `AutoTraderConfig`。
6. `trader.NewAutoTrader()` 的 `switch config.Exchange` 增加构造分支。
7. `config.json.example` 和 README/部署文档补配置示例。
8. 添加接口契约测试或至少针对精度、开平仓参数、订单状态转换的单元测试。

## 8. AutoTrader 周期

`trader.AutoTrader.Run()` 启动循环，首次立即执行 `runCycle()`，之后按 `ScanInterval` 定时执行。

`runCycle()` 的主要步骤：

1. 增加 `callCount`。
2. `syncAutoClosedOrders()` 检查 SL/TP 等自动平仓订单。
3. 检查 `stopUntil` 风控暂停。
4. 每 24 小时重置日盈亏。
5. `buildTradingContext()` 构造 `decision.Context`。
6. `detectAutoClosedPositions()` 检查仓位消失。
7. `decision.GetFullDecision()` 获取完整决策。
8. 打印 CoT 和决策。
9. `sortDecisionsByPriority()` 确保先平仓后开仓。
10. `executeDecisionWithRecord()` 执行动作。
11. `decisionLogger.LogDecision()` 保存记录。

执行动作支持：

- `open_long`
- `open_short`
- `close_long`
- `close_short`
- `update_stop_loss`
- `update_take_profit`
- `partial_close`
- `hold`
- `wait`

开仓保护：

- 同币种同方向已有持仓时拒绝叠加开仓。
- 开仓后设置止损、止盈，并调用 `decision.OnPositionOpened()` 创建交易计划。

## 9. 决策引擎

`decision.GetFullDecision()` 是核心入口：

1. 初始化默认风险参数。
2. 如果全局熔断正在冷却，直接返回 `ALL wait`。
3. 拉取候选币市场数据。
4. 调用 `CheckCircuitBreaker()` 判断是否触发熔断。
5. 计算相关性矩阵。
6. `evaluateExistingPositions()` 用 `PositionEvaluator` 和 `TradePlan` 处理已有持仓。
7. `shouldCallAIForNewOpportunities()` 判断是否需要 AI 搜索新机会。
8. 构造 system/user prompt 并调用 `mcp.Client.CallWithMessages()`。
9. `ExtractDecisionsRobust()` 解析 AI 输出。
10. `ValidateAndEnrichDecision()` 和 `validateOpenDecision()` 补全并过滤开仓决策。
11. 合并持仓决策与 AI 决策，持仓管理优先于新开仓。

关键状态：

- `TradePlanManager`: 管理 `TradePlan`，持久化到 `data/trade_plans.json`。
- 统计和收益序列：由 `decision.Initialize()` 初始化，退出时 `decision.Shutdown()` 保存。
- 熔断状态是包级全局状态，带 mutex 保护。

开发注意：

- 决策输出 schema 变化时，需要同步 `decision.Decision`、AI prompt、解析器、前端日志展示和测试。
- 交易计划的 key 当前按 symbol 管理；若未来支持同 symbol 多方向或多账户共享，应先重构 key 设计。
- 调整止损时必须使用 `CancelStopLossOrders()`，不要误删止盈；调整止盈时使用 `CancelTakeProfitOrders()`。

## 10. 市场数据和币池

`market.Get(symbol)`：

- 规范化 symbol。
- 使用 30 秒内存缓存。
- 并发抓取 Binance 3m、15m、1h、4h K 线。
- 计算 EMA、MACD、RSI、ADX、ATR、Bollinger、OI、funding rate 等。

`pool.GetMergedCoinPool(ai500Limit)`：

- 取 AI500 前 N 个评分币种。
- 合并 OI Top 数据。
- 去重并记录来源。
- API 失败时回退缓存或默认主流币。

新增技术指标时，必须处理数据不足场景；范围类指标应有属性基测试，例如 RSI 在 `[0,100]`，ATR 非负。

## 11. HTTP API

API server 在 `api/server.go`，CORS 允许所有来源。路由：

| Method | Path | 说明 |
| --- | --- | --- |
| Any | `/health` | 健康检查 |
| GET | `/api/competition` | 所有 trader 对比 |
| GET | `/api/traders` | trader 列表 |
| GET | `/api/status?trader_id=xxx` | 单 trader 状态 |
| GET | `/api/account?trader_id=xxx` | 单 trader 账户 |
| GET | `/api/positions?trader_id=xxx` | 单 trader 持仓 |
| GET | `/api/decisions?trader_id=xxx` | 决策日志，最多 10000 |
| GET | `/api/decisions/latest?trader_id=xxx` | 最新 5 条决策 |
| GET | `/api/statistics?trader_id=xxx` | 决策统计 |
| GET | `/api/equity-history?trader_id=xxx` | 收益曲线数据 |
| GET | `/api/performance?trader_id=xxx` | AI 学习表现分析 |

约定：

- 支持 `trader_id` 的接口缺失参数时默认第一个 trader。
- 错误返回 `{"error":"..."}`。
- 新增接口后要同步 `web/src/lib/api.ts` 和 `web/src/types/index.ts`。

## 12. 前端结构

`web/src/App.tsx`：

- 页面：`competition` 和 `trader`。
- URL hash 保存页面状态。
- SWR 定时拉取 trader 列表、状态、账户、持仓、最新决策、统计。
- 语言：`LanguageContext` + `i18n/translations.ts`。

核心组件：

- `CompetitionPage`: 多 trader 竞赛页。
- `ComparisonChart`: 多 trader 对比图。
- `EquityChart`: 单 trader 收益曲线。
- `AILearning`: AI 学习/表现分析。

前端约定：

- API base 是 `/api`，依赖 Vite proxy 或部署层反代。
- 新增后端字段时优先补 TypeScript 类型，再改 UI。
- 图表数据字段应与后端 JSON tag 对齐，不要在组件里猜字段名。

## 13. 数据持久化

| 路径 | 内容 |
| --- | --- |
| `data/trade_plans.json` | 当前交易计划 |
| `data/` 其他 JSON | decision 模块统计、收益等运行状态 |
| `decision_logs/{trader_id}/decision_*.json` | 每个 trader 的周期决策日志 |
| `coin_pool_cache/latest.json` | AI500 币池缓存 |

运行时目录通常不应作为功能变更的一部分提交，除非用户明确要求保存样例数据。

## 14. 测试策略

当前测试覆盖：

- `config`: 配置加载、校验、默认值、属性基测试。
- `decision`: 风控、熔断、parser、persistence、takeprofit、decision 等。
- `market`: 数据和指标。
- `pool`: 币池。
- `logger`: 决策日志。
- `api`、`manager`、`trader`: 有单元测试或集成式测试。
- `web/src/utils`: Vitest 测试。

变更建议：

- 配置、风控、parser、指标、交易计划这类纯逻辑优先加属性基测试。
- 新交易所实现至少用 fake client 或 mock 层覆盖参数格式、精度和错误路径。
- 新 API 端点至少覆盖 handler 的成功、缺失 trader、manager 返回错误。
- 前端工具函数写 Vitest；组件变更如风险较高，可增加 React 测试或手动截图验证。

## 15. 安全和开发禁区

- 不要把真实密钥、私钥、助记词、账户地址写入仓库。
- 不要在测试中触发真实交易下单。
- 不要改变 `Trader` 接口语义而不更新所有实现。
- 不要让调整止损的逻辑取消止盈，或让调整止盈的逻辑取消止损。
- 不要绕过 `decision` 风控校验直接执行 AI 开仓决策。
- 不要在前端硬编码 trader ID，应该从 `/api/traders` 获取。
- 不要把运行时日志和缓存作为普通功能改动提交。

## 16. 已有辅助知识

项目已有 `.kiro/skills/`：

- `add-exchange.md`
- `add-api-endpoint.md`
- `add-ai-model.md`
- `add-technical-indicator.md`
- `add-invalidation-condition.md`
- `add-circuit-breaker.md`

后续做对应任务时，应先读这些文件，再结合 `skills/nofx-development/` 的通用流程执行。
