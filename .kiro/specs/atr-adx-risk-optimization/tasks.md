# ATR ADX Risk Optimization Tasks

## Phase 1: Baseline and Compatibility

- [x] 1.1 Review `requirements.md` and `design.md` against current `decision`, `market`, `config`, `logger`, `trader`, and `cmd/replay` modules.
- [x] 1.2 Add a compatibility note for existing logs where `stop_distance_pct` is ratio semantics, and define new explicit ratio/percent fields.
- [x] 1.3 Add replay/report-only baseline for 2026-05-13 to 2026-05-16 local and 161 samples: stop distance, TP distance, ADX state, same-side exposure, R multiple where possible.
  - Local baseline: `/tmp/nofx_local_replay_20260513_20260516.json`; 161 baseline: `/tmp/nofx_161_replay_20260513_20260516.json`.
- [x] 1.4 Confirm no runtime secrets, account identifiers, or exchange credentials are copied into fixtures or spec artifacts.
- [x] 1.5 Decide initial rollout mode: report-only diagnostics first, strict safe mode second.

## Phase 2: Strategy Risk Config and Profiles

- [x] 2.1 Add `StrategyRiskConfig`, `InstrumentProfileConfig`, and `StrategySafeModeConfig` to `config/config.go`.
- [x] 2.2 Implement `NormalizeStrategyRisk()` with percent-to-ratio normalization and legacy fallback when `strategy_risk` is absent.
- [x] 2.3 Define default profiles for `btc_eth`, `major_alt`, `high_beta_alt`, `non_crypto`, and `default`, including `exchange_full_tp_mode=algorithmic_full` and full TP min-R defaults.
- [x] 2.4 Add explicit profile matching for BTC/ETH, major alts, high beta alts, and XAG-like non-crypto symbols.
- [x] 2.5 Add `rollback_legacy_validation` support so the new validation can be disabled without code rollback.
- [x] 2.6 Pass normalized strategy risk policy from config through `main`, `manager`, `AutoTraderConfig`, and `decision.Context`; add `AddTraderWithPolicies()` or equivalent manager path instead of hiding it inside frequency-only setup.
- [x] 2.7 Expose active strategy risk summary in `AutoTrader.GetStatus()` without credentials: enabled/rollback state, ADX timeframe, profile names, and exchange full TP mode.
- [x] 2.8 Add config tests for defaults, percent normalization, invalid profile values, explicit symbol matching, full TP mode defaults, and rollback behavior.

## Phase 3: Market Indicator Corrections

- [x] 3.1 Add DI+ and DI- series to `MidTermData15m` and `MidTermData1h`.
- [x] 3.2 Implement Wilder-style ADX calculation or a clearly named compatible helper that returns ADX, DI+, and DI- series.
- [x] 3.3 Preserve legacy top-level 4h `CurrentADX`, `CurrentDIPlus`, and `CurrentDIMinus` fields for compatibility.
- [x] 3.4 Add `GetDirectionalSnapshot(data, timeframe)` and `GetATR(data, timeframe)` helpers.
- [x] 3.5 Add report-only comparison diagnostics for old simplified DX-like value vs new Wilder ADX before strict ADX gate is enabled.
- [x] 3.6 Update market formatting/prompt compact output only where needed to display the selected ADX/ATR timeframe.
- [x] 3.7 Add market tests for insufficient data, ADX/DI ranges, 1h snapshot selection, 4h fallback, ATR timeframe selection, and old-vs-new ADX comparison fields.

## Phase 4: Open Risk Normalization

- [x] 4.1 Add `decision/strategy_risk.go` with `ResolveInstrumentProfile()` and `NormalizeOpenDecisionRisk()`.
- [x] 4.2 Extend `Decision` and logger action structs with profile, requested/final stop, requested/final TP, exchange full TP, legacy stop pct ratio, explicit stop ratio, explicit stop percent, TP ratio/percent, net RR, rewrite flags, and degraded ATR diagnostics.
- [x] 4.3 Implement ATR stop floor: `max(profile.ATRMultiplier * ATR, currentPrice * profile.MinStopPct)`.
- [x] 4.4 Rewrite or reject AI stop loss when tighter than the effective ATR/floor stop.
- [x] 4.5 Rewrite or reject AI take profit when closer than `stopDistance * minNetRR + feeSlippage`.
- [x] 4.6 Ensure stop/TP rewrites preserve direction validity for long and short decisions.
- [x] 4.7 Ensure missing ATR uses `FallbackStopPct`, marks degraded diagnostics, and still blocks micro stops.
- [x] 4.8 Reorder `validateOpenDecision()` so profile resolution and risk normalization run before profile-aware `EvaluateOpenGate()`, then net RR and sizing use normalized values.
- [x] 4.9 Extend `OpenGateInput` with profile, strategy risk policy, and risk normalization snapshot.
- [x] 4.10 Add unit tests for 0.03% stop, missing ATR, wider-than-floor AI stop, long/short TP rewrite, exchange full TP rewrite, invalid direction after rewrite, and validation call order.

