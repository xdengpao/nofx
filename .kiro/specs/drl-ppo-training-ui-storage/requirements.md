# DRL-PPO 训练 UI 与存储路径 Requirements

## 背景

当前 NOFX 已具备 DRL-PPO 策略核心、Python 训练脚本、历史数据抓取、回测 API 雏形和 Web 监控界面。现阶段训练数据、模型文件、训练日志、回测输出和运行时 JSON 状态仍默认散落在项目目录下，例如 `backtest_data/`、`models/drl/`、`logs/`、`data/`、`decision_logs/` 和 `coin_pool_cache/`。随着 DRL-PPO 训练数据和模型产物增大，这些目录不适合继续放在源码工作区。

本需求要求将训练与运行数据迁移并持久化到可配置的存储根目录；原生运行默认值为 `/Volumes/light2/nofx`，Docker Compose 默认使用项目本地 runtime 目录并挂载到容器内 `/app/runtime`，两种场景均可通过环境变量覆盖。同时新增一个面向 DRL-PPO 策略训练的前端 UI 模块，匹配后端训练任务模块 `drl_ppo_train`（用户提到的 `ddr_ppo_train` 按现有项目命名归一为 `drl_ppo_train`）。UI 应能完成数据覆盖检查、训练启动、进度监控、日志查看、模型产物管理、评估与部署准备，但不得绕过现有交易风控或直接触发未确认的实盘开仓。

## Feature Summary

- 引入可配置的存储根目录，原生运行默认 `/Volumes/light2/nofx`，承载训练数据、模型、日志、回测输出和必要运行状态。
- 提供安全的数据迁移与路径配置机制；Docker Compose 默认挂载项目本地 `./runtime` 到 `/app/runtime`，并允许通过环境变量切换到外置盘或其他宿主机路径。
- 新增后端 DRL-PPO 训练任务 API，对接现有 Python 训练脚本与训练进程生命周期。
- 新增 Web DRL-PPO 训练 UI 模块，提供训练配置、任务列表、日志、指标、模型和部署状态视图。
- 保持交易服务与训练服务职责分离：训练生成模型，部署或切换模型必须通过显式配置/确认，并继续走 NOFX 公共风控链路。

## Glossary

- **Storage Root**：统一存储根目录，可通过配置项或环境变量覆盖；原生运行默认 `/Volumes/light2/nofx`，Docker Compose 容器内默认生效路径为 `/app/runtime`。
- **Compose Runtime Mount**：Docker Compose 场景下挂载到容器 `/app/runtime` 的宿主机路径，默认使用项目本地路径，例如 `./runtime`，可通过环境变量覆盖为 `/Volumes/light2/nofx` 或其他路径。
- **Runtime Data**：NOFX 运行时 JSON 状态和日志，包括 `data/`、`decision_logs/`、`coin_pool_cache/`。
- **Training Artifacts**：DRL-PPO 训练相关产物，包括历史数据库、训练日志、SB3 `.zip` 模型、ONNX 模型、评估报告和训练元数据。
- **drl_ppo_train Job**：后端管理的 DRL-PPO 训练任务，负责拉起、监控、取消和记录 Python 训练进程。
- **Deployable Model**：已训练并通过基础校验，可被 DRL trader 配置引用的 ONNX 模型。

## Requirements

### 1. 可配置存储根目录与路径规范

**User Story:** 作为系统维护者，我希望训练数据和运行数据统一迁移到可配置的存储根目录，以便释放源码目录空间并长期保存大体积模型与历史数据，同时允许 Docker Compose 默认使用项目本地 runtime 目录。

#### Acceptance Criteria

