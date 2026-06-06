# Tasks: Chanlun V2 Stale Signal Suppression

- [x] Add Chanlun V2 stale suppression state and helper methods in `strategy/chanlunv2`.
- [x] Integrate terminal freshness lifecycle suppression into `applyChanlunV2FreshnessGuard`.
- [x] Surface suppressed-repeat diagnostics in `GetFullDecision` without creating `OpenRejection` actions.
- [x] Preserve rejected markers after suppressed repeats and add/adjust Chanlun V2 tests.
- [x] Split trader execution log wording between freshness rejection and open gate rejection.
- [x] Run targeted backend tests and full backend build.
- [x] Commit, push to GitHub, and deploy to the 161 server.
