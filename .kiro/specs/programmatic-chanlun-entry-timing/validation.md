# Programmatic Chanlun Entry Timing Cross Validation

## Scope

This cross validation checks the entry-timing spec against:

- the observed 161 stale/invalid open rejections;
- the existing Chanlun signal generation and main-signal decision pipeline;
- the already deployed signal freshness guard and suppression state;
- the existing preview layer that uses closed 15m component candles;
- implementation readiness for config, state, API, tests, and rollout.

## Cross Validation Result

Overall status: pass with implementation constraints.

The spec is internally consistent and matches the current production failure mode. It should move to implementation, but it must be implemented as a pre-open refactor, not as a looser freshness-guard parameter change.

### CV1 Incident-To-Requirement Fit

The 161 examples are explained by one common pattern:

- `SignalCloseTime` points to the confirmed Chanlun segment end;
- `DecisionCloseTime` points to the latest closed 1h trade candle;
- the signal age becomes 5 to 19 1h candles;
- price has often crossed the structure target or moved outside `SL/current/TP` shape;
- the freshness guard correctly rejects the open.

Requirements R1, R2, R3, R5, R7, and R8 directly address this. The spec does not try to make stale signals valid; it prevents stale structures from becoming open candidates.

Status: pass.

### CV2 Current Code Path Confirms The Root Cause

Relevant current behavior:

- `strategy/chanlun/signals.go` builds `ChanlunSignal` with `SignalCloseTime = segment.EndTime`, `TriggerCloseTime = segment.EndTime`, and `SourceLayer = main_signal`.
- `strategy/chanlun/engine.go` sets each main signal's `DecisionCloseTime` to the latest closed trade candle.
- `evaluateMainSignals` stores the confirmed signal and immediately calls `signalToMainDecision`.
- `signalToMainDecision` converts the structure signal into `open_long/open_short/add_long/add_short` when there is no existing opposite position.
- `applyProgrammaticSignalGuard` then rejects target-crossed, invalid SL/TP shape, expired age, or low remaining RR.

This validates the spec's design goal: the stale structure must be stopped before `signalToMainDecision`, unless a fresh entry trigger exists.

Status: pass.

### CV3 Previous Freshness Guard Remains Necessary But Insufficient

The existing freshness guard is a correct final safety layer:

- it blocks `target_already_crossed` before generic shape errors;
- it blocks expired signals after the hard lifetime;
- it decays aged signals and validates remaining net RR;
- it writes rejected markers and suppression state without writing `executed_signals`.

However, it is not a complete trading model because the old structure has already been promoted to an open candidate before the guard runs. The new spec correctly keeps the guard as final defense while adding a pre-conversion entry-timing layer.

Status: pass.

### CV4 Lower-Timeframe Preview Is Feasible But Needs Reframing

The existing preview path already proves that closed 15m component candles are available:

- `previewClosedComponents` selects closed 15m candles after the latest closed 1h candle;
- two closed 15m candles produce `preview_2x15m`;
- preview markers are watchlist by default;
- pilot opens require explicit config and capped risk.

The gap is semantic: preview currently creates synthetic 1h-like Chanlun signals, but it does not yet create an explicit `EntryTrigger` linked to a parent 1h structure signal. The spec's `EntryTrigger` model is compatible with the existing preview mechanism, but implementation should reframe preview as trigger generation rather than a parallel direct open path.

Status: pass with constraint.

### CV5 State Semantics Are Compatible

The existing state model already separates:

- executed signals;
- rejected/suppressed signals;
- recent signal markers.

This supports the spec requirement that background-only structures must not be written to `executed_signals`. The implementation still needs a new key distinction:

- stale/background structure id suppression;
- rejected entry trigger id suppression;
- executed entry trigger id.

This distinction is required so an old suppressed structure does not block a genuinely new fresh trigger under the same parent structure.

Status: pass with required implementation detail.

### CV6 Config Coverage Is Explicit Enough

The tasks require config coverage through:

- external JSON config;
- normalized config profile;
- runtime `decision.ProgrammaticStrategyPolicy`;
- manager conversion;
- `config_hash`.

That matches the existing programmatic config architecture. A missing mapping layer would create unsafe default drift, so this remains a mandatory implementation item.

Status: pass.

### CV7 Requirements-Design-Tasks-Validation Matrix

| Requirement | Design Coverage | Task Coverage | Validation Coverage | Verdict |
| --- | --- | --- | --- | --- |
| R1 separate structure and trigger | structure signal, entry trigger, proposed flow | terminology, `EntryTrigger`, refactor evaluation | A1, A4 | pass |
| R2 fresh event required | direct 1h rule plus 15m trigger layer | stale direct-open prevention, 15m trigger evaluation | A1, A2, A4 | pass |
| R3 entry window validation | long/short hard shape, target-crossed, RR | entry window validation task | A3 plus unit tests | pass |
| R4 lower-timeframe confirmation | closed 15m trigger layer, preview constraints | 15m trigger, preview 2/3 handling | A4, A5 | pass |
| R5 avoid replayed opens | separate structure/trigger ids and state | background/rejected/executed ids | A6 | pass |
| R6 configurability | `entry_timing` config sketch | config model, defaults, hash tests | rollback/config tests | pass |
| R7 observability | source layer, trigger status, diagnostics | API and marker updates | operational validation | pass |
| R8 safety semantics | no target-cross rewrite, continuation separate | target-crossed trigger block | A3, rollback criteria | pass |
| R9 replay readiness | replay/backtest phase | replay fixture task | regression dataset | pass |

