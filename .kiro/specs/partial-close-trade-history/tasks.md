# 部分平仓历史成交优化 Tasks

## Phase 1: Logger 数据结构与 replay 基础

- [x] 扩展 `logger.TradeOutcome`，新增 `event_type`、`is_partial`、`close_quantity`、`remaining_quantity`、比例、订单、信号、策略、PnL 来源和对账状态等可选字段。
- [x] 新增 `logger.TradeEventStats`，并在 `PerformanceAnalysis` 中增加 `recent_trade_events` 和 `trade_event_stats`。
- [x] 扩展 `openPositionTrace`，增加 `remainingQuantity`，确保 open/add lot 可以被部分消耗。
- [x] 新增导出的 `TradeReplayResult` 和 `BuildTradeReplay(records)`，统一返回完整闭合交易、成交事件和 unmatched 诊断。
- [x] 保留 `BuildTradeOutcomes(records)` 的旧签名，使其委托 `BuildTradeReplay()` 并只返回完整闭合交易。
- [x] 新增 `BuildTradeEvents(records)`，用于返回完整平仓、自动平仓和部分平仓事件。
- [x] 新增 `effectiveCloseQuantity(action)`，优先使用 `close_quantity`，兼容旧日志 `quantity`。
- [x] 新增 `isSkippedPartialClose(action)`，识别 `final_action=partial_close_skipped` 或数量为 0 的跳过记录。

## Phase 2: partial_close lot 消耗与 PnL

- [x] 新增 `resolvePartialCloseSide()`，按 `strategy_metadata.side`、record positions、现有 open lot 唯一方向的顺序解析部分平仓方向。
- [x] 新增 FIFO lot 消耗 helper，支持部分平仓消耗一个或多个 open/add lot。
- [x] 实现 partial close 事件生成：按实际消耗数量聚合为一条 `event_type=partial_close` 记录。
- [x] 对跨 lot 的部分平仓使用加权 entry price、汇总 PnL、汇总 position value 和 margin used。
- [x] full close 只关闭剩余 lot，并继续生成现有完整交易 outcome。
- [x] 将 full close 和 auto close 同步追加到 `recent_trade_events`，标记 `event_type=full_close` 或 `auto_close`。
- [x] 对 missing open 的 partial close 生成 `missing_open_for_partial_close` unmatched。
- [x] 对无法确定 side 的 partial close 生成 `missing_side_for_partial_close` unmatched。

## Phase 3: 执行层记录补强

- [x] 扩展 `trader/auto_trader.go` 现有 `extractOrderID(order map[string]interface{}) int64`，支持 `int64`、`int`、`float64`、`json.Number`、`string`。
- [x] 将 open、full close、partial close 中的订单 ID 记录统一改为调用扩展后的 `extractOrderID()`。
- [x] 更新 `executePartialCloseWithRecord()`，在正常部分平仓路径写入 `final_action`、`close_quantity`、`executed_close_percentage` 和订单 ID。
- [x] 更新 `executePartialCloseWithRecord()` 的跳过路径，明确写入 `final_action=partial_close_skipped`、`close_quantity=0` 和跳过原因。
- [x] 更新 `executePartialCloseWithRecord()` 的自动修正全平路径，确保 `final_action=close_long/close_short` 并按完整平仓进入 replay。
- [x] 在 partial close action metadata 中确保写入 `side`，供旧 action 名无法推断方向时 replay 使用。

## Phase 4: 交易所成交对账 metadata

- [x] 将 Binance Futures 与 Hyperliquid 当前 TODO panic 的 `GetOrderStatus()`、`GetTradeHistory()`、`GetOrderHistory()` 改为返回明确 `unsupported` error，避免对账增强触发 panic。
- [x] 新增 `AutoTrader.enrichCloseFillMetadata()` 或等价 helper，执行层 best effort 查询订单状态和成交历史。
- [x] 当订单 ID 有效且 `GetOrderStatus()` 可用时，写入订单状态、已成交数量和均价。
- [x] 当 `GetTradeHistory()` 能按订单 ID 匹配成交时，聚合成交均价、数量、手续费和 realized PnL。
- [x] 将对账结果写入 `DecisionAction.StrategyMetadata`：`reconciled`、`reconciliation_status`、`avg_fill_price`、`filled_quantity`、`realized_pnl`、`commission`。
- [x] 当交易所不支持或查询失败时，写入 `reconciled=false` 和中文 `reconciliation_reason`，不得影响交易执行成功返回。
- [x] 在 logger 事件生成中优先读取交易所对账 metadata，存在真实 realized PnL 时设置 `pnl_source=exchange`。
- [x] 无真实成交 metadata 时使用本地估算，并设置 `pnl_source=estimated` 和相应 `reconciliation_status`。

