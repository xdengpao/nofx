# 可观测性字段审计

## 兼容原则

旧 `decision_logs` 继续可读。新增字段均为可选字段或零值字段，旧日志 JSON 反序列化不会失败；前端 TypeScript 字段也使用可选类型。

## `logger.DecisionRecord`

新增 `risk_state`，用于记录周期级风险状态：

- `trader_id`、`exchange`
- `max_risk_per_trade`、`effective_max_risk_per_trade`
- `total_risk_budget`、`remaining_risk_budget`
- `max_daily_loss_pct`、`max_account_drawdown_pct`
- `ai_backoff_until`、`consecutive_ai_fails`
- `open_gate_reasons`

## `logger.DecisionAction`

新增 action 级风险和执行字段：

- `risk_usd`
- `gate_state`、`gate_reasons`
- `execution_risk`
- `stop_loss_set`、`take_profit_set`
- `protection_error`
- `high_risk`、`high_risk_reason`
- `remaining_position_usd`

## `ExecutionQualityStats`

新增统计：

- `open_attempts`
- `open_failures`
- `open_rejected_count`
- `protection_order_failures`
- `high_risk_execution_failures`
- `recent_high_risk_errors`
- `recent_open_rejection_reasons`
- `protection_order_failure_rate`
- `high_risk_execution_failure_rate`

识别规则兼容旧日志：如果新结构化字段不存在，仍会从 `error`、`protection_error`、`high_risk_reason` 和旧错误文本中识别 AI 失败、保护单失败、开仓拒绝和高危裸仓。
