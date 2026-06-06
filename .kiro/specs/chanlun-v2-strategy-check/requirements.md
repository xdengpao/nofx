# 缠论 V2 策略检查

## 背景

当前前端“策略检查”页面只在 `programmatic` 缠论 v1 trader 下启用，依赖 `/api/strategy/symbols`、`/api/strategy/signals` 和 `/api/market/klines` 三个接口展示标的、K 线和信号标记。`chanlun_v2` 已经接入交易循环，但没有提供同等检查报告，导致 v2 trader 无法像 v1 一样在页面上排查结构、买卖点、拒绝/执行状态和 K 线深度。

## 术语

- 策略检查：前端 Trader 详情页中的 K 线、信号、诊断和持仓管理信号区域。
- v1：`programmatic` 决策模式，Go 实现的程序化缠论策略。
- v2：`chanlun_v2` 决策模式，Go 调用 Rust 缠论库的策略。
- Signal Report：后端返回给前端的策略信号报告，沿用 v1 的 `StrategySignalReport` JSON 契约。

## Requirements

1. **v2 trader SHALL 显示策略检查入口**
   - User Story: 作为操作者，我希望选择 `chanlun_v2` trader 时也能看到策略检查区域，以便用同一套 UI 排查 v2 策略。
   - WHEN Trader 状态的 `decision_mode` 为 `chanlun_v2`
   - THEN 前端 SHALL 请求策略标的、信号报告和市场 K 线。
   - WHEN Trader 状态为 AI 决策模式
   - THEN 前端 SHALL 保持现有 AI 模式提示。

2. **v2 引擎 SHALL 暴露策略标的池**
   - User Story: 作为操作者，我希望策略检查下拉框列出 v2 本周期分析过的标的，以便切换查看。
   - WHEN v2 策略完成一次决策周期
   - THEN `/api/strategy/symbols` SHALL 返回该 trader 的分析标的和持仓标记。
   - IF 尚无策略周期结果
   - THEN 接口 SHALL 返回空标的列表而不是错误。

3. **v2 引擎 SHALL 返回信号报告**
   - User Story: 作为操作者，我希望 v2 买卖点能显示在 K 线上，以便检查信号来源和时间位置。
   - WHEN `/api/strategy/signals` 查询 v2 trader 和 symbol
   - THEN 后端 SHALL 返回与 v1 兼容的 `StrategySignalReport`，包含 `decision_mode=chanlun_v2`、策略名称、版本、timeframe 元数据、诊断消息和信号标记。
   - IF 当前 symbol 没有信号
   - THEN 后端 SHALL 返回空 `signals`、空或诊断性 `signal_markers`，并返回中文诊断消息。
   - WHEN 请求 `view=audit`、`layers`、`statuses`、`from`、`to` 或 `limit`
   - THEN v2 信号报告 SHALL 支持同等过滤字段，避免破坏前端现有控件。

4. **v2 K 线深度 SHALL 使用 v2 配置**
   - User Story: 作为操作者，我希望策略检查 K 线数量匹配 v2 策略分析深度，以便看到完整结构上下文。
   - WHEN `/api/market/klines` 查询 v2 trader 且未显式传入 `limit`
   - THEN 后端 SHALL 根据 `chanlun_v2_strategy.history_depth[timeframe]` 决定 K 线数量。
   - IF 配置深度超过接口上限
   - THEN 后端 SHALL 截断到上限并在 `limit_source` 中说明。
   - WHEN query 显式传入 `limit`
   - THEN query SHALL 优先于配置深度。

5. **实现 SHALL 保持 v1 兼容**
   - User Story: 作为操作者，我希望 v1 策略检查不受 v2 改动影响。
   - WHEN `decision_mode=programmatic`
   - THEN 现有 v1 策略检查 API、marker 过滤和 K 线深度行为 SHALL 保持不变。
   - IF v2 Rust 分析库不可用
   - THEN 已有策略决策错误处理 SHALL 保持，策略检查空报告不应因无历史信号直接失败。
