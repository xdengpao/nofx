# Project Structure

```
nofx/
├── main.go                         # 启动入口：加载配置、初始化 pool/decision、创建 TraderManager、启动 API/trader、优雅退出
├── main_test.go
├── pipeline_test.go                # 构建/运行链路相关测试
├── Makefile                        # build/test/install/docker 编排
├── config.json.example             # 带注释示例配置，实际 config.json 必须是严格 JSON
├── .env.example                    # Docker 端口和时区环境变量示例
│
├── config/
│   ├── config.go                   # Config/TraderConfig、动态候选池、频率策略、策略风控配置归一化与校验
│   └── config_test.go
│
├── api/
│   ├── server.go                   # Gin API、CORS、/health 与 /api/* 路由
│   └── server_test.go
│
├── manager/
│   ├── trader_manager.go           # 多 AutoTrader 生命周期、OrderTracker 管理、竞赛对比数据
│   └── trader_manager_test.go
│
├── trader/
│   ├── interface.go                # 交易所统一 Trader 接口和订单/成交记录类型
│   ├── auto_trader.go              # 单 trader 主循环、上下文构建、决策执行、日志记录
│   ├── binance_futures.go          # Binance Futures 实现
│   ├── hyperliquid_trader.go       # Hyperliquid 实现
│   ├── aster_trader.go             # Aster 实现
│   ├── order_tracker.go            # 止损/止盈自动平仓追踪
│   ├── auto_close_event.go         # 自动平仓事件归因与去重
│   ├── execution_preflight.go      # 下单前确定性检查
│   ├── execution_protection.go     # 保护单执行风险记录
│   ├── exchange_calibration.go     # 交易所最小名义额校准规则
│   └── trader_test.go
│
├── decision/
│   ├── decision.go                 # GetFullDecision、prompt、AI调用、开仓验证、最终决策限制
│   ├── types.go                    # Context、Decision、TradePlan、策略/频率/risk state 类型
│   ├── risk.go                     # 熔断、统计、相关性、市场状态等风险逻辑
│   ├── strategy_risk.go            # ATR/ADX/profile 风控和 SL/TP/RR 规范化
│   ├── open_gate.go                # 开仓 gate：置信度、ADX、BTC、相关性、执行质量、亏损模式
│   ├── position_sizing.go          # 账户风险预算、保证金、最小名义额统一 sizing
│   ├── loss_mode.go                # 基于 rolling performance 的亏损模式
│   ├── parser.go                   # 交易计划失效条件解析器
│   ├── persistence.go              # TradePlanManager、统计、收益、已平仓记录、JSON 持久化
│   ├── takeprofit.go               # 分批止盈、移动止损、动态 TP
│   ├── utils.go                    # 解析、格式化、通用 helper
│   └── *_test.go
│
├── market/
│   ├── data.go                     # Binance Futures 行情、3m/15m/1h/4h 指标、30 秒缓存
│   └── data_test.go
│
├── pool/
│   ├── coin_pool.go                # 默认币、AI500、OI Top、静态合并池和缓存
│   ├── dynamic_candidate_pool.go   # 动态候选池快照、评分、分层、过滤、刷新
│   └── *_test.go
│
├── mcp/
│   └── client.go                   # DeepSeek/Qwen/custom OpenAI-compatible AI client
│
├── logger/
│   ├── decision_logger.go          # 决策日志、统计、表现分析、执行质量
│   ├── replay.go                   # 离线 replay、对账、策略病因诊断、rolling performance
│   └── *_test.go
│
├── cmd/
│   ├── replay/main.go              # 读取 decision_logs 生成 replay JSON
│   └── calibrate-exchange/main.go  # 读取 exchangeInfo 生成最小名义额校准报告
│
├── testutil/                       # Go 测试 helper 和随机数据生成器
│
├── web/                            # React/Vite 前端
│   ├── src/App.tsx                 # 页面路由、SWR 数据加载、语言/Trader 选择
│   ├── src/components/             # CompetitionPage、ComparisonChart、EquityChart、AILearning
│   ├── src/contexts/               # LanguageContext
│   ├── src/i18n/                   # 翻译文本
│   ├── src/lib/api.ts              # /api client
│   ├── src/types/                  # TypeScript 类型
│   └── src/utils/                  # 日期、分页、CSV、颜色等工具及 Vitest 测试
│
├── docker/                         # 后端/前端 Dockerfile
├── docker-compose.yml              # 后端、前端、卷和健康检查
├── nginx/nginx.conf                # 前端容器反代配置
├── docs/project-spec.md            # 项目事实基线文档
├── skills/nofx-development/        # 仓库内 Codex 项目技能与 project-map
├── .kiro/skills/                   # 任务级开发指南
├── .kiro/specs/                    # 已完成/进行中的规格文档
│
├── data/                           # 运行时：trade_plans、统计、动态候选池快照等
├── decision_logs/                  # 运行时：按 trader 分目录的决策日志
└── coin_pool_cache/                # 运行时：AI500/OI Top 缓存
```

