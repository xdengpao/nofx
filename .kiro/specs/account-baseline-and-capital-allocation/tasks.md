# Account Baseline And Capital Allocation Tasks

## Phase 1: API Semantics And Backend Baseline

- [x] 1. Add explicit account semantic fields
  - Add `equity_source`, `initial_balance_role`, `baseline_source`, and `strategy_baseline` to `AutoTrader.GetAccountInfo()`.
  - Keep existing `total_equity`, `available_balance`, `total_pnl`, `total_pnl_pct`, `cost_basis`, `initial_balance`, and `pnl_source` unchanged.
  - Add a regression test with `initial_balance=10`, exchange equity about `44.48`, and reconstructed `cost_basis` about `44.41`.

- [x] 2. Extend decision account snapshot fields
  - Add optional `strategy_baseline`, `baseline_source`, and `equity_source` to decision log account state.
  - Add optional `allocation_enabled`, `allocated_balance`, `allocated_available_balance`, and `allocated_used_margin` to decision log account state.
  - Add optional sizing-equity fields if allocation uses a denominator different from exchange `total_equity`.
  - Ensure old decision logs without these fields still parse.
  - Add logger JSON round-trip tests.

- [x] 3. Extend `/api/equity-history` baseline metadata
  - Include `strategy_baseline`, `baseline_source`, and `equity_source` per history point when available.
  - Preserve current `cost_basis` and `total_pnl_pct` behavior.
  - Detect legacy records with missing/ambiguous `cost_basis` and mark them as `return_reliable=false` or `baseline_source=legacy_*`.
  - Avoid publishing misleading current-strategy return percentages for legacy points such as the 161 `10 -> 44.48` startup history.
  - Add API tests for historical points using backend baseline rather than first equity.

## Phase 2: Frontend Display Clarification

- [x] 4. Update frontend account type definitions
  - Sync `web/src/types.ts` and `web/src/types/index.ts`.
  - Add optional fields for baseline source and allocation fields.
  - Keep all current fields backward-compatible.

- [x] 5. Clarify account cards in trader detail page
  - Label `total_equity` as exchange account equity.
  - Add or repurpose a compact card/subtitle for configured initial balance and strategy baseline.
  - Show `pnl_source`/`baseline_source` concisely without adding long instructional text.
  - Ensure mobile layout does not overflow.

- [x] 6. Fix `EquityChart` baseline calculation
  - Prefer backend `point.total_pnl_pct` for percent mode.
  - Prefer backend `point.total_pnl` or clearly labeled raw equity for USDT mode, and align labels/badges with that choice.
  - Fallback to `point.cost_basis`, `account.cost_basis`, then `account.initial_balance`.
  - Remove first valid `total_equity` as primary implicit baseline.
  - Skip or raw-equity-render legacy points marked `return_reliable=false`.
  - Add Vitest coverage for the 161-like `10 vs 44.48` case.

## Phase 3: Optional Capital Allocation Config

- [x] 7. Add trader capital allocation config
  - Add optional `capital_allocation.enabled` and `capital_allocation.allocated_balance` to `config.TraderConfig`.
  - Validate explicit enabled allocation requires `allocated_balance > 0`.
  - Preserve old config behavior when the block is absent.
  - Update `config.json.example` with comments explaining `initial_balance` vs `capital_allocation`.

- [x] 8. Pass allocation config into `AutoTrader`
  - Extend `AutoTraderConfig` and `manager.TraderManager` wiring.
  - Expose allocation status in `GetStatus()` and `GetAccountInfo()`.
  - Add manager/config tests.

## Phase 4: Allocation-Aware Sizing And Risk

- [x] 9. Add allocation state to trading context
  - Compute allocated used margin from current trader positions.
  - Compute allocated available capital as `allocated_balance - allocated_used_margin`.
  - Return `allocation_enabled`, `allocated_balance`, `allocated_available_balance`, and `allocated_used_margin` in account API.

- [x] 10. Make position sizing allocation-aware
  - Add explicit sizing equity/available fields to account context or sizing input.
  - Use allocation values only when allocation is enabled.
  - Update total-risk denominator and remaining-risk-budget calculations to use allocated capital when enabled.
  - Ensure `decision.CalculateTotalRisk()`, open gate risk normalization, and `CalculatePositionSizing()` receive consistent allocation-aware values.
  - Keep exchange equity fields unchanged for display.
  - Add tests proving allocation disabled preserves current sizing.

- [x] 11. Add allocation rejection diagnostics
  - Reject open-like decisions when allocated capital cannot satisfy margin/min notional.
  - Use structured reason code such as `position_sizing.allocation_insufficient`.
  - Include allocated/exchange available capital and required margin in diagnostics.
  - Ensure no real order call occurs for allocation-rejected decisions.

## Phase 5: Replay And Observability

- [x] 12. Update replay/log readers
  - Surface baseline source and allocation state when present.
  - Preserve compatibility with old logs.
  - Add tests for mixed old/new log records.

- [x] 13. Add a small diagnostic endpoint or status summary if needed
  - Include account semantic summary in `/api/status` or existing account response only.
  - Avoid adding a new endpoint unless frontend needs it.

## Phase 6: Validation

- [x] 14. Run targeted backend tests
  - Note: 本机缺少 Rust `cargo` 和 `libchanlun_v2`，默认 CGO 链接会失败；本轮用 `CGO_ENABLED=0` 走 `chanlunv2` stub 完成账户语义、sizing、API、日志相关验证。
  - Passed: `CGO_ENABLED=0 go test ./config ./trader ./api ./decision ./logger`
  - `go test ./config ./trader ./api ./decision ./logger`

- [x] 15. Run frontend validation
  - `cd web && npm run test`
  - `cd web && npm run build`

- [x] 16. Cross-check spec against implementation
  - Verify `/api/account` still returns old fields.
  - Verify frontend labels distinguish exchange equity, strategy baseline, and configured initial balance.
  - Verify allocation disabled behavior matches current 161 service behavior.
  - Verify allocation enabled caps sizing without mutating displayed exchange equity.
