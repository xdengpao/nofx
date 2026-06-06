# Chanlun V2 Signal Freshness Time Alignment Cross-Check

## 结论

requirements、design、tasks 的主方向与现有代码一致：当前 V2 的核心缺口确实集中在 `strategy/chanlunv2/engine.go` 的信号时间语义和 open-like 决策前置过滤，以及 `strategy/chanlunv2/report.go` 的 marker 元数据展示。

本次交叉验证只修正 spec 文档，没有修改业务代码。

## 已对齐的代码事实

- `strategy/chanlunv2/engine.go` 当前在 `signalToDecision()` 中把 `signal_close_time` 和 `decision_close_time` 都设置为 `sig.Timestamp` 归一化后的值。
- `strategy/chanlunv2/engine.go` 当前在 `rawDecisions` 后直接进入 `validateChanlunV2Decisions()`，没有独立 V2 freshness gate。
- `decision.ValidateStrategyDecisions()` 已包含 `ValidateAndEnrichDecision()`、open gate、position sizing 相关校验和最终频率/持仓限制，因此 freshness gate 应放在它之前。
- `decision.NewOpenRejectionFromDecision()` 已能复制 `signal_id`、`signal_close_time`、`decision_close_time`、`trade_intent` 和 `StrategyMetadata`。
- `logger.DecisionAction.Timestamp` 已表示动作发生 wall-clock 时间；`trader.appendOpenRejectionsToRecord()` 已从 `OpenRejection` 复制策略元数据。
- `web/src/utils/strategyMarkers.ts` 已优先用 `display_close_time` / action `decision_close_time` 定位动作 marker，后端补正确字段即可复用现有前端行为。
- 共享 `strategy/chanlun.SignalMarker` 和前端 `SignalMarker` 类型已包含 `freshness_state`、`age_candles`，但 V2 尚未系统填充。

## 已修正的 Spec 偏差

- 配置命名从 `soft_max_age_candles` / `hard_max_age_candles` / `reject_target_crossed` 改为项目已有风格：`soft_age_candles` / `max_lifetime_candles` / `missed_target_guard`。
- `enabled` 和 `missed_target_guard` 改为可选 bool 指针语义，支持“未配置走默认、显式 false 关闭”。
- 增加要求：V2 分析和 `decision_close_time` 必须使用本轮 `PrepareCycleContext` 已准备的 closed K 线，避免 `analyzeSymbol()` 二次抓取导致时间锚点漂移。
- 明确 `multiLevelResult` 需要保存 latest closed K 线时间，供 `signalToDecision()` 和 `managePositions()` 复用。
- 明确已有 marker/type 字段与缺失字段：`freshness_state`、`age_candles` 已存在；`evaluation_close_time`、`action_timestamp`、`stale_reason` 需要按需要补齐。

## 执行前风险点

- `strategy/chanlunv2` 当前直接调用 `market.GetKlines()`，测试需要注入 prepared K 线或引入可替换 fetcher，避免单测触网。
- stale gate 生成的 rejection 应与 open gate rejection 区分 reason code，例如 `freshness_gate.signal_expired`，否则仍会污染 open gate 噪声分析。
- V2 `setLatestReport()` 会重建 `SignalMarkers`，后续实现要避免新的 ready marker 覆盖同一 `signal_id` 的 rejected/stale 生命周期状态。
- 线上部署前仍需确认 161 服务器未跟踪 Rust 构建产物不被清理或提交。