## Phase 5: Risk-Based Position Sizing

- [x] 5.1 Extend existing `PositionSizingInput` with profile name and extend `PositionSizingResult` with fee/slippage reserve, total risk USD, total risk percent, and risk cap reason; do not duplicate the existing `FeeSlippagePct` input.
- [x] 5.2 Calculate max position size from `equity * effectiveRiskPct / (stopDistanceRatio + feeSlippagePct)`, while keeping legacy `stop_distance_pct` as ratio for old readers.
- [x] 5.3 Preserve `RiskUSD` as stop-only risk for compatibility while adding total risk fields for auditability.
- [x] 5.4 Apply profile max risk, loss mode cap, remaining risk budget, and range-regime cap using the strictest value.
- [x] 5.5 Reject trades whose risk-based size falls below exchange/profile minimum notional.
- [x] 5.6 Log requested size, adjusted size, sizing reason, stop distance ratio/percent, and total effective risk.
- [x] 5.7 Add sizing tests for tiny stop, wide ATR stop, max risk cap, loss-mode risk cap, min-notional rejection, fee/slippage reserve, and partial-close infeasibility.

## Phase 6: ADX Regime Hard Gate

- [x] 6.1 Add profile-aware `applyADXRegimeGate()` to `decision/open_gate.go`.
- [x] 6.2 Use `GetDirectionalSnapshot()` with policy `ADXTimeframe`, defaulting to 1h.
- [x] 6.3 Block trend-following opens when selected ADX is below profile minimum.
- [x] 6.4 Require DI/EMA directional alignment for 20-25 ADX transitional regimes.
- [x] 6.5 Require DI alignment for ADX > 25 trend regimes.
- [x] 6.6 Keep existing extreme ADX chase protection for extended high beta longs.
- [x] 6.7 Attach structured ADX diagnostics to `OpenRejection.GateDiagnostics`.
- [x] 6.8 Support report-only ADX gate diagnostics so strict rollout can compare blocked opportunities before enabling live blocks.
- [x] 6.9 Add open gate tests for low ADX block, transitional ADX confidence, wrong DI block, missing ADX behavior, report-only diagnostics, and existing high ADX chase behavior.

## Phase 7: Correlation and Exposure Guardrails

- [x] 7.1 Refactor same-side exposure and correlation concentration gates to share normalized side/profile logic.
- [x] 7.2 Apply high-correlation same-side limits to both longs and shorts.
- [x] 7.3 In loss mode or range regime, default same-side high-correlation max to 1.
- [x] 7.4 Outside loss/range regimes, allow profile-specific limits and risk reduction for second same-side high-correlation exposure.
- [x] 7.5 Block additions when any same-side position is floating loss beyond profile threshold.
- [x] 7.6 Add diagnostics with target symbol, existing symbols, correlation state, side, profile, and final risk multiplier.
- [x] 7.7 Add tests for ETH/BCH same-side short stacking, BTC/alt same-side long stacking, high beta restrictions, and loss-mode strictness.

## Phase 8: Trade Plan and Exit Policy

