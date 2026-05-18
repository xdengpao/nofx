# Programmatic Signal Staleness Guard Design

## Problem Summary

The current programmatic path treats any detected structural signal as an open candidate if the signal id has not executed. That is too permissive when the latest segment ended several candles before the current decision candle.

`SOLUSDT` and `XAGUSDT` showed the failure mode:
- the signal direction and confidence were strong;
- open gate allowed the aligned short setup;
- the structure target had already been crossed before evaluation;
- final validation rejected the trade because `current_price` was already below `take_profit`.

The rejection protected capital, but it happened too late and with a generic message.

## Options Evaluated

### Option A: Rely On Existing Final Validation

Keep the current behavior and let `validateOpenDecisionWithOptions` reject invalid SL/TP shapes.

Pros:
- no code change;
- final safety remains correct.

Cons:
- logs are ambiguous and do not explain that the signal was stale or missed;
- high-confidence stale signals continue to look like almost-valid opens;
- repeated rejected markers make operations harder to interpret;
- this does not solve the root cause.

Verdict: insufficient.

### Option B: Recalculate SL/TP Around Current Price

When a target is already crossed, rewrite stop and target using ATR/profile distances and allow the trade if risk checks pass.

Pros:
- can turn strong trend context into more executable trades;
- uses existing strategy risk profile concepts.

Cons:
- changes the strategy from structure-entry to trend-continuation chase;
- can open after the original profit target has already been achieved;
- requires separate rules for continuation entries, invalidation, and backtest expectations;
- higher behavioral risk for live deployment.

Verdict: do not use as the default fix. This can be a future, separately specified continuation strategy.

### Option C: Strict 1-2 Candle Expiry

Reject every signal whose age exceeds 1-2 trade candles.

Pros:
- simple to implement and explain;
- would have rejected the observed `SOLUSDT` and `XAGUSDT` stale candidates;
- lowers chance of chasing old structural signals.

Cons:
- too blunt for Chanlun structure, because a segment or fractal may be confirmed several candles after the actual swing;
- ignores whether price is still inside a valid entry-to-target range;
- may reject valid pullback or second-entry opportunities where the original structure target is still intact;
- creates hidden coupling between timeframe, volatility, and signal type.

Verdict: useful as a soft freshness boundary, but not appropriate as the only hard rejection rule.

### Option D: Adaptive Freshness And Missed-Target Guard

Before converting or validating a main signal as an open/add decision, check signal age and current-price relation to target.

Pros:
- directly solves stale/missed-entry behavior;
- keeps strategy semantics intact;
- produces clear logs and markers;
- preserves existing open gate and risk checks;
- low implementation risk.

Cons:
- may skip some trend continuation trades that later keep moving;
- needs config defaults and tests across long/short signal types.

Verdict: recommended.

### Option E: Lower-Timeframe Preview Layer

Use closed 15m candles inside the current 1h window to preview a possible 1h signal before the full 1h candle closes.

Pros:
- addresses the upstream cause of missed entries by detecting opportunities earlier;
- keeps confirmed 1h logic intact;
- allows conservative staging: watchlist first, pilot position second, confirmation third;
- produces useful diagnostics before the final 1h signal is available.

Cons:
- preview signals can repaint when the remaining 15m candles change the 1h high, low, or close;
- a half-formed 1h candle is not equivalent to a closed 1h candle;
- requires separate marker states, dedupe keys, and reconciliation with confirmed signals;
- pilot entries need tighter risk caps and clear invalidation handling.

Verdict: recommended as a separate preview layer, not as a replacement for closed-1h confirmation.

## Age Rule Assessment

The original "reject after 1-2 trade candles" rule is directionally correct but operationally too strict as a hard rule.

It is reasonable as a `soft_age_candles` default because an entry signal should normally be acted on soon after it appears. However, Chanlun signals are not simple one-bar patterns. They are derived from fractals, strokes, segments, and centers; those structures can be confirmed after the price movement has already advanced. A strict 1-2 candle TTL would reduce bad chases, but it would also reject legitimate signals that remain structurally valid and still have enough reward left.

The better rule is:
- reject immediately when the target is already crossed;
- reject immediately when stop/target shape is invalid relative to current price;
- reject immediately when remaining net RR is below the existing minimum;
- use 1-2 candles as the soft freshness window;
- use a wider hard maximum lifetime only as the final expiry.

