# Account Baseline And Capital Allocation Design

## Overview

当前资金显示不一致的根因不是 Aster 余额错误，而是系统把不同语义字段放在同一个视觉上下文里：

- `initial_balance`: 配置字段，当前只是初始资金/兜底基准。
- `total_equity`: 实时交易所账户净值。
- `cost_basis`: 从交易日志和当前净值重建的收益基准。
- `EquityChart.initialBalance`: 前端本地推断的第一条有效历史净值。

设计目标是先修正展示和 API 语义，再提供可选的策略资金隔离能力。展示修正可以独立上线；策略资金隔离涉及 sizing 和风险预算，应作为后续任务分阶段启用。

## Design Principles

- 真实余额不伪装：`total_equity` 始终表示交易所真实净值。
- 基准来源显式：收益率、成本基准和初始配置资金都必须有来源字段。
- 前端不自行猜基准：图表优先使用后端计算结果。
- 资金隔离可选：未配置 allocation 时保持当前实盘行为。
- 风控先于便利：allocated capital 过小导致无法满足最小下单额时必须拒绝开仓。
- 兼容旧日志：新增字段使用 `omitempty`，旧日志缺失字段仍可 replay。

## Architecture

```mermaid
flowchart TD
    A[Exchange GetBalance] --> B[AutoTrader GetAccountInfo]
    C[Decision logs / trade events] --> B
    D[Trader config initial_balance / allocated_balance] --> B
    B --> E[/api/account]
    C --> F[/api/equity-history]
    D --> F
    E --> G[Account cards]
    F --> H[EquityChart]

    D --> I[buildTradingContext]
    I --> J[decision sizing / risk budget]
    J --> K[open gate and execution preflight]
```

## Current Behavior

`AsterTrader.GetBalance()` maps Aster USDT balance fields to normalized keys:

- `totalWalletBalance`
- `availableBalance`
- `totalUnrealizedProfit`

`AutoTrader.GetAccountInfo()` calculates:

- `total_equity = totalWalletBalance + totalUnrealizedProfit`
- `cost_basis = total_equity - realized/unrealized trade pnl`
- `initial_balance = at.initialBalance`

`EquityChart` currently calculates chart baseline as:

```ts
const initialBalance = validHistory[0]?.total_equity || account?.total_equity || 100
```

This should be replaced by backend-provided `cost_basis` / `total_pnl_pct`.

Important legacy nuance: existing 161 history contains early records where `cost_basis` is missing and the history endpoint falls back to configured `initial_balance=10`, producing `total_pnl_pct` above 300% for the first real equity point. The implementation must mark those points as legacy or avoid using their percent return as the current strategy baseline until the backend can reconstruct a reliable `cost_basis`.

## Data Model

### Backend Account Response

Extend account response map in `trader/auto_trader.go`:

```go
map[string]any{
    "total_equity": totalEquity,
    "available_balance": availableBalance,
    "wallet_balance": totalWalletBalance,
    "unrealized_profit": totalUnrealizedProfit,
    "equity_source": "exchange_balance",

    "initial_balance": at.initialBalance,
    "initial_balance_role": "configured_baseline_fallback",
    "cost_basis": accountPnL.CostBasis,
    "strategy_baseline": accountPnL.CostBasis,
    "baseline_source": accountPnL.Source,
    "pnl_source": accountPnL.Source,

    "allocation_enabled": allocation.Enabled,
    "allocated_balance": allocation.AllocatedBalance,
    "allocated_available_balance": allocation.AvailableBalance,
    "allocated_used_margin": allocation.UsedMargin,
}
```

Keep old fields unchanged.

### Decision Account State

Extend `logger.AccountState` or related decision record account snapshot:

```go
StrategyBaseline          float64 `json:"strategy_baseline,omitempty"`
BaselineSource            string  `json:"baseline_source,omitempty"`
EquitySource              string  `json:"equity_source,omitempty"`
AllocationEnabled         bool    `json:"allocation_enabled,omitempty"`
AllocatedBalance          float64 `json:"allocated_balance,omitempty"`
AllocatedAvailableBalance float64 `json:"allocated_available_balance,omitempty"`
AllocatedUsedMargin       float64 `json:"allocated_used_margin,omitempty"`
```

