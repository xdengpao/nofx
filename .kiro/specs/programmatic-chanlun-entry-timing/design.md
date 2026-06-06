# Programmatic Chanlun Entry Timing Design

## Problem Summary

The current Chanlun implementation identifies the last confirmed structural signal from the latest segments and may convert it directly into an open decision.

This causes a mismatch:

- Chanlun structure confirmation is delayed by fractal/stroke/segment rules.
- `SignalCloseTime` is the segment end time, not the current decision candle.
- The same old segment can remain the latest segment for many later 1h candles.
- SL/TP are derived from the old segment high/low.
- By the time the open decision is evaluated, current price is often outside the original entry window.

The recently added freshness guard correctly rejects these cases, but it also reveals that the strategy has no reliable executable trigger. It finds structure, then usually arrives too late.

## Design Goal

Turn the programmatic Chanlun engine into a two-step model:

1. Detect and persist structure signals.
2. Open only when a fresh entry trigger validates that the structure is still tradable at the current price.

This keeps Chanlun useful as a structure model while preventing stale structure from being treated as a live trade instruction.

## Current Flow

Current simplified flow:

1. Load closed klines for 3m, 15m, 1h, 4h.
2. Build 1h normalized candles, fractals, strokes, segments, centers.
3. `DetectSignals` checks the last three segments.
4. `buildSignal` creates `buy/sell` signal with:
   - `price = segment.End`;
   - long `SL = segment.Low`, `TP = segment.High`;
   - short `SL = segment.High`, `TP = segment.Low`;
   - `SignalCloseTime = segment.EndTime`.
5. `analyzeMainSignal` sets `DecisionCloseTime = latest 1h close`.
6. `signalToMainDecision` converts the signal to `open_long/open_short`.
7. freshness guard and open gate validate.

The failure occurs between steps 4 and 6: an old segment is still allowed to become a current open decision.

## Proposed Flow

New simplified flow:

1. Detect 1h structure signals.
2. Store structure signals and markers as `source_layer=structure`.
3. Evaluate whether each structure signal has a fresh entry trigger.
4. If no fresh trigger exists, emit `structure_background_only` or `waiting_for_fresh_entry_trigger`.
5. If a fresh trigger exists, build an entry decision from the trigger, not from the old segment alone.
6. Validate entry window, freshness guard, open gate, risk, and final decision validation.
7. Record entry trigger execution/rejection separately from structure signal detection.

## Core Concepts

### Structure Signal

A structure signal is a Chanlun interpretation of 1h structure.

It answers: "What does the latest confirmed structure imply?"

It does not answer: "Can I open at the current price now?"

Fields:

- `SignalID`;
- `SignalType`;
- `Direction`;
- `AnalysisTF`;
- `SignalCloseTime`;
- `StructureTarget`;
- `StopLoss`;
- `TakeProfit`;
- diagnostics and confidence.

### Entry Trigger

An entry trigger is a fresh tradable event linked to one structure signal.

It answers: "Is there a current, executable entry in the structure direction?"

Fields:

- `EntryTriggerID`;
- `ParentSignalID`;
- `TriggerType`;
- `TriggerTimeframe`;
- `TriggerCloseTime`;
- `EntryReferencePrice`;
- `CurrentPrice`;
- `EntryWindowState`;
- `RemainingNetRR`;
- `TriggerConfidence`;
- `InvalidationReason`.

Example trigger types:

- `new_structure_segment`;
- `preview_2x15m_watchlist`;
- `preview_3x15m_pilot`;
- `pullback_retest_resume`;
- `breakout_continuation`;
- `confirmed_1h_upgrade`.

### Entry Window

An entry window defines where current price is still tradable relative to the original structure.

For a short:

- hard shape: `SL > current > TP`;
- not target-crossed: `current > TP`;
- remaining net RR above threshold;
- optional max chase distance from signal price or structure midpoint;
- optional minimum distance from stop to avoid immediate invalidation.

For a long:

- hard shape: `SL < current < TP`;
- not target-crossed: `current < TP`;
- remaining net RR above threshold;
- optional max chase distance from signal price or structure midpoint;
- optional minimum distance from stop.

## Options Evaluated

### Option A: Increase Hard Lifetime

Raise `max_lifetime_candles` from 4 to a larger number.

Rejected.

This would allow more old structures through, but observed failures already crossed target or invalidated SL/TP. It increases bad chases without solving stale structure replay.

### Option B: Use Current Price To Rewrite TP/SL

When the original structure is no longer tradable, calculate a new ATR-based stop and target.

Rejected as a default behavior.

This changes the strategy from structure entry to trend continuation. It may be useful later, but it needs a separate module, ids, risk model, and backtest.

