---
inclusion: manual
---

# 技能: 添加新 API 端点

## 概述

当需要为前端仪表盘添加新的 HTTP API 端点时，按照以下步骤操作。

## 步骤

### 1. 在 api/server.go 中注册路由

```go
// 在 setupRoutes() 中添加
apiGroup.GET("/new-endpoint", s.handleNewEndpoint)
```

### 2. 实现处理函数

```go
func (s *Server) handleNewEndpoint(c *gin.Context) {
    traderID := c.Query("trader_id")
    if traderID == "" {
        traderID = s.getDefaultTraderID()
    }

    // 获取数据
    data, err := s.traderManager.GetSomeData(traderID)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, data)
}
```

规范:
- 使用 `trader_id` 查询参数，缺失时默认第一个 trader
- 错误返回 `{"error": "描述"}` 格式
- CORS 已由中间件统一处理

### 3. 在 TraderManager 中添加数据获取方法

如果需要从 TraderManager 获取数据，在 `manager/trader_manager.go` 中添加方法。

### 4. 在前端添加 API 调用

在 `web/src/lib/api.ts` 中添加:

```typescript
export const fetchNewData = (traderId: string) =>
  fetch(`${API_BASE}/api/new-endpoint?trader_id=${traderId}`).then(r => r.json())
```

### 5. 添加 TypeScript 类型

在 `web/src/types/index.ts` 中定义响应类型。

### 6. 使用 SWR 集成

```typescript
const { data } = useSWR(
  `/api/new-endpoint?trader_id=${traderId}`,
  fetcher,
  { refreshInterval: 15000 }
)
```
