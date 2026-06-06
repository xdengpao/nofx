# Programmatic Confidence And Execution State Fix Design

## Confidence Calculation

`strategy/chanlun/signals.go` will extend `SignalInput` with market context. `buildSignal` will call a local confidence helper and store both the numeric score and factor diagnostics in `SignalDiagnostics.Metrics`.

The helper will start from a signal-type baseline, then apply bounded adjustments:
- stronger buy/sell continuation signals receive higher base than reversal signals;
- ADX and DI alignment increase score only when direction agrees with the intended side;
- counter-DI and counter-trend market state reduce score;
- aligned market state, momentum, divergence, and MA kiss provide small positive adjustments;
- missing market data degrades but does not panic.

Scores are clamped to a conservative range so malformed inputs cannot produce impossible values.

## State Marking

`evaluateMainSignals` will use a read-only `HasExecutedSignal` check to skip already executed signal ids. It will no longer call `MarkExecuted` before validation.

`OnExecutionResult` will mark a signal as executed only after a successful `open_long`, `open_short`, `add_long`, or `add_short` final action. Rejected, failed, held, or skipped signals will update markers but not `executed_signals`.

For legacy runtime state polluted by the old pre-validation write, `HasExecutedSignal` will treat an `executed_signals` entry as retryable when the latest marker for that signal is `rejected` or `failed`. A later successful open/add can overwrite the stale entry and restore normal dedupe behavior.

## Testing

Add focused tests for:
- aligned trend signals getting dynamic confidence above the short open gate baseline;
- counter-direction signals receiving lower confidence;
- execution state not being written on failed execution;
- execution state being written after successful open/add execution.
