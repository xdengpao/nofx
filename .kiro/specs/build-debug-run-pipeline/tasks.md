# 实现计划：编译、调试与运行流水线

## 概述

为 NOFX 量化交易系统创建统一的 Makefile 构建编排、VS Code 调试/任务配置，并验证/增强已有的 Docker 部署和环境配置。采用增量方式：先建立核心构建入口（Makefile），再配置 IDE 调试环境，然后验证 Docker 和配置模板，最后通过属性测试和单元测试确保正确性。

## Tasks

- [x] 1. 创建 Makefile 统一构建编排器
  - [x] 1.1 创建项目根目录 `Makefile`，包含所有必需目标
    - 定义变量：`BINARY_NAME=nofx`、`VERSION`（git describe）、`BUILD_TIME`（UTC 时间戳）、`GIT_COMMIT`（短 hash）、`LDFLAGS`（-X main.Version/BuildTime/GitCommit）
    - 实现 `build` 目标：`go mod download` + `go build -ldflags` 注入版本信息，输出 `nofx` 二进制
    - 实现 `build-quick` 目标：无 ldflags 的快速编译
    - 实现 `run` 目标：依赖 `build`，执行 `./nofx`
    - 实现 `test` 目标：依次运行 `test-backend` 和 `test-frontend`
    - 实现 `test-backend` 目标：`go test ./...`
    - 实现 `test-frontend` 目标：`cd web && npm run test`
    - 实现 `test-coverage` 目标：`go test -coverprofile=coverage.out ./...`
    - 实现 `dev` 目标：打印提示信息，引导开发者手动启动 `cd web && npm run dev`
    - 实现 `docker-up` 目标：`docker compose up -d --build`
    - 实现 `docker-down` 目标：`docker compose down`
    - 实现 `clean` 目标：删除 `nofx` 二进制和 `web/dist/` 目录
    - 实现 `install` 目标：`go mod download` + `cd web && npm ci`
    - 声明所有目标为 `.PHONY`
    - _Requirements: 7.2, 7.3, 7.4, 7.5, 1.1, 1.2, 1.4, 1.5, 5.1, 5.4, 5.7_

  - [x] 1.2 编写 Property 3 属性测试：Makefile 目标完备性
    - **Property 3: Makefile 目标完备性**
    - 在 `pipeline_test.go`（新建）中实现，遍历需求规定的所有目标名称（build, run, test, test-backend, test-frontend, dev, docker-up, docker-down, clean），验证 Makefile 文件中包含 `^<target>:` 模式的规则定义
    - **Validates: Requirements 7.2**

- [x] 2. 配置 VS Code 调试环境
  - [x] 2.1 创建 `.vscode/launch.json` 调试配置
    - 添加 Launch 配置：使用 dlv 调试 `main.go`，工作目录 `${workspaceFolder}`，`CGO_ENABLED=1`
    - 添加 Attach 配置：附加到 `localhost:2345` 的 Delve 调试服务器
    - _Requirements: 2.2, 2.3, 2.6_

  - [x] 2.2 创建 `.vscode/tasks.json` 任务配置
    - 添加任务：Backend: Build（`make build`，设为默认构建任务，配置 `$go` 问题匹配器）
    - 添加任务：Backend: Run（`make run`）
    - 添加任务：Backend: Test（`make test-backend`）
    - 添加任务：Frontend: Install（`cd web && npm ci`）
    - 添加任务：Frontend: Build（`cd web && npm run build`）
    - 添加任务：Frontend: Test（`cd web && npm run test`）
    - 添加任务：Docker: Deploy（`make docker-up`）
    - 添加任务：All: Test（`make test`）
    - _Requirements: 7.1_

- [x] 3. Checkpoint - 确保 Makefile 和 VS Code 配置文件语法正确
  - 确保所有配置文件格式正确，ask the user if questions arise.

- [x] 4. 创建 ldflags 构建辅助函数与属性测试
  - [x] 4.1 在 `pipeline_test.go` 中实现 `BuildLdflags` 辅助函数和解析函数
    - 创建 `BuildLdflags(version, buildTime, gitCommit string) string` 函数，生成 `-X main.Version=... -X main.BuildTime=... -X main.GitCommit=...` 格式的 ldflags 字符串
    - 创建 `ParseLdflags(ldflags string) (version, buildTime, gitCommit string)` 解析函数，从 ldflags 字符串中提取三个变量值
    - _Requirements: 1.2, 7.3_

  - [x] 4.2 编写 Property 1 属性测试：ldflags 版本注入格式正确性
    - **Property 1: ldflags 版本注入格式正确性**
    - 使用 gopter 生成随机版本字符串、ISO 8601 时间戳和短 commit hash，验证 `BuildLdflags` → `ParseLdflags` 的 round-trip 正确性
    - 使用 `testutil.DefaultTestParameters()`（100 次迭代，Seed(42)）
    - **Validates: Requirements 1.2, 7.3**

