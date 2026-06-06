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
- [x] Add dry-run reconciliation output for local and 161 samples. 本机 dry-run 已完成；161 已在部署后用临时 Docker builder 只读挂载 `decision_logs` 执行新版 replay：`record_count=1543`, `raw_close_actions=9`, `deduplicated_close_actions=5`, `duplicate_close_count=4`, `balance_delta=-1.26401558`, `rr_rejection_rate=67.95%`。
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

- [x] Deploy fixes to one instance first. 已提交并推送 `94285cd` 到 `origin/jzhbnofxdev`，161 `/home/ubuntu/appai3/nofx` 已拉取该提交并重建重启；后端 `/health` 返回 `ok`，前端返回 HTTP 200。
- [!] Run in safe mode for at least 24 hours. 已启动部署后安全观测窗口；截至 2026-05-16 13:01:50 +0800，161 生成 4 个新周期，动作为 wait/open_rejected 后 wait，未开新仓。该任务必须等待真实 24 小时样本，不能在本轮伪造完成。
- [x] Compare raw vs deduplicated replay reports after deployment. 161 部署后 replay 已完成：原始平仓 9、去重平仓 5、重复候选 4（`BCHUSDT_short`, `BNBUSDT_long`, `BTCUSDT_short`, `ETHUSDT_short`），拒绝桶 `rr=106`, `btc_gate=34`, `confidence=12`, `invalidation=1`, `other=3`。
- [!] Confirm no new duplicate `AUTO_CLOSE_DETECTED` records enter statistics. 部署后 4 个周期内 `AUTO_CLOSE_DETECTED=0`、`close_long/close_short=0`，暂无新增重复平仓证据；仍需至少一个真实平仓事件或 24 小时窗口确认。
- [!] Review win rate, PF, MFE giveback, and rejection buckets before enabling balanced/active behavior. 当前 161 replay 显示 recent_3 胜率 0%、PF 0、recent_10 胜率 20%、PF 0.3115，且最近 3 笔均亏、loss streak=4；拒绝仍以 RR 为主。样本尚未覆盖 24 小时安全运行，不建议启用 balanced/active。
