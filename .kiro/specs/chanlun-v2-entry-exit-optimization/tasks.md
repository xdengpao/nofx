# Chanlun V2 Entry Exit Optimization Tasks

## Phase 1: Baseline And Fixtures

- [x] 1. Save a sanitized 161 log analysis fixture for V2 stale cases
  - Include BNBUSDT, CLUSDT, DOGEUSDT, HYPEUSDT stale examples without secrets.
  - Keep runtime `decision_logs/` out of source control; store only minimal test fixture under `strategy/chanlunv2/testdata/`.
  - Verify fixture covers ages 3, 8, 19, 27, 58 trade candles.

- [x] 2. Add a V2 log analysis test/helper
  - Parse `DecisionRecord` actions and freshness metadata.
  - Assert stale parent structures do not become executable open decisions.
  - Assert zero quantity open attempts are represented as rejection in new flow.

## Phase 2: Config And Lifecycle State

- [x] 3. Add `chanlun_v2_strategy.entry_timing` config
  - Define config struct, defaults and validation.
  - Mirror programmatic `entry_timing.entry_zone` shape for `max_chase_ratio`, `min_remaining_net_rr` and `max_chase_atr_multiplier`.
  - Add `third_point_quality` config for `G/R/N/G_ATR` thresholds with ATR/percentile/timeframe-normalized defaults.
  - Include percentile lookback and max percentile thresholds when `use_symbol_percentiles` is enabled.
  - Include defaults in config hash.
  - Add config tests for old config compatibility and override behavior.

- [x] 4. Add `chanlun_v2_strategy.position_management` config
  - Define breakeven, partial TP, structure break, floating drawdown and reverse close options.
  - Set conservative defaults and validation.
  - Add config tests.

- [x] 5. Implement V2 lifecycle state store
  - Add in-memory map with mutex under `strategy/chanlunv2.Engine`.
  - Add optional persistence path under `data/`.
  - Add lifecycle statuses and terminal reason codes.
  - Add tests for restart/load behavior with temp files.

## Phase 3: Parent Structure Layer

- [x] 6. Split V2 structure signal conversion from open decision creation
  - Convert trade timeframe signals into parent structure markers first.
  - Keep existing strategy check marker behavior for structure visibility.
  - Do not create open-like decisions when direct open is disabled.

- [x] 7. Add parent structure terminal checks
  - Target crossed.
  - Remaining RR below threshold.
  - Watch window expired.
  - Invalid stop/take-profit structure.
  - Persist terminal lifecycle state and diagnostics.

- [x] 8. Add regression tests for 161 stale parent structures
  - BNB-like age 19/20 should be background or terminal, not open.
  - CLUSDT-like age 8/9 should not enter open gate.
  - DOGE buy3 age 58 should be terminal.

## Phase 4: Entry Trigger Layer

- [x] 9. Implement `EntryTrigger` model and stable ID helper
  - Include parent signal lineage.
  - Include trigger close time and trigger timeframe.
  - Ensure IDs change for new triggers but remain stable across repeated scans of the same trigger K line.

- [x] 10. Implement 15m pullback/retest/resume trigger
  - Use closed 15m K lines from prepared market data.
  - Confirm direction, entry zone and RR.
  - Compute side-aware `P0/P1/G/R/N/G_ATR` for third buy/sell quality when the trigger is breakout-retest based.
  - Resolve `P0` from `Signal.CenterID` and `AnalysisResult.Centers` (`ZG` for buy3, `ZD` for sell3); add a missing-center fallback diagnostic.
  - Derive breakout candle, `P1` and breakout extreme from closed quality-timeframe K lines or confirmed fractals.
  - Use ATR, symbol percentiles or timeframe-normalized candle counts instead of hardcoded 1h percent thresholds.
  - Reject `G <= 0`, excessive `G_ATR`, excessive `R`, and overlong `N` according to config.
  - Add tests for long, short and missing-center fallback.

- [x] 11. Implement continuation and micro confirmation trigger rules
  - Add `breakout_continuation`.
  - Add optional `micro_reversal_confirm` using 3m data.
  - Ensure V2 trigger type normalization accepts `micro_reversal_confirm` instead of reusing the V1 allowed list unchanged.
  - Keep default allowed types configurable.

- [x] 12. Convert valid entry triggers into open decisions
  - Use `entry_trigger_id` as executable signal ID.
  - Use `trigger_close_time` for executable freshness.
  - Update V2 freshness metadata precedence so `entry_trigger_close_time` / `trigger_close_time` wins over parent structure time for entry-trigger opens.
  - Ensure `decision.NewOpenRejectionFromDecision()` and V2 rejection marker conversion preserve executable trigger time when parent and trigger times both exist.
  - Preserve `parent_signal_id` and `parent_signal_close_time`.
  - Add tests proving parent old + trigger fresh can pass freshness.

