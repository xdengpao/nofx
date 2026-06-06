# Chanlun V2 Signal Freshness Time Alignment Tasks

## Phase 1: 配置与时间基础

- [x] 1. 增加缠论 V2 信号新鲜度配置
  - 在 `config.ChanlunV2StrategyConfig` 增加 `signal_freshness` 可选配置。
  - 字段沿用程序化策略命名：`enabled`、`soft_age_candles`、`max_lifetime_candles`、`soft_age_by_signal_type`、`max_lifetime_by_signal_type`、`min_remaining_net_rr`、`missed_target_guard`、`confidence_decay_per_aged_candle`。
  - 设置保守默认值：启用、soft=1 根、hard=2 根、target crossed 拒绝。
  - 验证点：旧 `config.json` 不需要新增字段也能加载。

- [x] 2. 将 freshness 配置纳入归一化与 config hash
  - 更新 config normalize/validate 逻辑，确保非法负数或零值按默认值处理。
  - 更新 `hashChanlunV2Config()` 或等价 hash 输入，使 freshness 配置改变会改变 `config_hash`。
  - 补 `config` 或 `strategy/chanlunv2` 配置测试。
  - 验证命令：`CGO_ENABLED=0 go test ./config ./strategy/chanlunv2`。

- [x] 3. 增加缠论 V2 评估 K 线锚点 helper
  - 在 `strategy/chanlunv2` 增加 helper 获取 trade timeframe 最新闭合 K 线 close time。
  - 将 `multiLevelResult` 扩展为保存每个 level/timeframe 的 latest closed K 线时间。
  - 优先让 `analyzeSymbol()` 使用 `PrepareCycleContext` 已写入 `ctx.MarketDataMap` 的 closed klines；仅在没有 prepared data 的只读报告路径回退到直接抓取。
  - 确保该 helper 使用当前周期实际分析的 closed klines，不另抓不一致数据。
  - 验证点：`decision_close_time >= signal_close_time`；若异常则回退并输出诊断。

## Phase 2: 缠论 V2 决策时间语义

- [x] 4. 更新 `signalToDecision()` 时间字段
  - 调整 `strategy/chanlunv2/engine.go`，让 `signal_close_time` 保持结构信号时间。
  - 将 `decision_close_time` 设置为本轮 evaluation close time，而不是默认等于 signal close time。
  - 在 `StrategyMetadata` 写入 `evaluation_close_time`。
  - 保持 `signal_id` 继续基于 `signal_close_time`，避免生命周期 key 变化。

- [x] 5. 更新持仓管理决策时间字段
  - 检查 `managePositions()` 产生的 close/reduce 决策。
  - 对持仓管理动作保留结构信号时间，同时写入本轮 evaluation close time。
  - 验证点：持仓管理 marker 不被 freshness open gate 误伤。

- [x] 6. 补充缠论 V2 时间语义测试
  - 覆盖 BNB-like 场景：`signal_close_time` 很早，当前 evaluation close time 较新。
  - 断言 `signal_id` 使用旧结构时间，`decision_close_time` 使用本轮评估 K 线。
  - 断言 evaluation close time 来自注入/准备的 closed K 线，避免二次抓取导致锚点漂移。
  - 验证命令：`CGO_ENABLED=0 go test ./strategy/chanlunv2`。

## Phase 3: Freshness Gate

- [x] 7. 实现 `applyChanlunV2FreshnessGuard`
  - 在 `strategy/chanlunv2` 中新增 open-like 决策过滤函数。
  - 输入原始 `[]decision.Decision`，输出可继续验证的 decisions 和 freshness rejections。
  - 忽略 wait/hold/close 等非 open-like 动作。
  - 验证点：hard-expired 信号不会进入 `ValidateStrategyDecisions()`。

- [x] 8. 计算信号年龄与状态
  - 根据 trade timeframe duration 计算 `age_candles`。
  - 写入 `freshness_state`：`fresh`、`aged`、`expired`、`target_crossed`、`rr_invalid`。
  - 将 `age_candles`、`freshness_state`、`stale_reason` 写入 `Decision.StrategyMetadata` 和 marker。

