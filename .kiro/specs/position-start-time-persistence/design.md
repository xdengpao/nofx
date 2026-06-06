# Design

## Current Behavior

`PositionEvaluator` calculates holding duration from `TradePlan.CreatedAt`. `AutoTrader.resolvePositionStartTime()` currently uses:

1. `AutoTrader.positionFirstSeenTime` in memory.
2. `TradePlan.CreatedAt` if a plan exists.
3. `time.Now()` as fallback.

`TradePlan.CreatedAt` is persisted, so normal bot-opened positions survive restart. The weak point is the fallback path when no plan exists.

## Proposed Behavior

Add `position_start_times` to `decision.PersistentData`, keyed by `trader_id:symbol:side`, with legacy fallback keys for compatibility.

`AutoTrader.resolvePositionStartTime()` will use this order:

1. In-memory `positionFirstSeenTime`.
2. Existing `TradePlan.CreatedAt`.
3. Persisted `position_start_times`.
4. Exchange-provided position timestamp from the raw position map.
5. Current time as last resort.

The selected timestamp is written back to both memory and `position_start_times`, so the next restart has a durable source even if the plan is later unavailable.

## Exchange Timestamp Fallback

`AutoTrader` will read common position timestamp fields from the position map:

- `openTime`
- `entryTime`
- `positionTime`
- `createTime`
- `updateTime`

The Aster adapter will pass through `updateTime` from `/fapi/v3/positionRisk` when present. Timestamps are normalized to milliseconds.

## Cleanup

`decision.OnPositionClosedScoped()` and `decision.OnPositionClosedSimpleScoped()` remove the matching `position_start_times` entry. This covers normal closes, emergency closes, and auto-close reconciliation paths that already call these callbacks.

## Observability

When a start time is resolved from the persistent fallback, exchange fallback, or current-time fallback, log the source. This makes future restart investigations faster.
