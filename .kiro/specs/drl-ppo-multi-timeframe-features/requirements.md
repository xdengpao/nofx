# DRL-PPO Multi-Timeframe Features Requirements

## 背景

当前 DRL-PPO 训练与推理链路已经支持单交易对、单时间框架的 PPO 模型训练、ONNX 导出、模型评估、训练 UI、历史数据覆盖检查和 DRL trader 推理。现有训练脚本通过单个 `timeframe` 加载历史 K 线，观测空间维度固定为 `observation_window * 16 + 3`，实盘推理也按同样单时间框架特征构建。

作为量化交易员，用户希望在保留现有单时间片模型的同时，新增一个可选的 `multi_timeframe_features` 版本，使模型在主决策周期上仍输出单个连续仓位动作，但可使用多个已收盘时间框架的上下文特征辅助判断趋势、波动和市场状态。该能力必须严格防止未来函数，并且不能破坏已训练的单时间框架模型、现有 DRL trader 配置和训练 UI。

## Feature Summary

- 保留原有 `single_timeframe` 训练、评估、ONNX 导出和实盘推理路径，默认行为不变。
- 新增可选 `multi_timeframe_features` 特征模式，允许在主时间框架之外引入辅助时间框架特征。
- 支持训练 UI 配置主时间框架、辅助时间框架、覆盖检查、补数据、训练任务、模型元数据和 staging config。
- 后端训练 API、训练脚本、模型注册、评估脚本和 DRL 推理配置均记录并校验特征模式、时间框架列表和输入维度。
- 多时间框架特征必须按决策时间点对齐，只能使用已收盘 K 线，避免回测和训练结果虚高。

## 非目标

- 本阶段不实现多交易对组合级 DRL；仍聚焦单 symbol 模型。
- 本阶段不要求 Transformer、LSTM、注意力模型或 PMformer；仍使用当前 PPO/SB3 训练框架。
- 本阶段不改变 NOFX 标准执行链路、公共风控、仓位 sizing、止损止盈和交易所适配器。
- 本阶段不自动替换正在实盘运行的 DRL 模型；部署仍只生成 staging config 或配置建议。
- 本阶段不要求多频率模型一定优于单频率模型；必须通过评估和消融实验验证。

## Glossary

- **single_timeframe**：当前单时间框架特征模式。训练和推理只使用一个 `timeframe` 的 K 线序列，输入维度为 `observation_window * 16 + 3`。
- **multi_timeframe_features**：新增多时间框架特征模式。以一个主时间框架驱动决策步进，并在每个决策点拼接或聚合多个辅助时间框架的已收盘上下文特征。
- **Primary Timeframe**：主时间框架，决定训练环境 step 粒度、动作输出频率、奖励计算和实盘推理触发使用的市场序列。
- **Context Timeframes**：辅助时间框架，例如 `15m/1h/4h`。仅用于构建上下文特征，不直接决定训练 step。
- **As-of Alignment**：按主时间框架当前决策时间，对每个辅助时间框架选择 `close_time <= decision_time` 的最后已收盘 K 线或窗口。
- **Feature Schema Version**：模型输入特征结构版本，用于区分单时间框架模型和多时间框架模型，防止配置加载维度不匹配。
- **Ablation Test**：消融实验，对比 `single_timeframe`、`multi_timeframe_features` 不同配置在相同数据区间上的指标差异。

## Requirements

### 1. 特征模式兼容性

**User Story:** 作为量化交易员，我希望新增多时间框架特征时不破坏已有单时间框架模型，以便已有 DRL trader 和训练任务继续可用。

#### Acceptance Criteria

1. WHEN 用户未显式选择特征模式 THEN 训练 API、训练脚本、评估脚本和 DRL trader 配置 SHALL 默认使用 `single_timeframe`。
2. WHEN 使用 `single_timeframe` THEN 系统 SHALL 保持现有 `timeframe`、`observation_window`、输入维度和 ONNX 导出行为不变。
3. WHEN 已有单时间框架模型元数据缺少 `feature_mode` THEN 系统 SHALL 将其兼容识别为 `single_timeframe`。
4. WHEN 加载单时间框架模型 THEN DRL 推理引擎 SHALL 使用旧的 `observation_window * 16 + 3` 维度校验。
5. WHEN 新增多时间框架字段 THEN 后端 SHALL 对旧请求 JSON 保持向后兼容，不得要求旧 UI 或旧脚本传递新增字段。
6. WHEN 单时间框架训练任务运行 THEN 不需要检查或抓取辅助时间框架数据。
7. WHEN 生成 staging config THEN `single_timeframe` 模型 SHALL 继续生成原有 `drl_strategy.timeframe` 配置。

