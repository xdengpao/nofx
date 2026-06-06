# Tasks: Chanlun V2 Stale Diagnostic Downgrade

- [x] Create requirements/design/tasks spec documents.
- [x] Implement V2 freshness metadata enrichment and early hard-expired signal downgrade.
- [x] Preserve rejected marker visibility for downgraded stale signals.
- [x] Emit explicit V2 `signal_count` diagnostics and update rolling/daily signal counters to honor explicit zero.
- [x] Add targeted tests for downgrade behavior and signal-count fallback.
- [x] Run targeted backend tests and build.
