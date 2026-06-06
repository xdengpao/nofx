# Tech Stack & Build

## 后端

- 语言和模块：`go.mod` 声明 `go 1.25.0`，module 为 `nofx`。
- HTTP：`github.com/gin-gonic/gin`，API 默认监听 `:8080`。
- 交易所依赖：`github.com/adshao/go-binance/v2`、`github.com/sonirico/go-hyperliquid`、`github.com/ethereum/go-ethereum`。
- AI 通信：`mcp.Client` 调用 OpenAI-compatible Chat Completions，内置 DeepSeek、Qwen、自定义 API 三种路径。
- 市场数据：`market` 包通过 Binance Futures 公共 HTTP API 抓取 K 线、OI、funding，并在内存中缓存 30 秒。
- 本地持久化：无数据库，运行状态和日志写 JSON 文件，主要在 `data/`、`decision_logs/{trader_id}/`、`coin_pool_cache/`。
- 测试：标准 `testing`，属性基测试使用 `github.com/leanovate/gopter`；前端工具测试使用 Vitest 和 fast-check。

## 前端

- 位置：`web/`，独立 npm 项目。
- 栈：React 18、TypeScript 5.8、Vite 6、Tailwind CSS 3、Recharts、SWR、Zustand。
- 开发服务器：Vite 默认 `:3000`，把 `/api` 代理到 `http://localhost:8080`。
- 页面：竞赛总览与单 trader 详情，使用 URL hash 保持页面状态。
- 数据刷新：SWR 按账户/持仓约 15 秒、最新决策/统计约 30 秒刷新。

## 配置

- 运行配置：`config.json`，通常从 `config.json.example` 复制后去掉注释并填入真实值。
- Docker 环境变量：`.env.example` 提供 `NOFX_BACKEND_PORT`、`NOFX_FRONTEND_PORT`、`NOFX_TIMEZONE`。
- 核心配置段：
  - `traders[]`：trader ID、名称、启用状态、AI 模型、交易所、凭证、初始余额、扫描间隔。
  - `leverage`：BTC/ETH 与 altcoin 杠杆。
  - `use_default_coins`、`default_coins`、`coin_pool_api_url`、`oi_top_api_url`。
  - `dynamic_candidate_pool`：动态候选池开关、刷新时间、池大小、流动性过滤、快照路径。
  - `trading_frequency`：`safe`、`balanced`、`active` 开仓频率档位及回滚阈值。
  - `strategy_risk`：ATR/ADX/profile 风控灰度配置，代码已支持但示例配置可能未完整展开。
  - `max_daily_loss`、`max_drawdown`、`stop_trading_minutes`、`api_server_port`。
- `config.json.example` 为带注释的示例，不是严格 JSON；实际 `config.json` 需要是可被 `encoding/json` 解析的 JSON。

## HTTP API

- 健康检查：`Any /health`。
- 总览：`GET /api/competition`、`GET /api/traders`。
- Trader-scoped 读接口：`GET /api/status`、`/api/account`、`/api/positions`、`/api/decisions`、`/api/decisions/latest`、`/api/statistics`、`/api/equity-history`、`/api/performance`。
- Trader-scoped 接口使用 `?trader_id=xxx`；缺省时后端选择第一个 trader。
- API 错误响应保持 `{"error":"..."}`，新增字段时同步 `web/src/lib/api.ts` 和 `web/src/types/index.ts`。

## 常用命令

```bash
# 后端
go build -o nofx
go build ./...
go run main.go
go test ./config
go test ./decision
go test ./market ./pool
go test ./api ./manager ./trader
go test ./...

# Makefile
make build
make build-quick
make run
make test-backend
make test-frontend
make install

# 离线分析与校准
go run ./cmd/replay -log-dir decision_logs
go run ./cmd/replay -log-dir decision_logs -trader binance_qwen -output replay.json
go run ./cmd/calibrate-exchange -exchange aster
go run ./cmd/calibrate-exchange -exchange binance

# 前端
cd web && npm ci
cd web && npm run dev
cd web && npm run test
cd web && npm run build

# Docker / 脚本
docker compose up -d --build
docker compose down
./start.sh start --build
```

## 验证策略

- 窄改动优先跑对应包测试；共享后端契约、风控、交易执行、配置加载改动后至少跑 `go test ./...` 或 `go build ./...`。
- 修改前端 API client、类型或 UI 时跑 `cd web && npm run build`，工具函数改动跑 `cd web && npm run test`。
- `go test ./...` 中部分路径可能触达网络或交易所相关逻辑；运行前先确认测试使用 mock、fixture 或只读接口。
- 不要在测试中真实下单；新增交易所或执行路径时用 fake client/mock 覆盖参数、精度、错误路径和订单状态转换。

## 技术约束

- `trader.Trader` 是交易所抽象核心，变更接口必须同步 Binance、Hyperliquid、Aster 和所有测试。
- `decision` 包仍有包级状态和 mutex，包括统计、收益、交易计划、熔断状态；多 trader 通过 `TraderID` scope 降低冲突。
- 调整止损必须使用 `CancelStopLossOrders()`，调整止盈必须使用 `CancelTakeProfitOrders()`，不要用废弃的 `CancelStopOrders()` 破坏保护单。
- 运行时目录 `data/`、`decision_logs/`、`coin_pool_cache/` 通常不作为功能改动提交。
