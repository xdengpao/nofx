---
inclusion: manual
---

# 技能: 添加新技术指标

## 概述

当需要为 AI 决策引擎添加新的技术指标时，按照以下步骤操作。

## 步骤

### 1. 在 market/data.go 中实现计算函数

```go
// 私有函数，小写开头
func calculateNewIndicator(closes []float64, period int) float64 {
    // 实现计算逻辑
    // 确保处理数据不足的边界情况
}
```

约束:
- RSI 类指标返回值必须在 [0, 100]
- ATR 类指标返回值必须 >= 0
- EMA 类指标返回值必须 > 0

### 2. 添加到 Data 结构体

在 `market.Data` 的对应时间框架结构中添加新字段。

### 3. 在 Get() 函数中调用

在获取 K 线数据后调用新指标的计算函数。

### 4. 添加到 Format() 输出

在 `Format()` 函数中将新指标格式化为 AI 可读文本。

### 5. 如需序列数据

在 `calculateIntradaySeriesEnhanced` 中添加序列计算，序列长度上限 10。

### 6. 编写属性基测试

```go
// Feature: quant-trading-system, Property N: 新指标值域
func TestProperty_NewIndicatorRange(t *testing.T) {
    // 使用 gopter 生成随机 K 线数据
    // 验证返回值在预期范围内
}
```