### Option C: Only Open On Newly Confirmed Structure Segment

Convert to open only when `signal_close_time == latest_trade_close_time`.

Partially recommended.

This prevents old structures from replaying. However, it may still miss trades because segment confirmation can happen after the best entry area. It should be the direct 1h open rule, but not the only entry path.

### Option D: Structure Background + 15m Entry Trigger

Use 1h Chanlun as direction/background and closed 15m candles for fresh entry triggers.

Recommended.

This respects Chanlun structure while reducing lateness. It also matches the previous preview requirement: 2 closed 15m candles for watchlist, 3 closed 15m candles for optional pilot.

### Option E: Keep Current Behavior With Better Suppression

Suppress repeated stale rejects.

Already done as an operational noise fix, but insufficient as a trading strategy.

## Recommended Design

### 1h Structure Layer

The structure layer continues to run on closed 1h candles.

Output:

- structure markers;
- latest structure state per symbol;
- background bias;
- invalidation levels;
- target levels.

Direct open from this layer is allowed only if:

- `signal_close_time == latest 1h close`, or within a configured direct-open tolerance;
- current price remains inside entry window;
- remaining net RR passes;
- open gate passes.

Default direct-open tolerance: `0` trade candles for direct structure opens.

### 15m Entry Trigger Layer

The trigger layer evaluates closed 15m candles after a 1h structure signal exists.

Default trigger candidates:

- `preview_2x15m`: watchlist marker only;
- `preview_3x15m`: optional pilot only when enabled;
- `pullback_retest_resume`: price pulls back into an entry zone then resumes in structure direction;
- `confirmed_1h_upgrade`: after 1h close confirms the structure and entry remains valid.

The trigger layer must not use unclosed 15m data.

### Entry Decision Construction

The open decision should be built from:

- parent structure signal for direction and invalidation levels;
- trigger close time for freshness;
- current price for entry validation;
- configured TP/SL policy.

Default TP/SL behavior:

- keep original structure SL/TP if current price remains inside valid range and remaining net RR is enough;
- reject if target crossed;
- reject if SL/TP shape invalid;
- do not rewrite TP/SL unless a separate continuation module is enabled.

### State Model

Add or extend state to track:

- structure signals;
- entry triggers;
- executed entry trigger ids;
- rejected entry trigger ids;
- background-only structure ids;
- suppressions for missed/invalidated structure entries.

Recommended key model:

- `structure_signal_id`: stable id based on 1h structure;
- `entry_trigger_id`: hash of trader, symbol, parent signal id, trigger type, trigger timeframe, trigger close time, config hash.

This allows a new 15m trigger for the same structure signal to be evaluated while preventing replay of the same stale trigger.

### Observability

The signal API should show structure and entry trigger lineage:

```json
{
  "signal_id": "structure-id",
  "source_layer": "structure",
  "status": "background",
  "entry_triggers": [
    {
      "entry_trigger_id": "trigger-id",
      "trigger_type": "pullback_retest_resume",
      "status": "ready",
      "trigger_close_time": 1779090299999,
      "entry_window_state": "valid",
      "remaining_net_rr": 3.1
    }
  ]
}
```

Open rejection logs should say whether rejection came from:

- stale structure;
- missing trigger;
- invalid entry window;
- target crossed;
- open gate;
- final validation.

## Configuration Sketch

```json
{
  "programmatic_strategy": {
    "entry_timing": {
      "enabled": true,
      "direct_structure_open": false,
      "direct_open_max_age_candles": 0,
      "require_fresh_trigger": true,
      "trigger_timeframe": "15m",
      "allowed_trigger_types": [
        "preview_2x15m_watchlist",
        "preview_3x15m_pilot",
        "pullback_retest_resume"
      ],
      "entry_zone": {
        "mode": "structure_range",
        "max_chase_ratio": 0.35,
        "min_remaining_net_rr": 2.5
      },
      "pilot": {
        "enabled": false,
        "risk_fraction": 0.3,
        "min_confidence": 90
      },
      "continuation_after_target_crossed": "disabled"
    }
  }
}
```

## Migration Plan

Phase 1: Report-only entry timing.

- Keep existing guard.
- Mark old 1h structures as background-only.
- Emit trigger diagnostics without changing opens.
- Verify that old rejected signals stop appearing as open candidates.

Phase 2: Enforce fresh trigger requirement.

- Disable direct open from stale 1h structure.
- Convert only valid entry triggers into open decisions.
- Add tests for no-trigger wait behavior.

Phase 3: Optional pilot triggers.

- Enable `preview_3x15m` pilot only behind config.
- Keep risk capped and logs explicit.

Phase 4: Replay/backtest calibration.

- Replay recent history to tune entry zone, trigger types, and confidence thresholds.