1. WHEN 原生运行且系统配置未显式指定存储根目录 THEN 默认 Storage Root SHALL 为 `/Volumes/light2/nofx`。
2. WHEN 用户通过配置项或环境变量指定 Storage Root THEN 系统 SHALL 使用该值作为训练、回测、模型和运行数据根目录。
3. WHEN 同时存在配置项和环境变量 THEN 系统 SHALL 按明确优先级解析，并在诊断接口中返回最终生效的 Storage Root 和来源。
4. WHEN Storage Root 初始化 THEN 系统 SHALL 创建以下子目录：`backtest_data/`、`models/drl/`、`logs/drl_ppo_train/`、`backtest_runs/`、`data/`、`decision_logs/`、`coin_pool_cache/`、`tmp/`、`trash/`。
5. WHEN 生效 Storage Root 不存在或不可写 THEN 初始化 SHALL 失败并返回清晰中文错误，不得静默回退到源码目录。
6. WHEN 路径配置包含相对路径 THEN 系统 SHALL 基于 Storage Root 解析训练和运行数据路径，而不是基于当前工作目录。
7. WHEN 路径配置包含绝对路径 THEN 系统 SHALL 仅允许位于 Storage Root 内的路径，除非用户显式设置允许外部路径的开发开关。
8. WHEN Docker Compose 启动后端且未显式配置挂载路径 THEN compose SHALL 默认将项目本地路径（例如 `./runtime`）挂载到容器内稳定路径 `/app/runtime`，并默认设置后端容器内 Storage Root 为 `/app/runtime`，不得默认挂载到 `/Volumes/light2/nofx`。
9. WHEN 用户通过环境变量显式配置 Docker Compose 挂载路径 THEN compose SHALL 使用该宿主机路径挂载到 `/app/runtime`，并允许该路径指向 `/Volumes/light2/nofx`。
10. WHEN Docker Compose 挂载路径与后端 Storage Root 配置不同 THEN 后端 SHALL 以容器内 `/app/runtime` 作为运行时根目录，并在诊断接口中显示宿主机挂载来源和容器内生效路径。
11. WHEN 后端、训练脚本和前端展示路径 THEN SHALL 使用统一路径映射，避免宿主机路径与容器内路径混淆。

### 2. 数据迁移与兼容性

**User Story:** 作为系统维护者，我希望已有本地数据可安全迁移到当前生效的 Storage Root，以便不中断当前训练、回测和交易监控历史。

#### Acceptance Criteria

1. WHEN 执行迁移命令 THEN 系统 SHALL 将 `backtest_data/`、`models/drl/`、`logs/`、`backtest_runs/`、`data/`、`decision_logs/`、`coin_pool_cache/` 迁移到 Storage Root 对应子目录。
2. WHEN 目标目录已存在同名文件 THEN 迁移 SHALL 默认不覆盖，并生成冲突报告。
3. WHEN 用户启用覆盖模式 THEN 迁移 SHALL 先创建带时间戳的备份或校验目标 hash 后再覆盖。
4. WHEN 迁移完成 THEN 系统 SHALL 输出迁移摘要，包括文件数量、总字节数、跳过项、冲突项和错误项。
5. WHEN 迁移完成 THEN 本地源码目录 SHALL 可保留轻量级占位或 symlink，但不得把大体积训练产物重新写回源码目录。
6. WHEN 迁移失败 THEN 系统 SHALL 保持源数据不变，并提供可重试的错误信息。
7. WHEN Git 状态检查 THEN 运行时数据和训练产物 SHALL 继续被 `.gitignore` 忽略，避免误提交模型、日志、历史库或密钥配置。

### 3. DRL-PPO 训练任务后端

**User Story:** 作为量化研究员，我希望后端提供可管理的 `drl_ppo_train` 任务，以便通过 API 和 UI 启动、观察和取消训练。

#### Acceptance Criteria

1. WHEN API 请求创建训练任务 THEN 后端 SHALL 创建 `drl_ppo_train` job，并记录参数、状态、开始时间、日志路径和输出模型路径。
2. WHEN 训练任务启动 THEN 后端 SHALL 调用 `training/drl/scripts/train.py` 或等效封装，使用 Storage Root 下的历史数据库和输出目录。
3. WHEN 训练任务运行 THEN 后端 SHALL 持续采集进程 PID、运行时长、当前 timesteps、最近日志片段和资源状态。
4. WHEN 训练任务完成 THEN 后端 SHALL 标记状态为 `completed`，并记录该训练模式的实际产物；普通训练 SHALL 记录 `.zip` 模型、`.onnx` 模型、训练指标和模型元数据，rolling 训练 SHALL 记录 rolling summary 和 window 模型产物。
5. WHEN 训练任务失败 THEN 后端 SHALL 标记状态为 `failed`，保留日志并返回中文错误摘要。
6. WHEN 用户取消训练任务 THEN 后端 SHALL 优雅发送终止信号；若超时仍未退出 THEN SHALL 强制终止，并将状态标记为 `cancelled`。
7. WHEN 后端重启 THEN 已完成和失败任务 SHALL 可从 Storage Root 的 job 元数据中恢复；运行中任务 SHALL 被标记为 `unknown` 或 `interrupted`，不得误报为成功。
8. WHEN 同时启动多个训练任务 THEN 后端 SHALL 支持配置最大并发数，默认只允许 1 个 DRL-PPO 训练任务运行。
9. WHEN 训练参数包含 symbol、timeframe、数据时间范围、total_timesteps、observation_window、费用和滑点 THEN 后端 SHALL 校验字段范围并返回清晰错误。
10. WHEN 训练任务使用真实市场数据 THEN 数据抓取仍 SHALL 使用公共行情接口，不需要交易所私钥。

