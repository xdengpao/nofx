# 交易全流程评估报告

## 评估基线

本轮评估基于代码审计、现有单元测试和当前 `decision_logs/` 离线 replay，并完成了主要 P0/P1 交易安全优化实现。

2026-05-07 增量 replay 默认排除 `.bak`/backup 目录后，共读取 34,572 条决策日志，配对 161 笔闭合交易，理论 PnL 为 -9.2138 USDT；最近20笔交易 PnL 为 -3.7338 USDT，胜率 20.0%，Profit Factor 0.4490，最近3笔全部亏损。该口径更接近当前线上目录；若需要复盘历史备份，可显式使用 `cmd/replay -include-backups`。

## 全流程结论

| 流程节点 | 当前状态 | 主要风险 | 优化方向 |
| --- | --- | --- | --- |
| 配置与启动 | 多 trader、交易所、AI key、杠杆、扫描间隔已配置化 | 默认值写回曾失效；风险预算仍以默认值为主 | 已修复默认值写回；风险参数先端到端传透，后续再引入 `RiskConfig` |
| 数据与候选币池 | AI500、OI Top、默认币池可合并，候选币有来源 | 非 Binance 执行时仍需关注行情源价差 | 已增加候选标的评分、数据质量、过滤原因、是否进入 prompt 和行情源/执行所价差 warning |
| 账户与上下文 | `buildTradingContext()` 汇总账户、持仓、候选币、历史表现 | trader 级 AI 状态、执行质量、风险参数曾不完整 | 已补 `TraderID`、`Exchange`、AI 状态、风险预算、执行质量 |
| AI 调用与解析 | 支持 OpenAI-compatible API，解析失败会保留 trace | AI 分析间隔曾不跨周期，失败和主动 wait 易混淆 | 已补 `FullDecision` AI 调用状态和 AutoTrader 持久状态 |
| 风控与熔断 | 有全局熔断、rolling performance gate、最大账户回撤硬停 | 近期样本显示全局负期望，继续开仓会放大亏损 | 已让熔断读取上下文 `MaxDailyLossPct`；新增全局 rolling gate，最近20笔 PF<0.8 且胜率<35% 暂停新开仓24小时，最近3笔连续亏损暂停12小时 |
| 开仓准入 | 已校验市场数据、重复持仓、杠杆、仓位、RR、单笔/总风险 | 行为变化需要按 trader/time window replay 量化误伤 | 已抽取 `EvaluateOpenGate()`，统一拒绝原因，并接入市场、相关性、执行质量、AI backoff 和全局 rolling gate；最终合并后再硬拦截超过3个持仓容量的新增开仓 |
| 仓位 sizing | 已有 AI 仓位与风险校验，rolling 可收缩单笔风险 | 真实滑点和交易所本地精度仍需持续校准 | 已新增 position sizing 模块，所有 AI 仓位走同一验证；Aster/Binance exchangeInfo 外部校准已生成 |
| 交易执行 | 统一 `Trader` 接口，先平后开，执行动作会写日志 | 紧急平仓默认关闭，需灰度启用 | 已实现保护单结构化结果、止损重试、高危裸仓标记和 `EnableEmergencyClose` |
| 持仓与平仓 | 持仓计划、移动止损、分批止盈、小额保护较完整 | 动态 TP 可能只更新本地计划，未同步交易所保护单 | 自适应分批止盈新增可执行性检查，避免小仓位反复输出注定失败的 `partial_close`；仍需明确本地计划与交易所 TP 的同步边界 |
| 自动平仓与订单追踪 | `OrderTracker` 可识别 SL/TP/强平，快照路径也能检测仓位消失 | 无 order id 的降级识别仍需线上验证 | 已引入 auto-close event 和 exactly-once key |
| 日志/API/前端 | `/api/performance` 已含 rolling、execution、unmatched，前端有策略学习视图 | 前端依赖存在 audit 漏洞和 bundle size 警告 | 已扩展 execution quality 和前端展示字段，`npm run build` 已通过 |
| Replay 与灰度 | 已新增离线 replay/report-only 输出 | 时间窗和 trader 过滤仍需上线时明确 | replay 默认排除备份目录，支持 `-trader`、`-from`、`-to`、`-include-backups` |

## Phase 2 配置与状态传递路径

