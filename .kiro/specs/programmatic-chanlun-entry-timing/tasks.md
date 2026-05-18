# Programmatic Chanlun Entry Timing Tasks

- [x] Add `entry_timing` config model to `ProgrammaticStrategyConfig`, normalized profile, runtime policy, manager conversion, and `config_hash`.
- [x] Add entry timing defaults that disable stale direct structure opens and require fresh triggers by default.
- [x] Introduce structure-vs-entry terminology in `ChanlunSignal`, `SignalMarker`, and decision metadata.
- [x] Add `EntryTrigger` or equivalent runtime struct with stable `entry_trigger_id` linked to parent structure `signal_id`.
- [x] Refactor main signal evaluation so 1h Chanlun detections are stored as structure/background markers before any open decision is built.
- [x] Prevent direct open from a structure signal when `signal_close_time < latest_trade_close_time` unless a fresh trigger exists.
- [x] Implement entry window validation using current price, structure SL/TP, target-crossed status, and remaining net RR.
- [x] Add report-only diagnostics for `structure_background_only` and `waiting_for_fresh_entry_trigger`.
- [x] Implement closed-15m entry trigger evaluation for current 1h bucket.
- [x] Keep `preview_2x15m` as watchlist-only by default.
- [x] Keep `preview_3x15m` pilot disabled by default and require capped risk plus stronger filters when enabled.
- [x] Add state tracking for background-only structure ids, rejected entry trigger ids, and executed entry trigger ids.
- [x] Ensure existing `suppressed_signals` continues to suppress stale/missed structure entries without blocking new fresh entry triggers.
- [x] Update `signalToMainDecision` so open decisions carry `entry_trigger_id`, `parent_signal_id`, trigger timeframe, trigger close time, and entry window diagnostics.
- [x] Update strategy signal API and markers to expose structure/entry lineage.
- [x] Add tests for stale structure becoming background-only instead of open candidate.
- [x] Add tests for newly confirmed 1h structure direct-open path when enabled and price is valid.
- [x] Add tests for 15m pullback/retest trigger producing a valid open candidate.
- [x] Add tests for target-crossed structure refusing trigger creation.
- [x] Add tests for old suppressed structure not blocking a new entry trigger id.
- [x] Add config normalization and config hash tests for `entry_timing`.
- [x] Add replay/report fixture for recent BTC/ETH/SOL/XAG/XRP/CL examples showing old signals are background-only.
- [x] Run `go test ./strategy/chanlun ./decision ./trader ./config`.
- [x] Run `go test ./...`.

Implementation notes:

- `pullback_retest_resume` now uses closed component candles only: the prior component must retest the structure-side entry zone against the signal direction, and the latest closed component must resume in the signal direction.
- The 161 BTC/ETH/SOL/XAG/XRP/CL replay fixture is deterministic and focused on entry-timing classification, not exchange replay fidelity.