### Config

Add optional trader-level allocation config. Keep naming explicit and avoid changing existing `initial_balance` semantics:

```go
type TraderCapitalAllocationConfig struct {
    Enabled          bool    `json:"enabled,omitempty"`
    AllocatedBalance float64 `json:"allocated_balance,omitempty"`
}

type TraderConfig struct {
    InitialBalance    float64                       `json:"initial_balance"`
    CapitalAllocation TraderCapitalAllocationConfig `json:"capital_allocation,omitempty"`
}
```

Alternative flat field for easier config:

```json
"capital_allocation": {
  "enabled": true,
  "allocated_balance": 10.0
}
```

Do not overload `initial_balance` as allocation. This avoids silently changing production sizing.

## Account PnL Computation

Keep `computeAccountPnLSummary()` behavior for existing fields, but clarify source names:

- `trade_logs_plus_unrealized`: logs exist, cost basis reconstructed from current equity minus trade PnL.
- `current_equity_cost_basis_no_trades`: no trade events and zero PnL; current equity is the only reliable baseline.
- `configured_initial_fallback`: current equity based reconstruction invalid; configured initial balance used as fallback.

Potential follow-up:

```go
type accountPnLSummary struct {
    TotalPnL       float64
    TotalPnLPct    float64
    RealizedPnL    float64
    CostBasis      float64
    StrategyBaseline float64
    Source         string
}
```

For the 161 example:

```text
total_equity = 44.48471397
trade_pnl    = 0.07176231
cost_basis   = 44.41295166
initial_balance = 10.0
```

The UI should display this as:

- Exchange equity: `44.48 USDT`
- Strategy baseline: `44.41 USDT`
- Config initial balance: `10.00 USDT`
- PnL: `+0.07 USDT (+0.16%)`

## Equity History API

`/api/equity-history` already returns `cost_basis` and computed `total_pnl_pct`. Ensure:

1. It preserves `cost_basis` from records.
2. It returns `baseline_source` when available.
3. It can include `strategy_baseline` for clarity.
4. It filters or marks zero-time placeholder records so the frontend can ignore them.
5. It marks legacy or unreliable return points so the frontend does not treat old fallback percentages as current strategy performance.

Suggested `EquityPoint` extension:

```go
type EquityPoint struct {
    Timestamp        string  `json:"timestamp"`
    TotalEquity      float64 `json:"total_equity"`
    AvailableBalance float64 `json:"available_balance"`
    TotalPnL         float64 `json:"total_pnl"`
    TotalPnLPct      float64 `json:"total_pnl_pct"`
    CostBasis        float64 `json:"cost_basis,omitempty"`
    StrategyBaseline float64 `json:"strategy_baseline,omitempty"`
    BaselineSource   string  `json:"baseline_source,omitempty"`
    EquitySource     string  `json:"equity_source,omitempty"`
    ReturnReliable   bool    `json:"return_reliable"`
    PositionCount    int     `json:"position_count"`
    MarginUsedPct    float64 `json:"margin_used_pct"`
    CycleNumber      int     `json:"cycle_number"`
}
```

## Frontend Design

### Account Cards

In `web/src/App.tsx`, keep the current cards but make labels explicit:

- `交易所净值`: `account.total_equity`
- `可用余额`: `account.available_balance`
- `策略盈亏`: `account.total_pnl` and `account.total_pnl_pct`
- `配置初始资金`: `account.initial_balance`

If allocation is enabled, add:

- `策略分配资金`
- `策略可用资金`
- `策略占用保证金`

Avoid long explanatory copy in the main card body. Use concise labels and tooltip/title text if needed.

### EquityChart

Modify `web/src/components/EquityChart.tsx`:

