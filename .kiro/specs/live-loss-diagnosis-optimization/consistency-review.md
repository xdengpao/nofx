# Live Loss Diagnosis Optimization Consistency Review

## Review Scope

Reviewed on 2026-05-16:

- `.kiro/specs/live-loss-diagnosis-optimization/requirements.md`
- `.kiro/specs/live-loss-diagnosis-optimization/design.md`
- `.kiro/specs/live-loss-diagnosis-optimization/tasks.md`
- Existing implementation in `trader/`, `decision/`, `logger/`, and `config/`

## Result

The spec is consistent with the current code after the amendments in this review. No code changes were made.

## Confirmed Code Alignment

### Close lifecycle root cause

The documented duplicate close path matches current code:

- `AutoTrader.runCycle()` calls `detectAutoClosedPositions(ctx.Positions)` and then `updatePositionSnapshots(ctx.Positions)` before executing the current cycle's decisions.
- `executeCloseLongWithRecord()` and `executeCloseShortWithRecord()` call `decision.OnPositionClosedScoped()` and `orderTracker.StopTracking()`, but do not remove the closed symbol/side from `lastPositions`.
- On the next cycle, `detectAutoClosedPositions()` sees the old snapshot missing from current exchange positions and calls `handleAutoCloseEvent()`.
- `handleAutoCloseEvent()` calls `OnPositionClosedScoped()` when entry/exit/quantity are present.
- `OnPositionClosedScoped()` currently appends a `ClosedTradeRecord` and updates statistics even when `plan == nil`, producing records with empty direction/zero plan fields.

This supports Phase 2 as the first implementation phase.

### Replay behavior

`logger.BuildTradeOutcomes()` already pairs open/close actions from decision logs and records unmatched close actions when no open exists. This explains why `cmd/replay` can show fewer deduplicated trades than `data/trade_plans.json`; the persistent statistics are polluted, while replay can detect unmatched auto-close actions. Phase 3 correctly extends replay instead of rewriting raw logs.

### Order history availability

The `Trader` interface already exposes `GetOrderHistory()` and `GetTradeHistory()`, and `OrderTracker` uses them to construct `AutoClosedOrder` with entry, exit, quantity, leverage, PnL, close time, order ID, and commission. The spec now explicitly allows exchange-confirmed close metadata to count even if a local plan is missing, while snapshot-only no-plan fallback remains non-counted.

### Loss mode vs existing rolling gates

`logger.BuildRollingPerformance()` already computes recent loss streaks and lower `EffectiveMaxRiskPerTrade`. However, `decision.EvaluateOpenGate()` does not call `applyRollingPerformanceGate()`, and existing tests assert old rolling gates should be ignored. The spec now states loss mode must be an explicit deduplicated state, not an accidental re-enable of old rolling-gate blocking.

### Exit policy scope

`decision/takeprofit.go` already contains the current soft-stop and trailing-stop constants:

- soft stop at `-5.0%`
- no-momentum threshold `MFE < 3.0%`
- no-momentum loss threshold `-3.0%`
- no-momentum hold threshold `60` minutes
- breakeven threshold `6.0%`

Phase 6 is therefore a calibration/change task, not a greenfield feature.

## Amendments Applied

1. Requirement 2.3 now distinguishes no-plan/no-metadata invalid close records from exchange-confirmed close metadata.
2. Requirement 3 now includes optional read-only order/trade history reconciliation.
3. Design now states counted close records require either an active plan or validated exchange close metadata.
4. Design now documents that old rolling gates are intentionally ignored and loss mode should be separate.
5. Tasks now include close-event source metadata, optional order-history reconciliation, and test handling for existing rolling-gate expectations.

## Remaining Implementation Risks

- Adding a counted close path without active plans requires a clear `ClosedTradeInput` or equivalent, otherwise `OnPositionClosedScoped()` may keep accepting incomplete records.
- Lifecycle dedupe must not suppress legitimate future re-entry on the same symbol/side after the TTL or after a new open event.
- Reconciliation from live exchange history should be read-only and optional; tests should use exported JSON or mocks, never live credentials.
- Repaired statistics should be produced as dry-run or separate output first, because runtime `data/trade_plans.json` is mutable service state.

## Recommendation

Proceed with Phase 2 and Phase 3 before any strategy threshold changes. The current top priority is making close accounting idempotent and replay metrics trustworthy.
