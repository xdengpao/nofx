# Account Baseline And Capital Allocation Requirements

## Background

161 服务中 `aster_chanlun_v2` 的配置包含 `initial_balance: 10.0`，但前端详情页显示：

- total equity: `44.48 USDT`
- available balance: `44.48 USDT`
- cost basis: `44.41 USDT`
- total PnL: `0.07 USDT`

经检查，当前行为符合代码实现但字段语义容易误导：

1. `initial_balance` 是 trader 配置字段，目前只用于初始化校验、状态展示和 PnL 成本基准兜底。
2. `/api/account.total_equity` 来自交易所实时账户余额，即 `wallet_balance + unrealized_profit`，不是配置资金。
3. `cost_basis` 在有交易事件时按 `current_equity - trade_pnl` 计算，而不是固定使用 `initial_balance`。
4. `EquityChart` 前端收益曲线使用第一条有效历史净值作为图表基准，因此曲线从 `44.42` 附近开始，而不是从 `10.00` 开始。
5. 当前没有“策略资金隔离”或“allocated capital”概念，开仓 sizing 和风险预算仍基于交易所账户可用资金/总净值，而不是 `initial_balance=10`。

本规格目标是把三个概念明确拆开：

- Exchange account equity: 交易所真实账户净值，用于展示真实资产。
- Strategy baseline: 策略收益统计的起始/成本基准，用于计算 PnL 百分比和净值曲线。
- Allocated capital: 可选的策略资金上限，用于 sizing 和风险预算，让某个 trader 只按指定资金运行。

## Glossary

- Exchange account equity: 交易所返回的钱包余额加未实现盈亏。
- Available balance: 交易所返回的可用余额。
- Initial balance: 兼容旧配置的 trader 初始资金字段；默认不再暗示它等于交易所账户净值。
- Strategy baseline: 策略收益曲线和收益率计算基准。可来自配置、策略首次启动净值、历史成本基准或交易日志重建。
- Cost basis: 当前收益统计使用的成本基准，即 `current_equity - realized/unrealized trade pnl` 或显式 baseline。
- Allocated capital: 单 trader 可使用的策略资金上限。启用后，开仓 sizing、风险预算和前端“策略资金”展示应按它计算。
- Account display mode: 前端展示资金时区分交易所账户资金与策略统计资金的模式。

## Requirements

### Requirement 1: 前端必须清晰区分交易所净值、策略基准和配置初始资金

**User Story:** 作为用户，我希望详情页明确告诉我 `44.48` 是交易所真实账户净值，`10.00` 是策略配置资金或初始资金，避免误以为配置不生效。

#### Acceptance Criteria

1. WHEN `/api/account` 返回账户信息 THEN 响应 SHALL 包含 `total_equity`、`available_balance`、`wallet_balance`、`unrealized_profit`、`initial_balance` 和 `cost_basis`。
2. WHEN 前端详情页展示资金卡片 THEN 页面 SHALL 用不同标签展示“交易所账户净值”和“配置初始资金/策略基准”。
3. WHEN `initial_balance` 与 `total_equity` 不一致 THEN 页面 SHALL NOT 将其视为错误；页面 SHOULD 显示一个简短说明或字段标签，表明二者语义不同。
4. WHEN `pnl_source` 为 `trade_logs_plus_unrealized` THEN 页面 SHALL 显示收益率基于交易日志和当前未实现盈亏重建。
5. WHEN `pnl_source` 为 fallback 类型 THEN 页面 SHOULD 显示基准来源，以便用户知道收益率是否可靠。

### Requirement 2: 后端账户 API 必须输出结构化的资金语义字段

**User Story:** 作为前端和运维使用者，我希望 API 返回字段能直接说明每个金额的来源和用途，而不是只能从代码推断。

#### Acceptance Criteria

1. WHEN 调用 `GET /api/account?trader_id=...` THEN 后端 SHALL 返回 `equity_source`，说明 `total_equity` 来自交易所实时余额。
2. WHEN 后端计算收益统计 THEN 响应 SHALL 返回 `baseline_source` 或继续兼容 `pnl_source`，并区分配置基准、当前净值兜底、交易日志重建。
3. WHEN `initial_balance` 只作为兼容字段存在 THEN API SHALL 返回 `initial_balance_role`，例如 `configured_baseline_fallback`。
4. IF 后端引入 `strategy_baseline` THEN API SHALL 返回其金额和来源。
5. Existing clients SHALL continue to work with old fields: `total_equity`、`available_balance`、`total_pnl`、`total_pnl_pct`、`cost_basis` must remain backward-compatible.

### Requirement 3: 净值曲线必须使用后端明确基准，不能在前端自行用第一条净值冒充初始余额

