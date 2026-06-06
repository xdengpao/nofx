---
name: nofx-development
description: Project-specific development guidance for the NOFX agentic trading OS. Use when Codex works on this repository's Go backend, React dashboard, AI decision engine, risk controls, exchange adapters, AI provider integrations, market indicators, API endpoints, configuration, tests, deployment files, or docs.
---

# NOFX Development

## Overview

Use this skill to make changes in the NOFX repository without rediscovering the architecture from scratch. The canonical project spec is `docs/project-spec.md`; read `references/project-map.md` when you need file-level routing, extension recipes, or validation commands.

## First Steps

1. Inspect `git status --short` before editing and preserve user changes.
2. Read the smallest relevant code slice before changing behavior.
3. For broad or unfamiliar changes, read `docs/project-spec.md` and `references/project-map.md`.
4. Check `.kiro/skills/` for task-specific recipes before adding exchanges, AI providers, API endpoints, indicators, invalidation conditions, or circuit breaker behavior.
5. Prefer existing project patterns over new abstractions.

## Project Shape

The backend is Go module `nofx`. `main.go` loads config, initializes `pool` and `decision`, starts `api.Server`, then starts all enabled `trader.AutoTrader` instances through `manager.TraderManager`.

The trading loop lives in `trader/auto_trader.go`: build context, call `decision.GetFullDecision()`, sort closes before opens, execute through `trader.Trader`, and persist a `logger.DecisionRecord`.

The frontend is a separate Vite/React project in `web/`, with API wrappers in `web/src/lib/api.ts` and shared response types in `web/src/types/index.ts`.

## Development Rules

- Do not commit or hardcode real API keys, private keys, wallet secrets, or account credentials.
- Do not trigger real order placement from tests.
- Keep `trader.Trader` interface semantics stable; if the interface changes, update every exchange implementation.
- Preserve the split between `CancelStopLossOrders()` and `CancelTakeProfitOrders()` so adjusting one side does not delete the other.
- Keep trader-scoped API endpoints using `trader_id`; default to the first trader only for read endpoints that already follow that convention.
- Keep backend JSON tags and frontend TypeScript field names in sync.
- Treat `data/`, `decision_logs/`, and `coin_pool_cache/` as runtime state unless the user explicitly asks for sample data changes.
- Use Chinese for logs, comments, and user-facing messages when touching existing Chinese backend code; keep code identifiers and API paths in English.

## Common Workflows

### Backend logic

Start at the package that owns the behavior, then inspect callers. For decision/risk work, read `decision/types.go`, `decision/decision.go`, and the specific file under change. Add focused tests near the package under test; use `gopter` for invariants and generated inputs where existing tests do.

### Exchange work

Read `.kiro/skills/add-exchange.md` and `references/project-map.md`. Implement all methods in `trader.Trader`, including order history/status for `OrderTracker`. Wire config validation, manager mapping, `AutoTraderConfig`, constructor selection, examples, and tests.

### API and dashboard work

Change `api/server.go` first, then update `web/src/lib/api.ts`, `web/src/types/index.ts`, and affected components. For dashboard data, prefer typed API wrappers and SWR patterns already used in `web/src/App.tsx`.

### AI decision changes

Update the domain type, prompt construction/parsing, validation/enrichment, execution handling, logs, frontend display, and tests together. Confirm action ordering still closes or reduces exposure before opening new exposure.

## Validation

Use targeted checks first:

```bash
go test ./config
go test ./decision
go test ./market ./pool
go test ./api ./manager ./trader
cd web && npm run test
cd web && npm run build
```

Run `go test ./...` or `go build ./...` before handoff when touching shared backend contracts. Run the frontend build when changing API response types, dashboard components, or TypeScript utilities.

## Known Sharp Edges

- `Config.Validate()` currently sets per-trader default `Exchange` and `ScanIntervalMinutes` on a range-copy variable, so those defaults do not write back to `c.Traders`.
- `config.json.example` contains comments and is not strict JSON.
- Some tests or flows can call network-backed code if not carefully isolated; inspect test helpers before running broad commands.
