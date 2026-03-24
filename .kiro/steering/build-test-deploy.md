---
inclusion: manual
---

# 构建、测试与部署

## 构建

```bash
# 编译后端
go build -o nofx .

# 带版本信息编译
go build -ldflags "-X main.Version=1.0.0 -X main.BuildTime=$(date -u +%Y%m%d%H%M%S) -X main.GitCommit=$(git rev-parse --short HEAD)" -o nofx .

# 前端构建
cd web && npm install && npm run build
```

## 测试

```bash
# 运行所有测试
go test ./... -count=1 -timeout 120s

# 运行特定模块测试
go test ./decision/... -v
go test ./config/... -v
go test ./market/... -v

# 运行属性基测试 (verbose)
go test ./decision/... -v -run TestProperty

# 静态检查
go vet ./...
gofmt -l .

# 前端类型检查
cd web && npx tsc --noEmit
```

## 运行

```bash
# 默认配置
./nofx

# 指定配置文件
./nofx /path/to/config.json

# PM2 部署
pm2 start pm2.config.js
```

## Docker 部署

```bash
# 构建并启动
docker-compose up -d --build

# 查看日志
docker-compose logs -f backend
docker-compose logs -f frontend
```

## 目录结构

```
data/                          → 持久化数据
  trade_plans.json             → 交易计划 + 统计 + 收益率
decision_logs/{trader_id}/     → 每周期决策日志
coin_pool_cache/               → 币种池缓存
  latest.json                  → AI500 缓存
  oi_top_latest.json           → OI Top 缓存
```
