# 程序化策略回测 Spec 交叉验证

## 验证结论

`requirements.md`、`design.md` 和 `tasks.md` 当前整体一致，可以进入实现。三份文档均保持以下关键约束：

- 回测仅用于本地开发测试环境，不部署到 161，不接入真实交易执行。
- 回测复用现有程序化策略与公共风控，但通过历史行情 provider 隔离实时行情、AI 和生产状态文件。
- 历史数据抓取区间 `data_from/data_to`、正式统计区间 `backtest_from/backtest_to` 和 `warmup_from` 已区分。
- 主交易级别支持 `15m/1h/4h`，并保留无新主级别 K 线时已有持仓管理继续运行的语义。
- 独立回测操作页面已经进入 requirements、design 和 tasks，且入口/API 默认生产禁用。
- 蜡烛图复盘要求已覆盖买点下方、卖点上方、多信号分层、`signal_close_time` 与 `decision_close_time` 成对显示。

## 查漏补缺

1. `chanlun.Engine` 的 provider 注入通道需要在实现中补齐。
   - 原因：design 已要求 `decision.PrepareCycleContext()` 接收历史行情 provider，但现有 Engine 是调用者，必须把 provider/clock 从回测 runner 传入 Engine。
   - 方案：为 `strategy/chanlun.Engine` 增加可选 `MarketDataProvider` 和 `DisableOITopFetch` 字段；`GetFullDecision()` 调用 `PrepareCycleContext()` 时带入这些字段。

2. 本地回测 API 需要明确默认关闭策略。
   - 原因：requirements 要求生产前端不显示入口；design 仅给出环境变量示例。
   - 方案：后端按 `NOFX_BACKTEST_API_ENABLED=true` 注册 `/api/backtest/*`；前端先请求 `/api/backtest/health`，只有启用时才显示 `#/backtest` 入口。

3. SQLite driver 选择需要在实现中落地。
   - 原因：requirements 允许“SQLite 或等价本地嵌入式存储”，design 建议优先 SQLite。
   - 方案：使用纯 Go `modernc.org/sqlite`，降低 CGO 环境要求；历史库默认路径仍为 `backtest_data/nofx_history.sqlite`。

4. 回测 v1 的非目标需要在报告中显式声明。
   - 原因：funding、liquidation、历史 OI 首期不强制建模，容易造成收益高估。
   - 方案：报告固定输出 `funding_mode=disabled`、`oi_mode=disabled`、`liquidation_mode=not_modelled`，并写入 assumptions。

5. tasks 规模较大，执行时按可验证阶段推进。
   - 原因：本功能横跨 historydb、market、decision、backtest、cmd、api、web。
   - 方案：每完成一个可测试阶段更新 `tasks.md` 勾选；无法在本轮完整满足的条目不提前勾选。

## 实现顺序

1. 本地忽略规则、配置解析、时间语义和脱敏快照。
2. `historydb` SQLite store、数据质量、抓取 source、CLI。
3. `market.BuildDataFromKlines()` 无网络构建入口和 decision/Engine provider 注入。
4. `backtest` provider、paper broker、runner、report、batch。
5. 本地回测 API、job manager、前端独立页面和 marker 复盘。
6. 测试、示例配置、任务勾选与最终验证。