### 2. 多时间框架训练配置

**User Story:** 作为量化研究员，我希望在训练时选择主时间框架和辅助时间框架，以便比较不同多周期上下文组合的效果。

#### Acceptance Criteria

1. WHEN 用户选择 `multi_timeframe_features` THEN 训练请求 SHALL 包含一个主时间框架和零个或多个辅助时间框架。
2. WHEN 主时间框架为空 THEN 系统 SHALL 使用当前表单中的 `timeframe` 作为主时间框架。
3. WHEN 辅助时间框架为空 THEN 系统 SHALL 允许训练继续，但 SHOULD 提示该配置等价于单时间框架上下文，建议使用 `single_timeframe`。
4. WHEN 用户选择辅助时间框架 THEN 辅助时间框架 SHALL 支持 `3m`、`15m`、`1h`、`4h`，且不得重复。
5. WHEN 辅助时间框架包含主时间框架 THEN 系统 SHALL 自动去重或返回清晰中文校验错误。
6. WHEN 主时间框架非法 THEN 后端 SHALL 返回中文错误：`primary_timeframe必须是3m、15m、1h或4h`。
7. WHEN 辅助时间框架非法 THEN 后端 SHALL 返回中文错误并指出非法值。
8. WHEN 多时间框架配置保存到模型元数据 THEN SHALL 记录 `feature_mode`、`primary_timeframe`、`context_timeframes`、`feature_schema_version` 和 `input_shape`。

### 3. 多时间框架历史覆盖与补数据

**User Story:** 作为量化研究员，我希望训练前同时检查主时间框架和辅助时间框架覆盖，以便避免多频率训练因某个周期缺失而失败。

#### Acceptance Criteria

1. WHEN 用户选择 `multi_timeframe_features` 并点击检查缺口 THEN 后端 SHALL 对主时间框架和所有辅助时间框架分别检查覆盖。
2. WHEN 任一时间框架覆盖不足 THEN 覆盖检查 SHALL 返回按 timeframe 分组的缺失区间。
3. WHEN 所有时间框架覆盖充足 THEN 覆盖检查 SHALL 返回 `ok=true` 且缺失区间为空。
4. WHEN UI 展示当前覆盖 THEN SHALL 同时展示每个参与训练 timeframe 的 count、from、to、data_hash 和覆盖状态。
5. WHEN 用户点击补历史数据 THEN UI SHALL 对主时间框架和辅助时间框架一起提交 fetch job，避免只补主时间框架。
6. WHEN fetch job 完成 THEN UI SHALL 刷新所有参与 timeframe 的 coverage 状态。
7. WHEN 数据源仍只支持 `binance-futures` THEN UI SHALL 继续清楚显示数据源限制。
8. WHEN 辅助时间框架缺失但用户允许不完整数据训练 THEN 后端 SHALL 要求显式 `allow_incomplete_data=true`，并在 job 元数据中记录该风险。

### 4. 多时间框架特征构建

**User Story:** 作为量化研究员，我希望多时间框架特征严格按决策时间对齐，以便模型不会看到未来 K 线。

#### Acceptance Criteria

1. WHEN 构建多时间框架训练样本 THEN 主时间框架 SHALL 决定环境 step、奖励计算和动作时间点。
2. WHEN 构建任一辅助时间框架特征 THEN 系统 SHALL 仅使用 `close_time <= 当前主时间框架决策时间` 的已收盘 K 线。
3. WHEN 某辅助时间框架在当前决策点没有可用已收盘 K 线 THEN 系统 SHALL 使用零填充或缺失标记，并在诊断中记录 missing context。
4. WHEN 使用多时间框架窗口 THEN 每个 timeframe 的窗口长度 SHALL 可配置或使用默认值，且默认不超过现有 `observation_window` 的合理上限。
5. WHEN 输出观测向量 THEN 系统 SHALL 明确记录每个 timeframe 的特征段位置和维度。
6. WHEN 观测向量中包含账户特征 THEN 账户特征 SHALL 只出现一次，不得按 timeframe 重复拼接。
7. WHEN 特征标准化 THEN 每个 timeframe 的价格和成交量统计 SHALL 在各自窗口内独立归一化，避免不同周期尺度互相污染。
8. WHEN 检测到输入 NaN、Inf 或维度不匹配 THEN 训练和推理 SHALL 失败或降级为 wait，并输出清晰中文诊断。

