# Programmatic Signal Staleness Guard Requirements

## Background

161 `aster_deepseek` is running `decision_mode=programmatic`. After the dynamic confidence fix, new aligned short signals can reach high confidence and pass the open gate. On 2026-05-18 14:00, `SOLUSDT` and `XAGUSDT` produced high-confidence `open_short` candidates, but final validation rejected them with `做空止损必须>当前价>止盈`.

Runtime state confirmed that both signals were structurally valid when detected, but stale by the time they were evaluated:
- `SOLUSDT`: signal time `2026-05-18 05:59:59`, decision time `2026-05-18 13:59:59`, signal `SL=86.92`, `TP=85.89`, current close near `84.82`.
- `XAGUSDT`: signal time `2026-05-17 21:59:59`, decision time `2026-05-18 13:59:59`, signal `SL=75.87`, `TP=75.60`, current close near `74.94`.

For a short entry the system requires `stop_loss > current_price > take_profit`. In both cases price had already crossed below the structure target, so the open was a missed-target entry rather than a valid fresh entry.

To reduce missed entries, the strategy should add a lower-timeframe preview layer. The preview layer uses closed 15m candles from the current 1h window to estimate whether a 1h signal may form, without treating the estimate as equivalent to a confirmed closed-1h signal.

## Requirements

### R1 Signal Freshness Classification

WHEN a programmatic main signal is converted to an open/add decision, THE system SHALL evaluate signal freshness before final open validation.

Freshness SHALL be measured from `signal_close_time` to `decision_close_time` using the strategy trade timeframe.

The system SHALL NOT use a fixed 1-2 trade-candle age rule as the only reason to reject a signal.

The system SHALL classify freshness into at least:
- `fresh`: inside the soft freshness window;
- `aged`: older than the soft window but not expired;
- `expired`: older than the hard maximum lifetime.

The default soft freshness window SHOULD be conservative:
- `sell1/buy1`: 2 trade candles;
- `sell2/buy2`: 2 trade candles;
- `sell3/buy3`: 1 trade candle.

The default hard maximum lifetime SHOULD be less aggressive than the soft window:
- `sell1/buy1`: 4 trade candles;
- `sell2/buy2`: 4 trade candles;
- `sell3/buy3`: 2 trade candles.

WHEN a signal is `aged` but not `expired`, THE system SHALL allow it to continue only if price structure, missed-target, remaining reward/risk, open gate, and existing risk checks still pass.

WHEN a signal is `expired`, THE system SHALL reject or skip the open/add candidate unless a separately specified continuation strategy explicitly authorizes it.

### R2 Missed Target Guard

WHEN a programmatic short signal is evaluated, THE system SHALL reject the open/add candidate if the current price is already at or below the signal's structure target.

WHEN a programmatic long signal is evaluated, THE system SHALL reject the open/add candidate if the current price is already at or above the signal's structure target.

The rejection reason SHALL clearly distinguish this from generic SL/TP shape errors, for example `target_already_crossed` or `信号目标已穿越`.

### R3 Entry Price Structure Guard

WHEN a programmatic open/add candidate is built from a signal, THE system SHALL verify that the signal stop and target remain valid relative to the current market price before passing the candidate to the generic decision validator.

For long entries the required structure is `stop_loss < current_price < take_profit`.

For short entries the required structure is `stop_loss > current_price > take_profit`.

If the structure is invalid, THE system SHALL reject the candidate with a programmatic-specific reason and SHALL NOT rely only on the generic decision error.

### R4 Stale Signal State Handling

WHEN a signal is rejected because it is stale, target-crossed, or structurally invalid, THE system SHALL mark the signal marker as `rejected` with the exact reason.

THE system SHALL NOT write stale or target-crossed signals to `executed_signals`.

THE system SHALL maintain stale/target-crossed retry suppression separately from `executed_signals`.

THE system MAY keep the confirmed signal in state for chart/history visibility, but it SHALL NOT repeatedly attempt the same stale or target-crossed open every cycle.

The suppression SHALL be keyed by trader, symbol, signal id, action, and rejection reason code. It SHALL remain retryable for genuinely new signal ids, new preview phases, or a separately specified continuation strategy.

### R5 Configurability

The soft freshness window and hard maximum lifetime SHALL be configurable in the programmatic strategy policy.

The configuration SHALL allow per-signal-type overrides while keeping safe defaults when no explicit config is supplied.

The configuration SHOULD allow a confidence decay or stricter minimum-confidence adjustment for `aged` signals, without applying that decay to already `expired` signals.

The missed-target guard SHALL be enabled by default for programmatic open/add decisions.

The config SHALL be represented in both the external `programmatic_strategy` config model and the runtime `ProgrammaticStrategyPolicy`, and SHALL contribute to the programmatic `config_hash`.

### R6 Observability

Decision logs, `cot_trace`, strategy signal APIs, and marker state SHALL expose:
- `signal_close_time`;
- `decision_close_time`;
- age in trade candles;
- freshness state (`fresh`, `aged`, or `expired`);
- stale/target-crossed rejection reason;
- signal price, stop loss, take profit, current price, remaining reward, remaining risk, and remaining net RR used for validation.

The generic error `做空止损必须>当前价>止盈` SHALL remain available as a final safety check, but programmatic stale-signal rejections SHOULD be reported earlier with more actionable diagnostics.

### R7 Safety

THE system SHALL NOT silently recalculate a new take profit for a missed-target signal and then open a trade unless a separate trend-continuation strategy explicitly authorizes that behavior.

THE system SHALL prefer skipping missed entries over chasing after the structure target has already been reached.

THE system SHALL prefer price-structure and remaining-RR validation over age-only rejection for non-expired signals.

THE system SHALL preserve existing open gate, ADX/DI, BTC environment, risk budget, and position-limit checks.

### R8 Lower-Timeframe Preview Signal Layer

WHEN the trade timeframe is `1h` and the component timeframe is `15m`, THE system SHALL be able to build preview signals from closed 15m candles inside the currently forming 1h candle.

The preview layer SHALL NOT use an unclosed 15m candle as a confirmed input.

The preview layer SHALL support at least:
- `preview_2x15m`: after 2 closed 15m candles in the current 1h window;
- `preview_3x15m`: after 3 closed 15m candles in the current 1h window;
- `confirmed_1h`: after the full 1h candle closes.

`preview_2x15m` SHALL be treated as an early warning or watchlist signal by default and SHALL NOT open a full position by default.

`preview_3x15m` MAY open a pilot position only when explicitly enabled and only when stronger filters pass.

The pilot position risk SHALL be capped below the normal confirmed signal risk. The default pilot risk cap SHOULD be 25%-40% of the normal open risk.

WHEN the 1h candle closes, THE system SHALL reconcile preview signals with the confirmed 1h signal:
- if the confirmed signal still agrees, THE system MAY upgrade, add, or keep the position according to existing risk limits;
- if the confirmed signal disappears or reverses, THE system SHALL avoid further opens and MAY tighten risk or exit according to position-management rules;
- if the preview target has already been crossed, THE missed-target guard SHALL still reject chasing entries.

Preview signal diagnostics SHALL clearly mark `preview_phase`, `preview_source_timeframe`, `preview_closed_components`, and whether the signal is confirmed or provisional.

Preview opens SHALL preserve all existing open gate, BTC environment, ADX/DI, remaining RR, target-crossed, price-structure, risk budget, and position-limit checks.
