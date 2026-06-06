---
name: nofx-dev
description: Use for NOFX agentic crypto futures trading OS development, especially new features, complex changes, quick fixes, backend/frontend/API/risk/exchange/AI provider work, trading behavior changes, tests, deployment files, or any request that asks Codex to act as this project's trading-system development engineer. Loads project steering docs, chooses Spec or Vibe workflow, and enforces focused validation.
metadata:
  short-description: NOFX trading OS development workflow
---

# NOFX Development

Use this skill when developing, fixing, reviewing, or documenting functionality in the NOFX agentic trading OS.

## Startup Context

Before starting feature work or non-trivial code changes, read these project steering files:

1. `.kiro/steering/product.md` - product definition and core capabilities
2. `.kiro/steering/tech.md` - architecture, stack, commands, and testing
3. `.kiro/steering/structure.md` - directory structure and module ownership

For broad or unfamiliar changes, also read `docs/project-spec.md` and `skills/nofx-development/references/project-map.md` if present.

After reading, briefly confirm the relevant architecture understanding to the user.

## Choose Workflow

For new features or complex behavior changes, ask the user to choose:

- **Spec mode**: Requirements -> Design -> Tasks. Use for new features, large trading behavior changes, multi-module API/frontend/backend work, or work where traceability matters.
- **Vibe mode**: Direct implementation. Use for small fixes, focused refactors, narrow tests, and quick improvements.

If the user explicitly asks for direct implementation or the change is clearly small, proceed in Vibe mode without blocking on a workflow question.

## Spec Mode

Create a folder under `.kiro/specs/` named with English kebab-case, such as `exchange-order-history-fix`.

Generate these files in order, asking for confirmation after each major document:

1. `requirements.md`
   - Background and feature summary
   - Glossary if needed
   - Numbered requirements with user stories and SHALL/WHEN/IF-THEN acceptance criteria

2. `design.md`
   - Overview and design principles
   - Mermaid architecture diagram when useful
   - Technical implementation plan with files, structs, functions, API contracts, and data structures
   - Risk controls, exchange behavior, compatibility, and migration notes

3. `tasks.md`
   - Phased task list
   - Use `- [ ]` checkboxes
   - Keep tasks independently verifiable

After all three documents are complete, ask the user whether to:

1. Execute tasks
2. Cross-check requirements, design, tasks, and existing code for consistency
3. Stop after documentation

When executing tasks, update `tasks.md` from `- [ ]` to `- [x]` as each task completes. If a task cannot pass review after retries, mark it `- [!]` with the failure reason.

## Vibe Mode

Implement directly without creating spec documents. Keep changes focused, follow existing architecture, and validate with the narrowest meaningful tests or checks.

## Project Map

- Startup flow: `main.go` loads config, initializes `pool` and `decision`, starts `api.Server`, then starts enabled traders through `manager.TraderManager`.
- Trading loop: `trader/auto_trader.go` builds context, calls `decision.GetFullDecision()`, sorts closes before opens, executes through `trader.Trader`, and writes `logger.DecisionRecord`.
- Exchange abstraction: `trader/interface.go` defines the unified `Trader` contract; implementations live in `trader/binance_futures.go`, `trader/hyperliquid_trader.go`, and `trader/aster_trader.go`.
- Decision engine: `decision/decision.go`, `decision/types.go`, `decision/risk.go`, `decision/persistence.go`, `decision/parser.go`, and `decision/takeprofit.go`.
- API surface: `api/server.go`, `manager/trader_manager.go`, `web/src/lib/api.ts`, and frontend TypeScript types.
- Frontend: separate Vite/React project under `web/`, with SWR data fetching and Tailwind CSS.

## Task-Specific Recipes

Check `.kiro/skills/` before implementing these areas:

- Add exchange: `.kiro/skills/add-exchange.md`
- Add API endpoint: `.kiro/skills/add-api-endpoint.md`
- Add AI model/provider: `.kiro/skills/add-ai-model.md`
- Add circuit breaker behavior: `.kiro/skills/add-circuit-breaker.md`
- Add invalidation condition: `.kiro/skills/add-invalidation-condition.md`
- Add technical indicator: `.kiro/skills/add-technical-indicator.md`

## Code Quality Review

After generating or modifying code, self-review against relevant hooks in `.kiro/hooks/`:

- `go-write-review.kiro.hook` for Go safety and style
- `test-file-convention.kiro.hook` for test placement and naming
- `ts-type-check.kiro.hook` for frontend TypeScript edits
- `run-tests-after-task.kiro.hook` and `full-build-test.kiro.hook` for validation expectations

Review for:

- No hardcoded API keys, private keys, wallet secrets, or real account credentials
- No tests that place real orders or depend on live trading side effects
- Correct error handling and synchronization in Go
- Stable `trader.Trader` interface semantics across all exchange implementations
- Backend JSON tags and frontend TypeScript field names staying in sync
- Risk controls, action ordering, stop-loss, and take-profit behavior preserved

If review fails:

1. Fix issues and review again.
2. If it still fails, fix and review one more time.
3. On a third failure, report unresolved issues. In Spec mode, mark the task `- [!]`; in Vibe mode, report the failed review clearly.

## Project Conventions

- Chinese is preferred for logs, comments, error messages, user-facing backend messages, and docs unless the existing local style says otherwise.
- Variables, functions, structs, modules, API paths, and JSON field names use English.
- Keep runtime state out of normal feature changes: `data/`, `decision_logs/`, and `coin_pool_cache/`.
- Preserve the split between `CancelStopLossOrders()` and `CancelTakeProfitOrders()`.
- Keep trader-scoped API endpoints using `trader_id`; default to the first trader only for read endpoints that already follow that convention.
- Prefer existing project patterns over new abstractions.

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

Run `go test ./...` or `go build ./...` before handoff when touching shared backend contracts. Run `cd web && npm run build` when changing frontend types, API clients, or UI components.

## Known Sharp Edges

- `Config.Validate()` currently sets per-trader default `Exchange` and `ScanIntervalMinutes` on a range-copy variable, so those defaults do not write back to `c.Traders`.
- `config.json.example` contains comments and is not strict JSON.
- Some tests or flows can call network-backed code if not carefully isolated; inspect test helpers before running broad commands.
