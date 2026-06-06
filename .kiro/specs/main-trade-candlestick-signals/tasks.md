# 主交易 K 线蜡烛图与买卖点标记 Tasks

## Phase 1: 后端 K 线数量配置化

- [x] 在 `trader/auto_trader.go` 新增 `MarketKlineLimitResolution` 或等价结构，包含 `limit`、`configured_limit`、`limit_source`。
- [x] 在 `trader/auto_trader.go` 新增 timeframe 到 `ProgrammaticStrategyPolicy.HistoryDepth` 的映射 helper。
- [x] 在 `trader/auto_trader.go` 新增 `ResolveMarketKlineLimit(timeframe string, explicitLimit int)`，支持 query limit、programmatic history depth、default fallback 和 1000 上限。
- [x] 在 `manager/trader_manager.go` 新增 `ResolveMarketKlineLimit(traderID, timeframe string, explicitLimit int)`，复用对应 AutoTrader 配置。
- [x] 更新 `api/server.go` 的 `/api/market/klines`，未传 `limit` 时调用配置化 resolver。
- [x] 更新 `/api/market/klines` 响应，增加 `configured_limit` 和 `limit_source`。
- [x] 为 `AutoTrader.GetMarketKlines()` 增加可替换行情 fetcher 或等价测试钩子，默认仍使用 `market.GetKlines`。
- [x] 保持非法 timeframe 的 400 中文错误和旧 query limit 兼容。

## Phase 2: 后端 SignalMarker 交易意图字段

- [x] 扩展 `strategy/chanlun.SignalMarker`，新增 `final_action`、`trade_intent`、`position_side` 字段。
- [x] 新增或更新 `derivePositionSide()`，从 metadata side、final action、action、direction 中稳定推导目标持仓方向。
- [x] 新增或更新 `deriveTradeIntent()`，按 `final_action` 优先规则推导 `open_long/open_short/add_long/add_short/reduce_long/reduce_short/close_long/close_short/reduce_skipped`。
- [x] 更新 `signalToMarker()`：主信号初始 marker 只表达信号分类，不误填交易意图。
- [x] 更新 `decisionToMarker()`：填充 `action`、`position_side`、`trade_intent`。
- [x] 更新 `OnExecutionResult()`：保留原始 `action`，写入 `final_action`，并按最终动作重算 `trade_intent`。
- [x] 更新 rejected/failed marker 生成逻辑，确保被风控拒绝的策略动作也能保留结构化 action 和 trade_intent。
- [x] 保持旧 state 文件兼容，缺失新增字段时不影响 API 返回。

## Phase 3: 前端 API 与类型契约

- [x] 更新 `web/src/lib/api.ts` 的 `getMarketKlines()`，使 `limit` 变为可选，只有显式传入正数时才写 query。
- [x] 更新 `TraderDetailsPage`，请求主交易 K 线时不再传硬编码 `80`。
- [x] 更新 `web/src/types.ts` 的 `SignalMarker` 和 `MarketKlineResponse` 新字段。
- [x] 同步更新 `web/src/types/index.ts`，避免重复类型定义漂移。

## Phase 4: 前端 marker 纯函数

- [x] 新增 `web/src/utils/strategyMarkers.ts`。
- [x] 实现 `normalizeEpochMs()`，统一秒、毫秒、微秒/纳秒时间单位。
- [x] 实现 `signalLabel()`，将 `buy1/buy2/buy3/sell1/sell2/sell3` 转为 `B1/B2/B3/S1/S2/S3`。
- [x] 实现 `resolveTradeIntent()`，优先使用 `trade_intent`，否则按 `final_action || action` 和 `position_side || direction` 推导。
- [x] 实现 `tradeIntentLabel()`，将机器枚举转为中文展示。
- [x] 实现 `markerBelongsToKline()`，按归一化后的 `close_time` 匹配 marker 与 K 线。
- [x] 实现 marker 状态/方向辅助函数，供图表和 badge 复用。

## Phase 5: 蜡烛图组件

