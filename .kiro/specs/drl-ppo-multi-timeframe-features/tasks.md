# DRL-PPO Multi-Timeframe Features Tasks

## 执行原则

- 每个任务完成后更新本文件，将 `- [ ]` 改为 `- [x]`；如果三次修复后仍无法通过，标为 `- [!]` 并写明原因。
- 默认行为必须保持 `single_timeframe`，旧训练请求、旧模型元数据、旧 staging config 和旧 DRL trader 配置继续可用。
- 多时间框架特征必须严格使用 `close_time <= primary_decision_time` 的已收盘辅助 K 线，不允许未来函数。
- 训练、评估、staging config 和运行时推理只生成实验产物或配置建议，不自动修改正在运行的 trader 配置，不重启交易服务。
- 测试不得触发真实下单，不输出交易所 API key、私钥、钱包私密字段或完整 secret。

## Phase 1: 特征模式常量、配置与兼容归一化

- [ ] 在 Go 侧增加统一 feature mode/schema 常量：`single_timeframe`、`multi_timeframe_features`、`drl_ppo_single_v1`、`drl_ppo_multi_tf_v1`。
- [ ] 扩展 `config.DRLStrategyConfig`，新增 `feature_mode`、`primary_timeframe`、`context_timeframes`、`context_window`、`feature_schema`、`context_missing_policy`、`max_missing_context` 字段。
- [ ] 实现 DRL 策略配置归一化：空 `feature_mode` 默认为 `single_timeframe`，空 `primary_timeframe` 使用旧 `timeframe`，旧配置不需要新增字段。
- [ ] 实现多时间框架校验：仅允许 `3m`、`15m`、`1h`、`4h`；辅助 timeframe 去重、排序，并移除或拒绝与 primary 相同的值。
- [ ] 对非法 `feature_mode`、`primary_timeframe`、`context_timeframes` 返回清晰中文错误。
- [ ] 确保 `single_timeframe` 忽略辅助 timeframe，输入维度仍为 `observation_window * 16 + 3`。
- [ ] 为配置兼容增加测试，覆盖旧 config、空 feature mode、非法主周期、非法辅助周期、辅助周期包含主周期、context_window 默认值。
- [ ] 运行验证：`go test ./config`。

## Phase 2: 训练请求、历史覆盖与补数据 API

- [ ] 扩展 `drltrain.TrainRequest`，新增 `feature_mode`、`primary_timeframe`、`context_timeframes`、`context_window` 字段，并保持旧 JSON 请求兼容。
- [ ] 在 `drltrain` 中实现训练请求归一化：空 `feature_mode` 为 `single_timeframe`，空 `primary_timeframe` 为 `timeframe`，`context_window` 默认为 `min(observation_window, 60)`。
- [ ] 扩展训练请求校验，支持多时间框架请求，并对非法 primary/context timeframe 返回中文错误。
- [ ] 修改 `drltrain.Manager.buildCommand()`，向训练脚本传入 `--feature-mode`、`--primary-timeframe`、`--context-timeframes`、`--context-window`，旧单周期命令仍可运行。
- [ ] 扩展训练 job metadata，记录归一化后的 feature mode、primary timeframe、context timeframes、context window、allow incomplete data 风险。
- [ ] 修改 `drltrain.Manager.writeCompletionSummary()`，由 Go manager 统一写入模型 `metadata.json`，并合并训练脚本产出的 feature summary（如存在）。
- [ ] 扩展 `POST /api/drl-ppo/history/gaps`，多时间框架模式下分别检查 primary 和 context timeframes，并返回 `timeframe_results`。
- [ ] 保持 gap 响应顶层 `ok/detail/gaps` 字段兼容旧 UI；新 UI 使用 `timeframe_results` 展示每个周期的覆盖状态。
- [ ] 扩展 `POST /api/drl-ppo/history/fetch` 的调用路径，补数据时归一化并去重 primary/context timeframes，避免只补主周期。
- [ ] 增加 API 和 manager 测试，覆盖旧单周期请求、多时间框架请求、覆盖不足分组返回、fetch timeframes 去重、显式 `allow_incomplete_data=true`。
- [ ] 运行验证：`go test ./drltrain ./api ./historydb`。

## Phase 3: Python 多时间框架数据加载与特征构建

