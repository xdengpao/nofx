---
inclusion: manual
---

# 技能: 集成新 AI 模型提供商

## 概述

当需要集成新的 AI 模型提供商时，按照以下步骤操作。

## 步骤

### 1. 在 mcp/client.go 中添加配置方法

```go
func (c *Client) SetNewProviderAPIKey(apiKey string) {
    c.APIKey = apiKey
    c.BaseURL = "https://api.newprovider.com"
    c.Model = "model-name"
}
```

如果 API 格式与 OpenAI 兼容，可直接使用 `SetCustomAPI`。
如果不兼容，需要在 `CallWithMessages` 中添加新的请求格式分支。

### 2. 在 config/config.go 中添加验证

```go
// 在 TraderConfig 中添加字段
NewProviderKey string `json:"new_provider_key,omitempty"`

// 在 Validate() 中添加验证
if trader.AIModel == "new_provider" && trader.NewProviderKey == "" {
    return fmt.Errorf("trader[%d]: 使用 NewProvider 时必须配置 new_provider_key", i)
}
```

同时更新 `ai_model` 的合法值检查。

### 3. 在 manager/trader_manager.go 中注册

在 `AddTrader()` 的 AI 模型配置分支中添加新提供商。

### 4. 关键参数

保持与现有模型一致:
- `temperature`: 0.5 (提高 JSON 格式稳定性)
- `max_tokens`: 2000
- 超时: 120 秒
- 重试: 3 次 (仅网络错误)

### 5. 更新配置示例

在 `config.json.example` 和 `CUSTOM_API.md` 中添加新提供商的配置说明。