Default recommendation for the 1h trade timeframe:
- `buy1/sell1`: soft 2 candles, hard 4 candles;
- `buy2/sell2`: soft 2 candles, hard 4 candles;
- `buy3/sell3`: soft 1 candle, hard 2 candles.

This would still reject the observed cases:
- `SOLUSDT` age was about 8 trade candles and target had already been crossed;
- `XAGUSDT` age was about 16 trade candles and target had already been crossed.

But it avoids rejecting a 3-candle-old `sell2` that still has valid shape, strong DI alignment, and enough remaining reward/risk.

## Preview Signal Layer

The preview layer should reduce signal lateness without weakening the semantics of confirmed 1h signals.

### Synthetic Preview Candle

For a 1h trade timeframe and 15m component timeframe, build a provisional 1h candle only from closed 15m candles in the current 1h bucket:
- open: first closed 15m open in the hour;
- high: max high of closed 15m candles in the hour;
- low: min low of closed 15m candles in the hour;
- close: latest closed 15m close;
- close_time: latest closed 15m close time;
- preview phase: `preview_2x15m` or `preview_3x15m`.

The engine must not use an unclosed 15m candle as confirmed input. If live intra-candle data is later introduced, it should be marked separately as `live_preview` and should not share the same confidence or execution rules.

### Preview Phases

`preview_2x15m`:
- created after two closed 15m candles in the active 1h window;
- default behavior is watchlist/marker only;
- no full open by default;
- may prepare diagnostics, remaining RR, target-crossed state, and candidate priority.

`preview_3x15m`:
- created after three closed 15m candles in the active 1h window;
- may allow a pilot open only when explicitly enabled;
- pilot risk defaults to 25%-40% of normal risk;
- must require stronger filters than confirmed 1h, including ADX/DI alignment, BTC environment compatibility, remaining net RR, and no target-crossed state.

`confirmed_1h`:
- generated after the full 1h candle closes;
- remains the canonical structural signal;
- may confirm, upgrade, add to, keep, or reject a preview-originated position according to existing risk and position-management rules.

### Preview Reconciliation

Preview signal ids should not collide with confirmed signal ids. Include `preview_phase` and the component close time in preview stable ids.

When the 1h signal confirms:
- if direction and structure agree, the confirmed signal can adopt the preview lineage and decide whether to upgrade size;
- if the confirmed signal disappears, the preview marker should become `preview_invalidated`;
- if the confirmed signal reverses, the preview marker should become `preview_reversed` and no further same-side open should be attempted;
- if the preview target is crossed before confirmation, the existing missed-target guard should reject chasing or upgrading.

Preview-originated positions should carry metadata:
- `entry_source=preview`;
- `preview_phase`;
- `preview_signal_id`;
- `expected_confirm_close_time`;
- `normal_risk_fraction`.

Position management should be able to tighten or exit a preview-originated pilot if the confirmed 1h signal fails.

## State And Dedupe

Stale, target-crossed, and invalid-structure rejections must not be stored in `executed_signals`, because they are not real executions. They also must not be reattempted every cycle.

Use a separate suppression concept in strategy state:
- key: trader id, symbol, signal id, action, rejection reason code;
- value: rejected_at, signal_close_time, decision_close_time, freshness_state, current_price, stop_loss, take_profit;
- scope: suppress the same open/add attempt for the same signal lineage;
- retry: allow genuinely new signal ids, new preview phases, or explicitly configured continuation entries.

The existing marker state remains the user-facing audit trail. The suppression state is an execution-control aid and should not be confused with successful execution dedupe.

## Recommended Flow

1. `analyzeMainSignal` continues to detect and store structural signals.
2. The preview layer may generate provisional `preview_2x15m` and `preview_3x15m` markers from closed 15m candles before the 1h candle closes.
3. Before appending a signal-derived open/add decision, the engine runs a programmatic pre-open signal guard.
4. The guard checks:
   - age in trade candles;
   - freshness state (`fresh`, `aged`, `expired`);
   - preview phase and whether the signal is provisional;
   - current price versus structure target;
   - current price versus stop/target shape;
   - remaining reward/risk and net RR using current price;
   - optional confidence decay or stricter min-confidence treatment for `aged` signals.
5. If the signal fails, emit an `OpenRejection` or marker rejection with a programmatic reason.
6. If the signal passes, continue through existing open gate and final validation.
7. When the confirmed 1h candle closes, reconcile any preview lineage before allowing upgrade/add decisions.

## Implementation Mapping