### 4. 历史数据准备与覆盖检查

**User Story:** 作为量化研究员，我希望 UI 在训练前检查历史数据覆盖并可触发补数据，以便避免训练因数据缺失失败。

#### Acceptance Criteria

1. WHEN 打开 DRL-PPO 训练 UI THEN 前端 SHALL 展示 Storage Root 下历史数据库覆盖情况，包括 source、symbol、timeframe、count、from、to 和 data_hash。
2. WHEN 用户选择 symbol、timeframe 和训练时间范围 THEN 后端 SHALL 返回覆盖是否充足及缺口区间。
3. WHEN 覆盖不足且用户确认补数据 THEN UI SHALL 调用后端历史数据 fetch job。
4. WHEN fetch job 运行 THEN UI SHALL 展示状态、插入数量、重复数量、失败 symbol/timeframe 和请求统计。
5. WHEN 数据覆盖不足且用户仍尝试训练 THEN 后端 SHALL 拒绝训练或要求显式确认允许不完整数据训练。
6. WHEN 数据源目前只支持 `binance-futures` THEN UI SHALL 清楚展示该限制，不得暗示 Aster/Hyperliquid 历史源已实现。

### 5. DRL-PPO 训练 UI

**User Story:** 作为量化研究员，我希望在 Web 中配置和启动 DRL-PPO 训练，以便不依赖手写 shell 命令管理训练流程。

#### Acceptance Criteria

1. WHEN 访问 Web 应用 THEN 用户 SHALL 能进入 DRL-PPO 训练页面或标签页。
2. WHEN 打开训练页面 THEN UI SHALL 展示训练配置表单，包括 symbol、timeframe、start/end、total_timesteps、observation_window、initial_balance、fees、slippage、rolling、output_model_name。
3. WHEN 用户提交训练表单 THEN UI SHALL 调用后端创建 `drl_ppo_train` job，并显示 job ID。
4. WHEN job 运行 THEN UI SHALL 自动刷新状态、timesteps、日志尾部、耗时和产物路径。
5. WHEN job 完成 THEN UI SHALL 展示训练指标、评估入口、ONNX 导出状态和模型文件。
6. WHEN job 失败 THEN UI SHALL 显示错误摘要和日志定位，不得只显示通用失败文案。
7. WHEN 训练页面存在运行中 job THEN UI SHALL 提供取消按钮，并在取消前要求确认。
8. WHEN 存在多个历史 job THEN UI SHALL 提供任务列表、状态过滤和最近任务置顶。
9. WHEN UI 展示日志 THEN SHALL 支持增量加载或 tail，避免一次性加载超大日志导致页面卡顿。
10. WHEN UI 展示模型路径或日志路径 THEN SHALL 隐藏或压缩宿主机绝对路径，只展示 Storage Root 相对路径和文件名。

### 6. 模型管理、评估与部署准备

**User Story:** 作为量化研究员，我希望管理训练产出的模型并在部署前查看质量指标，以便避免把劣质模型直接接入交易服务。

#### Acceptance Criteria

1. WHEN 训练产出模型 THEN 后端 SHALL 为模型保存元数据，包括 model_version、symbol、timeframe、训练数据范围、timesteps、observation_window、创建时间和指标。
2. WHEN UI 展示模型列表 THEN SHALL 展示 `.zip`、`.onnx`、评估报告和是否可部署。
3. WHEN 用户请求评估模型 THEN 后端 SHALL 调用评估脚本，输出年化收益、Sharpe、Sortino、最大回撤、胜率和方向准确率。
4. WHEN 模型方向准确率或关键指标低于阈值 THEN UI SHALL 标记为“不建议部署”。
5. WHEN 用户准备部署模型到 DRL trader THEN UI SHALL 只生成配置建议或 staging 配置；实际交易服务切换 SHALL 需要显式确认。
6. WHEN 部署模型路径写入 trader 配置 THEN 系统 SHALL 使用 Storage Root 内模型路径，并确保容器内路径可访问。
7. WHEN 当前后端未启用真实 ONNX Runtime build tag THEN UI SHALL 明确提示“当前为 stub 推理，仅验证链路，不使用 ONNX 输出交易信号”。

### 7. 训练服务与交易服务隔离

