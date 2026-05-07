# 交易全流程评估与优化 — 实施总结

## 完成范围

本轮已按 spec 执行交易全流程优化，覆盖配置、AI 状态、开仓 gate、仓位 sizing、执行保护、交易计划作用域、自动平仓去重、日志/API/前端观测和 replay。

主要落地项：

- 修复 `Config.Validate()` 默认 `Exchange` 和 `ScanIntervalMinutes` 写回问题，并补配置测试。
- 将 `max_daily_loss`、`max_drawdown`、杠杆、扫描间隔、风险预算、AI 分析间隔和执行质量传入 `AutoTrader` 与 `decision.Context`。
- 增加 trader 级 AI 调用状态，区分 attempted、succeeded、failure 和 backoff，避免主动 wait 与 AI 失败混淆。
- 新增 `decision.EvaluateOpenGate()`，把 rolling gate、BTC 市场状态、相关性集中、short 侧 stricter gate、执行质量和 AI backoff 纳入开仓准入。
- 新增 `decision.CalculatePositionSizing()`，统一名义仓位、保证金、止损风险、手续费滑点、最小名义额和分批退出可行性。
- 新增执行 preflight、保护单结构化结果、止损重试、高危裸仓标记和可选 `EnableEmergencyClose`。
- 将交易计划迁移到 `trader_id:symbol:side` 作用域，同时保留旧 wrapper 和旧 `trade_plans.json` 兼容迁移。
- 抽取自动平仓事件处理和 dedupe key，统一 `syncAutoClosedOrders()` 与持仓快照检测路径。
- 扩展 `ExecutionQualityStats`、`DecisionRecord`、`DecisionAction`、`/api/performance` 测试和前端类型/策略学习展示。
- 新增 `cmd/replay` 与 `logger.BuildReplayReport()`，支持 report-only/dry-run 风格的离线 gate 对比报告。

## 2026-05-07 增量优化

当前 `decision_logs/` replay 默认排除 `.bak`/backup 目录后，读取 34,572 条日志，得到 161 笔闭合交易，理论 PnL -9.2138 USDT；最近20笔胜率 20.0%、Profit Factor 0.4490，最近3笔全部亏损。基于该风险基线，本次追加落地：

- 新增全局 rolling performance gate：最近20笔 PF<0.8 且胜率<35% 时暂停所有新开仓24小时；最近3笔连续亏损且总 PnL<0 时暂停12小时；冷却后仍以更高置信度和半风险降权。
- 将 `GlobalGate` 接入开仓验证和默认仓位 sizing，确保硬暂停和降权在决策验证、风险预算、自动补仓位中一致生效。
- 在决策合并后增加最终持仓上限硬拦截，超过3个持仓容量的新增开仓会被丢弃，并写入 CoT trace；若只剩被拒绝开仓，则返回 `wait`。同时将 `mergeDecisions()` 改为稳定保留输入顺序，避免容量不足时随机选择开仓。
- 在自适应分批止盈输出前检查本次平仓名义额和剩余仓位名义额，跳过交易所注定拒绝的小额 `partial_close`。

## P1/P2/P3 与外部校准

- P1：开仓验证失败会写入 `FullDecision.OpenRejections`，交易日志生成 `open_rejected` 动作，执行质量统计可累计真实风控拒绝原因；同时 `risk_state` 记录有效风险预算、AI backoff 和 open gate 原因。
- P2：候选币增加评分、数据质量、过滤原因、是否进入 prompt、行情源/执行交易所价差 warning，并写入 `candidate_details`。
- P3：`cmd/replay` 默认排除 `.bak`/backup 目录，并支持 `-trader`、`-from`、`-to`、`-include-backups`，避免离线评估口径被备份样本污染。
- 外部校准：新增 `cmd/calibrate-exchange`，已对 Aster 和 Binance 公开 `exchangeInfo` 生成校准报告；Aster 样本最小名义额为 5 USDT，当前系统开仓 10 USDT 更保守；Binance BTCUSDT/ETHUSDT 最小名义额分别为 50/20 USDT，已接入交易执行层的 symbol 级最小名义额校准。

## 验证结果

- `git diff --check` 通过。
- `go test ./logger ./api` 通过。
- `go test ./decision` 通过。
- `go test ./trader` 通过。
- `go test ./config ./decision ./logger ./trader ./api ./cmd/replay` 通过。
- `go test ./...` 通过。
- `node node_modules/typescript/bin/tsc` 在 `web/` 下通过。
- `cd web && npm run build` 通过。
- 2026-05-07 增量验证：`timeout 60s go test ./decision -run 'TestEvaluateOpenGate_GlobalRollingBlock|TestEnforceFinalDecisionLimits|TestEffectiveOpenGate_AppliesGlobalPenalty|TestEvaluateAdaptiveScaledExit' -count=1` 通过。
- 2026-05-07 增量验证：`go test ./logger -count=1`、`go test ./trader ./market -count=1` 通过。
- P1/P2/P3/外部校准验证：`go test ./logger ./cmd/replay ./cmd/calibrate-exchange -count=1`、`go test ./trader -run 'TestDeterminePartialClosePlan|TestCalibratedMinOrderValue|TestEvaluateExecutionPreflight' -count=1` 通过。

## 前端环境修复记录

本机最初的 `web/node_modules` 不健康：

- `npm run build` 最初失败于 `tsc: command not found`，原因是当前 `web/node_modules/.bin` 未生成。
- `npm rebuild` 和 `npm ci` 失败于 `esbuild` postinstall，`web/node_modules/esbuild/bin/esbuild` 在当前 macOS/Node 环境执行时报 `Unknown system error -88`。
- 处理方式：使用 `npm ci --ignore-scripts` 安装 JS 依赖，再用 `go install github.com/evanw/esbuild/cmd/esbuild@v0.25.11` 构建同版本 esbuild 二进制，并替换 `node_modules/@esbuild/darwin-arm64/bin/esbuild`。
- 修复后原始命令 `cd web && npm run build` 已通过。

本次 build 仍有非阻塞提示：Browserslist/baseline-browser-mapping 数据较旧，以及单个 JS chunk 超过 500 kB。两者不影响构建通过。

`npm ci --ignore-scripts` 完成后报告 8 个 npm audit 漏洞，其中 2 个 moderate、6 个 high；本轮未执行 `npm audit fix`，避免自动升级依赖带来额外前端变更。

## 上线建议

- 先使用 replay/report-only 观察新 gate 的拒绝原因、开仓减少量和理论风险变化。
- `EnableEmergencyClose` 保持默认关闭，确认各交易所保护单失败语义后再逐 trader 灰度开启。
- 重点监控保护单失败率、高危执行失败、open rejection reason、AI failure、unmatched action 和 auto-close dedupe 命中情况。
- 有真实 `decision_logs/{trader_id}` 后运行：

```bash
go run ./cmd/replay -log-dir decision_logs -output .kiro/specs/trading-flow-evaluation-optimization/replay-sample.json
```
