# Product Overview

NOFX 是一个面向加密货币永续合约市场的 agentic trading OS。当前实现已经形成闭环：多 trader 配置、候选币池、市场数据、AI 决策、确定性风控、交易所执行、订单追踪、决策日志、离线 replay 和 Web 监控。

## 产品定位

系统运行多个 AI trader。每个 trader 有独立的 `trader_id`、账户凭证、AI 模型、交易所配置、扫描周期和决策日志目录。当前支持：

- AI 提供方：DeepSeek、Qwen、自定义 OpenAI-compatible Chat Completions API。
- 交易所：Binance Futures、Hyperliquid、Aster DEX，统一收敛到 `trader.Trader` 接口。
- 市场范围：当前实盘执行和指标链路聚焦加密货币永续合约；更广市场愿景属于路线图，不应在代码中假设已经实现。

## 核心循环

每个 `AutoTrader` 首次启动后立即执行一个周期，之后按 `scan_interval_minutes` 定时运行：

1. 同步已有持仓和交易计划，检查止损/止盈等自动平仓事件。
2. 构建 `decision.Context`：账户、持仓、候选币、市场数据、历史表现、执行质量和风险策略。
3. 先用交易计划、失效条件、分批止盈、移动止损等逻辑评估已有持仓。
4. 在开仓频率策略允许时调用 AI 搜索新机会。
5. 对 AI 开仓建议做确定性校验和改写，包括 ATR/ADX profile、净 RR、相关性、亏损模式、仓位 sizing 和最小下单额。
6. 合并持仓管理决策与 AI 决策，并按“先平仓、后开仓”执行。
7. 写入 `decision_logs/{trader_id}/decision_*.json`，供前端、统计和离线 replay 使用。

## 关键能力

- 多智能体竞赛：多个 trader 可同时运行，前端展示收益曲线、账户状态、持仓和最新决策。
- 动态候选池：合并默认币、AI500、OI Top、交易所成交额 Top、当前持仓和历史表现，生成 `data/dynamic_candidate_pool.json` 快照。
- 多时间框架市场数据：3m、15m、1h、4h K 线，指标包括 EMA、MACD、RSI、ADX/DI、ATR、Bollinger、OI 和 funding。
- 交易计划生命周期：`TradePlan` 支持 trader scope、失效条件解析、持仓首次出现时间、分批止盈、移动止损、动态 TP 和已平仓统计。
- 风控与开仓准入：熔断、账户回撤硬停、总风险预算、相关性集中、亏损模式、频率档位、执行质量 gate、ATR/ADX profile 风控。
- 执行保护：下单前 preflight、交易所最小名义额校准、同币同方向防叠仓、止损和止盈取消逻辑分离、保护单失败风险标记。
- 可观测性：决策日志记录 prompt、CoT、执行动作、候选币详情、risk state、open rejection、策略病因诊断和 rolling performance。
- 运维工具：`cmd/replay` 做离线复盘和日志对账，`cmd/calibrate-exchange` 校准交易所最小下单限制。

## 产品边界

- 这是自动交易实验系统，不保证收益；默认应小额、受控、可回滚地运行。
- 不允许在仓库中提交真实 API key、私钥、助记词、真实账户地址或生产资金配置。
- 测试和 replay 不应触发真实下单；需要交易所数据时优先使用只读接口、fixture 或 mock。

## 语言约定

日志、错误信息、注释、面向用户的后端信息和项目文档优先使用中文。代码标识符、JSON 字段、API 路径、包名和函数名使用英文。