- [x] 8.1 Extend `TradePlan` with profile name, initial risk distance, initial ATR, effective SL/TP, exchange full TP, full TP mode/min-R, fee/slippage, and min net RR.
- [x] 8.2 Persist new plan fields on position open while keeping legacy plan loading and recovered existing positions compatible.
- [x] 8.3 Change fixed TP priority so strict plans do not full-close at raw AI TP; full close is allowed only at algorithmic exchange full TP/final R target.
- [x] 8.4 Add R-multiple helpers using `InitialRiskDistance`, with fallback to legacy `abs(entry-stop)`.
- [x] 8.5 Move stop to breakeven or small locked profit when MFE reaches 1R and exchange constraints allow.
- [x] 8.6 At 2R/3R, prefer scaled exit plus ATR trailing; if partial close is below min notional, tighten stop instead.
- [x] 8.7 Use profile ATR multiplier for trailing distance and protect against too-tight trailing during volatility expansion.
- [x] 8.8 Keep execution-layer full-position `SetTakeProfit()` after open, but pass the normalized algorithmic full TP price and record TP protection mode/errors in `DecisionAction`.
- [x] 8.9 Update order tracker/replay close-source handling so exchange full TP fills are distinguishable from local scaled/trailing exits.
- [x] 8.10 Add tests for legacy TP behavior, strict-plan algorithmic full TP behavior, `SetTakeProfit()` price selection, 1R breakeven, 2R scaled exit, min-notional fallback, ATR trailing, MFE giveback, and TP protection failure logging.

## Phase 9: Prompt and Candidate Output

- [x] 9.1 Add profile name, selected ATR, min stop, min TP, ADX/DI gate state, and executable directions to candidate prompt rows.
- [x] 9.2 Exclude blocked candidates from actionable prompt sections or mark them as `non_executable`.
- [x] 9.3 Ensure prompt constants come from strategy risk policy/profile instead of hardcoded stale values.
- [x] 9.4 Add prompt tests for ATR stop floor, net RR, ADX gate, profile display, and blocked candidate handling.
- [x] 9.5 Ensure validation still rejects/re-writes invalid AI output even if prompt text is changed.

## Phase 10: Replay Disease Diagnostics

- [x] 10.1 Extend replay report with strategy disease buckets: micro stop, micro TP, low ADX entry, counter-DI entry, same-side correlation stacking, profile mismatch, and premature full TP.
- [x] 10.2 Compute requested vs final stop/TP distances where logs contain both values; for old logs, fall back to action records, decisions, trade plans, and exchange close snapshots.
- [x] 10.3 Compute R multiple for closed trades when entry, exit, and initial stop are available; for legacy plans, fall back to `abs(entry-stop)`.
- [x] 10.4 Group replay metrics by profile, symbol, side, and close reason.
- [x] 10.5 Support optional exchange close JSON reconciliation without mutating runtime data.
- [x] 10.6 Add report-only output for rejected/re-written stop, TP, exchange full TP, and ADX gate counts.
- [x] 10.7 Add replay tests using redacted fixtures or synthetic records covering each disease bucket, legacy log fallback, and exchange full TP close-source classification.

## Phase 11: API, Logs, and Frontend Compatibility

- [x] 11.1 Extend decision logs with optional risk normalization fields while preserving older log readers.
- [x] 11.2 Expose active strategy risk summary and profile defaults in `/api/status`.
- [x] 11.3 If frontend types are touched, update TypeScript types for profile, risk normalization, stop ratio/percent, net RR, and disease buckets. Frontend types were not touched in this backend-only rollout.
- [x] 11.4 Keep UI changes optional; backend safety must not depend on frontend deployment.
- [x] 11.5 Add API/logger compatibility tests for missing new fields and old logs.

## Phase 12: Validation, Deployment, and Monitoring

- [x] 12.1 Run targeted tests: `go test ./market -run ADX`.
- [x] 12.2 Run targeted decision tests: `go test ./decision -run 'OpenGate|PositionSizing|ValidateOpenDecision|TakeProfit'`.
- [x] 12.3 Run replay/logger tests: `go test ./logger ./cmd/replay`.
- [x] 12.4 Run execution/config/manager tests: `go test ./trader ./config ./manager`.
- [x] 12.5 Run broader backend tests if shared contracts changed: `go test ./api ./manager ./mcp`.
- [ ] 12.6 Deploy first with report-only diagnostics or `rollback_legacy_validation=true`; confirm no service health regression.
- [ ] 12.7 Enable strict safe mode: max risk <= 0.5%, max positions <= 1, daily open limit <= 1.
- [ ] 12.8 Monitor at least 24 hours before balanced/active recovery.
- [ ] 12.9 Review deduplicated PF, win/loss ratio, R multiple, MFE giveback, ADX rejection buckets, and profile performance before widening risk.
- [ ] 12.10 Keep rollback path documented and avoid rewriting historical statistics automatically.
