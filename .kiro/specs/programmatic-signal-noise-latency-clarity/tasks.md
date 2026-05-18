# 程序化缠论信号去重、时效与可视化清晰度 Tasks

## Phase 1: Backend Lifecycle Keys

- [x] Add lifecycle fields to `strategy/chanlun/types.go` for `ChanlunSignal`, `SignalMarker`, `SignalReport`, marker summary, and report filters.
- [x] Implement `StableStructureKey()` using trader, normalized symbol, direction, signal type, timeframes, center id, and segment start/end time, excluding `config_hash`.
- [x] Implement `MarkerLifecycleKey()` with priority for entry trigger, trade action, preview, structure, position management, and legacy fallback.
- [x] Populate `StructureKey`, `LifecycleKey`, `ParentStructureKey`, and `ReasonCode` in `buildSignal()`, preview signal construction, entry trigger construction, `signalToMarker()`, and `decisionToMarker()`.
- [x] Ensure `StableSignalID()` remains backward compatible for existing action lineage and execution state.

## Phase 2: StateStore Upsert, Suppression, And Compaction

- [x] Extend `ProgrammaticSymbolState` with optional `SignalLifecycles` while preserving `RecentSignalMarkers`.
- [x] Add lifecycle-aware upsert that merges repeated structure/preview markers by lifecycle key instead of only `signal_id|timeframe|close_time`.
- [x] Preserve executed, failed, and rejected trade-action markers as audit-critical during merge and compaction.
- [x] Keep `ExecutedSignals` and real action de-duplication keyed by actual signal id / entry trigger id; do not replace execution state with `structure_key`.
- [x] Add semantic suppression helpers keyed by trader, symbol, structure key, action or intent, and reason code.
- [x] Record `target_already_crossed`, `entry_window_invalid`, `signal_expired`, and repeated stale structures as semantic suppressions without appending new default-visible markers.
- [x] Add lazy state compaction for old markers on load, write, or report build, with `collapsed_count`, first seen time, last seen time, and latest reason preserved.
- [x] Keep old state files compatible when new lifecycle fields are missing.

## Phase 3: Signal Report Denoising And API Options

- [x] Add `SignalReportOptions` and `LatestSignalsWithOptions()` / `EmptySignalReportWithOptions()` in `strategy/chanlun`.
- [x] Implement default report view that returns latest representative markers per lifecycle and hides ordinary preview history, repeated suppressed checks, and stale invalidated history.
- [x] Implement audit report view that returns full bounded history with filters applied.
- [x] Compute `SignalMarkerSummary` including raw count, returned count, hidden count, collapsed count, preview hidden count, and latency summary.
- [x] Add report filtering by layers, statuses, time range, and limit.
- [x] Update `manager.TraderManager` with option-aware strategy signal retrieval.
- [x] Update `api/server.go` to parse `view`, `include_history`, `layers`, `statuses`, `from`, `to`, and `limit` query parameters.
- [x] Preserve legacy `/api/strategy/signals?trader_id=...&symbol=...` behavior as the default denoised view without returning 500 to older clients.
- [x] Verify `view=audit` / `include_history=true` returns raw bounded history as the rollout fallback for old visual behavior and diagnostics.

## Phase 4: Diagnostics And Logs

- [x] Add marker lifecycle summary to `StrategyDiagnostics`.
- [x] Add user-facing diagnostics for old structure background, waiting for fresh trigger, target crossed, entry window invalid, and repeated suppression.
- [x] Replace noisy repeated preview log lines with per-symbol summary messages when nothing actionable changed.
- [x] Ensure diagnostics include source layer, status, reason code, structure time, decision time, age candles, and folded counts.

## Phase 5: Frontend Types And API Client

- [x] Audit frontend `../types` and `./types` imports, then update both `web/src/types.ts` and `web/src/types/index.ts` with lifecycle fields, display category, hidden/collapsed fields, marker summary, report filters, and strategy signal query types, or safely consolidate the duplicate type sources.
- [x] Update `web/src/lib/api.ts` so `getStrategySignals()` accepts default/audit view and filter options.
- [x] Keep existing callers working when no options are passed.

## Phase 6: Unified Signal Display Model

