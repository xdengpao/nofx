# DRL-PPO 训练 UI 与存储路径 Tasks

## 执行原则

- 每个任务完成后更新本文件，将 `- [ ]` 改为 `- [x]`；如果三次修复后仍无法通过，标为 `- [!]` 并写明原因。
- 不在测试或迁移过程中触发真实下单，不读取或输出交易所私钥、API key、钱包私密字段。
- Docker Compose 默认只挂载项目本地 `./runtime` 到 `/app/runtime`；外置盘 `/Volumes/light2/nofx` 只能通过显式环境变量覆盖。
- 训练 API 默认 disabled；只有配置或环境变量明确启用时才注册训练路由。

## Phase 1: Storage 配置与路径解析

- [x] 在 `config/config.go` 增加 `StorageConfig` 和 `DRLPPOTrainConfig`，支持 `storage` 与 `drl_ppo_train` 配置段。
- [x] 实现环境变量覆盖逻辑：`NOFX_STORAGE_ROOT`、`NOFX_CONTAINER_STORAGE_ROOT`、`NOFX_STORAGE_ALLOW_EXTERNAL_PATHS`、`NOFX_DRL_TRAIN_API_ENABLED`、`NOFX_DRL_TRAIN_MAX_CONCURRENCY`、`NOFX_DRL_TRAIN_PYTHON_BIN`。
- [x] 新增 `storage/` 包，实现 `RuntimeConfig`、`Layout`、`ResolveRuntimeConfig`、`NewLayout`、`EnsureLayout`、`ResolveUnderRoot`、`RelForDisplay`。
- [x] 确保原生运行默认 Storage Root 为 `/Volumes/light2/nofx`，但不在 Docker Compose 配置中默认使用该路径。
- [x] 实现 Storage Root 子目录初始化：`backtest_data/`、`models/drl/`、`logs/drl_ppo_train/`、`backtest_runs/`、`data/`、`decision_logs/`、`coin_pool_cache/`、`tmp/`、`trash/`。
- [x] 增加 `storage` 单元测试，覆盖 env/config/default 优先级、原生默认 Root 不存在时失败、相对路径解析、Root 外绝对路径拒绝、开发开关允许外部路径、目录初始化失败。
- [x] 运行验证：`go test ./storage ./config`。

## Phase 2: Docker Compose 默认本地挂载

- [x] 修改 `docker-compose.yml`，将后端运行时数据统一挂载为 `${NOFX_RUNTIME_HOST_PATH:-./runtime}:/app/runtime`。
- [x] 删除 Compose 默认的 `./models:/app/models:ro`、`./data:/app/data`、`./decision_logs:/app/decision_logs` 运行时挂载，保留 `./config.json:/app/config.json:ro`。
- [x] 在 Compose 后端环境变量中设置 `NOFX_STORAGE_ROOT=${NOFX_CONTAINER_STORAGE_ROOT:-/app/runtime}`、`NOFX_CONTAINER_STORAGE_ROOT=${NOFX_CONTAINER_STORAGE_ROOT:-/app/runtime}` 和 `NOFX_RUNTIME_HOST_PATH=${NOFX_RUNTIME_HOST_PATH:-./runtime}`。
- [x] 更新 `.env.example`，新增 `NOFX_RUNTIME_HOST_PATH=./runtime`、`NOFX_CONTAINER_STORAGE_ROOT=/app/runtime`，并说明外置盘覆盖方式。
- [x] 更新 `.gitignore`，确保 `runtime/`、训练日志、历史库、模型产物继续被忽略。
- [x] 验证默认 Compose 配置包含 `./runtime` 和 `/app/runtime`，且不包含默认 `/Volumes/light2/nofx`。
- [x] 验证覆盖配置：`NOFX_RUNTIME_HOST_PATH=/Volumes/light2/nofx docker compose config` 显示外置盘宿主机路径和容器 `/app/runtime`。

## Phase 3: 后端运行时目录接入

- [x] 修改 `main.go`，配置加载后解析 `storage.RuntimeConfig` 和 `storage.Layout`，用 `storage.EnsureLayout` 替代固定 `./data` 初始化。
- [x] 将 `decision.Config.DataDir` 从固定 `./data` 改为 `layout.Data`。
- [x] 为 `pool` 增加缓存目录注入方式，使 AI500 和 OI Top 缓存写入 `layout.CoinPoolCache`。
- [x] 修改 `trader.AutoTrader` 决策日志目录来源，使用 `layout.DecisionLogs/<trader_id>`，保留旧路径解析兼容展示。
- [x] 修改 `strategy/drl` lifecycle 默认模型输出、archive 和自动重训练 DataPath 注入，使用 `layout.ModelsDRL` 与 `layout.HistoryDB`。
- [x] 在创建 live DRL trader 前解析 `DRLStrategyConfig.ModelPath`：相对路径基于 Storage Root，绝对路径默认必须位于 Storage Root 内。
- [x] 为 `api.Server` 增加 options 或 `NewServerWithOptions`，传入 Storage Layout 和后续训练 Manager，同时保持现有测试可兼容。
- [x] 增加或调整测试，覆盖 main/API 初始化时 runtime 路径传递不再固定到源码目录。
- [x] 运行验证：`go test ./decision ./pool ./trader ./manager ./api`。

