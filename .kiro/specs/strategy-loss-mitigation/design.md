# 策略亏损缓解 — 设计文档

## 日志结论

最近 1 小时内没有发生新交易，亏损不是最近 1 小时产生的，而是历史已实现亏损沉淀到账户净值中。最近日志暴露出两个工程问题：

1. `GetFullDecision()` 在 AI API 调用失败时只写进进程日志，继续返回空决策，最终 `DecisionRecord.success=true` 且 `cot_trace=无决策输出`。这会让后续复盘无法区分“模型主动观望”和“AI 链路失败”。
2. 账户已约 -25% 回撤，超过 `max_drawdown=20.0`，但当前决策模块只有冷却型熔断，冷却结束后会以新的回撤水平作为基准继续允许交易，缺少最大账户回撤硬停。

历史交易层面，亏损集中在 BCH、ASTER、LTC、XRP 等品种；策略多次在高相关山寨币上顺势开仓，随后在失效条件或止损附近亏损退出。代码已有单笔风险和 RR 校验，但缺少账户级“亏到阈值后停止继续试错”的硬约束。

## 设计

### 1. AI 失败显式返回错误

修改 `decision.GetFullDecision()`：

- 构造 `userPrompt` 后，成功和失败路径都写入 `FullDecision.UserPrompt`。
- `mcpClient.CallWithMessages()` 返回 error 时，构造包含 `ALL wait` 的 `FullDecision`，`CoTTrace` 写明 AI 调用失败，并返回非 nil error。
- `ExtractDecisionsRobust()` 返回 error 时，构造包含 `ALL wait` 的 `FullDecision`，`CoTTrace` 写明解析失败和响应摘要，并返回非 nil error。

`trader.AutoTrader.runCycle()` 已经在 `err != nil` 时把记录标记为失败并保存，因此不需要扩大调用面。

### 2. 最大账户回撤硬停

新增决策级配置：

- `decision.Config.MaxAccountDrawdownPct`
- 包级配置值默认 20.0
- 归一化函数：`0 < value <= 1` 视为比例并乘以 100；`value > 1` 视为百分比；`value <= 0` 使用默认值

修改 `main.initializeModules()`：

- 将 `cfg.MaxDrawdown` 传入 `decision.Config.MaxAccountDrawdownPct`

修改 `decision.Context`：

- 增加 `MaxAccountDrawdownPct`
- `initializeDefaults()` 从全局配置注入默认值

修改 `decision.GetFullDecision()`：

- 保留入口熔断、市场数据、熔断检查、相关性计算和已有持仓评估。
- 在调用 AI 前检查账户总回撤。
- 若触发硬停，返回持仓管理决策 + `ALL wait`，不调用 AI。

### 3. 测试策略

目标测试放在 `decision/decision_test.go`：

- `normalizeAccountDrawdownPct` 的 20.0、0.2、0 值行为。
- `isAccountDrawdownHardStopped` 在 -25% vs 20%、-19.9% vs 20%、阈值为 0 的行为。

验证命令：

```bash
go test ./decision
go test ./...
```
