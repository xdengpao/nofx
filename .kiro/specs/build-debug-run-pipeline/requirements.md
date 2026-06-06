# 需求文档：编译、调试与运行流水线

## 简介

为 NOFX 加密货币量化交易系统建立完整的编译、调试和运行流水线（Build, Debug, Run Pipeline）。涵盖 Go 后端编译与运行、React 前端构建与开发服务器、全栈 Docker 部署、测试执行（含属性测试）以及环境配置管理，使开发者能够通过统一的流程完成日常开发、调试和部署工作。

## 术语表

- **Pipeline**: 编译、调试与运行流水线，指从源码到可执行产物再到运行/部署的完整自动化流程
- **Backend**: Go 语言编写的后端服务，入口为 `main.go`，编译产物为 `nofx` 二进制文件
- **Frontend**: 位于 `web/` 目录的 React + TypeScript 前端应用，使用 Vite 构建
- **Build_System**: 负责将源码编译为可执行产物的构建系统，后端使用 `go build`，前端使用 `vite build`
- **Debug_Config**: IDE 调试配置，包括 VS Code 的 `launch.json` 和 `tasks.json`
- **Test_Runner**: 测试执行器，后端使用 `go test`（含 gopter 属性测试），前端使用 `vitest`
- **Docker_Stack**: 基于 Docker Compose 的全栈容器化部署，包含后端、前端和 Nginx 反向代理
- **Config_Manager**: 环境配置管理，包括 `config.json`（运行时配置）和 `.env`（环境变量）
- **ldflags**: Go 编译器链接标志，用于在编译时注入版本号、构建时间、Git 提交哈希等元数据
- **PBT**: 属性基测试（Property-Based Testing），使用 gopter 库生成随机输入验证程序正确性属性

## 需求

### 需求 1：后端编译构建

**用户故事：** 作为量化软件开发工程师，我希望能够一键编译 Go 后端并注入版本元数据，以便生成可追溯的生产级二进制文件。

#### 验收标准

1. WHEN 开发者执行后端编译命令, THE Build_System SHALL 将 `main.go` 及所有依赖包编译为名为 `nofx` 的可执行二进制文件
2. WHEN 开发者执行带版本注入的编译命令, THE Build_System SHALL 通过 ldflags 将 Version、BuildTime 和 GitCommit 变量注入到二进制文件中
3. IF 编译过程中出现语法错误或依赖缺失, THEN THE Build_System SHALL 输出包含文件名和行号的错误信息并以非零退出码终止
4. THE Build_System SHALL 在编译前自动执行 `go mod download` 确保所有依赖已下载
5. WHEN 开发者执行快速编译命令, THE Build_System SHALL 在不注入版本信息的情况下完成编译（用于开发迭代）

### 需求 2：后端运行与调试

**用户故事：** 作为量化软件开发工程师，我希望能够在本地直接运行后端服务并使用 IDE 断点调试，以便快速定位和修复交易逻辑问题。

#### 验收标准

1. WHEN 开发者执行运行命令, THE Backend SHALL 从 `config.json` 加载配置并在配置指定的端口（默认 8080）启动 HTTP 服务
2. WHEN 开发者在 VS Code 中启动调试会话, THE Debug_Config SHALL 以 dlv（Delve）调试模式启动后端，支持断点、变量查看和单步执行
3. THE Debug_Config SHALL 包含至少两个启动配置：一个用于直接运行（Launch），一个用于附加到已运行进程（Attach）
4. WHEN 后端服务启动成功, THE Backend SHALL 在日志中输出 API 服务器端口号和已启用的 Trader 数量
5. IF `config.json` 文件不存在, THEN THE Backend SHALL 输出明确的错误提示并以非零退出码终止
6. THE Debug_Config SHALL 将工作目录设置为项目根目录，确保 `config.json` 和 `data/` 目录路径正确解析

### 需求 3：前端构建与开发服务器

**用户故事：** 作为量化软件开发工程师，我希望能够构建前端生产包和启动开发服务器（含热更新），以便高效开发和调试交易仪表盘界面。

#### 验收标准

1. WHEN 开发者执行前端安装命令, THE Build_System SHALL 在 `web/` 目录下安装所有 npm 依赖
2. WHEN 开发者执行前端构建命令, THE Build_System SHALL 先执行 TypeScript 类型检查（`tsc`），再执行 Vite 构建，将产物输出到 `web/dist/` 目录
3. IF TypeScript 类型检查发现错误, THEN THE Build_System SHALL 输出类型错误详情并终止构建流程
4. WHEN 开发者启动前端开发服务器, THE Frontend SHALL 在端口 3000 启动 Vite 开发服务器，并将 `/api` 路径代理到后端的 8080 端口
5. WHEN 前端源码文件发生变更, THE Frontend SHALL 通过 Vite HMR（热模块替换）在浏览器中即时更新，无需手动刷新页面

### 需求 4：全栈 Docker 部署

**用户故事：** 作为量化软件开发工程师，我希望能够通过 Docker Compose 一键部署完整的前后端服务栈，以便在生产环境或测试环境中快速搭建系统。