## Phase 4: Backtest 与历史数据路径统一

- [x] 修改 `api/backtest_handlers.go`，默认历史库使用 `layout.HistoryDB`，默认输出目录使用 `layout.BacktestRuns`。
- [x] 对 backtest API 中用户传入的 `db`、`output_dir`、report file 路径使用 `storage.ResolveUnderRoot` 校验。
- [x] 在 DRL backtest engine 创建前解析 `drl_strategy.model_path`，确保相对路径指向 Storage Root 内模型文件。
- [x] 保持 `historydb.Open` 底层接口不变，由调用方传入已解析路径。
- [x] 增加 API 测试，覆盖默认路径、Root 外路径拒绝、report 文件路径穿越拒绝。
- [x] 运行验证：`go test ./api ./backtest ./historydb`。

## Phase 5: Storage 诊断与数据迁移

- [x] 实现 `GET /api/storage/diagnostics`，返回生效 Storage Root、来源、Compose 宿主机挂载源、容器内路径、目录状态和磁盘容量。
- [x] 实现 `storage` 迁移服务，支持从 `backtest_data/`、`models/drl/`、`logs/`、`backtest_runs/`、`data/`、`decision_logs/`、`coin_pool_cache/` copy 到 Storage Root。
- [x] 迁移默认不覆盖目标同名文件，并生成冲突报告。
- [x] 覆盖模式下先备份到 `trash/migration_backup_<timestamp>/` 或校验 hash 后替换。
- [x] 新增 `cmd/storage-migrate/main.go`，支持 dry-run、overwrite、report 输出。
- [x] 实现 `POST /api/storage/migrations` 和 `GET /api/storage/migrations/:migration_id`，可查询迁移报告。
- [x] 增加测试，覆盖迁移摘要、冲突、覆盖备份、失败时源数据不变、路径穿越拒绝。
- [x] 运行验证：`go test ./storage ./api`。

## Phase 6: DRL-PPO 训练任务管理器

- [x] 新增 `drltrain/` 包，定义训练请求、job metadata、状态枚举、进度、模型产物、评估结果类型。
- [x] 实现训练参数校验：symbol、timeframe、start/end、total_timesteps、observation_window、initial_balance、fee、slippage、output_model_name。
- [x] 实现历史数据覆盖检查，训练前确认 `layout.HistoryDB` 中指定 source/symbol/timeframe/range 数据充足。
- [x] 实现 job 创建与持久化，将 `job.json`、`stdout.log`、`stderr.log`、`summary.json` 写入 `layout.TrainJobs/<job_id>/`。
- [x] 实现 Python 训练进程启动，调用 `training/drl/scripts/train.py`，传入 Storage Root 下的 history DB 和模型输出路径。
- [x] 更新 `training/drl/scripts/train.py` 和 `training/drl/agents/ppo_agent.py`，支持 `--progress-path` 并通过 Stable Baselines callback 写入 JSONL 结构化进度。
- [x] 后端优先从 `progress.jsonl` 读取 timesteps 和更新时间；旧脚本或无进度文件时退化为日志更新时间和运行时长。
- [x] 兼容 rolling 训练产物，记录 rolling summary 和 window `.zip` 模型；普通训练记录 `.zip` 与 `.onnx`。
- [x] 实现并发限制，默认同一时间只允许 1 个 DRL-PPO 训练 job 运行。
- [x] 实现 job 查询、列表、取消；取消先尝试优雅终止，超时后强制终止。
- [x] 实现后端重启恢复，已完成/失败 job 可恢复，运行中 job 标为 `interrupted`。
- [x] 实现日志 tail/offset 读取，避免一次读取超大日志。
- [x] 使用 fake Python 脚本增加单元测试，覆盖创建、完成、失败、取消、并发限制、恢复和日志读取。
- [x] 运行验证：`go test ./drltrain`。

## Phase 7: DRL-PPO API、模型注册与评估

- [x] 新增 `api/drl_ppo_train_handlers.go`，仅在训练 API enabled 时注册 `/api/drl-ppo/*` 路由。
- [x] 实现 `GET /api/drl-ppo/health`，返回模块启用状态、Python 可用性、Storage Root 摘要和 stub ONNX Runtime 提示。
- [x] 实现 `GET /api/drl-ppo/history/coverage`、`POST /api/drl-ppo/history/gaps`、`POST /api/drl-ppo/history/fetch`，复用 `historydb` 和公共 Binance Futures 数据源。
- [x] 实现 `GET /api/drl-ppo/jobs`、`POST /api/drl-ppo/jobs`、`GET /api/drl-ppo/jobs/:job_id`、`POST /api/drl-ppo/jobs/:job_id/cancel`、`GET /api/drl-ppo/jobs/:job_id/logs`。
- [x] 实现模型 registry，扫描 `layout.ModelsDRL/**/metadata.json`，只返回 Storage Root 相对路径。
- [x] 实现模型评估接口，调用 `training/drl/scripts/evaluate.py` 并写入 `evaluation.json`。
- [x] 实现 staging config 接口，只生成 DRL trader 配置建议，不自动写入 `config.json`，不重启 trader。
- [x] API 响应中屏蔽 secret，只返回 trader_id、exchange、decision_mode、model_version 和凭证是否存在。
- [x] 增加 API 测试，覆盖 disabled 状态、创建训练、查询、取消、日志、模型列表、评估失败、Root 外路径拒绝。
- [x] 运行验证：`go test ./api ./drltrain ./historydb`。

