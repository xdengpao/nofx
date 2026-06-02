# 深度强化学习（DRL-PPO）自动化交易策略 Requirements

## 背景

当前 NOFX 支持三种决策模式：`ai`（LLM 调用）、`programmatic`（缠论规则引擎）和 `chanlun_v2`（Rust FFI 缠论）。这三种模式各有优劣：AI 模式泛化能力强但输出不稳定，程序化策略确定性好但对非线性市场结构适应力有限。

为增强系统在高波动加密货币市场的趋势捕获能力，需新增基于**近端策略优化（Proximal Policy Optimization, PPO）**的深度强化学习决策模式。PPO 在相关研究中对加密货币市场（如 ETH-USDT）展现出极强的盈利潜力，其核心优势在于训练过程稳定、能处理连续动作空间、并能通过自适应学习捕捉市场体制变化。

本策略采用多维技术指标作为观测空间输入，输出连续仓位信号（-1 到 +1），结合 CCXT 统一交易接口实现多交易所自动化执行，并通过蒙特卡洛模拟和波动率压力测试进行严谨的风险回测。

## 目标

- 新增 `drl` 决策模式，允许 trader 通过配置选择使用 PPO 强化学习策略进行交易决策。
- DRL 策略引擎接收多时间框架行情数据和技术指标，输出标准 `decision.Decision` 动作。
- 支持基于 OpenAI Gym 兼容环境的离线训练和在线推理双模式。
- 提供完整的回测分析框架，包含夏普比率、最大回撤、蒙特卡洛 VaR 和波动率压力测试。
- 支持滚动窗口训练策略以适应加密货币市场的非平稳性。
- DRL 策略输出仍受现有公共风控层约束（熔断、回撤硬停、仓位 sizing、保护单同步等）。

## 非目标

- 本阶段不要求实现分布式训练或 GPU 集群支持；单机 CPU/GPU 训练即可。
- 本阶段不要求实现 PMformer（部分多变量 Transformer）预测增强；预留接口但不实现。
- 本阶段不要求支持多资产组合级 DRL（如 A2C/SAC 多币同时决策）；聚焦单币 PPO。
- 本阶段不要求用 CCXT 替换现有交易所连接器；DRL 策略输出仍走现有执行链路。
- 本阶段不要求保证策略盈利，所有行为只保证可配置、可训练、可回测、可验证。
- 本阶段不实现稳定币避险的自动对冲执行；仅在回测报告中提供模拟分析。

## 术语

- **PPO（Proximal Policy Optimization）**：一种策略梯度强化学习算法，通过限制策略更新步幅保证训练稳定性。
- **观测空间（Observation Space）**：DRL 环境在每个时间步提供给 Agent 的状态向量，包含行情数据、技术指标和账户状态。
- **动作空间（Action Space）**：Agent 输出的决策空间，本策略采用连续空间 [-1, +1]，正数代表做多仓位比例，负数代表做空仓位比例。
- **奖励函数（Reward Function）**：评估 Agent 动作优劣的信号，本策略以资产总价值变化率为核心奖励。
- **回合（Episode）**：一次完整的训练序列，从起始时间到终止时间。
- **滚动窗口（Rolling Window）**：训练时使用最近 N 天数据训练，随时间推移窗口前移，用于适应市场体制变化。
- **蒙特卡洛模拟**：基于几何布朗运动生成随机价格路径，估算策略在不同市场情景下的表现分布。
- **VaR（Value at Risk）**：在给定置信水平下，策略在特定时间段内可能遭受的最大损失。
- **波动率压力测试**：引入极端波动冲击因子（如 30%），模拟"黑天鹅"事件对策略的影响。
- **方向准确性（Directional Accuracy）**：预测价格变动方向的准确率，比绝对误差更直接影响交易利润。
- **特征工程**：将原始行情数据转换为适合模型输入的技术指标和统计特征的过程。

## Requirements

### 1. 决策模式扩展

**User Story:** 作为量化交易员，我希望通过配置选择 DRL 策略模式，以便利用强化学习的自适应能力捕获市场趋势。

#### Acceptance Criteria

