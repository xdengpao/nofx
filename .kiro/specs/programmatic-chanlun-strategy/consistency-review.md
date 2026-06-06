# Spec / Code 一致性复查

## 结论

本次复查对照了 `requirements.md`、`design.md`、`tasks.md` 与当前代码架构。整体方向一致：新增 `decision_mode=programmatic`，程序化策略只生成标准 `decision.Decision`，现有强制风控、交易计划、保护单同步、preflight、执行和日志仍作为公共层。

复查中发现 6 个会影响实现落地的边界问题，已同步补充到 `design.md` 和 `tasks.md`。当前 spec 可以进入实现阶段，但实现时应优先处理配置/路由/行情契约，避免后续策略核心写完后再回头改主链路。

## 已修正的关键不一致

### 1. 自定义标的池与行情拉取顺序

当前代码的行情拉取发生在 `decision.fetchMarketDataForContext()`，来源是 `ctx.CandidateCoins`、当前持仓和 `BTCUSDT`。如果 programmatic `symbol_pool.override/append` 中包含不在全局候选池的 symbol，按原设计先 `PrepareCycleContext()` 再解析 symbol universe，会导致自定义 symbol 没有行情。

已补充：

- `PrepareCycleContext()` 支持显式 `MarketSymbols`。
- Programmatic engine 先解析最终 symbol universe，再调用公共准备层拉取行情。
- 当前持仓和 `BTCUSDT` 仍强制保留。

### 2. Programmatic 模式不应被 AI 配置阻塞

当前 `config.Validate()` 对所有 trader 都要求 `ai_model` 合法，并按 AI provider 要求 API key。requirements 已要求 programmatic 不调用 AI API，但 design/tasks 原来只写了 key 非必需，没有明确 `ai_model` 是否可空。

已补充：

- `decision_mode=programmatic` 时 `ai_model` 可为空或只作为展示字段。
- AI key/provider 校验不阻塞 programmatic trader。
- 前端和 API 用 `decision_mode` 判断 trader 类型，不再只看 `ai_model`。

### 3. AutoTrader 公共执行前同步边界

当前 `AutoTrader.runCycle()` 在进入 `decision.GetFullDecision()` 前执行 `syncAutoClosedOrders()`、`detectAutoClosedPositions()` 和 `reconcileStaleTradePlans()`。这些不是 AI 私有逻辑，programmatic 模式也必须保留。

已补充：

- 自动平仓检测和 stale plan 修复保留在 mode routing 之前。
- `applyAICallState()` 只在 AI 模式且 `AICallAttempted=true` 时更新 AI backoff。

### 4. 加仓与 execution preflight 冲突

当前 `EvaluateExecutionPreflight()` 会拒绝同 symbol 同方向已有持仓，这对普通开仓正确，但会直接挡住 `add_long/add_short`。

已补充：

- `ExecutionPreflightInput` 需要新增 `Intent` 或 `AllowSameSidePosition`。
- 加仓跳过同向持仓拒绝，但继续校验反向持仓、最小名义额、价格、数量和杠杆。

### 5. 程序化频率控制缺少闭合 K 线记忆

requirements 要求新开仓/加仓默认只在 `trade_level` 新 K 线闭合后重新确认。仅靠 `signal_id` 去重不足以表达“上一根闭合 K 线已经分析过”。

已补充：

- programmatic state 持久化 `last_analyzed_closed_kline`。
- 用于每个 `symbol/timeframe` 记录最近已分析的闭合 K 线。

### 6. API / 日志兼容细节

当前 `market.Kline` 没有 JSON tag，直接返回会变成 Go 字段名；当前 logger/replay helper 只识别 `open_long/open_short` 为开仓动作。

已补充：

- K 线 API 使用专用 DTO，输出 snake_case 字段。
- logger/replay 至少识别 `add_long/add_short` 为 open-like 动作，避免旧日志统计读取被新 action 破坏。

## 仍需实现时重点自检

- `decision.GetFullDecision()` 中公共逻辑和 AI 私有逻辑交织较深，抽取 `PrepareCycleContext()` 时必须用 AI 默认兼容测试兜住。
- `market.calculateADXSeries()` 当前是未导出函数，programmatic 策略应通过 `market.GetWithHistory()` 获得 ADX/DI，而不是在 `strategy/chanlun` 中复制算法。
- `strategy/chanlun` 不应被 `decision` 反向导入，避免 import cycle；推荐 `trader.AutoTrader` 持有 engine，engine 依赖 `decision` 类型。
- Replay/backtest 仍按本期非目标处理，只做新增字段和 `add_*` 日志读取兼容。
- `data/programmatic_strategy_state.json` 是运行时状态，不作为普通代码改动提交。

## 当前建议

可以进入 implementation，但建议按 tasks 的 Phase 1 到 Phase 4 先完成配置、行情、路由、公共校验和加仓 action 契约，再进入 `strategy/chanlun` 核心算法。这样能先把主交易链路稳定住，后续缠论信号只是接入标准决策管道。
