# Programmatic Signal Staleness Guard Cross Validation

## Scope

This validation cross-checks the spec against:
- observed 161 runtime behavior for `SOLUSDT` and `XAGUSDT`;
- current programmatic strategy code paths;
- existing config normalization and runtime policy mapping;
- state semantics for `executed_signals` and signal markers.

## Findings

### V1 Requirements Match The Incident

The spec correctly addresses the observed failure:
- both signals had high confidence and passed open-gate direction checks;
- both were rejected because current price had crossed below the short structure target;
- target-crossed and invalid-structure guards are the correct earlier rejection point.

Status: pass.

### V2 Strict Age-Only Rejection Was Correctly Replaced

The spec no longer treats 1-2 trade candles as a hard rejection rule. It now classifies signals as `fresh`, `aged`, or `expired`, and uses target-crossed, price structure, and remaining RR as the hard checks for non-expired signals.

Status: pass.

### V3 Preview Layer Is Feasible But Must Stay Provisional

The preview design is feasible with existing `1h` trade and `15m` sub timeframe data. It is correctly limited to closed 15m candles and does not equate a partial 1h candle with a confirmed 1h signal.

Status: pass with constraint: preview opens must remain opt-in and capped.

### V4 State Semantics Needed A Separate Suppression Concept

The previous spec said stale signals must not write `executed_signals` and also must not be repeatedly attempted. Those two requirements conflict unless a separate suppression state exists.

Resolution: requirements and design now specify stale/target-crossed retry suppression separate from `executed_signals`.

Status: fixed in spec.

### V5 Config Mapping Needed Explicit Implementation Coverage

The current code has separate external config, normalized profile, runtime policy, and manager mapping layers:
- `config.ProgrammaticStrategyConfig`;
- `config.ProgrammaticStrategyProfile`;
- `decision.ProgrammaticStrategyPolicy`;
- `manager.decisionProgrammaticStrategyPolicy`.

The previous spec named the JSON config shape but did not explicitly require all mapping layers or config hash coverage.

Resolution: requirements and design now require config representation in all layers and `config_hash` inclusion.

Status: fixed in spec.

### V6 Existing Final Validation Remains Necessary

The proposed guard is not a replacement for `validateOpenDecisionWithOptions`; it improves earlier explainability and avoids repeated stale attempts. The existing final validation remains the last safety check.

Status: pass.

## Implementation Readiness

The spec is ready for implementation planning. The highest-risk implementation areas are:
- preview/confirmed signal lineage reconciliation;
- avoiding duplicate opens between preview and confirmed layers;
- preserving chart markers while suppressing repeated stale attempts;
- ensuring config defaults do not accidentally enable pilot trading.

Recommended implementation order:
1. Config structs, normalization, runtime policy mapping, and tests.
2. Freshness classification and target-crossed guard.
3. Suppression state separate from `executed_signals`.
4. Preview watchlist markers.
5. Optional preview pilot opens.
6. Confirmation reconciliation and position-management integration.

