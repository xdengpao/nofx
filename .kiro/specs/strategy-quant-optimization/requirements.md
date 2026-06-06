# 策略量化优化 — 需求文档

## 背景

截至 2026-05-06 13:07 CST，`aster_deepseek` 历史决策日志共 34,001 条，覆盖 2026-02-24 至 2026-05-06。按开平仓记录配对得到 154 笔已闭合交易，估算总 PnL 为 -8.6894 USDT，胜率 37.66%，Profit Factor 0.7355，最大账户净值回撤约 -18.57%。

亏损集中在 BCHUSDT、ASTERUSDT、LTCUSDT、XRPUSDT；做空侧表现明显更差，46 笔空单合计约 -6.9857 USDT。执行层还有 284 次 `partial_close` 失败，主要原因是部分平仓订单名义额小于交易所 5 USDT 最小限制。当前 DeepSeek API 还返回 402 `Insufficient Balance`，会导致新开仓 AI 决策不可用。

## 需求

### Requirement 1: 建立可复现的交易表现归因

1.1 WHEN 读取历史 `decision_*.json`，THEN 系统 SHALL 能按时间顺序配对 `open_long/open_short` 与 `close_long/close_short/auto_close_*`，生成闭合交易样本。

1.2 WHEN 生成交易样本，THEN 每笔交易 SHALL 包含 symbol、side、open_time、close_time、open_price、close_price、quantity、leverage、pnl_usd、pnl_pct、open_reason、close_reason。

1.3 WHEN 统计策略表现，THEN 系统 SHALL 输出总体胜率、Profit Factor、平均盈利、平均亏损、最大回撤、按 symbol/side 的 PnL 和胜率。

1.4 WHEN 决策日志缺少可配对开仓或平仓，THEN 系统 SHALL 标记为 unmatched，不得静默计入胜率。

1.5 WHEN 当前日志 `DecisionAction` 不包含开平仓 reasoning，THEN 系统 SHALL 通过扩展日志字段或从 `decision_json` 回填的方式补齐 open_reason/close_reason。

### Requirement 2: 引入滚动绩效降权和禁交易名单

2.1 WHEN 某 symbol 在最近至少 5 笔闭合交易中 Profit Factor < 0.8 且总 PnL < 0，THEN 新开仓候选 SHALL 被降权或过滤。

2.2 WHEN 某 symbol 在最近至少 8 笔闭合交易中 Profit Factor < 0.5，THEN 系统 SHALL 进入 24 小时禁交易冷却。

2.3 WHEN 某 side 在最近至少 20 笔闭合交易中 Profit Factor < 0.8，THEN 同方向新开仓 SHALL 提高置信度门槛到 90，并将单笔风险减半。

2.4 WHEN symbol 处于禁交易冷却，THEN 持仓管理 SHALL 继续执行，但 SHALL NOT 新增该 symbol 同方向仓位。

### Requirement 3: 执行层必须避免无效部分平仓

3.1 WHEN `partial_close` 的预计平仓名义额 < 5 USDT，THEN 系统 SHALL NOT 向交易所提交该部分平仓订单。

3.2 WHEN `partial_close` 后预计剩余仓位名义额 < 10 USDT，THEN 系统 SHALL 自动改为全平，避免残留小额仓位。

3.3 WHEN `partial_close` 因名义额过小被跳过，THEN 系统 SHALL 至少执行保护动作：移动止损、记录该档位状态或输出明确跳过原因，避免每个周期重复失败。

3.4 WHEN 部分平仓失败，THEN 失败 SHALL 计入执行质量指标，并在前端/日志可见。

3.5 WHEN 执行动作为 `auto_close_long` 或 `auto_close_short`，THEN 历史归因 SHALL 按对应方向纳入闭合交易统计。

### Requirement 4: 强化开仓质量门槛

4.1 WHEN 新开仓候选是历史亏损名单中的 BCHUSDT、ASTERUSDT、LTCUSDT、XRPUSDT，THEN 系统 SHALL 要求更高置信度、更低风险和更严格多时间框架一致性。

4.2 WHEN BTC 市场状态为 RANGING 或 SQUEEZE，THEN 山寨币趋势跟随新开仓 SHALL 降低频率或禁用，除非候选币种相对 BTC 低相关且自身 ADX/DI/成交量同时确认。

4.3 WHEN 候选 symbol 与 BTC 相关性 > 0.8，THEN 同方向已有高相关持仓时 SHALL 禁止新增。

4.4 WHEN 新开仓是 short，THEN SHALL 要求趋势强度和失效条件更严格，因为历史 short 侧 PnL 显著为负。

### Requirement 5: 风险应随绩效动态收缩

5.1 WHEN 最近 10 笔闭合交易 Profit Factor < 1.0，THEN `MaxRiskPerTrade` SHALL 从 2% 降到 1%。

5.2 WHEN 最近 20 笔闭合交易 Profit Factor < 0.8，THEN `MaxRiskPerTrade` SHALL 降到 0.5%，且每日新开仓上限降到 1 笔。

5.3 WHEN 最近 10 笔闭合交易 Profit Factor > 1.3 且最大回撤小于 5%，THEN 系统 MAY 逐步恢复到默认风险。

5.4 WHEN AI API 不可用或余额不足，THEN 系统 SHALL 禁止新开仓，且不得把该周期计入交易策略胜率。

### Requirement 6: 测试和验收

6.1 SHALL 添加历史日志归因统计的单元测试或 fixture 测试。

6.2 SHALL 添加 `partial_close` 最小名义额保护测试。

6.3 SHALL 添加 symbol/side rolling performance gate 测试。

6.4 SHALL 运行 Go 测试和前端构建，确保优化不破坏现有服务。