- [x] 5. 扩展配置 round-trip 属性测试
  - [x] 5.1 在 `config/config_test.go` 中扩展 Property 2 属性测试：配置加载端口保持
    - **Property 2: 配置加载端口保持（JSON round-trip via LoadConfig）**
    - 使用 `testutil.GenConfig()` 生成随机 Config，序列化为 JSON 写入临时文件，通过 `LoadConfig` 重新加载，验证 `APIServerPort`、`Leverage.BTCETHLeverage`、`Leverage.AltcoinLeverage`、`MaxDailyLoss`、`MaxDrawdown` 等数值字段与原始值一致
    - 注意：现有 `TestProperty1_ConfigSerializationRoundTrip` 使用 `json.Unmarshal` 直接反序列化，本测试需通过 `LoadConfig`（含文件读取 + Validate）验证完整链路
    - **Validates: Requirements 2.1, 6.1**

- [x] 6. 验证和增强 Docker 部署配置
  - [x] 6.1 验证 `docker-compose.yml` 健康检查和重启策略
    - 在 `pipeline_test.go` 中添加单元测试，读取 `docker-compose.yml` 文件内容，验证：
      - 后端服务包含 `healthcheck` 配置（interval: 30s, timeout: 10s, retries: 3）
      - 后端服务包含 `restart: unless-stopped`
      - 前端服务包含 `healthcheck` 配置
      - 前端服务包含 `depends_on`
      - 端口映射使用 `${NOFX_BACKEND_PORT:-8080}` 和 `${NOFX_FRONTEND_PORT:-3000}` 环境变量
    - 验证 `docker/Dockerfile.backend` 包含多阶段构建和 HEALTHCHECK 指令
    - 验证 `docker/Dockerfile.frontend` 包含 HEALTHCHECK 指令
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7_

  - [x] 6.2 修复 `docker-compose.yml` 后端健康检查命令
    - 当前使用 `curl -f`，但 Alpine 运行时镜像未安装 curl；Dockerfile.backend 已使用 `wget`
    - 将后端健康检查命令从 `curl -f` 改为 `wget --no-verbose --tries=1 --spider`，与 Dockerfile 保持一致
    - _Requirements: 4.4, 4.7_

- [x] 7. 验证环境配置模板和 .gitignore
  - [x] 7.1 在 `pipeline_test.go` 中添加环境配置验证测试
    - 验证 `config.json.example` 文件存在且为有效 JSON
    - 验证 `.env.example` 文件存在且包含 `NOFX_BACKEND_PORT`、`NOFX_FRONTEND_PORT`、`NOFX_TIMEZONE` 变量
    - 验证 `.gitignore` 包含 `config.json` 和 `.env` 条目
    - 验证 `nginx/nginx.conf` 包含 `/api/` 代理和 `/health` 端点配置
    - 验证 `web/vite.config.ts` 包含端口 3000 和 `/api` 代理配置
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 3.4_

- [x] 8. Checkpoint - 运行全量测试验证
  - 执行 `go test ./...` 确保所有后端测试通过（包括新增的 pipeline_test.go 和扩展的 config_test.go），ask the user if questions arise.

- [x] 9. 集成验证与最终连接
  - [x] 9.1 验证 Makefile 与 VS Code 任务的端到端连接
    - 在 `pipeline_test.go` 中添加测试，验证 `.vscode/tasks.json` 包含所有必需任务名称（Backend: Build, Backend: Run, Backend: Test, Frontend: Install, Frontend: Build, Frontend: Test, Docker: Deploy, All: Test）
    - 验证 `.vscode/launch.json` 包含 Launch 和 Attach 两个调试配置
    - 验证 `tasks.json` 中 Backend: Build 任务设置了 `isDefault: true`
    - _Requirements: 7.1, 2.2, 2.3_

- [x] 10. Final checkpoint - 确保所有测试通过
  - 执行 `go test ./...` 确保所有测试通过，ask the user if questions arise.

## Notes

- 标记 `*` 的任务为可选，可跳过以加速 MVP 交付
- 每个任务引用了具体的需求编号，确保可追溯性
- `pipeline_test.go` 为新建文件，集中放置流水线相关的属性测试和结构验证测试
- 已有的 `config/config_test.go` 中 Property 1 使用 `json.Unmarshal` 直接反序列化；新增的 Property 2 通过 `LoadConfig` 验证完整文件加载链路
- Docker 相关验证以读取文件内容的单元测试方式实现，不依赖 Docker 运行时环境
- `docker-compose.yml` 的健康检查命令需要修复（curl → wget），这是一个实际的 bug fix