| 参数/状态 | 端到端路径 | 当前结果 |
| --- | --- | --- |
| `max_daily_loss` | `config.Config.MaxDailyLoss` -> `manager.AddTrader()` -> `AutoTraderConfig.MaxDailyLoss` -> `buildTradingContext().MaxDailyLossPct` -> `decision.CheckCircuitBreaker()` | 已接入；未设置时保留 10% 默认熔断阈值 |
| `max_drawdown` | `config.Config.MaxDrawdown` -> `decision.Initialize(MaxAccountDrawdownPct)` 和 `AutoTraderConfig.MaxDrawdown` -> `Context.MaxAccountDrawdownPct` -> `isAccountDrawdownHardStopped()` | 已接入；支持 `0.2` 和 `20` 两种写法 |
| 杠杆 | `config.Leverage` -> `manager.AddTrader()` -> `AutoTraderConfig` -> `Context.BTCETHLeverage/AltcoinLeverage` -> `validateOpenDecision()` | 已接入 |
| 扫描间隔 | `TraderConfig.ScanIntervalMinutes` -> `Validate()` 默认写回 -> `GetScanInterval()` -> `AutoTraderConfig.ScanInterval` -> ticker 与 AI 失败 backoff | 已接入 |
| 单笔风险预算 | `trader.DefaultMaxRiskPerTrade` -> `manager.AddTrader()` -> `AutoTraderConfig.MaxRiskPerTrade` -> `Context.MaxRiskPerTrade`，rolling 后生成 `EffectiveMaxRiskPerTrade` | 已接入；后续可迁移到 `RiskConfig` |
| 总风险预算 | `trader.DefaultTotalRiskBudget` -> `manager.AddTrader()` -> `AutoTraderConfig.TotalRiskBudget` -> `Context.TotalRiskBudget` -> `calculateRemainingRiskBudget()` | 已接入 |
| AI 分析间隔 | `trader.DefaultAnalysisInterval` -> `manager.AddTrader()` -> `AutoTraderConfig.AnalysisIntervalMin` -> `Context.AnalysisIntervalMin` -> `shouldCallAIForNewOpportunities()` | 已接入 |
| AI 调用状态 | `FullDecision.AICallAttempted/Succeeded/FailureReason` -> `AutoTrader.applyAICallState()` -> 下一周期 `buildTradingContext()` | 已接入；成功刷新 `LastAnalysisTime`，失败只刷新 attempt/backoff |
| 执行质量 | `DecisionLogger.AnalyzePerformance()` -> `PerformanceAnalysis.Execution` -> `Context.ExecutionQuality` | 已接入；粒度后续在 Phase 1 扩展 |

## 优化项优先级

| 优化项 | 优先级 | 预期收益 | 误伤风险 | 复杂度 | 验证方式 | 回滚策略 |
| --- | --- | --- | --- | --- | --- | --- |
| 配置默认值写回与 Phase 2 状态持久 | P0 | 避免配置失效和 AI 频繁调用 | 低 | 低 | `go test ./config ./decision ./trader` | 回退对应字段注入与状态更新 |
| 开仓后 SL/TP 失败补救 | P0 | 避免裸仓和不可控亏损 | 中 | 中 | fake trader 覆盖 SL 失败、重试、紧急平仓 | 配置关闭 `EnableEmergencyClose`，保留 warn 模式 |
| 自动平仓 exactly-once | P0 | 避免重复统计和重复清理计划 | 中 | 中高 | 双路径同事件、无 order id 降级测试 | 关闭新 dedupe，仅保留原路径 |
| OpenGate 结构化拒绝 | P1 | 提升可解释性，降低坏开仓 | 中 | 中 | RR、预算、rolling、市场状态、相关性测试 | report-only 模式不硬拦截 |
| Position sizing 统一计算 | P1 | 将 AI 仓位转为账户风险口径 | 中 | 中 | ATR、手续费滑点、最小名义额、保证金测试 | 保留旧 AI sizing 上限校验 |
| 执行质量统计扩展 | P1 | 让保护单失败/开仓拒绝进入后续 gate | 低 | 中 | logger fixture 和 API 响应测试 | 旧字段保持兼容，新字段可选 |
| 候选币数据质量与过滤原因 | P2 | 降低残缺数据驱动开仓 | 低 | 中 | pool/market 降级 fixture | 仅写日志，不影响开仓 |
| 前端 StrategyHealth | P2 | 提升人工巡检效率 | 低 | 中 | TypeScript build 和组件测试 | 隐藏新增组件入口 |
| 离线 replay/report-only | P2 | 上线前量化误伤和收益 | 低 | 中高 | replay fixture 对比报告 | 不接入实盘执行路径 |
| 全局负期望开仓暂停 | P0 | 避免最近窗口显著负期望时继续扩大战损 | 中 | 低 | rolling gate 单元测试与 replay 验证 | 移除 `GlobalGate` 接入或调高触发阈值 |
| 最终持仓容量硬拦截 | P0 | 防止 AI 或合并逻辑绕过3个持仓上限 | 低 | 低 | 决策合并后单元测试 | 回退 `enforceFinalDecisionLimits()` 调用 |
| 分批止盈可执行性前置检查 | P0 | 降低 99% 级别 partial close 失败噪声和无效订单 | 低 | 低 | 小仓位/可执行仓位单元测试 | 回退 `canExecuteScaledExit()` 检查 |
| Replay 样本口径过滤 | P3 | 避免备份目录污染离线评估 | 低 | 低 | replay filter 单元测试 | 使用 `-include-backups` 恢复旧递归口径 |
| 外部 exchangeInfo 校准 | 校准 | 校验最小名义额、tick/step size，减少交易所拒单 | 低 | 低 | `cmd/calibrate-exchange` fixture 和公开端点报告 | 保留系统默认值，校准映射可按报告回退 |

## 本轮实现后剩余风险

1. 当前 replay 已默认排除备份目录，但策略收益、误伤率和退出原因分布仍需按 trader/time window 复跑补证。
2. `EnableEmergencyClose` 默认关闭；实盘开启前应先观察保护单失败率和交易所撤单/平仓语义。
3. 动态 TP 仍需要进一步明确“本地计划更新”和“交易所保护单更新”的同步边界。
4. 无保护单 order id 的自动平仓降级识别虽然已去重，仍建议用真实成交历史验证置信度。
5. 前端 `npm run build` 已通过，但 npm 依赖审计仍提示 8 个漏洞，且 Vite 输出单个 JS chunk 超过 500 kB；建议另开依赖升级/代码分包任务处理。