### CV8 No Blocking Contradictions Found

No blocking conflict was found between requirements, design, tasks, and validation.

The following clarifications should be preserved during implementation:

- `preview_2x15m` remains watchlist-only by default.
- `preview_3x15m` remains disabled for pilot opens unless explicitly configured.
- `direct_structure_open` should default to false, or direct open age should default to zero closed trade candles.
- Target-crossed structures must not be silently rewritten into continuation trades.
- The existing freshness guard remains active after entry trigger refactoring.

### CV9 Implementation Readiness

Ready for implementation.

Recommended implementation order:

1. Add `entry_timing` config and runtime policy with safe defaults.
2. Add structure/entry metadata fields and `EntryTrigger` identity.
3. Refactor `evaluateMainSignals` so stale 1h structures become background markers before open conversion.
4. Add entry window validation before open gate.
5. Convert closed-15m preview logic into explicit trigger generation.
6. Add state separation for parent structure ids and entry trigger ids.
7. Update signal API, markers, diagnostics, and replay fixtures.
8. Run focused Chanlun/config/decision/trader tests, then `go test ./...`.

## Acceptance Criteria

### A1 Stale Structure No Longer Opens

Given a `sell2` structure signal whose `signal_close_time` is older than the latest closed 1h candle,
when no fresh entry trigger exists,
then the engine emits a structure/background marker and does not produce `open_short`.

Expected reason:

- `structure_background_only`, or
- `waiting_for_fresh_entry_trigger`.

### A2 Fresh Direct Structure Open Is Still Possible When Enabled

Given direct 1h structure opens are enabled,
and a structure signal closes on the latest 1h candle,
and current price satisfies the entry window,
and open gate/risk checks pass,
then the engine may produce an open candidate.

### A3 Current Price Window Blocks Chasing

Given a short structure signal,
when current price is at or below the structure target,
then no entry trigger should open.

Expected reason:

- `target_already_crossed`;
- `entry_window_missed`.

### A4 15m Trigger Can Revive A Background Structure

Given a 1h structure signal is older than the latest 1h close but still valid,
when a closed-15m pullback/retest/resumption trigger appears,
and current price remains inside the entry window,
then the engine may create an entry trigger with a new `entry_trigger_id`.

The parent `structure_signal_id` must remain in metadata.

### A5 Preview Signals Do Not Open Full Size By Default

Given `preview_2x15m` is detected,
then it must be watchlist-only by default.

Given `preview_3x15m` is detected,
then it must not open unless pilot mode is explicitly enabled.

### A6 Suppression Does Not Block New Trigger IDs

Given a structure signal was previously suppressed as stale or target-crossed,
when a genuinely new entry trigger id is created under a still-valid structure,
then the new trigger can be evaluated.

Given the same trigger id was rejected,
then repeated cycles should not reattempt it.

## Test Plan

### Unit Tests

- Structure signal older than latest trade close returns no open decision.
- Fresh latest-close structure can open when direct-open config is enabled.
- Long entry window rejects `current <= stop` and `current >= target`.
- Short entry window rejects `current >= stop` and `current <= target`.
- Remaining net RR below threshold rejects before open gate.
- `entry_trigger_id` is stable for same parent signal, trigger type, timeframe, close time, and config hash.
- `entry_trigger_id` changes when trigger close time or trigger type changes.
- Suppressed structure id does not equal executed entry trigger id.

### Integration Tests

- `GetFullDecision` with stale BTC/SOL/XAG-style signals produces wait/background diagnostics, not open rejections.
- Closed 15m trigger path creates watchlist marker for `preview_2x15m`.
- Optional `preview_3x15m` pilot creates capped-size open only when enabled and confidence threshold passes.
- Confirmed 1h upgrade preserves parent/trigger lineage.

### Regression Dataset

Use the 161 examples as replay fixtures:

- `BTCUSDT sell2 short`: `signal_close=2026-05-17 20:59:59`, `decision_close=2026-05-18 15:59:59`, age 19.
- `SOLUSDT sell2 short`: age 10.
- `ETHUSDT buy2 long`: age 14.
- `XAGUSDT sell2 short`: age 18.
- `XRPUSDT buy2 long`: age 13.
- `CLUSDT sell1 short`: age 5.

Expected after refactor:

- These are structure/background or invalidated markers.
- They do not become direct open candidates.
- They do not repeatedly appear as open gate rejections.

## Operational Validation

After deployment to 161:

1. Confirm `/api/decisions/latest?trader_id=aster_deepseek` shows no repeated stale open rejections for known old signals.
2. Confirm `/api/strategy/signals` shows structure/background markers with clear trigger status.
3. Confirm backend health returns OK.
4. Confirm no stale structure id is written to `executed_signals`.
5. Confirm a new valid trigger can still pass through existing open gate when all risk checks allow it.

## Rollback Criteria

Rollback or disable `entry_timing.enabled` if:

- valid fresh signals stop appearing even when `signal_close_time == latest_trade_close_time`;
- entry trigger ids collide across distinct triggers;
- preview triggers create open decisions when pilot is disabled;
- final validation receives malformed SL/TP decisions that should have been caught by entry window validation.