- [x] 13. Add trigger rejection and quality diagnostics
  - `entry_trigger_expired`
  - `entry_zone_chased`
  - `entry_rr_invalid`
  - `entry_trigger_low_confidence`
  - `third_point.reentered_center`
  - `third_point.deep_retracement`
  - `third_point.range_after_breakout`
  - `strong_third_buy` / `strong_third_sell`
  - Surface counts in `StrategyDiagnostics`.

## Phase 5: Open Validation And Sizing Fail-Safe

- [x] 14. Tighten V2 open sizing fail-safe
  - Ensure open-like V2 decisions cannot reach exchange execution with `Quantity <= 0`.
  - Convert zero quantity/min notional/margin failures to `open_rejected`.
  - Ensure deterministic execution-layer open failures are caught before exchange calls or recorded as `open_rejected`, not failed `open_long/open_short`.
  - Add tests matching historical `仓位大小必须>0`.

- [ ] 15. Verify existing open gates still run after trigger conversion
  - BTC bearish high beta long block.
  - ADX/DI conflict.
  - Down structure countertrend block.
  - Loss mode/execution quality gates.

- [ ] 16. Add replay-style conversion metrics
  - parent seen
  - trigger ready
  - third-point quality distribution
  - freshness rejected
  - open gate rejected
  - sizing rejected
  - executed

## Phase 6: Position Management

- [x] 17. Implement V2 breakeven stop movement decision
  - Trigger by R multiple or profit pct.
  - Include fee/buffer.
  - Use `CancelStopLossOrders()` for stop-loss adjustment only.

- [x] 18. Implement V2 partial take profit decision
  - Trigger by R multiple or structure target.
  - Enforce per-position cooldown and max partial count.
  - Use `CancelTakeProfitOrders()` only for take-profit replacement.

- [x] 19. Implement V2 structure break close/partial close
  - Use 15m or configured timeframe.
  - Confirm close beyond structure level for configured bars.
  - Add long and short tests.

- [x] 20. Implement V2 floating drawdown management
  - Track peak favorable R/pct after activation.
  - Close partial or full according to config.
  - Add tests for activation and non-activation cases.

- [ ] 21. Upgrade reverse signal close
  - Require configured confidence and optional sub/micro confirmation.
  - Keep reverse close as risk-reducing.
  - Validate V2 risk-reducing actions with `decision.ValidateRiskReducingStrategyDecisions()` or an equivalent wrapper before execution.
  - Do not auto-reverse without a fresh entry trigger and open gate pass.
  - Verify all V2 risk-reducing actions are sorted before new opens and are not blocked by open freshness.

## Phase 7: Logs, API And Frontend

- [ ] 22. Extend V2 strategy diagnostics
  - Add parent/trigger counts and latency metrics.
  - Add close reason distribution.
  - Add rejection reason categories.

- [x] 23. Extend marker metadata
  - Add parent/trigger lineage fields.
  - Add structure-to-trigger latency.
  - Add third-point quality fields: `third_point_quality_category`, `support_gap_pct`, `support_gap_atr`, `retracement_ratio`, `pullback_candles`.
  - Add backend `chanlun.SignalMarker` fields and copy them through V2 marker/rejection helpers.
  - Ensure old strategy marker tests still pass.

- [ ] 24. Update frontend strategy display
  - Show parent structure time, trigger time, evaluation K line and action time.
  - Distinguish background structure, trigger-ready, rejected and position-management markers.
  - Sync `web/src/types.ts` and `web/src/types/index.ts` with new third-point marker fields.
  - Keep mobile text within containers.

## Phase 8: Validation

- [x] 25. Run targeted backend tests
  - `CGO_ENABLED=0 go test ./config ./strategy/chanlunv2`
  - `CGO_ENABLED=0 go test ./decision ./trader ./logger`
  - `CGO_ENABLED=0 go test ./api ./manager`

- [x] 26. Run frontend tests/build if UI or TypeScript changes
  - `cd web && npm run test`
  - `cd web && npm run build`

- [x] 27. Run full backend build
  - `CGO_ENABLED=0 go build ./...`

- [x] 28. Perform consistency review
  - Cross-check requirements, design, tasks and code paths.
  - Confirm no runtime logs, data files or credentials are included.
  - Confirm no test can place real exchange orders.

## Phase 9: Rollout

- [ ] 29. Deploy report-only mode to 161
  - Confirm parent/trigger metrics appear.
  - Confirm old BNB/HYPE stale structures remain non-executable.

- [ ] 30. Enable validation-only mode
  - Compare trigger latency and rejection rates against baseline.
  - Confirm no zero quantity open action reaches execution.

- [ ] 31. Enable pilot mode for a small symbol set
  - Start with reduced risk fraction.
  - Monitor open conversion, rejected reasons and close action distribution.

- [ ] 32. Promote to full V2 entry/exit mode after live validation
  - Keep rollback path to current freshness-only behavior.
  - Document final recommended config for `aster_chanlun_v2`.