1. WHEN trader 配置 `decision_mode` 为 `drl` THEN DRL 策略引擎 SHALL 接管策略层交易能力。
2. WHEN `decision_mode=drl` THEN 系统 SHALL 不调用 AI API 生成交易决策。
3. WHEN `decision_mode=drl` THEN 系统 SHALL 加载预训练模型文件进行推理，若模型文件不存在则启动失败并返回清晰中文错误。
4. WHEN 同一系统运行多个 trader THEN 每个 trader SHALL 可独立选择 `drl` 模式，互不影响。
5. WHEN DRL 策略运行 THEN 现有公共风控层（熔断、回撤硬停、交易计划、保护单同步、open gate、仓位 sizing）SHALL 可覆盖或拒绝策略输出。
6. WHEN `decision_mode` 为不支持的值 THEN 配置校验 SHALL 失败并返回清晰中文错误。

### 2. DRL 策略配置

**User Story:** 作为量化交易员，我希望 DRL 策略的模型路径、观测窗口、动作阈值和风控参数可配置，以便按不同市场和账户调参。

#### Acceptance Criteria

1. WHEN `decision_mode=drl` THEN 系统 SHALL 要求或自动补全 `drl_strategy` 配置块。
2. IF DRL 配置缺失可安全默认的字段 THEN 系统 SHALL 使用保守默认值（如观测窗口 60、动作阈值 0.1）。
3. IF DRL 配置缺失必须字段（如 `model_path`）或字段范围非法 THEN 配置校验 SHALL 失败。
4. WHEN 配置指定 `symbols` 列表 THEN DRL 引擎 SHALL 仅对配置币种进行决策。
5. WHEN 配置指定 `timeframe` THEN 系统 SHALL 按该时间框架获取行情数据（支持 3m/15m/1h/4h）。
6. WHEN 配置指定 `action_threshold` THEN Agent 输出绝对值低于阈值时 SHALL 映射为 `wait` 动作。
7. WHEN 配置指定 `max_position_pct` THEN 单次开仓不得超过账户净值的该比例。

### 3. 观测空间与特征工程

**User Story:** 作为量化交易员，我希望 DRL 策略使用丰富的技术指标识别市场趋势和波动，以提升决策质量。

#### Acceptance Criteria

1. WHEN DRL 引擎构建观测向量 THEN SHALL 包含最近 N 个时间步（默认 60）的标准化 OHLCV 数据。
2. WHEN DRL 引擎构建观测向量 THEN SHALL 包含以下技术指标：MACD（含 MACD 线、信号线、柱状图）、EMA（短期/长期）、RSI（14 周期）、ATR（14 周期）、CCI（20 周期）、布林带（20 周期，2 标准差）。
3. WHEN DRL 引擎构建观测向量 THEN SHALL 包含账户状态：当前仓位比例、未实现盈亏比例、可用保证金比例。
4. WHEN 历史数据不足以计算某指标（如启动初期） THEN 系统 SHALL 使用 0 填充并在诊断中标记 `insufficient_data`。
5. WHEN 观测向量构建完成 THEN 所有特征 SHALL 经过 Z-Score 标准化（基于滚动窗口统计）。
6. WHEN 配置启用 `prediction_enhancement` THEN 系统 SHALL 预留外部预测模型接口（本阶段返回 0 向量）。

### 4. 动作空间与决策映射

**User Story:** 作为量化交易员，我希望 DRL Agent 的连续输出被正确映射为 NOFX 的标准交易动作。

#### Acceptance Criteria

1. WHEN Agent 输出值在 (+threshold, +1] 范围 THEN SHALL 映射为 `open_long` 动作，仓位大小为 `|output| * max_position_pct * equity`。
2. WHEN Agent 输出值在 [-1, -threshold) 范围 THEN SHALL 映射为 `open_short` 动作，仓位大小为 `|output| * max_position_pct * equity`。
3. WHEN Agent 输出值在 [-threshold, +threshold] 范围 THEN SHALL 映射为 `wait` 动作。
4. WHEN 已有多头持仓且 Agent 输出转为负值 THEN SHALL 映射为 `close_long` 后（若超过阈值）再 `open_short`。
5. WHEN 已有空头持仓且 Agent 输出转为正值 THEN SHALL 映射为 `close_short` 后（若超过阈值）再 `open_long`。
6. WHEN 动作映射完成 THEN 所有输出 SHALL 为标准 `decision.Decision` 结构，包含 Symbol、Action、PositionSizeUSD、Leverage、StopLoss、TakeProfit、Confidence 和 Reasoning。
7. WHEN Confidence 字段生成 THEN SHALL 基于 Agent 输出绝对值映射到 [0, 100] 区间。

### 5. PPO 训练环境

