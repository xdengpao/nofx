---
inclusion: manual
---

# 技能: 添加新熔断条件

## 概述

当需要添加新的风险熔断条件时，按照以下步骤操作。

## 步骤

### 1. 在 decision/risk.go 的 CheckCircuitBreaker 中添加检查

```go
// 在 CheckCircuitBreaker 函数中添加新条件
if newConditionMet {
    return &CircuitBreakerState{
        IsTriggered:     true,
        TriggerReason:   "描述新条件",
        TriggerTime:     time.Now(),
        CooldownMinutes: 30, // 设置冷却时间
    }
}
```

### 2. 确定冷却时间

参考现有熔断条件:
- 严重条件 (市场崩盘/大幅回撤): 120 分钟
- 一般条件 (连续亏损/保证金过高): 30 分钟

### 3. 添加所需的上下文数据

如果新条件需要额外数据，在 `decision.Context` 中添加字段，
并在 `trader/auto_trader.go` 的 `buildTradingContext()` 中填充。

### 4. 编写属性基测试

扩展 Property 25 (熔断条件触发) 的测试:

```go
// Feature: quant-trading-system, Property 25: 熔断条件触发
// 添加新条件的测试用例
```

### 5. 更新前端显示

在前端的系统状态展示中添加新熔断条件的显示。
