# 策略亏损缓解 — 需求文档

## 背景

截至 2026-05-06 10:29 CST，最近 1 小时 `aster_deepseek` 共有 20 条决策日志，周期 6816-6835。账户净值稳定在约 59.995 USDT，初始资金 80 USDT，累计盈亏约 -20.0045 USDT，回撤约 -25.0%。该 1 小时内没有持仓、没有开平仓动作，`input_prompt` 为空，`decision_json` 为空，`cot_trace` 为“无决策输出”，但 `success=true`。

向前追溯同一 trader 的历史动作，主要亏损来自多笔趋势跟随交易在失效条件触发后亏损退出，且账户已超过 `config.json` 中 `max_drawdown=20.0` 的最大回撤阈值。

## 需求

### Requirement 1: AI 调用失败必须可观测

1.1 WHEN AI API 调用失败，THEN 决策记录 SHALL 保存本次 `user_prompt`，并将 `success` 标记为 `false`。

1.2 WHEN AI API 调用失败，THEN `cot_trace` SHALL 包含明确的失败原因，而不是“无决策输出”。

1.3 WHEN AI 响应解析失败，THEN 决策记录 SHALL 保存本次 `user_prompt` 和响应摘要，并将 `success` 标记为 `false`。

1.4 WHEN AI 明确输出 `wait`，THEN 系统 SHALL 继续将该周期视为成功观望，不与调用失败混淆。

### Requirement 2: 最大账户回撤硬停

2.1 WHEN 账户总回撤达到或超过配置的最大回撤阈值，THEN 系统 SHALL 禁止新的 AI 开仓机会搜索。

2.2 WHEN 最大账户回撤硬停触发且当前无持仓，THEN 系统 SHALL 记录 `ALL wait` 决策，reasoning 明确写出当前回撤和阈值。

2.3 WHEN 最大账户回撤硬停触发但仍有持仓，THEN 系统 SHALL 继续执行已有持仓管理决策，但 SHALL NOT 调用 AI 搜索新开仓。

2.4 WHEN `max_drawdown` 以 `20.0` 形式配置，THEN 系统 SHALL 按 20% 解释；WHEN 以 `0.2` 形式配置，THEN 系统 SHALL 按 20% 解释。

### Requirement 3: 保留现有交易安全约束

3.1 开仓验证 SHALL 继续校验止损止盈、风险回报比、单笔风险、总风险预算、杠杆上限和重复持仓。

3.2 持仓管理 SHALL 继续优先于新开仓。

3.3 调整止损和止盈 SHALL 继续分别使用 `CancelStopLossOrders()` 和 `CancelTakeProfitOrders()`。

### Requirement 4: 测试

4.1 SHALL 添加最大回撤阈值归一化测试。

4.2 SHALL 添加最大回撤硬停判断测试。

4.3 SHALL 运行目标包测试验证补丁。