**User Story:** 作为量化研究员，我希望有一个基于 OpenAI Gym 的训练环境，以便用历史数据训练和评估 DRL 模型。

#### Acceptance Criteria

1. WHEN 训练环境初始化 THEN SHALL 使用历史 OHLCV 数据构建回合，每个时间步推进一根 K 线。
2. WHEN Agent 执行动作 THEN 环境 SHALL 模拟手续费（默认 taker 0.05%）和滑点（默认 0.03%）。
3. WHEN 计算奖励 THEN SHALL 使用资产总价值变化率（Gross Value Change），而非单纯价格预测误差。
4. WHEN 账户净值回撤超过配置阈值（默认 20%）THEN 回合 SHALL 提前终止并给予负奖励惩罚。
5. WHEN 训练采用滚动窗口模式 THEN SHALL 按配置的窗口大小和步进周期切换训练数据。
6. WHEN 训练完成 THEN SHALL 输出模型权重文件（ONNX 格式）和训练指标日志。
7. WHEN 训练环境 reset THEN 初始状态 SHALL 包含充足历史数据以计算所有技术指标。

### 6. 回测与风险分析

**User Story:** 作为量化交易员，我希望有完整的回测分析框架评估 DRL 策略的鲁棒性和风险特征。

#### Acceptance Criteria

1. WHEN 回测完成 THEN 报告 SHALL 包含：总 ROI、年化收益率、夏普比率、索提诺比率、最大回撤、胜率、盈亏比、交易频率。
2. WHEN 回测完成 THEN 报告 SHALL 包含按时间段（日/周/月）分解的收益曲线。
3. WHEN 配置启用蒙特卡洛模拟 THEN 系统 SHALL 基于几何布朗运动生成不少于 2000 条随机价格路径。
4. WHEN 蒙特卡洛模拟完成 THEN 报告 SHALL 包含 95% 和 99% 置信水平的 VaR 值和潜在损失概率分布。
5. WHEN 配置启用波动率压力测试 THEN 系统 SHALL 引入可配置的波动冲击因子（默认 30%），模拟极端行情下的策略表现。
6. WHEN 配置启用稳定币避险模拟 THEN 报告 SHALL 对比 100% 策略仓位与 70% 策略 + 30% USDT 组合的回撤差异。
7. WHEN 回测使用 DRL 模型 THEN SHALL 复用现有 `backtest.Runner` 框架，DRL 引擎作为策略插件接入。

### 7. 模型生命周期管理

**User Story:** 作为量化研究员，我希望能管理 DRL 模型的训练、验证、部署和更新流程。

#### Acceptance Criteria

1. WHEN 系统启动且 `decision_mode=drl` THEN SHALL 从配置的 `model_path` 加载 ONNX 模型。
2. WHEN 模型文件不存在或格式损坏 THEN 系统 SHALL 启动失败并返回清晰中文错误。
3. WHEN 配置启用 `auto_retrain` THEN 系统 SHALL 按配置周期（默认每周）使用最新数据重新训练并热更新模型。
4. WHEN 模型热更新 THEN SHALL 验证新模型在最近验证集上的方向准确性不低于阈值（默认 55%），否则保留旧模型。
5. WHEN 模型推理 THEN SHALL 记录每次推理的输入特征摘要、输出动作和推理耗时到决策日志。
6. WHEN 多个 trader 使用 DRL 模式 THEN 每个 trader SHALL 可独立指定不同模型文件。

### 8. 可观测性与诊断

**User Story:** 作为量化交易员，我希望能实时观察 DRL 策略的决策过程和模型状态。

#### Acceptance Criteria

1. WHEN DRL 策略生成决策 THEN `FullDecision.StrategyDiagnostics` SHALL 包含：当前观测向量摘要、Agent 原始输出值、动作映射结果、模型版本。
2. WHEN DRL 策略生成决策 THEN `FullDecision.CoTTrace` SHALL 包含人类可读的决策推理链（如"RSI=72 超买 + MACD 死叉 → Agent 输出 -0.65 → 开空 65% 仓位"）。
3. WHEN API 请求 `/api/strategy/drl/status` THEN SHALL 返回模型版本、最后推理时间、累计推理次数、平均推理耗时。
4. WHEN API 请求 `/api/strategy/drl/features` THEN SHALL 返回最近一次观测向量的完整特征值和标准化前后对比。
5. WHEN DRL 策略触发交易动作 THEN 决策日志 SHALL 记录完整上下文，格式与现有 AI/programmatic 日志一致。
