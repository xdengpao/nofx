---
inclusion: manual
---

# 技能: 添加新交易所适配器

## 概述

当需要集成新的交易所时，按照以下步骤实现统一 Trader 接口。

## 步骤

### 1. 创建交易所文件

在 `trader/` 目录下创建 `{exchange}_trader.go`，实现 `Trader` 接口:

```go
package trader

type NewExchangeTrader struct {
    // 交易所特定字段
}

func NewNewExchangeTrader(config ...params) (*NewExchangeTrader, error) {
    // 初始化
}
```

### 2. 实现 Trader 接口的所有方法

参考 `trader/interface.go` 中定义的接口，必须实现:
- `GetBalance()` / `GetPositions()`
- `OpenLong()` / `OpenShort()` / `CloseLong()` / `CloseShort()`
- `SetLeverage()` / `GetMarketPrice()`
- `SetStopLoss()` / `SetTakeProfit()`
- `CancelStopLossOrders()` / `CancelTakeProfitOrders()` / `CancelAllOrders()`
- `FormatQuantity()`
- `GetOrderHistory()` / `GetTradeHistory()` / `GetOrderStatus()`

### 3. 关键实现要点

- 数量/价格精度: 实现 `FormatQuantity` 确保符合交易所精度要求
- 市价单模拟: 如交易所不支持市价单，使用 IOC 限价单模拟 (参考 Hyperliquid)
- 缓存机制: 对频繁调用的 API (余额/持仓) 实现缓存 (参考 Binance 的 15 秒缓存)
- 杠杆检查: `SetLeverage` 先检查当前值，避免不必要的 API 调用

### 4. 注册到配置系统

在 `config/config.go` 的 `Validate()` 中添加新交易所的验证逻辑:
- 添加 exchange 类型检查
- 添加必填密钥字段验证

### 5. 注册到 TraderManager

在 `manager/trader_manager.go` 的 `AddTrader()` 中添加新交易所的创建分支。

### 6. 更新配置示例

在 `config.json.example` 中添加新交易所的配置示例。