### 5. 训练脚本与评估脚本

**User Story:** 作为量化研究员，我希望 Python 训练和评估脚本支持多时间框架模式，以便从 UI 和命令行都能复现实验。

#### Acceptance Criteria

1. WHEN 调用训练脚本且 `--feature-mode single_timeframe` THEN 行为 SHALL 与当前脚本一致。
2. WHEN 调用训练脚本且 `--feature-mode multi_timeframe_features` THEN 脚本 SHALL 加载主时间框架和辅助时间框架历史数据。
3. WHEN 多时间框架历史数据加载完成 THEN 脚本 SHALL 构建统一的训练 DataFrame 或环境输入结构。
4. WHEN 训练脚本导出 ONNX THEN SHALL 使用多时间框架模式对应的 observation dimension。
5. WHEN 训练完成 THEN SHALL 写入模型元数据，包含 feature mode、timeframes、input shape、feature schema version、训练数据范围和代码版本摘要。
6. WHEN 调用评估脚本评估多时间框架模型 THEN 评估脚本 SHALL 按模型元数据恢复特征模式和时间框架配置。
7. WHEN 模型元数据缺失且用户未提供 feature mode THEN 评估脚本 SHALL 默认按 `single_timeframe` 处理。
8. WHEN 多时间框架模型导出失败 THEN job SHALL 标记为 failed，并保留 stdout/stderr 与错误摘要。

### 6. DRL 推理运行时

**User Story:** 作为交易系统维护者，我希望实盘推理能按模型特征模式构建输入，以便多时间框架模型不会因维度或数据不一致产生错误动作。

#### Acceptance Criteria

1. WHEN DRL trader 配置 `feature_mode=single_timeframe` 或未配置 feature mode THEN 推理 SHALL 使用现有单时间框架 FeatureBuilder。
2. WHEN DRL trader 配置 `feature_mode=multi_timeframe_features` THEN 推理 SHALL 使用多时间框架 FeatureBuilder。
3. WHEN 多时间框架推理运行 THEN 市场数据请求深度 SHALL 覆盖主时间框架和所有辅助时间框架。
4. WHEN 任一辅助时间框架缺失最新已收盘上下文 THEN 推理 SHALL 记录诊断；若缺失超过配置阈值 THEN 输出 wait 而不是开仓。
5. WHEN ONNX 模型 input shape 与当前特征构建维度不匹配 THEN trader 启动 SHALL 失败并返回清晰中文错误。
6. WHEN 决策日志写入 DRL 诊断 THEN SHALL 记录 feature mode、primary timeframe、context timeframes、input dimension、各 timeframe 可用 K 线数量和 missing context 数量。
7. WHEN 多时间框架模型输出动作 THEN 动作映射、仓位 sizing、止损止盈和公共风控 SHALL 复用现有 DRL 输出链路。
8. WHEN 当前 ONNX Runtime 是 stub 后端 THEN 多时间框架链路 SHALL 仍可验证维度、特征和诊断，但 UI SHALL 明确提示不是实盘推理质量证明。

### 7. 训练 UI 扩展

**User Story:** 作为量化交易员，我希望训练 UI 能直观选择单时间框架或多时间框架模式，以便无需手写命令即可管理实验。

#### Acceptance Criteria

1. WHEN 打开 DRL-PPO 训练页面 THEN UI SHALL 显示 `feature_mode` 控件，默认值为 `single_timeframe`。
2. WHEN 用户选择 `single_timeframe` THEN UI SHALL 保持当前训练配置表单布局和行为。
3. WHEN 用户选择 `multi_timeframe_features` THEN UI SHALL 显示主时间框架和辅助时间框架选择控件。
4. WHEN 用户选择辅助时间框架 THEN UI SHALL 使用多选控件，避免手写逗号分隔字符串。
5. WHEN UI 展示训练参数说明 THEN SHALL 为 `feature_mode`、`primary_timeframe`、`context_timeframes` 增加参数说明。
6. WHEN 创建训练任务 THEN UI SHALL 将 feature mode 和 timeframes 发送给后端。
7. WHEN 展示训练任务和模型列表 THEN UI SHALL 显示模型是 `single_timeframe` 还是 `multi_timeframe_features`。
8. WHEN 展示模型评估和 staging config THEN UI SHALL 显示模型输入维度和时间框架配置。
9. WHEN 多时间框架覆盖不足 THEN UI SHALL 清楚展示具体缺失 timeframe 和时间段。

### 8. 模型注册、元数据与部署准备