## Phase 8: 前端 API、类型与页面入口

- [x] 新增 `web/src/types/drlPpoTraining.ts`，定义 storage diagnostics、coverage、job、log、model、evaluation、staging config 类型。
- [x] 新增 `web/src/lib/drlPpoTrainApi.ts`，封装 `/api/drl-ppo` 和 `/api/storage` 请求，错误处理保持现有风格。
- [x] 修改 `web/src/App.tsx`，新增 `drlPpoTraining` 页面状态和 `#drl-ppo-training` hash 路由。
- [x] 训练 API enabled 且 health 正常时展示 `DRL-PPO` 页面入口；disabled 时不展示启动训练入口。
- [x] 保持现有 `competition`、`trader`、`backtest` 页面行为不回退。

## Phase 9: DRL-PPO 训练 UI

- [x] 新增 `DRLPPOTrainingPage`，使用现有深色操作台风格，不做营销页。
- [x] 实现 Storage 状态面板，展示 Storage Root 相对路径、来源、可用空间、关键目录容量和 Compose 挂载提示。
- [x] 实现历史数据覆盖面板，展示 source、symbol、timeframe、count、from、to、data_hash，并支持缺口检查。
- [x] 实现补数据交互，覆盖不足时可确认启动 fetch job，并展示插入数、重复数、失败项和请求统计。
- [x] 实现训练表单，包含 symbol、timeframe、start/end、total_timesteps、observation_window、initial_balance、fees、slippage、rolling、output_model_name。
- [x] 数据覆盖不足时默认禁用训练；勾选允许不完整数据训练时需要确认。
- [x] 实现 job 列表，支持状态过滤、最近任务置顶、运行中 job 自动刷新、取消确认。
- [x] 实现日志查看器，按 offset/tail 增量加载 stdout/stderr，不一次性加载完整日志。
- [x] 实现模型列表与评估视图，展示 `.zip`、`.onnx`、评估报告、是否建议部署。
- [x] 实现 staging config 视图，展示配置建议、当前 trader/持仓风险提示和 stub ONNX Runtime 提示。
- [x] 路径展示全部使用 Storage Root 相对路径，不展示宿主机绝对路径。
- [x] 运行验证：`cd web && npm run build`；如新增工具函数测试，追加 `cd web && npm run test`。

## Phase 10: Docker 训练运行方式

- [x] 确认本机原生运行训练 API 可用，Docker 后端镜像中训练 API 默认 disabled。
- [x] 评估是否需要新增 Debian/Python slim trainer sidecar；如实现 sidecar，新增 `docker/Dockerfile.trainer` 并共享 `/app/runtime`。
- [x] 如果保留后端 exec runner，不把 `torch/stable-baselines3` 强行加入 Alpine 后端镜像，避免 musl 兼容问题。
- [x] 更新 Compose 文档，明确交易后端镜像和训练运行环境的边界。
- [x] 验证 Compose 默认启动不依赖外置盘、不依赖 Python 训练依赖也能启动交易/API 服务。

## Phase 11: 文档、示例与端到端验证

- [x] 更新 `config.json.example`，新增 `storage` 和 `drl_ppo_train` 示例，不包含真实密钥。
- [x] 更新 `training/drl/README.md` 或新增运行说明，描述 Storage Root、Compose 默认本地 runtime、外置盘覆盖和短训练示例。
- [x] 运行后端验证：`go test ./storage ./drltrain ./api ./backtest ./historydb ./config ./manager ./trader`。
- [x] 运行前端验证：`cd web && npm run build`。
- [x] 验证 Docker 默认挂载：`docker compose config` 中默认出现 `./runtime` 和 `/app/runtime`，不默认出现 `/Volumes/light2/nofx`。
- [x] 验证 Docker 外置盘覆盖：`NOFX_RUNTIME_HOST_PATH=/Volumes/light2/nofx docker compose config` 中出现外置盘宿主机路径和 `/app/runtime`。
- [x] 使用小规模 BTCUSDT 数据启动一次短训练 job，确认 UI 可见 `running -> completed` 或 `running -> failed`，并能查看日志和 summary。
- [x] 最后交付前检查 API/UI 不输出 secret，训练 job 不修改 trader 配置，部署准备只生成 staging config。
