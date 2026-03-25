# Project Structure

```
nofx/
├── main.go                    # Entry point: config loading, module init, trader startup, graceful shutdown
├── config.json                # Runtime configuration (not committed)
│
├── config/
│   └── config.go              # Config structs (TraderConfig, LeverageConfig) and JSON loading/validation
│
├── api/
│   └── server.go              # Gin HTTP server, REST endpoints under /api/*, CORS middleware
│
├── trader/                    # Trading execution layer
│   ├── interface.go           # `Trader` interface — unified contract for all exchanges
│   ├── auto_trader.go         # AutoTrader: main trading loop, cycle orchestration, position sync
│   ├── binance_futures.go     # Binance Futures implementation of Trader interface
│   ├── hyperliquid_trader.go  # Hyperliquid DEX implementation
│   ├── aster_trader.go        # Aster DEX implementation
│   └── order_tracker.go       # Tracks open orders, detects auto-closed positions (SL/TP fills)
│
├── manager/
│   └── trader_manager.go      # Manages multiple AutoTrader instances, order tracking service
│
├── decision/                  # AI decision engine and risk management
│   ├── decision.go            # Core: GetFullDecision, position evaluation, AI prompt building, validation
│   ├── types.go               # All domain types: TradePlan, Decision, Context, CircuitBreakerState, etc.
│   ├── risk.go                # Risk calculator, circuit breaker, correlation matrix, market regime detection
│   ├── parser.go              # Invalidation condition parser (regex-based)
│   ├── persistence.go         # TradePlanManager: save/load plans, statistics, returns to JSON
│   ├── takeprofit.go          # Tranche-based take-profit and trailing stop logic
│   ├── utils.go               # Shared helpers (sorting, formatting, extraction)
│   └── *_test.go              # Property-based tests (gopter)
│
├── mcp/
│   └── client.go              # AI API client: DeepSeek, Qwen, custom OpenAI-compatible endpoints
│
├── market/
│   └── data.go                # Market data: Binance kline fetching, indicator calculation (EMA, MACD, RSI, ADX, ATR, Bollinger), caching
│
├── pool/
│   └── coin_pool.go           # Coin pool: default coins, AI500 API, OI Top API, merged pool with dedup
│
├── logger/
│   └── decision_logger.go     # Decision logging to JSON files, performance analysis, Sharpe ratio
│
├── web/                       # React frontend (separate npm project)
│   ├── src/
│   │   ├── App.tsx            # Main app component
│   │   ├── components/        # UI: EquityChart, ComparisonChart, CompetitionPage, AILearning
│   │   ├── contexts/          # LanguageContext (i18n)
│   │   ├── i18n/              # Translation strings
│   │   ├── lib/api.ts         # API client wrapper
│   │   ├── types/             # TypeScript type definitions
│   │   └── utils/             # Utility functions
│   ├── package.json
│   ├── vite.config.ts
│   └── tailwind.config.js
│
├── docker/                    # Dockerfiles for backend and frontend
├── docker-compose.yml         # Full stack orchestration
├── nginx/nginx.conf           # Nginx reverse proxy config
└── decision_logs/             # Runtime: per-trader decision log JSON files
```

## Key Architectural Patterns

- **Interface-based exchange abstraction**: `trader.Trader` interface allows adding new exchanges without modifying core logic
- **Manager pattern**: `TraderManager` owns all `AutoTrader` instances and coordinates lifecycle
- **Cycle-based execution**: Each `AutoTrader.runCycle()` is a self-contained decision loop
- **Plan-driven position management**: Every open position has a `TradePlan` with SL/TP/invalidation conditions, evaluated each cycle
- **Global singleton state**: `decision` package uses package-level vars with mutex protection for statistics, returns, circuit breaker state, and the plan manager
- **JSON file persistence**: Trade plans, statistics, and returns are persisted to `./data/` as JSON; decision logs go to `decision_logs/{trader_id}/`