**User Story:** 作为量化研究员，我希望模型管理能区分单时间框架和多时间框架模型，以便部署时不会拿错配置或输入维度。

#### Acceptance Criteria

1. WHEN 训练任务完成 THEN 模型注册 SHALL 保存 feature mode、primary timeframe、context timeframes、input shape 和 feature schema version。
2. WHEN 模型列表读取旧模型 THEN 缺失 feature mode 的旧模型 SHALL 显示为 `single_timeframe`。
3. WHEN 生成 staging config THEN 多时间框架模型 SHALL 生成包含 feature mode 和 context timeframes 的 `drl_strategy` 配置建议。
4. WHEN 生成 staging config THEN 单时间框架模型 SHALL 保持现有配置结构。
5. WHEN 用户尝试部署多时间框架模型到不支持多时间框架的运行时 THEN UI SHALL 标记为不可部署或给出清晰警告。
6. WHEN 模型评估结果低于部署阈值 THEN 不论 feature mode 为何，UI SHALL 标记为不建议部署。
7. WHEN 模型文件、元数据或 input shape 不完整 THEN 模型 SHALL 不被标记为 deployable。

### 9. 消融实验与质量门槛

**User Story:** 作为量化交易员，我希望多时间框架模型必须通过基准对比，以便避免只是增加复杂度但没有稳定收益。

#### Acceptance Criteria

1. WHEN 训练多时间框架模型 THEN 系统 SHOULD 支持以相同 symbol、主 timeframe、日期区间生成单时间框架 baseline 对比。
2. WHEN UI 展示评估结果 THEN SHALL 支持并排查看单时间框架和多时间框架模型关键指标。
3. WHEN 多时间框架模型 Sharpe、最大回撤、方向准确率或收益回撤比未优于 baseline THEN UI SHOULD 标记为需要复核。
4. WHEN rolling 训练启用 THEN 多时间框架模型 SHALL 在每个 rolling window 记录独立指标。
5. WHEN 输出评估报告 THEN SHALL 记录是否存在辅助时间框架缺失、零填充比例和特征维度。
6. WHEN 消融实验未完成 THEN staging config SHALL 明确提示“未完成多时间框架基准对比”。

### 10. 安全与运行边界

**User Story:** 作为交易系统维护者，我希望多时间框架训练不会影响实盘交易服务，以便实验失败不会破坏现有风控。

#### Acceptance Criteria

1. WHEN 多时间框架训练任务运行 THEN SHALL 不修改正在运行的 trader 配置。
2. WHEN 多时间框架训练任务失败、取消或超时 THEN 交易服务 SHALL 不受影响。
3. WHEN 多时间框架模型生成后 THEN 系统 SHALL 不自动切换任何实盘 trader。
4. WHEN 历史数据补齐运行 THEN SHALL 使用公共行情接口，不需要交易所私钥。
5. WHEN API 返回任务、模型、staging config 或诊断信息 THEN SHALL 不包含交易所密钥、私钥、钱包私密字段或完整 secret。
6. WHEN 用户允许不完整数据训练 THEN job 元数据和模型元数据 SHALL 记录该风险。

### 11. 验证与交付

**User Story:** 作为开发维护者，我希望新增功能有明确测试覆盖，以便不会破坏现有单时间框架模型和交易链路。

#### Acceptance Criteria

1. WHEN 实现多时间框架特征构建 THEN SHALL 增加 Python 单元测试覆盖 as-of 对齐、防未来函数、缺失上下文、输入维度和标准化。
2. WHEN 实现 Go 推理特征构建 THEN SHALL 增加 Go 单元测试覆盖 single 与 multi 两种 feature mode 的维度和诊断。
3. WHEN 实现训练 API 扩展 THEN SHALL 增加 API 测试覆盖旧请求兼容、多时间框架请求校验、覆盖检查和模型元数据字段。
4. WHEN 实现 UI 扩展 THEN SHALL 通过 `cd web && npm run build`，并覆盖关键 TypeScript 类型。
5. WHEN 实现配置扩展 THEN SHALL 增加 `go test ./config` 覆盖默认值、非法时间框架和旧配置兼容。
6. WHEN 功能交付前 THEN SHALL 至少运行 `go test ./config ./historydb ./api ./drltrain ./strategy/drl` 和 `cd web && npm run build`。
7. WHEN 端到端验证 THEN SHALL 分别启动一次短 `single_timeframe` 训练和一次短 `multi_timeframe_features` 训练，并确认模型元数据、input shape、coverage、评估和 staging config 正确。
