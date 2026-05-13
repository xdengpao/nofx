# Tasks

- [x] Confirm incident timeline and current source of holding duration.
- [x] Add `position_start_times` persistence to `decision.PersistentData`.
- [x] Add scoped get/set/remove helpers for persisted position start times.
- [x] Update `AutoTrader` to resolve position start time from plan, persisted state, exchange timestamp, or current time.
- [x] Persist start time on bot open and on recovered existing positions.
- [x] Remove persisted start time on close callbacks.
- [x] Pass Aster `updateTime` through `GetPositions()` when available.
- [x] Add regression tests for restart recovery and cleanup.
- [x] Run tests.