- [ ] 在 `training/drl/data/loader.py` 增加 `load_multi_timeframe_klines()`，按 source/symbol/date range 加载 primary 和 context K 线。
- [ ] 新增 `training/drl/data/multiframe.py`，实现 timeframe 归一化、持续时间解析、context 排序、as-of 对齐 helper。
- [ ] 新增 `training/drl/env/multi_timeframe_features.py`，实现 `MultiTimeframeFeatureConfig`、多时间框架 observation dimension、feature layout 和 observation builder。
- [ ] 多时间框架 observation 按固定顺序拼接：primary window、各 context window、context status features、单份 account features。
- [ ] 对每个 context timeframe 使用 `close_time_ms <= primary close_time_ms` 的窗口，禁止使用未收盘高周期 K 线。
- [ ] 对 context 缺失窗口进行零填充，并输出 `available_ratio`、`staleness_ratio` 两个状态特征。
- [ ] 保持 `training/drl/env/features.py` 现有单周期 builder 行为不变。
- [ ] 增加 Python 单元测试，覆盖 as-of 对齐、防未来函数、context 缺失填充、feature layout offset、输入维度、NaN/Inf 检测和独立归一化。
- [ ] 运行验证：`cd training/drl && python -m pytest data env`。

## Phase 4: Python 训练、评估与 ONNX 导出脚本

- [ ] 修改 `training/drl/scripts/train.py`，新增 `--feature-mode`、`--primary-timeframe`、`--context-timeframes`、`--context-window` 参数，默认仍为 `single_timeframe`。
- [ ] 训练脚本在 `single_timeframe` 下继续使用现有 `CryptoTradingEnv` 和旧 observation dimension。
- [ ] 新增 `training/drl/env/multi_timeframe_env.py`，实现 `MultiTimeframeTradingEnv`，由 primary timeframe 驱动 step、执行价、奖励和终止条件。
- [ ] 训练脚本在 `multi_timeframe_features` 下加载多周期数据并创建 `MultiTimeframeTradingEnv`。
- [ ] 训练脚本在多周期模式下计算并输出 feature summary，至少包含 input dimension、feature layout、missing context 统计和 zero fill 统计，供 Go manager 写入模型 metadata。
- [ ] 修改 `training/drl/scripts/export_model.py`，支持从 metadata 或显式参数读取 observation dimension，避免单周期公式误用于多周期模型。
- [ ] 修改 `training/drl/scripts/evaluate.py`，优先从 metadata 恢复 feature mode/timeframes/input shape；缺失 metadata 时默认 `single_timeframe`。
- [ ] 确保多时间框架导出失败时训练 job 标记为 failed，并保留 stdout/stderr 与错误摘要。
- [ ] 增加训练脚本和评估脚本测试或 smoke 测试，覆盖单周期兼容、多周期维度、metadata 缺省兼容。
- [ ] 运行验证：`cd training/drl && python -m pytest`。

## Phase 5: 模型注册、staging config 与消融评估

- [ ] 扩展 `drltrain.ModelMetadata`，新增 `feature_mode`、`feature_schema`、`feature_schema_version`、`input_shape`、`primary_timeframe`、`context_timeframes`、`context_window`、`feature_layout` 字段。
- [ ] 修改 `drltrain.Manager.writeCompletionSummary()` 的模型 metadata 内容，记录 `feature_mode`、`feature_schema`、`feature_schema_version`、`input_shape`、`feature_layout`、primary/context timeframes、context window、数据范围和不完整数据风险。
- [ ] 模型 registry 读取旧 metadata 时归一化为 `single_timeframe`，并在可推断时生成旧 input shape。
- [ ] metadata reader 接受 `feature_schema_version` 作为 `feature_schema` 别名；新 metadata 可同时写出两个字段。
- [ ] 修改模型列表 API，返回 feature mode、input shape、primary/context timeframe 信息，并继续只展示 Storage Root 相对路径。
- [ ] 修改 staging config 生成逻辑：单周期模型保持旧结构，多周期模型增加 feature mode、schema、primary/context timeframes、context window。
- [ ] 在多周期 staging config warnings 中标记“未完成多时间框架基准对比”，除非已有可比 single-timeframe baseline。
- [ ] 扩展模型评估结果，支持 baseline model id、baseline metrics、baseline complete、ablation warnings。
- [ ] 增加模型 registry 和 staging config 测试，覆盖旧模型兼容、多周期 metadata、input shape 缺失不可部署、baseline warning。
- [ ] 运行验证：`go test ./drltrain ./api`。

## Phase 6: Go DRL 推理运行时多周期特征

