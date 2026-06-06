# Programmatic Signal Staleness Guard Tasks

- [x] Add signal freshness config to programmatic strategy policy with soft-age and hard-lifetime defaults.
- [x] Map signal freshness config through `ProgrammaticStrategyConfig`, normalized profile, runtime policy, manager conversion, and `config_hash`.
- [x] Implement a programmatic pre-open signal guard in the chanlun engine.
- [x] Reject target-crossed signals before generic open validation.
- [x] Apply aged-signal confidence decay or stricter diagnostics without hard-rejecting solely at 1-2 candles.
- [x] Reject expired signals after the configured hard maximum lifetime.
- [x] Validate remaining reward/risk and net RR for aged signals before generic open validation.
- [x] Add preview signal config for 1h trade timeframe using closed 15m component candles.
- [x] Map preview config through `ProgrammaticStrategyConfig`, normalized profile, runtime policy, manager conversion, and `config_hash`.
- [x] Implement `preview_2x15m` watchlist markers without default opening.
- [x] Implement optional `preview_3x15m` pilot decisions with capped risk and stronger filters.
- [x] Reconcile preview lineage when the confirmed 1h signal closes.
- [x] Add stale/target-crossed suppression state separate from `executed_signals`.
- [x] Emit structured rejection diagnostics and marker reasons.
- [x] Ensure stale/missed-target rejections do not write `executed_signals`.
- [x] Add tests for long/short expired, target-crossed, aged-valid, aged-invalid, fresh-valid, preview-watchlist, preview-pilot, and preview-confirmation cases.
- [x] Add config normalization and config hash tests for signal freshness and preview settings.
- [x] Re-run `go test ./strategy/chanlun ./decision ./trader`.
