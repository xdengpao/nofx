# 交易全流程评估与优化 — 任务

## Phase 0: 规格和基线确认

- [x] 0.1 复核 `requirements.md`、`design.md` 与当前代码实现的一致性，标记已实现、部分实现和未实现项
- [x] 0.2 检查本地或线上是否存在可用 `decision_logs/{trader_id}`，确认本轮评估使用代码审计、历史日志归因或两者结合
- [x] 0.3 生成交易全流程评估报告草稿，覆盖配置、数据、AI、风控、开仓、执行、持仓、平仓、订单追踪、日志/API/前端
- [x] 0.4 为每个优化项标注优先级、预期收益、误伤风险、复杂度、验证方式和回滚策略

## Phase 1: 评估与可观测性

- [x] 1.1 审计 `logger.DecisionRecord` 和 `logger.DecisionAction` 字段，设计兼容旧日志的风险状态、门控原因和执行风险字段
- [x] 1.2 扩展执行质量统计，增加保护单失败、高危执行失败、AI 失败、开仓拒绝等计数
- [x] 1.3 增强 `logger.BuildExecutionQuality()`，从决策日志中识别 partial close 失败、AI 失败、保护单失败和 unmatched 行为
- [x] 1.4 增强 `logger.BuildTradeOutcomes()` 或其调用层，确保 `auto_close_*`、unmatched、reasoning 回填继续兼容
- [x] 1.5 扩展 `/api/performance` 响应，返回 rolling gate、执行质量、unmatched 和最近高危错误
- [x] 1.6 在 `web/src/types/index.ts` 增加可选类型字段，保持旧后端响应兼容
- [x] 1.7 在前端策略学习或新增策略健康组件中展示有效风险、禁交易/降权名单、执行质量和最近拒绝原因
- [x] 1.8 添加 logger/API/frontend 相关测试或类型检查
- [x] 1.9 运行 `go test ./logger ./api`
- [x] 1.10 若改动前端，运行 `cd web && npm run build` — 已通过；环境修复方式：`npm ci --ignore-scripts` 后用 `go install github.com/evanw/esbuild/cmd/esbuild@v0.25.11` 构建同版本本地二进制，并替换 `node_modules/@esbuild/darwin-arm64/bin/esbuild`

## Phase 2: 配置与运行时状态修复

- [x] 2.1 修复 `config.Config.Validate()` 中 `Exchange` 和 `ScanIntervalMinutes` 默认值写入 range copy 的问题
- [x] 2.2 为配置默认值写回添加 `config` 单元测试
- [x] 2.3 梳理 `max_daily_loss`、`max_drawdown`、杠杆、扫描间隔、风险预算和 AI 分析间隔的端到端传递路径
- [x] 2.4 将有效风险参数和分析间隔补充到 `trader.AutoTraderConfig`
- [x] 2.5 在 `AutoTrader` 中增加 trader-scoped AI 分析状态：last attempt、last success、last analysis、backoff 和连续失败次数
- [x] 2.6 在 `buildTradingContext()` 注入 `TraderID`、`Exchange`、`LastAnalysisTime`、有效风险预算、最大账户回撤和执行质量
- [x] 2.7 扩展 `decision.FullDecision`，返回 AI 是否尝试、是否成功和失败原因
- [x] 2.8 在 `runCycle()` 中根据 `FullDecision` 更新 `AutoTrader` 的 AI 分析状态，确保分析间隔跨周期生效
- [x] 2.9 为 `shouldCallAIForNewOpportunities()` 和 `AutoTrader` AI 频率状态添加测试
- [x] 2.10 运行 `go test ./config ./decision ./trader`

## Phase 3: 开仓门控与仓位 sizing

- [x] 3.1 新增 `decision/open_gate.go`，定义 `OpenGateInput`、`OpenGateResult` 和 `EvaluateOpenGate()`
- [x] 3.2 将 `validateOpenDecision()` 逐步改为调用 `EvaluateOpenGate()`，保留现有错误语义和测试兼容性
- [x] 3.3 新增 `decision/position_sizing.go`，实现以账户风险为中心的仓位 sizing 结果结构和计算函数
- [x] 3.4 将 AI 提供的 `position_size_usd` 纳入统一 sizing 校验，验证名义仓位、保证金、止损风险、手续费滑点和最小名义额
- [x] 3.5 增加 BTC 市场状态 gate：崩盘禁新仓，高波动/震荡对山寨趋势跟随降频或降仓
- [x] 3.6 增加相关性集中度 gate，限制已有同向高相关持仓后的新增风险
- [x] 3.7 增加 short 侧默认更严格门控，校验置信度、趋势强度、BTC/ETH 方向和失效条件
- [x] 3.8 将 rolling symbol/side gate、执行质量 gate、AI backoff gate 合并进开仓准入结果
- [x] 3.9 在 `FullDecision.CoTTrace` 或决策日志中记录开仓被拒绝的结构化原因
- [x] 3.10 为 open gate 添加单元测试：RR 不足、风险超限、rolling block、short 置信度不足、相关性集中、市场崩盘
- [x] 3.11 为 position sizing 添加单元测试：ATR 止损、手续费滑点、可用保证金限制、最小名义额和无法分批退出
- [x] 3.12 运行 `go test ./decision`