- For USDT mode, either show raw `point.total_equity` with a clear account-equity label, or show `point.total_pnl` with a strategy-PnL label. Do not mix raw-equity line labels with strategy-PnL badges from a different baseline.
- For percent mode, use `point.total_pnl_pct` if present.
- Fallback order for manual calculation:
  1. `point.cost_basis`
  2. `account.cost_basis`
  3. `account.initial_balance`
  4. `point.total_equity - point.total_pnl`
- Do not use `validHistory[0].total_equity` as the primary baseline.
- If `baseline_source` marks a legacy or unreliable point, do not use that point to compute current header PnL; either render raw equity only or skip the percent value for that point.

## Capital Allocation Design

### Runtime Context

Add optional allocation state to `decision.Context.Account` or a dedicated field:

```go
type CapitalAllocationState struct {
    Enabled          bool
    AllocatedBalance float64
    AvailableBalance float64
    UsedMargin       float64
}
```

`AutoTrader.buildTradingContext()` should compute allocation state after positions are loaded:

```text
allocated_available = allocated_balance - margin_used_for_trader
```

When existing open risk is attributable to the trader, the allocation state should also expose:

```text
allocated_remaining_risk = allocated_risk_budget - open_risk_for_trader
```

For the current exchange setup, each AutoTrader owns one exchange account/trader scope, so margin used can initially use all positions returned by that trader. If shared account multi-trader allocation is introduced later, allocation attribution must use `TradePlan.TraderID`.

### Sizing Integration

When allocation is enabled:

- `ctx.Account.TotalEquityForSizing` or equivalent should use `allocated_balance`.
- `ctx.Account.AvailableBalanceForSizing` should use `min(exchange_available_balance, allocated_available_balance)`.
- Existing display `TotalEquity` should remain exchange equity.

Preferred implementation is to add explicit fields rather than mutating `TotalEquity`:

```go
type AccountInfo struct {
    TotalEquity      float64
    AvailableBalance float64
    SizingEquity     float64
    SizingAvailable  float64
}
```

Then update sizing code to prefer `SizingEquity/SizingAvailable` when positive.

### Risk Budget Integration

Allocation must affect risk denominator and remaining risk budget without mutating display equity:

- `decision.CalculateTotalRisk()` should divide by `SizingEquity` or allocated capital when allocation is enabled.
- `calculateRemainingRiskBudget()` and risk-state snapshots should use the allocation-aware denominator.
- `NormalizeOpenDecisionRisk()` and `CalculatePositionSizing()` should receive allocation-aware equity and available balance.
- `EvaluateOpenGate()` should keep its gate semantics but use the effective risk cap derived from allocation when sizing data is normalized.
- API and logs should expose both display equity and sizing equity so operators can verify which denominator was used.

### Open Rejections

If allocation caps prevent a valid order:

- Use `position_sizing.allocation_insufficient` when allocated available capital is below margin/min order requirements.
- Include diagnostics:
  - `allocated_balance`
  - `allocated_available_balance`
  - `exchange_available_balance`
  - `required_margin`
  - `min_order_value`

## Compatibility

- Existing `initial_balance` remains required and unchanged.
- Existing account API fields remain present.
- Older logs missing allocation fields still parse.
- Allocation defaults to disabled.
- `config.json.example` documents that `initial_balance` is not a wallet balance cap.

## Risks

- Changing chart semantics could surprise users who expect a raw equity curve. Mitigation: keep a toggle or label raw equity vs strategy PnL.
- Allocation sizing may interact with `strategy_risk.safe_mode`. Mitigation: tests for risk denominator and minimum notional.
- Multi-trader shared account allocation is harder than single trader allocation. Mitigation: first phase scopes allocation to the trader/exchange instance and uses current positions.

## Validation

Backend:

```bash
go test ./config ./trader ./api ./decision ./logger
```

Frontend:

```bash
cd web && npm run test
cd web && npm run build
```

Targeted checks:

- `initial_balance=10`, exchange equity `44.48` response keeps both values.
- Equity chart percent uses backend `total_pnl_pct`.
- Allocation disabled keeps current sizing behavior.
- Allocation enabled caps sizing and produces structured rejection when too small.