**User Story:** 作为用户，我希望净值曲线和顶部收益率使用同一个基准，避免一个地方按 `44.41` 算收益，另一个地方按历史第一点算收益。

#### Acceptance Criteria

1. WHEN `/api/equity-history` 返回历史点 THEN 每个点 SHALL 包含 `cost_basis` 或 `strategy_baseline`。
2. WHEN 前端渲染 `EquityChart` THEN 图表 PnL SHALL 优先使用历史点返回的 `total_pnl` 和 `total_pnl_pct`。
3. IF 历史点缺少 `total_pnl_pct` THEN 前端 MAY 使用后端返回的 `cost_basis` 计算，而 SHALL NOT 优先使用第一条有效 `total_equity`。
4. WHEN 历史中存在零时间或 `total_equity <= 1` 的无效点 THEN 前端 SHALL 继续过滤，不得把无效点作为曲线基准。
5. WHEN 用户切换 USDT/% 显示 THEN 两种模式 SHALL 使用同一套基准来源。
6. WHEN older decision logs contain legacy PnL semantics or missing `cost_basis` THEN backend SHALL mark the point with a legacy baseline source or omit unreliable return percent, so frontend does not display misleading 300%+ strategy return for the current trader baseline.

### Requirement 4: 可选策略资金上限必须能限制 sizing 和风险预算

**User Story:** 作为交易监督者，我希望 `aster_chanlun_v2` 可以只按 10 USDT 或其他指定资金运行，即使交易所钱包里有更多钱。

#### Acceptance Criteria

1. WHEN trader 配置启用 `allocated_balance` 或等价字段 THEN open sizing SHALL use `min(exchange_available_balance, allocated_remaining_balance)` as capital availability.
2. WHEN 计算账户风险预算 THEN total risk denominator SHALL use allocated capital if capital allocation is enabled.
3. WHEN 已有持仓占用策略资金 THEN available allocated capital SHALL deduct margin used and open risk attributable to that trader.
4. IF allocated capital is too small to satisfy exchange minimum notional THEN open-like decisions SHALL be rejected with `position_sizing.min_notional` or equivalent structured reason.
5. WHEN allocated capital is not configured THEN system SHALL preserve current behavior and size from exchange equity/available balance.
6. WHEN strategy allocation is enabled THEN account API SHALL return `allocated_balance`、`allocated_available_balance`、`allocated_used_margin` and `allocation_enabled`.

### Requirement 5: 配置兼容性必须保持

**User Story:** 作为维护者，我希望旧 `config.json` 不需要改动也能启动，新字段只是增强语义和可选资金隔离。

#### Acceptance Criteria

1. Existing `initial_balance` SHALL remain valid and required as today unless a separate migration explicitly changes it.
2. New allocation config fields SHALL be optional.
3. IF allocation fields are absent THEN default behavior SHALL match current production behavior.
4. `config.json.example` SHALL document the semantic difference between `initial_balance` and `allocated_balance`.
5. Config validation SHALL reject negative or zero allocated capital when allocation is explicitly enabled.

### Requirement 6: 日志和决策记录必须便于对账

**User Story:** 作为运维人员，我希望决策日志能解释每个周期使用了哪个资金基准，方便排查页面显示和交易 sizing 的差异。

#### Acceptance Criteria

1. WHEN a decision record is written THEN `account_state` SHALL include strategy baseline and allocation fields when available.
2. WHEN allocation is enabled and an open decision is rejected due to allocation limits THEN the rejection SHALL include allocation diagnostics.
3. WHEN account PnL is computed THEN the decision record SHALL preserve `cost_basis` and `pnl_source`.
4. Existing decision log readers and replay SHOULD tolerate missing new fields in older logs.
5. WHEN allocation is enabled THEN decision records SHALL preserve both display equity and sizing/allocation equity, so later replay can explain why a trade was smaller than exchange account equity allowed.

### Requirement 7: 测试必须覆盖当前 161 问题

**User Story:** 作为开发者，我希望回归测试直接覆盖“配置 10 但交易所净值 44.48”的场景，防止以后再次混淆字段语义。

#### Acceptance Criteria

1. Unit tests SHALL cover `initial_balance=10` and exchange equity `44.48` returning distinct API fields.
2. Unit tests SHALL cover `cost_basis = total_equity - trade_pnl` when trade logs exist.
3. Frontend tests SHALL cover equity chart using backend `total_pnl_pct` / `cost_basis` rather than first valid equity as implicit baseline.
4. Sizing tests SHALL cover allocation disabled preserving current behavior.
5. Sizing tests SHALL cover allocation enabled capping available capital.
6. Tests SHALL NOT call real exchange order APIs.
