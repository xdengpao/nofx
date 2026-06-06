# NOFX Project Map

Use this reference after loading `SKILL.md` when the task needs concrete file locations, extension paths, or validation commands.

## Core Runtime Flow

`main.go` loads `config.json`, initializes `pool` and `decision`, constructs `manager.TraderManager`, starts the Gin API server, then starts all enabled `trader.AutoTrader` instances.

Each `AutoTrader` cycle:

1. Sync auto-closed SL/TP orders.
2. Build `decision.Context` from account, positions, candidate coins, market data, and recent performance.
3. Call `decision.GetFullDecision()`.
4. Sort decisions so closes run before opens.
5. Execute via the configured `trader.Trader` implementation.
6. Persist a `logger.DecisionRecord` under `decision_logs/{trader_id}/`.

## Important Files

| Task area | Files to inspect first |
| --- | --- |
| App startup | `main.go`, `config/config.go`, `manager/trader_manager.go` |
| Exchange integration | `trader/interface.go`, `trader/*_trader.go`, `trader/auto_trader.go`, `config/config.go`, `manager/trader_manager.go` |
| AI provider | `mcp/client.go`, `config/config.go`, `trader/auto_trader.go`, `manager/trader_manager.go`, `config.json.example` |
| Decision/risk | `decision/decision.go`, `decision/types.go`, `decision/risk.go`, `decision/persistence.go`, `decision/takeprofit.go` |
| Invalidation parser | `decision/parser.go`, `decision/parser_test.go`, `.kiro/skills/add-invalidation-condition.md` |
| Market indicators | `market/data.go`, `market/data_test.go`, `.kiro/skills/add-technical-indicator.md` |
| Coin pool | `pool/coin_pool.go`, `pool/coin_pool_test.go` |
| API endpoint | `api/server.go`, `manager/trader_manager.go`, `web/src/lib/api.ts`, `web/src/types/index.ts`, `.kiro/skills/add-api-endpoint.md` |
| Frontend dashboard | `web/src/App.tsx`, `web/src/components/`, `web/src/lib/api.ts`, `web/src/types/index.ts`, `web/src/i18n/translations.ts` |
| Decision logs | `logger/decision_logger.go`, `decision_logs/{trader_id}/` |

## Extension Recipes

### Add an exchange

Read `.kiro/skills/add-exchange.md`. Keep `trader.Trader` semantics stable. Implement all interface methods, including order history/status methods used by `OrderTracker`. Register config fields and validation, map fields through `manager.TraderManager.AddTrader()`, add the constructor branch in `trader.NewAutoTrader()`, and update `config.json.example`.

### Add an AI provider

Prefer the existing custom OpenAI-compatible API path if possible. For a built-in provider, add a provider constant and setter in `mcp/client.go`, add `TraderConfig` fields and validation, wire `AutoTraderConfig`, and update examples/docs. Preserve timeout/retry behavior unless the provider requires otherwise.

### Add a decision action

Update `decision.Decision`, prompt generation/parsing, validation, `trader.AutoTrader.executeDecisionWithRecord()`, logging fields if needed, frontend display types, and tests. Confirm action priority in `sortDecisionsByPriority()`.

### Add an API endpoint

Register route in `api.Server.setupRoutes()`. Use `trader_id` query param and default to the first trader when omitted if the endpoint is trader-scoped. Return errors as `{"error":"..."}`. Add manager method if data crosses trader boundaries. Update `web/src/lib/api.ts` and TypeScript response types.

### Add a market indicator

Read `.kiro/skills/add-technical-indicator.md`. Implement calculation in `market/data.go`, handle short input safely, add fields to the relevant data structs, include formatted prompt output if the AI needs it, and add property-based tests for range/invariant behavior.

## Validation Matrix

Use the narrowest useful command first:

```bash
go test ./config
go test ./decision
go test ./market ./pool
go test ./api ./manager ./trader
go test ./...
cd web && npm run test
cd web && npm run build
```

Run `go build ./...` or `go test ./...` before handoff when touching shared backend contracts. Run `cd web && npm run build` when changing frontend types, API clients, or UI components.

## Current Implementation Notes

- `Config.Validate()` currently writes per-trader default `Exchange` and `ScanIntervalMinutes` to a range variable copy, so those defaults do not persist into `c.Traders`. Use index-based updates if fixing or relying on these defaults.
- `config.json.example` contains comments for readability and is not strict JSON.
- Runtime data in `data/`, `decision_logs/`, and `coin_pool_cache/` should usually not be changed for feature work.
- Do not hardcode API keys, private keys, account secrets, or real trading credentials.
- Stop-loss and take-profit cancellation are intentionally split: use `CancelStopLossOrders()` for SL updates and `CancelTakeProfitOrders()` for TP updates.
