---
inclusion: always
---

# NOFX 量化交易系统 - 项目概览

## 项目简介

NOFX 是一个 AI 驱动的加密货币量化交易操作系统，Go 后端 + React/TypeScript 前端。
模块名: `nofx`，Go 1.25+。

## 核心架构

```
main.go                    → 系统入口、信号处理、优雅退出
config/config.go           → JSON 配置加载与验证
manager/trader_manager.go  → 多 Trader 并发管理
trader/auto_trader.go      → 单个 AI 交易实例生命周期
trader/interface.go        → 统一交易所接口 (Trader interface)
trader/binance_futures.go  → Binance 期货实现
trader/hyperliquid_trader.go → Hyperliquid 实现
trader/aster_trader.go     → Aster DEX 实现
trader/order_tracker.go    → 订单追踪器
decision/decision.go       → AI 决策引擎核心
decision/takeprofit.go     → 持仓评估器 (9 级优先级链)
decision/risk.go           → 风险管理 + 熔断机制
decision/parser.go         → AI 响应解析 (4 级降级) + 失效条件解析
decision/persistence.go    → 原子写入持久化
decision/types.go          → 所有核心类型定义
decision/utils.go          → 工具函数
market/data.go             → 多时间框架市场数据 + 技术指标
pool/coin_pool.go          → 币种池管理 (AI500 + OI Top)
mcp/client.go              → AI API 客户端 (DeepSeek/Qwen/Custom)
logger/decision_logger.go  → 决策日志
api/server.go              → Gin HTTP API (11 个端点)
web/                       → React 18 + TypeScript + Vite + Tailwind
```

## 关键设计约束

- 单笔风险 ≤ 账户净值 2%，总风险预算 ≤ 8%
- 最大同时持仓 3 个
- 风险回报比最低 2.5:1
- 交易周期 3 分钟
- 熔断条件: BTC 1h 跌 >5% / 回撤超限 / 连续亏损 ≥5 / 保证金 >90%
- 数据持久化使用临时文件 + 重命名的原子写入

## 依赖

- go-binance/v2: Binance API
- go-ethereum: 以太坊签名 (Hyperliquid)
- gin: HTTP 框架
- go-hyperliquid: Hyperliquid SDK