## Phase 4: 交易执行保护和失败补救

- [x] 4.1 在 `trader` 包新增执行 preflight 结构，检查数量、名义额、价格边界、精度和重复持仓
- [x] 4.2 在 `executeOpenLongWithRecord()` 和 `executeOpenShortWithRecord()` 下单前接入 preflight
- [x] 4.3 将开仓后止损/止盈设置结果改为结构化记录，写入 `DecisionAction` 或执行日志
- [x] 4.4 为止损设置失败实现有限重试
- [x] 4.5 设计并实现 `EnableEmergencyClose` 行为：止损保护无法建立时按配置紧急平仓或标记高危裸仓
- [x] 4.6 确认调整止损继续只调用 `CancelStopLossOrders()`，调整止盈继续只调用 `CancelTakeProfitOrders()`
- [x] 4.7 针对 Hyperliquid/Aster 联动取消风险，保留并测试旧保护单查询与恢复逻辑
- [x] 4.8 确保部分平仓成功后剩余仓位重新设置保护止损，缺少新止损时回退到有效计划止损
- [x] 4.9 将高危执行失败反馈到执行质量统计，并用于后续开仓 gate
- [x] 4.10 添加 fake trader 测试：开仓成功但止损失败、紧急平仓、止盈恢复、部分平仓剩余保护、小额订单跳过
- [x] 4.11 运行 `go test ./trader`

## Phase 5: trader-scoped 计划和自动平仓 exactly-once

- [x] 5.1 为 `decision.Context`、交易计划和订单追踪补充 `TraderID` 作用域
- [x] 5.2 设计 `TradePlanManager` 新 key：`trader_id:symbol:side`
- [x] 5.3 为旧 `data/trade_plans.json` 设计兼容读取和备份迁移路径
- [x] 5.4 逐步迁移 `GetPlan`、`UpdatePlan`、`OnPositionOpened`、`OnPartialClose`、`OnStopLossUpdated`、`OnPositionClosed` 调用点
- [x] 5.5 保留短期兼容 wrapper，避免一次性修改导致测试大面积失效
- [x] 5.6 新增自动平仓事件结构和 dedupe key 生成逻辑
- [x] 5.7 将 `syncAutoClosedOrders()` 和 `detectAutoClosedPositions()` 统一接入 dedupe
- [x] 5.8 抽取统一 `handleAutoCloseEvent()`，负责统计更新、计划清理、订单追踪停止、孤儿订单撤销和日志写入
- [x] 5.9 为同 symbol 多 trader、同事件双路径发现、无 order id 降级识别添加测试
- [x] 5.10 运行 `go test ./decision ./trader ./logger`

## Phase 6: 离线 replay、dry-run 与灰度上线

- [x] 6.1 设计离线 replay 输入输出格式，支持读取 `decision_logs/{trader_id}` 或指定日志目录
- [x] 6.2 新增 replay 工具，输出新旧 gate 对比：开仓次数、拒绝原因、风险使用率、理论 PnL 和执行失败率
- [x] 6.3 支持 report-only 模式：计算新 gate 结果但不阻止实盘开仓，只写入日志/API
- [x] 6.4 支持 dry-run 或 paper 模式，确保不会调用真实交易所下单接口
- [x] 6.5 生成 replay 报告样例，并记录本地缺少历史日志时的阻塞说明
- [x] 6.6 为 replay 核心归因和 gate 对比添加 fixture 测试
- [x] 6.7 运行 `go test ./logger` 以及 replay 工具相关测试

## Phase 7: 全量验证和交付

- [x] 7.1 对照 `requirements.md` 检查每条需求是否被设计和任务覆盖
- [x] 7.2 对照 `design.md` 检查每个阶段是否有独立可验证任务
- [x] 7.3 运行目标包测试：`go test ./config ./decision ./logger ./trader ./api`
- [x] 7.4 若触及共享后端合约，运行 `go test ./...`
- [x] 7.5 若触及前端，运行 `cd web && npm run build` — 已通过；Vite build 输出 `dist/index.html`、CSS 和 JS bundle，仅保留 chunk size 与 Browserslist 数据过期提示
- [x] 7.6 汇总实现结果、剩余风险、未跑测试原因和上线开关建议