## Phase 5: Performance API 集成

- [x] 更新 `DecisionLogger.AnalyzePerformance()`，调用 `BuildTradeReplay()` 获取完整交易和成交事件。
- [x] 保持 `recent_trades`、`total_trades`、`win_rate`、`profit_factor` 等字段只基于完整闭合交易。
- [x] 使用 replay events 填充 `recent_trade_events`，按 `close_time` 降序展示。
- [x] 使用 replay events 构建 `trade_event_stats`，包含 partial close 事件数、估算 PnL、真实 PnL、待对账数量。
- [x] 确保 `/api/performance` 旧客户端兼容，新增字段均为可选 JSON 字段。
- [x] 手动或测试验证 161 旧日志形态：`action=partial_close`、`quantity>0`、无 `close_quantity` 时可生成估算事件。
- [x] 更新 `logger.BuildReplayReport()`，在保持完整交易统计不变的前提下，输出 partial close 成交事件数量、估算/真实 PnL 和待对账数量。

## Phase 6: 前端类型与历史成交表

- [x] 更新实际导入优先级更高的 `web/src/types.ts`，为 `TradeOutcome` 和 `PerformanceAnalysis` 增加成交事件字段。
- [x] 同步更新 `web/src/types/index.ts`，避免重复类型定义继续漂移。
- [x] 更新 `web/src/components/AILearning.tsx` 内部 `TradeOutcome` 类型，或复用统一类型避免重复定义漂移。
- [x] 将历史成交表数据源改为优先使用 `performance.recent_trade_events`，无事件时 fallback 到 `performance.recent_trades`。
- [x] 将历史成交数量、空状态判断、导出按钮启用状态和 CSV 导出入参统一改为使用同一个 `allTrades` 数据源。
- [x] 在历史成交表新增或调整“类型”展示，区分完整平仓、自动平仓和部分平仓。
- [x] 在历史成交表展示对账状态：真实成交、估算、待对账或不支持。
- [x] 在历史成交表展示 `close_reason`，缺失时保留旧的止损/止盈 fallback 文案。
- [x] 保持表格窄屏可横向滚动，新增列不得导致内容重叠。

## Phase 7: CSV 导出

- [x] 更新 `web/src/utils/exportCSV.ts` 的 `TradeOutcome` 类型，增加事件字段。
- [x] 扩展 CSV 表头，加入事件类型、是否部分平仓、平仓数量、请求比例、执行比例、订单 ID、信号 ID、PnL 来源、对账状态、平仓原因。
- [x] 更新 CSV 行生成逻辑，兼容旧完整交易和新部分平仓事件。
- [x] 更新 `web/src/utils/exportCSV.test.ts` 中表头列数和字段断言。

## Phase 8: 后端测试

- [x] 增加 logger 测试：open -> partial_close -> close，验证 `recent_trade_events=2`、`recent_trades=1`，且总关闭数量不超过开仓数量。
- [x] 增加 logger 测试：partial_close 后 full close 只对剩余数量计算完整交易 PnL。
- [x] 增加 logger 测试：partial_close 跨多个 open/add lot，验证 FIFO 消耗、加权开仓价和聚合 PnL。
- [x] 增加 logger 测试：`quantity=0` 或 `partial_close_skipped` 不生成成交事件。
- [x] 增加 logger 测试：旧日志只有 `quantity`、无 `close_quantity`、无 `final_action` 时可生成估算事件。
- [x] 增加 logger 测试：missing side 和 missing open 的 partial close 产生明确 unmatched reason。
- [x] 增加 trader 测试：`parseOrderID()` 覆盖 `int64`、`int`、`float64`、`json.Number`、`string` 和非法输入。
- [x] 增加 API 测试：`/api/performance` 返回 `recent_trade_events`，且完整交易统计不包含 partial close。

## Phase 9: 前端与集成验证

- [x] 运行 `go test ./logger ./trader ./api`。
- [x] 运行 `go build ./...`。
- [x] 运行 `cd web && npm run test`。
- [x] 运行 `cd web && npm run build`。
- [ ] 手动检查历史成交表：部分平仓标签、估算/待对账状态、原因展示、CSV 导出。
- [ ] 若后续执行部署，验证 161 `/api/performance?trader_id=aster_deepseek` 中旧 partial close 日志能出现在 `recent_trade_events`。