- [x] Add `web/src/utils/strategyDisplay.ts` with `SignalDisplayModel` and `SignalDisplayItem`.
- [x] Implement category derivation for structure background, preview watch, entry trigger, trade action, invalid/rejected, and position management.
- [x] Implement shared label, status text, tooltip rows, tone, default visibility, and priority mapping with no symbol-specific branches.
- [x] Implement latest-signal selection priority: executed/failed action, ready trigger, rejected action, latest structure summary, preview summary, then empty diagnostics.
- [x] Expose hidden and collapsed counts for page-level display.
- [x] Update `web/src/App.tsx` `StrategyInspector` to use `buildSignalDisplayModel()` for latest signal, chart markers, diagnostics, and hidden/collapsed summaries, replacing the current `source_layer === 'main_signal'` local filter.
- [x] Add uniform view/layer/status controls that persist across symbol changes.

## Phase 7: Candlestick Marker Layout

- [x] Update `web/src/utils/strategyMarkers.ts` to consume display-model labels, category, priority, and default-visible filtering.
- [x] Add pure helpers for grouping visual markers by candle and placement.
- [x] Add cluster generation when same candle/side marker count exceeds the visible label threshold.
- [x] Add adjacent-label collision detection and compact lower-priority labels into clusters when needed.
- [x] Update `web/src/components/StrategyCandlestickChart.tsx` to render compact labels, cluster badges, and cluster tooltip lists.
- [x] Remove the chart component's internal `source_layer === 'main_signal'` filter so it renders the already-filtered unified display markers.
- [x] Keep buy markers below candles, sell markers above candles, and avoid viewport-scaled font sizes.
- [x] Ensure tooltip shows structure time, decision time, age, source layer, status, reason, parent/trigger relationship, lifecycle key, and signal id.

## Phase 8: Backend Tests

- [x] Add tests for stable structure key across config hash changes in `strategy/chanlun`.
- [x] Add tests that repeated `target_already_crossed` structures do not append default-visible duplicate markers.
- [x] Add tests that preview_2x15m and preview_3x15m fold within the same 1h trade candle.
- [x] Add tests that new entry trigger ids are not blocked by parent structure suppression.
- [x] Add StateStore migration/compaction tests that preserve executed, failed, and rejected action markers.
- [x] Add API tests for default view, audit view, layer/status/time filters, limit handling, and legacy query compatibility.

## Phase 9: Frontend Tests

- [x] Add Vitest coverage for `buildSignalDisplayModel()` category derivation, priority, latest selection, hidden counts, and no symbol-specific behavior.
- [x] Add tests for compact labels and tooltip row generation across all display categories.
- [x] Add tests for marker grouping, cluster generation, adjacent collision handling, and stable sorting.
- [x] Add tests using脱敏 161-like fixtures covering BTCUSDT, ETHUSDT, SOLUSDT, XAGUSDT, XRPUSDT, and CLUSDT marker patterns.

## Phase 10: Validation And Review

- [x] Run `go test ./strategy/chanlun`.
- [x] Run `go test ./api ./manager`.
- [x] Run `cd web && npm run test`.
- [x] Run `cd web && npm run build`.
- [x] Verify frontend type imports use one synchronized contract and no stale duplicate `SignalMarker` / `StrategySignalReport` shape remains.
- [ ] Manually verify default strategy-check view against multi-symbol fixture or 161 test environment.
- [ ] Manually verify audit view can expand hidden preview/rejected/invalidated history.
- [x] Review Go changes against `.kiro/hooks/go-write-review.kiro.hook`.
- [x] Review frontend changes against `.kiro/hooks/ts-type-check.kiro.hook`.
- [x] Confirm no runtime `data/`, `decision_logs/`, `coin_pool_cache/`, production `config.json`, or credentials are committed.

## Phase 11: Rollout Checks

- [ ] Compare marker counts before and after on all active strategy symbols.
- [ ] Confirm default view no longer renders BTCUSDT-style 20+ raw markers as separate labels.
- [ ] Confirm ETHUSDT, SOLUSDT, XAGUSDT, XRPUSDT, CLUSDT, BCHUSDT use the same display template and folding rules.
- [ ] Confirm median raw marker latency remains visible in audit mode but default view labels it as old/background/suppressed context rather than current tradable signal.
- [ ] Confirm no live trading behavior changes: old structures do not become new opens, preview remains observe-only unless configured, and open/add still passes existing gates.
