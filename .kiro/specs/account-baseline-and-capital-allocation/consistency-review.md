# Account Baseline And Capital Allocation Consistency Review

## Review Scope

本轮交叉验证覆盖：

- Spec: `requirements.md`、`design.md`、`tasks.md`
- Current code: `config/config.go`、`manager/trader_manager.go`、`trader/auto_trader.go`、`trader/aster_trader.go`、`decision/*`、`logger/decision_logger.go`、`api/server.go`、`web/src/App.tsx`、`web/src/components/EquityChart.tsx`
- 161 current evidence:
  - `/api/status?trader_id=aster_chanlun_v2`: `initial_balance=10`
  - `/api/account?trader_id=aster_chanlun_v2`: `total_equity=44.4847`、`cost_basis=44.4130`、`total_pnl=0.0718`
  - `/api/equity-history?trader_id=aster_chanlun_v2`: early legacy point can report `cost_basis=10` and `total_pnl_pct=344%`

## Verdict

交叉验证通过，但发现 3 个需要回补的缺口，已同步修正到 requirements/design/tasks：

1. Allocation 需求要求扣减已有 open risk，但原设计/任务只写了 margin used。
2. Decision log 任务只补了 baseline 字段，漏了 allocation 字段。
3. Equity history 若直接让前端使用旧日志的 `total_pnl_pct`，会把 161 早期 `10 -> 44.48` 旧记录显示为当前策略 300%+ 收益。

修正后，Spec 与现有代码边界一致，可以进入实现阶段。

## Requirement Cross-Check

### R1: 前端区分交易所净值、策略基准、配置初始资金

Status: consistent.

Current code:

- `trader/aster_trader.go` reads Aster USDT `balance` and `availableBalance`.
- `trader/auto_trader.go` returns `total_equity = wallet + unrealized`.
- `web/src/App.tsx` currently labels `total_equity` simply as total equity and shows `cost_basis` only under PnL.

Spec fit:

- R1 does not require changing exchange-equity semantics.
- Tasks 4-6 cover type sync, card labels and chart baseline.

Implementation note:

- The UI should not imply `initial_balance=10` is a wallet cap unless capital allocation is enabled.

### R2: 后端账户 API 结构化资金语义

Status: consistent.

Current code:

- `/api/account` already returns old fields through `AutoTrader.GetAccountInfo()`.
- It lacks `equity_source`, `baseline_source`, `initial_balance_role`, and `strategy_baseline`.

Spec fit:

- Task 1 adds these fields while preserving old clients.
- No `trader.Trader` interface change is needed.

### R3: 净值曲线使用后端明确基准

Status: consistent after patch.

Current code:

- `EquityChart` uses `validHistory[0].total_equity` as implicit initial balance.
- `/api/equity-history` currently falls back missing `cost_basis` to configured `initial_balance`, which can make legacy records show large percentages.

Spec correction:

- Added requirement and tasks to mark legacy/unreliable history points.
- Frontend must not compute current PnL from the first valid equity point.
- Frontend must also avoid blindly displaying unreliable legacy `total_pnl_pct`.

Implementation note:

- Raw USDT equity chart is acceptable if labeled as exchange account equity.
- Strategy PnL/percent badges should use backend PnL fields from reliable baseline points.

### R4: 可选策略资金上限

Status: consistent after patch.

Current code:

- `decision.AccountInfo` has only display fields: `TotalEquity`, `AvailableBalance`, `MarginUsed`, etc.
- `decision.CalculateTotalRisk()` divides by `ctx.Account.TotalEquity`.
- `decision.CalculatePositionSizing()` receives account equity and available balance from the current account context.

Spec correction:

- Design now requires explicit sizing equity/available fields and allocation-aware risk denominator.
- Tasks now mention `CalculateTotalRisk()`, remaining risk budget, open gate normalization and sizing.

Implementation note:

- Do not mutate display `TotalEquity` to allocated capital. Add explicit sizing/allocation fields so UI and logs can explain both numbers.

### R5: 配置兼容性

Status: consistent.

Current code:

- `config.TraderConfig.InitialBalance` is required and validated as `>0`.
- New `capital_allocation` can be optional without changing existing configs.

Spec fit:

- New `capital_allocation` block is optional.
- Old config behavior remains unchanged.

Implementation note:

- Keep `initial_balance` required unless a separate migration changes the product contract.

### R6: 日志和决策记录对账

Status: consistent after patch.

Current code:

- `logger.AccountSnapshot` already stores `CostBasis`, `RealizedPnL`, and `PnLSource`.
- It lacks baseline source, equity source and allocation/sizing fields.

Spec correction:

- Tasks now explicitly add allocation fields to account snapshots and round-trip tests.

### R7: 测试覆盖 161 问题

Status: consistent.

Needed tests:

- Backend account test for `initial_balance=10` and exchange equity `44.48`.
- API history test for legacy missing `cost_basis` points.
- Frontend chart test for not using first equity as strategy baseline.
- Allocation disabled and enabled sizing tests.

## Design Cross-Check

### API Boundary

The design correctly keeps `/api/account` as the main semantic source. No new endpoint is required unless frontend complexity grows.

### Equity History

The original design was under-specified for old logs. It is now amended with `return_reliable` / `legacy_*` handling.

### Allocation

The design now separates:

- display equity: exchange account equity
- sizing equity: allocated capital when enabled
- available balance: exchange available
- sizing available: min(exchange available, allocation remaining)

This avoids the dangerous shortcut of replacing `TotalEquity` globally.

## Task Cross-Check

Tasks now cover all requirements:

- R1/R2: tasks 1, 4, 5
- R3: tasks 3, 6
- R4/R5: tasks 7-11
- R6: tasks 2, 12, 13
- R7: tasks 1, 3, 6, 10, 11, 14, 15

No blocker remains.

## Implementation Cautions

- `decision.CalculateTotalRisk()` currently uses `ctx.Account.TotalEquity`; changing this incorrectly could affect circuit breaker behavior. Prefer explicit sizing/allocation denominator helper.
- `web/src/components/EquityChart.tsx` has both raw equity line and PnL badge; keep labels aligned with whichever value is plotted.
- `api/server.go` history handling should avoid turning older fallback records into misleading current performance.
- `return_reliable` must be emitted when false; do not tag it with `omitempty` if the frontend relies on it to skip legacy return points.
- Runtime `decision_logs/` and `data/` must stay out of this spec implementation.

## Validation Checklist

- `go test ./config ./trader ./api ./decision ./logger`
- `cd web && npm run test`
- `cd web && npm run build`
- Manual API check on 161 after deployment:
  - `/api/account?trader_id=aster_chanlun_v2` shows both `total_equity=44.x` and `initial_balance=10`
  - account cards label exchange equity and configured initial balance separately
  - allocation disabled preserves current sizing behavior