- [x] 9. 实现 hard-expired 拒绝
  - 当 `age_candles > max_lifetime_candles` 时生成 `OpenRejection`。
  - reason 文案包含信号结构时间、评估 K 线时间、年龄和阈值。
  - 不调用 open gate，不增加行情 gate 拒绝噪声。

- [x] 10. 实现 target-crossed 与 RR-invalid 拒绝
  - 对 long：当前价已接近/超过目标价或剩余 RR 不满足要求时拒绝。
  - 对 short：当前价已接近/低于目标价或剩余 RR 不满足要求时拒绝。
  - 复用现有市场数据或当前 `market.Data`，避免真实下单依赖。

- [x] 11. 实现 soft-aged 降级策略
  - 对超过 soft 阈值但未 hard 过期的信号降低置信度或追加严格诊断。
  - 不直接绕过现有 open gate。
  - reason/diagnostics 应能在策略检查页展示。

- [x] 12. 接入 `GetFullDecision()` 流程
  - 在 `rawDecisions` 生成后、`validateChanlunV2Decisions()` 前执行 freshness guard。
  - 合并 freshness rejections 与 open gate rejections，保留二者原因来源。
  - 更新周期摘要，区分“信号过期拒绝”和“风控/open gate 拒绝”。

## Phase 4: Marker 与日志元数据

- [x] 13. 更新 `decisionToV2Marker()` 和 `signalToV2Marker()`
  - 纯结构 marker 使用 `signal_close_time` / `close_time` 锚定结构 K 线。
  - action/rejected marker 使用 `display_close_time = decision_close_time`。
  - marker 补齐/填充 `evaluation_close_time`、`freshness_state`、`age_candles`、`stale_reason`、可选 `action_timestamp`；其中 `freshness_state`、`age_candles` 在共享 marker 类型中已存在。

- [x] 14. 更新 rejected marker 状态回写
  - `markRejectedOpenMarkers()` 应按 `signal_id` 更新 marker。
  - freshness rejection 应显示为 stale/invalid/rejected 可识别状态，并保留原 `signal_id`。
  - 不允许后续 ready marker 覆盖已经 rejected 的同一生命周期状态。

- [x] 15. 更新 `OpenRejection` 与 `DecisionAction` 元数据传递
  - 确认 `OpenRejection` 保留 freshness 字段；必要时扩展 `decision.OpenRejection`。
  - 更新 `trader/auto_trader.go appendOpenRejectionsToRecord()`，写入或透传 `freshness_state`、`age_candles`、`evaluation_close_time`、`stale_reason`。
  - `DecisionAction.Timestamp` 继续作为动作发生时间。

- [x] 16. 补日志对账测试
  - 在 `trader` 或 `logger` 测试中覆盖 BNB-like stale rejection。
  - 断言 `DecisionAction.signal_id`、`signal_close_time`、`decision_close_time`、`timestamp` 同时存在且含义不同。

## Phase 5: API 与前端类型

- [x] 17. 更新 TypeScript 类型
  - 在 `web/src/types.ts` 和 `web/src/types/index.ts` 为 `SignalMarker`、`DecisionAction` 补齐可选字段：
    - `evaluation_close_time?: number`
    - `action_timestamp?: number`
    - `freshness_state?: string`
    - `age_candles?: number`
    - `stale_reason?: string`
  - 保留当前已经存在的 `freshness_state` / `age_candles` 类型定义，不做破坏性改名。
  - 保持旧响应可兼容。

- [x] 18. 更新策略 marker helper
  - 在 `web/src/utils/strategyMarkers.ts` 增加/调整 helper：
    - action marker 继续优先使用 `display_close_time || decision_close_time`。
    - 结构 marker 使用 `signal_close_time || close_time`。
    - tooltip 可读取 `action_timestamp` 和 `evaluation_close_time`。
  - 补 `strategyMarkers.test.ts` 覆盖三类时间。

- [x] 19. 更新策略显示模型
  - 在 `web/src/utils/strategyDisplay.ts` 的 tooltip rows 中将“决策时间”改为“评估K线”。
  - 增加“动作时间”行。
  - 对 `freshness_state`、`age_candles` 输出紧凑文案。
  - 补 `strategyDisplay.test.ts` 覆盖 stale marker 展示。

