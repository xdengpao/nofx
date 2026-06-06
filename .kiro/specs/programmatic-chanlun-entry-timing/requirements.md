# Programmatic Chanlun Entry Timing Requirements

## Background

The current programmatic Chanlun engine can identify useful market structure, but the identified structure is often too old to be used as a live open signal.

Observed on 161 `aster_deepseek` after the signal freshness guard deployment:

- `BTCUSDT sell2 short`: signal close `2026-05-17 20:59:59`, decision close `2026-05-18 15:59:59`, age 19 1h candles. Current price had crossed below TP, so the short was rejected as `target_already_crossed`.
- `SOLUSDT sell2 short`: age 10 1h candles. Current price had crossed below TP.
- `ETHUSDT buy2 long`: age 14 1h candles. Current price was already below the original stop side, so `SL < current < TP` was false.
- `XAGUSDT sell2 short`: age 18 1h candles. Current price was already above the short stop side or outside the original structure range.
- `CLUSDT sell1 short`: age 5 1h candles, rejected by the hard lifetime rule.

These rejections are correct. Relaxing the freshness guard would allow chasing missed moves or opening after invalidation. The root issue is upstream: the engine currently treats a structural signal derived from the latest confirmed segment as an open candidate, even when that segment ended many candles before the current decision candle.

The strategy must separate:

- structure/background signal: what the 1h Chanlun structure says;
- executable entry trigger: a fresh event that is tradable at the current price;
- continuation strategy: a separate optional strategy for trend continuation after the original structural target has been crossed.

## Requirements

### R1 Separate Structure Signals From Entry Triggers

WHEN the Chanlun engine detects `buy1/buy2/buy3/sell1/sell2/sell3`, THE system SHALL classify the result as a structure signal by default.

THE system SHALL NOT convert a structure signal directly into an open/add decision unless an executable entry trigger exists.

The entry trigger SHALL carry its own:

- `entry_trigger_id`;
- `entry_trigger_type`;
- `entry_trigger_timeframe`;
- `entry_trigger_close_time`;
- `entry_window_state`;
- `entry_reference_price`;
- `entry_invalidated` flag and reason when applicable.

The original structure signal id SHALL remain available for charting, audit, and lineage.

### R2 Fresh Event Requirement

WHEN a structure signal is evaluated for opening, THE system SHALL require a fresh event.

A fresh event MAY be one of:

- a newly confirmed 1h structure segment that ended at the latest closed trade candle;
- a 15m component trigger that confirms the direction inside the current 1h candle;
- a configured pullback/retest event that occurs after the structure signal and before the structure target is crossed.

The system SHALL NOT treat an old segment that still remains the last segment as a new open signal on every later 1h candle.

The default rule SHALL be:

- if `signal_close_time < latest_trade_close_time`, the structure signal is background only unless a fresh lower-timeframe trigger exists;
- if no fresh trigger exists, THE system SHALL emit diagnostics and markers but SHALL NOT produce an open/add decision.

### R3 Entry Window Validation

WHEN an entry trigger exists, THE system SHALL validate the current market price against an entry window before open gate validation.

For long entries:

- current price SHALL be above stop loss;
- current price SHALL be below take profit or structure target;
- current price SHALL leave enough remaining net RR after costs;
- current price SHOULD be inside a configurable entry band derived from the structure range or ATR.

For short entries:

- current price SHALL be below stop loss;
- current price SHALL be above take profit or structure target;
- current price SHALL leave enough remaining net RR after costs;
- current price SHOULD be inside a configurable entry band derived from the structure range or ATR.

If current price is already at or beyond the structure target, THE system SHALL reject the open as a missed structure entry and SHALL NOT recalculate a new TP by default.

### R4 Lower-Timeframe Confirmation

WHEN the 1h structure signal is background-only but direction remains relevant, THE system MAY use closed 15m candles to create an entry trigger.

The lower-timeframe trigger SHALL use only closed 15m candles.

The trigger MAY include:

- breakout continuation in the structure direction;
- pullback to a valid entry zone followed by direction resumption;
- 15m MA/DI alignment in the structure direction;
- 15m invalidation failure, for example failure to reclaim the short stop side.

`preview_2x15m` SHALL remain watchlist-only by default.

`preview_3x15m` MAY allow pilot entry only when explicitly enabled and only with capped risk.

Confirmed 1h signals SHALL remain canonical for structure lineage, not necessarily for immediate full-size entry.

### R5 Avoid Replayed Opens

THE system SHALL maintain state to prevent repeated open attempts from the same stale structure.

The dedupe/suppression model SHALL distinguish:

- executed entry triggers;
- rejected entry triggers;
- background-only structure signals;
- suppressed stale or missed structure signals.

WHEN a structure signal is background-only, THE system SHALL NOT write it to `executed_signals`.

WHEN a fresh entry trigger later appears for the same structure signal, THE system SHALL allow evaluation under a new `entry_trigger_id`.

### R6 Configurability

The programmatic strategy config SHALL support an entry timing policy.

The policy SHALL include defaults for:

- whether structure signals can open directly;
- required fresh-event mode;
- allowed lower-timeframe trigger types;
- entry zone width or ATR multiple;
- minimum remaining net RR;
- maximum trigger age;
- pilot risk fraction;
- minimum trigger confidence;
- whether trend continuation after target-crossed is disabled, report-only, or handled by a separate continuation module.

Safe defaults SHALL preserve current safety:

- no direct open from old 1h structure signals;
- no open after target crossed;
- no full-size open from preview by default;
- existing open gate, ADX/DI, BTC environment, risk budget, and position limit checks remain mandatory.

### R7 Observability

Decision logs, `cot_trace`, strategy signal API, and signal markers SHALL expose enough detail to explain why no open occurred.

Diagnostics SHALL distinguish:

- `structure_background_only`;
- `waiting_for_fresh_entry_trigger`;
- `entry_window_missed`;
- `target_already_crossed`;
- `entry_trigger_expired`;
- `entry_trigger_rejected_by_open_gate`;
- `entry_trigger_ready`.

Signal reports SHALL include:

- structure close time;
- latest trade close time;
- entry trigger close time when present;
- structure age;
- trigger age;
- current price;
- stop loss;
- take profit;
- remaining net RR;
- source layer (`structure`, `preview_signal`, `entry_trigger`, `confirmed_1h`).

### R8 Safety And Trading Semantics

THE system SHALL prefer missing a trade over opening after the original structure target has already been reached.

THE system SHALL NOT silently transform a missed Chanlun structure entry into a trend-continuation trade.

IF trend continuation entries are desired, THEY SHALL be implemented as a separate strategy module with separate signal ids, TP/SL logic, validation, and tests.

The existing freshness guard SHALL remain a final safety layer even after entry trigger refactoring.

### R9 Backtest And Calibration Readiness

The implementation SHALL make it possible to replay historical klines and compare:

- structure-only signals;
- fresh entry triggers;
- rejected stale structure signals;
- missed targets;
- valid entries that pass all gates.

The replay output SHOULD include enough fields to calibrate entry timing thresholds without relying on live logs only.