#### 验收标准

1. WHEN 开发者执行 Docker 部署命令, THE Docker_Stack SHALL 构建后端镜像（多阶段构建：TA-Lib 编译 → Go 编译 → Alpine 运行时）和前端镜像（Node 构建 → Nginx 运行时）
2. WHEN Docker 容器启动完成, THE Docker_Stack SHALL 使后端服务在 `NOFX_BACKEND_PORT`（默认 8080）端口可访问，前端服务在 `NOFX_FRONTEND_PORT`（默认 3000）端口可访问
3. THE Docker_Stack SHALL 通过 Nginx 反向代理将前端的 `/api/` 请求转发到后端容器
4. THE Docker_Stack SHALL 为后端容器配置健康检查（`/health` 端点，间隔 30 秒，超时 10 秒，3 次重试）
5. THE Docker_Stack SHALL 为前端容器配置健康检查（`/health` 端点返回静态 200 响应，不依赖后端状态）
6. THE Docker_Stack SHALL 将宿主机的 `config.json` 以只读方式挂载到后端容器，将 `decision_logs/` 目录挂载为可读写卷
7. IF 后端容器健康检查连续失败 3 次, THEN THE Docker_Stack SHALL 自动重启该容器（`restart: unless-stopped` 策略）

### 需求 5：测试执行

**用户故事：** 作为量化软件开发工程师，我希望能够一键运行所有后端和前端测试（包括属性基测试），以便在提交代码前验证系统正确性。

#### 验收标准

1. WHEN 开发者执行后端全量测试命令, THE Test_Runner SHALL 运行所有 Go 包中的测试文件（`go test ./...`），包括 `decision/`、`config/`、`market/`、`pool/`、`manager/`、`api/` 和根包的测试
2. WHEN 开发者执行后端单包测试命令, THE Test_Runner SHALL 仅运行指定包的测试（例如 `go test ./decision/...`）
3. THE Test_Runner SHALL 支持运行 gopter 属性基测试，使用 `testutil/` 包中的生成器生成随机输入
4. WHEN 开发者执行前端测试命令, THE Test_Runner SHALL 使用 vitest 以单次运行模式（`--run`）执行 `web/src/` 下所有 `*.test.ts` 文件
5. WHEN 测试执行完成, THE Test_Runner SHALL 输出测试通过/失败数量的汇总信息
6. IF 任何测试失败, THEN THE Test_Runner SHALL 以非零退出码终止并输出失败测试的详细信息（包括属性测试的反例）
7. WHEN 开发者执行带覆盖率的测试命令, THE Test_Runner SHALL 生成代码覆盖率报告

### 需求 6：环境配置管理

**用户故事：** 作为量化软件开发工程师，我希望有清晰的环境配置管理流程，以便在不同环境（开发、测试、生产）之间快速切换配置。

#### 验收标准

1. THE Config_Manager SHALL 提供 `config.json.example` 作为运行时配置模板，包含所有可配置字段及占位符说明
2. THE Config_Manager SHALL 提供 `.env.example` 作为环境变量模板，包含端口和时区配置
3. WHEN 开发者首次设置项目, THE Config_Manager SHALL 通过文档或脚本引导开发者从模板文件复制并填写实际配置
4. THE Config_Manager SHALL 确保 `config.json` 和 `.env` 文件被 `.gitignore` 排除，防止敏感信息（API 密钥、私钥）提交到版本控制
5. IF 开发者未创建 `config.json` 即尝试运行后端, THEN THE Config_Manager SHALL 输出提示信息，指引开发者从 `config.json.example` 复制配置文件
6. THE Config_Manager SHALL 支持通过环境变量 `NOFX_BACKEND_PORT`、`NOFX_FRONTEND_PORT` 和 `NOFX_TIMEZONE` 覆盖 Docker 部署的默认端口和时区设置

### 需求 7：统一构建任务编排

**用户故事：** 作为量化软件开发工程师，我希望有统一的 VS Code 任务配置和 Makefile，以便通过快捷键或单条命令完成常见的构建、测试和部署操作。

#### 验收标准

1. THE Pipeline SHALL 提供 VS Code `tasks.json` 配置，包含以下任务：后端编译、后端运行、后端测试、前端安装依赖、前端构建、前端测试、Docker 部署
2. THE Pipeline SHALL 提供 `Makefile`，包含以下目标：`build`（后端编译）、`run`（后端运行）、`test`（全量测试）、`test-backend`（后端测试）、`test-frontend`（前端测试）、`dev`（前端开发服务器提示）、`docker-up`（Docker 部署）、`docker-down`（Docker 停止）、`clean`（清理构建产物）
3. WHEN 开发者执行 `make build`, THE Build_System SHALL 通过 ldflags 注入当前 Git 提交哈希和构建时间到二进制文件
4. WHEN 开发者执行 `make test`, THE Test_Runner SHALL 依次运行后端全量测试和前端测试，任一失败则以非零退出码终止
5. WHEN 开发者执行 `make clean`, THE Build_System SHALL 删除 `nofx` 二进制文件和 `web/dist/` 目录