- [x] 20. 更新策略检查页面
  - 在 `web/src/App.tsx` 最新信号卡片显示结构时间、评估 K 线、动作时间和年龄。
  - 当 `signal_close_time` 与 `decision_close_time` 差距超过 soft 阈值时显示 stale/旧信号提示。
  - 保持卡片布局紧凑，避免移动端文字溢出。

- [x] 21. 更新蜡烛图 tooltip
  - 在 `web/src/components/StrategyCandlestickChart.tsx` hover tooltip 显示：
    - `结构`
    - `评估K线`
    - `动作`
    - `年龄`
    - `freshness_state`
  - 当配对时间不在当前 K 线窗口内，保留现有提示并明确是哪一类时间。

## Phase 6: 后端/API 测试

- [x] 22. 补 `strategy/chanlunv2` freshness 测试
  - 覆盖 fresh 信号通过。
  - 覆盖 soft-aged 信号降级但不直接拒绝。
  - 覆盖 hard-expired 信号拒绝。
  - 覆盖 long/short target-crossed。
  - 覆盖 RR-invalid。

- [x] 23. 补 marker 生命周期测试
  - 覆盖同一 `signal_id` 从 ready 更新为 stale/rejected。
  - 断言 `display_close_time = decision_close_time`。
  - 断言 `close_time/signal_close_time` 仍是结构时间。

- [x] 24. 补 API 对账测试
  - 在 `api` 或 `manager` 测试中构造同一 `signal_id` 的 latest decision 和 strategy signal。
  - 验证 `/api/strategy/signals` 与 `/api/decisions/latest` 可以通过 `signal_id` 对齐。
  - 确认 trader scope 不串 trader。

- [x] 25. 补 replay/logger 兼容测试
  - 旧日志缺少新字段时仍能读。
  - 新日志包含新 freshness 字段时 JSON 序列化/反序列化保持字段名一致。

## Phase 7: 前端测试与构建

- [x] 26. 运行前端单元测试
  - 执行：`cd web && npm run test`。
  - 覆盖 marker 锚点、stale 展示、tooltip rows。

- [x] 27. 运行前端生产构建
  - 执行：`cd web && npm run build`。
  - 验证 TypeScript 类型同步，无 optional 字段访问错误。

## Phase 8: 集成验证

- [x] 28. 运行后端 targeted tests
  - 执行：
    - `CGO_ENABLED=0 go test ./strategy/chanlunv2 ./decision ./trader ./logger`
    - `CGO_ENABLED=0 go test ./api ./manager`
  - 修复失败后复跑。

- [x] 29. 运行全量后端构建
  - 执行：`CGO_ENABLED=0 go build ./...`。
  - 验证无编译错误。

- [x] 30. 本地语义自检
  - 构造或复用 BNB-like fixture。
  - 验证 hard-expired BNB 旧信号不再进入 open gate。
  - 验证日志中 stale rejection 与 open gate rejection 来源不同。
  - 验证策略检查页显示结构时间、评估 K 线、动作时间。

## Phase 9: 提交与部署

- [x] 31. 代码审查与提交
  - 检查 `git diff --check`。
  - 确认没有提交 `data/`、`decision_logs/`、`coin_pool_cache/` 或真实密钥。
  - 提交信息建议：`Align chanlun v2 signal freshness times`。

- [x] 32. 推送 GitHub
  - 推送当前分支 `jzhbnofxdev` 到 GitHub。
  - 验证远端包含新提交。

- [x] 33. 部署到 161 服务器
  - 在 161 仓库确认工作区状态，保留未跟踪 Rust 构建产物。
  - 拉取最新提交。
  - 执行 `sudo docker compose up -d --build`。
  - 验证 `nofx-trading` 和 `nofx-frontend` healthy。

- [x] 34. 部署后线上验证
  - 查看 `/api/strategy/signals?trader_id=aster_chanlun_v2&symbol=BNBUSDT&view=audit`。
  - 查看 `/api/decisions/latest?trader_id=aster_chanlun_v2`。
  - 验证 BNB stale 信号不再每周期重复产生 open gate 拒绝。
  - 验证策略检查页图上动作 marker、右侧最新信号、决策日志三方时间字段可对账。