The config surface maps to existing code in three layers:
- external config: add `SignalFreshness` and `PreviewSignals` to `config.ProgrammaticStrategyConfig`;
- normalized profile: add matching fields to `config.ProgrammaticStrategyProfile` and include them in `hashProgrammaticProfile`;
- runtime policy: add matching fields to `decision.ProgrammaticStrategyPolicy` and map them in `manager.decisionProgrammaticStrategyPolicy`.

Normalization should live in `config/programmatic.go` beside the existing programmatic sub-config normalizers. Config tests should verify defaults, invalid values, partial override behavior, and hash changes.

Engine behavior should live in `strategy/chanlun`:
- guard and freshness classification near `evaluateMainSignals` / `signalToMainDecision`;
- marker conversion and rejection reasons near existing marker helpers;
- preview candle construction in a local helper that only consumes closed component candles.

Decision-layer validation should remain the final safety net. The programmatic guard should produce earlier, more specific rejection reasons.

## Guard Rules

For `open_short` and `add_short`:
- expired if `decision_close_time - signal_close_time` exceeds the configured hard maximum lifetime;
- aged if the age exceeds the soft freshness window but remains inside the hard maximum lifetime;
- missed target if `current_price <= signal.take_profit`;
- invalid shape if `signal.stop_loss <= current_price` or `signal.take_profit >= current_price`.

For `open_long` and `add_long`:
- expired if the age exceeds the configured hard maximum lifetime;
- aged if the age exceeds the soft freshness window but remains inside the hard maximum lifetime;
- missed target if `current_price >= signal.take_profit`;
- invalid shape if `signal.stop_loss >= current_price` or `signal.take_profit <= current_price`.

For `aged` signals:
- apply a configurable confidence decay or minimum-confidence uplift;
- require remaining net RR to pass the same threshold used by open validation;
- continue to require all existing open gate and risk checks.

The guard should use `marketData.CurrentPrice` when available. If the decision is tied to a closed candle, diagnostics may also include the latest closed candle close for auditability.

## Config Shape

Add optional fields under the programmatic strategy policy:

```json
{
  "signal_freshness": {
    "enabled": true,
    "soft_age_candles": 2,
    "max_lifetime_candles": 4,
    "soft_age_by_signal_type": {
      "buy3": 1,
      "sell3": 1
    },
    "max_lifetime_by_signal_type": {
      "buy3": 2,
      "sell3": 2
    },
    "missed_target_guard": true,
    "confidence_decay_per_aged_candle": 3,
    "min_remaining_net_rr": 2.5
  },
  "preview_signals": {
    "enabled": true,
    "component_timeframe": "15m",
    "trade_timeframe": "1h",
    "watch_after_closed_components": 2,
    "pilot_after_closed_components": 3,
    "allow_pilot_open": false,
    "pilot_risk_fraction": 0.3,
    "pilot_min_confidence": 90,
    "require_confirmed_upgrade": true
  }
}
```

If omitted, defaults should keep the guard enabled with a conservative max age.

## Observability

Each rejection should include a stable reason code and human text:
- `stale_signal`;
- `target_already_crossed`;
- `invalid_signal_price_structure`.

Diagnostics should include:
- `signal_id`;
- `signal_type`;
- `direction`;
- `signal_close_time`;
- `decision_close_time`;
- `age_candles`;
- `freshness_state`;
- `preview_phase`;
- `preview_source_timeframe`;
- `preview_closed_components`;
- `preview_confirmed`;
- `current_price`;
- `signal_price`;
- `stop_loss`;
- `take_profit`;
- `structure_target`;
- `remaining_reward_pct`;
- `remaining_risk_pct`;
- `remaining_net_rr`.

## Test Plan

Add focused tests for:
- short signal rejected when current price is below target;
- long signal rejected when current price is above target;
- expired signal rejected by age even if price is still inside SL/TP shape;
- aged but non-expired signal can continue when shape and remaining RR are valid;
- aged signal receives configured confidence decay or stricter threshold diagnostics;
- `preview_2x15m` emits marker/watchlist diagnostics and does not open by default;
- `preview_3x15m` can create only a capped pilot decision when explicitly enabled;
- preview signal ids do not collide with confirmed 1h signal ids;
- confirmed 1h reconciliation upgrades, invalidates, or reverses preview lineage correctly;
- fresh signal inside valid price structure still passes to existing validation;
- rejected stale/missed signals are not written to `executed_signals`;
- marker reason preserves the programmatic rejection code.