- [x] 新增 `web/src/components/StrategyCandlestickChart.tsx`，使用 SVG 自绘蜡烛图，不新增图表依赖。
- [x] 实现 OHLC 蜡烛渲染，上涨使用绿色，下跌使用红色。
- [x] 实现价格范围计算，包含 K 线 high/low 和 marker price，并添加上下 padding。
- [x] 实现水平滚动或响应式最小宽度，支持配置深度较大的 K 线数量。
- [x] 实现 K 线 hover/click tooltip，展示时间、OHLC、成交量和同根 K 线 marker。
- [x] 实现主信号 marker 叠加，只展示 `source_layer=main_signal` 且 `timeframe=trade_timeframe` 的 marker。
- [x] 实现 marker 定位：买点默认低点下方，卖点默认高点上方，有 `price` 时优先按 price 定位。
- [x] 实现同一 K 线多 marker 的 offset，避免完全遮挡。
- [x] 实现 marker 文本：信号分类 + 可用交易意图，例如 `B1 · 开多`、`S2 · 减多`。
- [x] 实现 marker tooltip，展示状态、级别、action、final_action、reason、signal_id。
- [x] 实现图例，说明 `B1/B2/B3/S1/S2/S3`、交易意图和状态含义。
- [x] 实现空状态和错误友好展示，避免回退为误导性表格。

## Phase 6: StrategyInspector 布局改造

- [x] 在 `web/src/App.tsx` 中引入 `StrategyCandlestickChart`。
- [x] 移除策略检查区主交易 K 线表格渲染逻辑。
- [x] 将策略检查区调整为蜡烛图主区域 + 最新信号/诊断摘要区域。
- [x] 保留“最新信号”展示，展示信号分类、方向、价格、SL、TP、结构目标和时间。
- [x] 保留“信号诊断”展示，包含 config hash、trade/component/micro timeframe。
- [x] 保留持仓管理 marker 摘要，仅展示最近若干条，不强制映射到主交易蜡烛图。
- [x] 确保 AI 决策模式下仍显示当前 AI 模式提示，不请求或展示程序化蜡烛图。
- [x] 确保 trade_timeframe 为 `15m` 时，前端请求和图表标题均显示 `15m`。

## Phase 7: 后端测试

- [x] 更新 `api/server_test.go`，使用 fake K 线 fetcher 验证 `/api/market/klines` 未传 `limit` 时可使用 programmatic `history_depth`，不得依赖真实行情网络。
- [x] 更新 `api/server_test.go`，使用 fake K 线 fetcher 验证显式 query limit 优先于配置深度。
- [x] 保留或增强非法 timeframe 返回 400 中文错误测试。
- [x] 增加 `strategy/chanlun` 测试，覆盖 `deriveTradeIntent()` 的开多、开空、加多、加空、减多、减空、平多、平空、减仓跳过。
- [x] 增加 `strategy/chanlun` 测试，覆盖 `partial_close` 被 `final_action=close_long/close_short` 覆盖为平多/平空。
- [x] 增加 `strategy/chanlun` 测试，覆盖 rejected marker 保留 action/trade_intent。

## Phase 8: 前端测试

- [x] 新增 `web/src/utils/strategyMarkers.test.ts`。
- [x] 测试 `normalizeEpochMs()` 支持秒、毫秒、微秒/纳秒。
- [x] 测试 `markerBelongsToKline()` 能正确匹配 close_time。
- [x] 测试 `resolveTradeIntent()` 的 final_action 优先级。
- [x] 测试 `partial_close + position_side=long/short` 映射为减多/减空。
- [x] 测试缺少交易动作字段时只保留信号分类，不强行推导开平仓。

## Phase 9: 验证与发布准备

- [x] 运行 `go test ./strategy/chanlun ./api`。
- [x] 运行 `go build ./...`。
- [x] 运行 `cd web && npm run test`。
- [x] 运行 `cd web && npm run build`。
- [!] 使用浏览器或截图检查策略检查区：当前环境未提供可用浏览器会话；已通过 `npm run build` 验证组件可编译，后续部署或本地浏览器中复核视觉细节。
- [!] 手动验证主交易级别为 `1h` 时展示 1h K 线：当前环境未提供可用浏览器会话；代码路径使用 `trade_timeframe` 请求对应级别，需在浏览器中复核。
- [!] 如配置或测试环境允许，手动验证主交易级别为 `15m` 时展示 15m K 线：已用 API fake fetcher 验证 `15m` 使用 `history_depth["15m"]`，浏览器视觉复核待部署/本地打开后完成。
- [x] 若后续部署 161，验证线上 `/api/market/klines` 返回 `configured_limit` 和 `limit_source`，策略检查区展示蜡烛图。
