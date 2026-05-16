# Live Loss Diagnosis Optimization Tasks

## Phase 1: Evidence and Baseline

- [x] Read NOFX steering docs and relevant trading architecture.
- [x] Analyze local `config.json` without exposing secrets.
- [x] Analyze local `decision_logs/aster_deepseek` and `data/trade_plans.json`.
- [x] Connect to 161 and analyze remote logs/order history snapshots.
- [x] Identify shared failure pattern across local and 161.
- [x] Produce Spec requirements/design/tasks documents.

## Phase 2: Close Event Correctness

- [x] Add tests proving manual close does not produce snapshot `AUTO_CLOSE_DETECTED` on the next cycle.
- [x] Add tests proving order-tracker auto-close still records exactly one closed trade.
- [x] Add lifecycle close ledger or equivalent state to `AutoTrader`.
- [x] Remove closed symbol/side from `lastPositions` after successful `close_long`/`close_short`.
- [x] Guard `detectAutoClosedPositions()` against recently manually closed lifecycle keys.
- [x] Add close-event source/metadata handling so exchange-confirmed auto-close can be counted without an active plan, while no-plan snapshot fallback is not counted.
- [x] Guard `OnPositionClosedScoped()` or its replacement writer against no-plan/no-exchange-metadata statistical updates.
- [x] Re-run targeted trader and decision tests.

## Phase 3: Replay and Statistics Repair

- [x] Extend replay report with raw vs deduplicated closed trade metrics.
- [x] Add duplicate close candidate detection by trader/symbol/side/lifecycle.
- [x] Add balance delta extraction from account snapshots.
- [x] Add optional read-only order/trade history reconciliation using `GetOrderHistory` / `GetTradeHistory` snapshots or exported JSON.
- [x] Add rejection reason bucketing for RR, BTC gate, confidence, invalidation, and execution errors.
- [!] Add dry-run reconciliation output for local and 161 samples. 本轮已完成本机 dry-run；161 新版 replay 输出需要先部署新代码或复制远端日志后执行，不能在未上线代码上伪造通过。
- [x] Decide whether to write repaired statistics file or only reset statistics after deployment. 决策：不改写原始日志，部署后只做统计重置/重建，必要时另写 repaired 文件。

## Phase 4: Loss Mode Guardrails

- [x] Define deduplicated loss-mode state and config defaults.
- [x] Feed loss-mode state into `decision.Context`.
- [x] Enforce loss-mode max risk, max positions, daily open cap, and min confidence.
- [x] Block high beta altcoin longs in loss mode unless BTC higher timeframe is supportive.
- [x] Update tests that currently assert old rolling gates are ignored, or keep them and add separate tests proving loss mode is the only new blocker.
- [x] Add tests for loss-mode trigger, recovery, and interaction with existing open gates.

## Phase 5: Candidate and Prompt Quality

- [x] Mark or exclude non-executable high beta long candidates when BTC gate is hard-blocking.
- [x] Add exact net RR formula to AI prompt.
- [x] Add candidate-level minimum TP/maximum SL guidance.
- [x] Add replay metric comparing RR rejection rate before/after prompt changes.
- [x] Add tests around prompt candidate ordering under BTC bearish gate.

## Phase 6: Exit Policy Calibration

- [x] Add tests reproducing observed MFE giveback cases.
- [x] Add configurable breakeven/lock-profit behavior at 1R or leveraged PnL threshold.
- [x] Tighten no-momentum soft stop thresholds for small-account live mode.
- [x] Ensure partial close is emitted only when Aster min notional can be satisfied.
- [x] Run `go test ./decision -run TakeProfit`.

## Phase 7: Deployment and Monitoring

- [!] Deploy fixes to one instance first. 需要提交/推送并由部署窗口执行；本轮未对实盘服务做重启。
- [!] Run in safe mode for at least 24 hours. 需要真实时间窗口，无法在单次执行中完成。
- [!] Compare raw vs deduplicated replay reports after deployment. 依赖部署后新日志。
- [!] Confirm no new duplicate `AUTO_CLOSE_DETECTED` records enter statistics. 依赖部署后至少一个扫描周期和后续平仓事件。
- [!] Review win rate, PF, MFE giveback, and rejection buckets before enabling balanced/active behavior. 依赖 24h safe-mode 样本。