**User Story:** 作为交易系统维护者，我希望训练任务不会干扰正在运行的交易服务，以便训练失败或资源占用不会影响实盘风控和订单追踪。

#### Acceptance Criteria

1. WHEN 训练任务运行 THEN 它 SHALL 不修改已启用 trader 的运行配置，除非用户执行显式部署动作。
2. WHEN 训练任务消耗大量 CPU/内存 THEN 系统 SHALL 提供并发限制和可配置资源提示，避免拖垮交易服务。
3. WHEN 交易服务正在运行 THEN 训练 UI SHALL 展示当前启用 trader 和是否为 DRL 模式，但不得自动停止或重启交易服务。
4. WHEN 用户请求将模型部署到交易服务 THEN UI SHALL 展示当前持仓数量、账户状态和风险提示。
5. WHEN 当前账户存在持仓 THEN 部署动作 SHALL 要求额外确认，且不得绕过持仓管理优先原则。
6. WHEN 训练任务失败、取消或超时 THEN 交易服务 SHALL 不受影响，现有 trader 继续运行。

### 8. 安全、审计与权限

**User Story:** 作为系统维护者，我希望训练 UI 和存储迁移不会泄露密钥或误操作实盘账户，以便保持自动交易系统安全边界。

#### Acceptance Criteria

1. WHEN API 返回训练任务、模型或存储路径信息 THEN SHALL 不包含 API key、私钥、钱包地址私密字段或完整 secret。
2. WHEN UI 展示 trader 配置摘要 THEN SHALL 只显示 trader_id、exchange、decision_mode、model_version 和凭证是否存在，不显示真实密钥。
3. WHEN 后端执行文件系统操作 THEN SHALL 限制在 Storage Root 内，防止路径穿越。
4. WHEN 用户触发取消、删除模型、覆盖迁移或部署配置 THEN UI SHALL 进行确认，并记录审计事件。
5. WHEN 删除模型或日志 THEN 默认 SHALL 移动到 Storage Root 下 `trash/` 或创建可恢复备份，而不是立即永久删除。
6. WHEN 训练 API 未启用时 THEN 相关接口 SHALL 返回 404 或清晰的 disabled 响应，前端 SHALL 显示模块未启用状态。

### 9. 可观测性与诊断

**User Story:** 作为量化研究员，我希望训练和存储状态可观测，以便快速定位数据缺失、训练卡住、模型导出失败等问题。

#### Acceptance Criteria

1. WHEN 打开 DRL-PPO 训练 UI THEN SHALL 展示 Storage Root 可用空间、已用空间和关键目录容量。
2. WHEN 训练任务运行 THEN SHALL 展示最近更新时间；若超过配置阈值没有日志或进度更新 THEN 标记为可能卡住。
3. WHEN 训练任务生成日志 THEN 后端 SHALL 支持按 offset 或 tail 行数读取日志。
4. WHEN 训练任务完成 THEN SHALL 保存结构化 summary JSON，供 UI 和后续审计读取。
5. WHEN 训练任务失败 THEN SHALL 将失败原因、退出码、最后日志和命令参数摘要写入 job 元数据。
6. WHEN 数据迁移完成 THEN SHALL 提供可查询的迁移报告。

### 10. 验证与交付

**User Story:** 作为开发维护者，我希望该功能有清晰测试覆盖，以便存储迁移、训练 API 和 UI 不破坏现有交易系统。

#### Acceptance Criteria

1. WHEN 实现存储路径配置和迁移逻辑 THEN SHALL 增加 Go 单元测试覆盖路径解析、目录初始化、迁移冲突和路径穿越拒绝。
2. WHEN 实现训练任务 API THEN SHALL 增加 API 测试覆盖创建、查询、取消、失败恢复和并发限制。
3. WHEN 实现训练 UI THEN SHALL 通过 TypeScript build，并为关键工具函数补测试。
4. WHEN 实现 Docker/compose 挂载变更 THEN SHALL 验证默认 `docker compose up -d` 使用项目本地 runtime 目录，并验证通过环境变量可将宿主机挂载路径切换到 `/Volumes/light2/nofx` 后 `/app/runtime` 仍可访问。
5. WHEN 功能交付前 THEN SHALL 运行至少 `go test ./api ./backtest ./historydb`、`cd web && npm run build`，如触及共享配置则追加 `go test ./config ./manager ./trader`。
6. WHEN 端到端验证 THEN SHALL 使用小规模 BTCUSDT 训练数据启动一次短训练 job，并确认 UI 可看到 running -> completed 或 failed 的状态变化。
