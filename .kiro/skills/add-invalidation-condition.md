---
inclusion: manual
---

# 技能: 添加新失效条件类型

## 概述

当需要支持新的交易计划失效条件时，按照以下步骤操作。

## 步骤

### 1. 在 decision/types.go 中定义常量

```go
const (
    ICT_NEW_CONDITION InvalidationConditionType = "new_condition"
)
```

### 2. 在 decision/parser.go 中添加解析逻辑

在 `ParseInvalidationCondition` 函数中:
- 添加格式化字符串解析模式: `{timeframe}:NEW_CONDITION:{params}`
- 添加自然语言解析模式 (中英文)

### 3. 在 FormatInvalidationCondition 中添加格式化

将条件转换为人类可读的中文描述。

### 4. 在 decision/utils.go 中添加评估逻辑

在 `checkStructuredInvalidation` 函数中添加新条件的评估分支。

### 5. 编写属性基测试

确保满足往返属性: 解析 → 格式化 → 再解析 = 等价条件对象。

```go
// Feature: quant-trading-system, Property 46: 失效条件解析-格式化往返
// 在现有测试中添加新条件类型的覆盖
```

### 6. 更新 AI 系统提示

在决策引擎的系统提示中告知 AI 新的失效条件格式。