## 启动与运行时所有权

- `main.go` 只负责启动编排：配置加载、数据目录、`pool` 和 `decision` 初始化、`TraderManager` 创建、API server、trader 启停和 shutdown。
- `manager.TraderManager` 拥有所有 `AutoTrader` 和 `OrderTracker`，负责多 trader 生命周期、自动平仓回调和前端对比数据。
- `trader.AutoTrader` 拥有单 trader 的运行状态、AI client、交易所实现、决策日志和执行流程。
- `decision` 包拥有跨周期的交易计划、统计、收益序列、熔断状态和开仓/风控规则。
- `logger` 包拥有决策日志 schema、性能分析、执行质量统计和 replay 报告。
- `web` 只通过 `/api` 读取后端状态，不直接读取本地日志文件。

## 关键架构模式

- 交易所接口抽象：新增交易所优先实现 `trader.Trader`，不要让核心循环依赖具体交易所。
- 多 trader scope：API、日志、交易计划和自动平仓处理都应保留 `trader_id`，读接口缺省时才默认第一个 trader。
- 周期式决策：`AutoTrader.runCycle()` 是主要业务边界，每周期生成一条完整 `DecisionRecord`。
- 持仓管理优先：已有持仓的平仓、止损、止盈、失效条件和自动平仓修复优先于新开仓。
- 确定性风控包围 AI：AI 只给建议；开仓必须通过 `ValidateAndEnrichDecision()`、`validateOpenDecision()`、`EvaluateOpenGate()`、`CalculatePositionSizing()` 和最终限制。
- JSON 文件持久化：状态简单可审计，但并发和多 trader key 需要格外谨慎。
- 前后端字段契约：后端 JSON tag、`web/src/lib/api.ts` 和 `web/src/types/index.ts` 必须同步。

## 扩展路径

- 新交易所：读 `.kiro/skills/add-exchange.md`，更新 `trader/interface.go` 语义时同步所有实现、manager 映射、配置校验、示例和测试。
- 新 API：读 `.kiro/skills/add-api-endpoint.md`，新增路由、manager 方法、前端 API client 和类型，错误返回保持 `{"error":"..."}`。
- 新 AI provider：优先复用 custom OpenAI-compatible API；内置 provider 需要改 `mcp/client.go`、`config`、`manager`、`AutoTraderConfig` 和示例。
- 新指标：读 `.kiro/skills/add-technical-indicator.md`，在 `market/data.go` 安全处理数据不足，补范围/不变量测试。
- 风控和交易行为：优先补 `decision` 单元测试或属性基测试，再视影响范围跑 `go test ./api ./manager ./trader`。

## 开发禁区

- 不提交真实密钥、私钥、钱包地址或真实账户配置。
- 不在测试里触发真实下单。
- 不把 `data/`、`decision_logs/`、`coin_pool_cache/` 当作普通功能改动提交。
- 不混用止损/止盈取消接口；`CancelStopOrders()` 已废弃，只可兼容旧实现。
- 不绕过 `decision` 的风控校验直接执行 AI 开仓。
- 不在前端硬编码 trader ID，应从 `/api/traders` 获取。
