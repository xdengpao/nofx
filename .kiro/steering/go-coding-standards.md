---
inclusion: fileMatch
fileMatchPattern: "**/*.go"
---

# Go 编码规范

## 命名约定

- 包名: 小写单词，不用下划线 (`decision`, `trader`, `market`)
- 导出函数/类型: 大写开头驼峰 (`GetFullDecision`, `TradePlan`)
- 私有函数/类型: 小写开头驼峰 (`evaluateExistingPositions`, `fixJSON`)
- 常量: 大写开头驼峰或全大写下划线 (`MaxRiskPerTrade`, `ICT_EMA_CROSS_DOWN`)
- 接口: 动词或 -er 后缀 (`Trader`, `Logger`)

## 错误处理

- 永远不忽略 error 返回值
- 使用 `fmt.Errorf("描述: %w", err)` 包装错误
- 配置层错误: 立即终止启动
- 交易所 API 错误: 记录日志，跳过当前周期
- AI API 错误: 3 次重试后降级到持仓评估
- 解析错误: 4 级降级策略，最终返回 wait 决策

## 并发模式

- 使用 `sync.RWMutex` 保护共享状态 (如 TradePlanManager)
- 使用 `context.Context` 传递取消信号
- 使用 channel 进行 goroutine 间通信
- 市场数据获取使用 `sync.WaitGroup` 并发

## 日志规范

- 使用 `log.Printf` 而非 `fmt.Println`
- 日志格式: emoji + 模块标识 + 消息
- 示例: `log.Printf("🔄 [%s] 开始新周期 #%d", traderID, cycleNum)`

## 安全要求

- 绝不硬编码 API 密钥或私钥
- 所有密钥通过 config.json 配置
- 日志中不打印完整密钥

## JSON 序列化

- 使用 `json:"field_name"` tag，snake_case 格式
- 可选字段使用 `omitempty`
- 持久化使用原子写入 (临时文件 + os.Rename)

## 测试规范

- 测试文件: `*_test.go`，与源文件同目录
- 属性基测试使用 `github.com/leanovate/gopter`
- 属性测试注释: `// Feature: quant-trading-system, Property N: [标题]`
- 每个属性测试至少 100 次迭代
- Mock 交易所接口用于单元测试
