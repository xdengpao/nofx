# Requirements

## Problem

After service restarts, existing exchange positions must keep their original holding duration. The current system persists `TradePlan.CreatedAt`, and the BCHUSDT incident on 2026-05-14 reused that value correctly. However, the in-memory `positionFirstSeenTime` map is not persisted. If a trade plan is missing, corrupted, or removed while the exchange position still exists, recovery falls back to `time.Now()` and may incorrectly place an old position back into the minimum holding protection window.

## Goals

1. Persist position start timestamps independently from active trade plans.
2. Rehydrate in-memory position start timestamps after restart from the best available source.
3. Preserve current behavior when a valid trade plan exists.
4. Remove persisted start timestamps when a position is confirmed closed.
5. Use exchange-provided position timestamps as a fallback when available.

## Non-Goals

1. Reconstruct exact open time for manual positions when neither local persistence nor exchange timestamp exists.
2. Change take-profit/stop-loss decision thresholds.
3. Change the minimum holding period semantics.

## Acceptance Criteria

1. A bot-opened position writes its start timestamp to persistent storage.
2. After restart, if the active `TradePlan` exists, holding duration uses the persisted plan time and is not reset.
3. If the active `TradePlan` is missing but a position start timestamp exists, recovery uses that timestamp instead of current time.
4. If neither local source exists but the exchange position has `updateTime`/`openTime`/`entryTime`, recovery uses that timestamp.
5. Closing or auto-closing a position removes its persisted start timestamp.
6. Tests cover plan-based recovery, position-start fallback recovery, exchange timestamp fallback, and cleanup on close.