- [ ] 扩展 `strategy/drl.DRLEngineConfig`，新增 feature mode、primary timeframe、context timeframes、context window、schema、missing context policy 字段。
- [ ] 修改 `DRLEngineConfig.ObservationDimension()`，按 feature mode 返回单周期或多周期输入维度。
- [ ] 修改 `DRLEngineConfig.MarketHistoryDepth()`，多周期模式下为 primary 和 context timeframes 设置显式 depth override，并对重复 timeframe 使用最大 depth；同时保持当前 `market.GetWithHistory()` 四周期数据构造兼容。
- [ ] 新增 `strategy/drl/multi_timeframe_feature_builder.go`，实现 `MultiTimeframeFeatureBuilder`。
- [ ] 多周期 builder 复用单周期每根 K 线 16 个特征的语义，按各 timeframe 独立窗口归一化。
- [ ] 多周期 builder 实现 as-of 选择、context 零填充、available/staleness 状态特征、feature layout 和输入维度校验。
- [ ] 扩展 `FeatureStats` 与诊断结构，记录 feature mode、primary/context timeframes、input shape、每个 context 可用 K 线数量、missing rows、last close time、staleness。
- [ ] 修改 `strategy/drl.Engine`，按 feature mode 分支调用单周期 builder 或多周期 builder，并保持动作映射、仓位 sizing、止损止盈和公共风控不变。
- [ ] 当多周期 context 完全缺失且 runtime policy 为 `wait` 时输出 wait，不产生开仓动作。
- [ ] 在 lifecycle/模型加载阶段校验 ONNX input shape 与配置生成维度；不匹配时启动失败并输出中文错误。
- [ ] 增加 Go 单元测试，覆盖单周期维度不变、多周期维度、MarketHistoryDepth、多周期 as-of、缺失 context wait、shape mismatch、诊断字段。
- [ ] 运行验证：`go test ./strategy/drl`。

## Phase 7: 前端类型、API client 与训练 UI

- [ ] 扩展 `web/src/types/drlPpoTraining.ts`，新增 `DRLPPOFeatureMode`、多周期训练请求字段、timeframe gap result、模型 metadata 字段、baseline evaluation 字段。
- [ ] 扩展 `web/src/lib/drlPpoTrainApi.ts`，gap/fetch/create job 请求支持 feature mode、primary timeframe、context timeframes、context window。
- [ ] 在 `DRLPPOTrainingPage` 增加 feature mode 分段控件，默认 `single_timeframe`。
- [ ] 多周期模式下展示 primary timeframe select、context timeframe 多选控件和 context window 数字输入；context 选择不允许手写逗号字符串。
- [ ] 切换到多周期模式时按 primary timeframe 预选默认 context timeframes，并移除 primary 自身。
- [ ] 为 `feature_mode`、`primary_timeframe`、`context_timeframes`、`context_window` 增加训练参数注释。
- [ ] 多周期 gap 检查展示每个 timeframe 的 role、count、from、to、data_hash、ok/detail 和缺失区间。
- [ ] 补历史数据按钮提交 primary 与 context timeframes 的去重列表；fetch job 完成后刷新所有参与周期覆盖状态。
- [ ] 启动训练时提交 feature mode/timeframes/context window；覆盖不足时禁用训练，除非用户显式允许不完整数据。
- [ ] 模型列表、job 详情、评估视图和 staging config 视图展示 feature mode、input shape、primary/context timeframes、baseline warning。
- [ ] UI 对 ONNX Runtime stub 状态继续提示“不是实盘推理质量证明”。
- [ ] 运行验证：`cd web && npm run build`；如新增工具函数，运行 `cd web && npm run test`。

## Phase 8: 端到端验证与安全检查

- [ ] 运行后端验证：`go test ./config ./historydb ./api ./drltrain ./strategy/drl`。
- [ ] 运行 Python 验证：`cd training/drl && python -m pytest`。
- [ ] 运行前端验证：`cd web && npm run build`。
- [ ] 启动一次短 `single_timeframe` 训练，确认旧维度 `observation_window * 16 + 3`、旧 metadata 兼容、旧 staging config 结构不变。
- [ ] 启动一次短 `multi_timeframe_features` 训练，例如 primary `3m` 加 context `15m/1h`，确认 coverage、metadata、input shape、feature layout、评估和 staging config 正确。
- [ ] 检查多周期训练样本日志或测试诊断，确认 context K 线均满足 `close_time <= primary_decision_time`。
- [ ] 检查多周期模型在缺失 context 或 shape mismatch 场景下不会产生开仓动作，并输出中文诊断。
- [ ] 检查 API/UI/model metadata/staging config 不输出 secret，不修改正在运行的 trader 配置，不自动切换实盘模型。
- [ ] 最终交付前对照 requirements、design、tasks 做一致性复核，确认每条 acceptance criteria 有实现或明确非阻塞说明。
