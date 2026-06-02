# 深度强化学习（DRL-PPO）自动化交易策略 Tasks

## Phase 1: 配置与决策模式扩展

- [ ] 在 `config/programmatic.go` 的 `normalizeDecisionMode` 中注册 `drl` 为合法决策模式，并添加常量 `DecisionModeDRL = "drl"`。
- [ ] 在 `config/config.go` 的 `TraderConfig` 中新增 `DRLStrategy DRLStrategyConfig` 字段。
- [ ] 实现 `DRLStrategyConfig` 和 `DRLFeatureConfig` 完整结构体定义，含 JSON tag 和字段注释。
- [ ] 实现 `NormalizeDRLStrategy()` 函数，补全默认值（observation_window=60, timeframe="4h", action_threshold=0.1, max_position_pct=0.3, default_leverage=5, stop_loss_atr_mult=2.0, take_profit_atr_mult=3.0, max_drawdown_pct=20）。
- [ ] 实现 DRL 配置校验：`model_path` 必须非空、`observation_window` ∈ [10, 200]、`action_threshold` ∈ (0, 1)、`max_position_pct` ∈ (0, 1]、`timeframe` 必须为支持值。
- [ ] 更新 `config.json.example`，加入完整的 DRL trader 配置示例（不含真实凭证）。
- [ ] 在 `config/config_test.go` 中增加 DRL 配置校验的正向和反向测试用例。
- [ ] 验证：`go test ./config/...`。

## Phase 2: DRL 引擎核心结构

- [ ] 创建 `strategy/drl/` 包目录结构：`types.go`、`engine.go`、`model.go`、`feature_builder.go`、`action_mapper.go`、`risk.go`、`diagnostics.go`。
- [ ] 在 `types.go` 中定义内部类型：`DRLEngineConfig`（从 `config.DRLStrategyConfig` 转换）、`Observation`、`InferenceResult`、`FeatureStats`。
- [ ] 实现 `engine.go` 中 `Engine` 结构体和 `NewEngine(cfg) (*Engine, error)` 构造函数，完成模型加载和组件初始化。
- [ ] 实现 `Engine.GetFullDecision(ctx *decision.Context) (*decision.FullDecision, error)` 主流程框架（遍历 symbols → 构建观测 → 推理 → 映射 → 合并）。
- [ ] 实现 `Engine.Close()` 释放 ONNX session 资源。
- [ ] 在 `go.mod` 中添加 `github.com/yalue/onnxruntime_go` 依赖。
- [ ] 验证：`go build ./strategy/drl/...`（编译通过即可，推理测试在后续 phase）。

## Phase 3: 特征工程

- [ ] 在 `feature_builder.go` 中实现 `FeatureBuilder` 结构体，持有配置和 `ZScoreNormalizer`。
- [ ] 实现 `FeatureBuilder.Build(klines []market.Kline, account decision.AccountInfo, position *decision.PositionInfo) ([]float32, error)`。
- [ ] 实现 OHLCV 特征提取：从 K 线序列提取最近 N 根的标准化 Open/High/Low/Close/Volume。
- [ ] 实现 MACD 特征计算：EMA12、EMA26、MACD 线、Signal 线（EMA9）、Histogram，取最近 N 个时间步。
- [ ] 实现 EMA 双线特征：短期 EMA 和长期 EMA 的标准化值。
- [ ] 实现 RSI 特征计算：14 周期 RSI，归一化到 [0, 1]。
- [ ] 实现 ATR 特征计算：14 周期 ATR，除以当前收盘价标准化。
- [ ] 实现 CCI 特征计算：20 周期 CCI，除以 200 并 clip 到 [-1, 1]。
- [ ] 实现布林带特征：20 周期 SMA ± 2σ，标准化为相对偏移。
- [ ] 实现账户状态特征：仓位比例（带方向）、未实现盈亏比例、可用保证金比例。
- [ ] 实现 `ZScoreNormalizer`：基于滚动窗口统计进行 Z-Score 标准化，epsilon=1e-8 防止除零。
- [ ] 处理数据不足情况：当历史 K 线不足时使用零填充，并设置诊断标记。
- [ ] 在 `feature_builder_test.go` 中编写单元测试：验证输出维度正确（observation_window × 16 + 3）、标准化值范围合理、零填充行为。
- [ ] 验证：`go test ./strategy/drl/...`。

## Phase 4: ONNX 模型推理

- [ ] 在 `model.go` 中实现 `ONNXModel` 结构体：session、inputName、outputName、inputShape、mutex。
- [ ] 实现 `Load(modelPath string, inputShape []int64) (*ONNXModel, error)`：验证文件存在性、初始化 ONNX Runtime session、获取输入/输出名称。
- [ ] 实现 `Infer(observation []float32) (float32, error)`：线程安全推理，验证输入长度，处理输出 clip 到 [-1, 1]。
- [ ] 实现 `Close() error`：释放 session 和 ONNX Runtime 资源。
- [ ] 处理错误场景：模型文件不存在返回中文错误、推理失败返回 0.0 + error、输出超范围 clip + 日志警告。
- [ ] 创建测试用 mock ONNX 模型（简单线性模型或固定输出），用于单元测试。
- [ ] 在 `model_test.go` 中编写测试：加载成功/失败、推理正常/异常、并发安全。
- [ ] 验证：`go test ./strategy/drl/...`。

## Phase 5: 动作映射与风控

- [ ] 在 `action_mapper.go` 中实现 `ActionMapper` 结构体和 `Map()` 方法。
- [ ] 实现 wait 区间逻辑：|rawAction| ≤ threshold 时返回 wait Decision。
- [ ] 实现开多逻辑：rawAction > threshold 且无持仓时，计算仓位大小并生成 open_long Decision。
- [ ] 实现开空逻辑：rawAction < -threshold 且无持仓时，计算仓位大小并生成 open_short Decision。
- [ ] 实现反向平仓逻辑：有多头仓位且信号转空时，生成 close_long + open_short；反向同理。
- [ ] 实现同向持仓逻辑：已有同向仓位时返回 hold Decision。
- [ ] 在 `risk.go` 中实现 ATR 基止损/止盈计算：SL = entry ∓ ATR × mult、TP = entry ± ATR × mult。
- [ ] 实现 Confidence 映射：int(|rawAction| × 100)，clip 到 [0, 100]。
- [ ] 填充 Decision 的 Reasoning 字段：包含关键指标摘要和 Agent 输出值。
- [ ] 在 `action_mapper_test.go` 中编写测试：覆盖所有分支（wait/open_long/open_short/close+reverse/hold）。
- [ ] 验证：`go test ./strategy/drl/...`。

## Phase 6: 诊断与可观测性

- [ ] 在 `diagnostics.go` 中实现 `DiagnosticsCollector`：收集每次推理的特征摘要、原始输出、映射结果、耗时。
- [ ] 实现 CoTTrace 生成：人类可读的决策推理链（如"RSI=72 超买 + MACD 死叉 → Agent 输出 -0.65 → 开空 65%"）。
- [ ] 实现 StrategyDiagnostics map 填充：observation_summary、raw_action、mapped_action、model_version、inference_time_ms。
- [ ] 实现推理统计累积器：累计推理次数、总耗时、平均耗时、最大耗时。
- [ ] 验证：`go test ./strategy/drl/...`。

## Phase 7: Trader 路由集成

> **文件路径说明**：trader 包位于 `trader/auto_trader.go`，`AutoTrader` 结构体在第 132 行，`AutoTraderConfig` 在第 30 行，决策模式路由在 `NewAutoTrader()` 函数中（约第 233 行）通过 `config.DecisionMode` 分支初始化对应引擎。

- [ ] 在 `trader/auto_trader.go` 的 `AutoTraderConfig` 结构体中新增 `DRLStrategyConfig config.DRLStrategyConfig` 字段（与已有 `ProgrammaticStrategyPolicy` 和 `ChanlunV2StrategyConfig` 同级）。
- [ ] 在 `trader/auto_trader.go` 的 `AutoTrader` 结构体中新增 `drlEngine *drl.Engine` 字段（与已有 `programmaticEngine` 和 `chanlunV2Engine` 同级）。
- [ ] 在 `NewAutoTrader()` 函数的 DecisionMode 分支中增加 `drl` 分支：当 `config.DecisionMode == "drl"` 时创建 DRL engine（`drl.NewEngine()`），初始化失败返回中文错误 "初始化DRL策略引擎失败"。
- [ ] 确保 `decision_mode=drl` 时不初始化 `mcp.Client`（AI provider），且 `config.AIModel` 自动设为 `"drl"`。
- [ ] 启动日志使用与现有模式一致的格式：`log.Printf("🧮 [%s] 使用DRL策略: 模型=%s", config.Name, config.DRLStrategyConfig.ModelPath)`。
- [ ] 将 DRL engine 生命周期绑定到 trader：启动时 `NewEngine()`，停止/优雅关闭时调用 `drlEngine.Close()` 释放 ONNX 资源。
- [ ] 在交易决策循环（`runCycle` 或等效函数）中，当 `DecisionMode=="drl"` 时调用 `drlEngine.GetFullDecision(ctx)` 获取决策。
- [ ] DRL 策略输出经过 `decision.ValidateStrategyDecisions()` 公共风控验证（与 programmatic/chanlun_v2 一致）。
- [ ] DRL 策略输出与持仓管理决策通过现有 merge 逻辑合并（与 programmatic/chanlun_v2 一致）。
- [ ] 确保 DRL 决策写入决策日志，`FullDecision.DecisionMode` 设为 `"drl"`，`StrategyName` 和 `StrategyVersion` 从模型配置填充。
- [ ] 在 `manager/trader_manager.go` 的 `AddTrader()` 中确认 DRL 配置透传路径：`config.DRLStrategy` → `AutoTraderConfig.DRLStrategyConfig`。
- [ ] 验证 ONNX Runtime 依赖兼容性：确认 `CGO_ENABLED=1` 环境下 `go build ./trader/...` 编译通过（需系统安装 libonnxruntime）。使用 build tag `//go:build drl` 隔离 ONNX 依赖，确保无 ONNX Runtime 环境下其他模式仍可编译。
- [ ] 验证：`go test ./trader/... ./decision/...`（DRL 相关测试使用 mock 或 build tag 隔离）。

## Phase 8: 回测集成

- [ ] 在 `backtest/config.go` 的 `StrategyConfig` 中新增 `DRLStrategy config.DRLStrategyConfig` 字段。
- [ ] 在 `backtest/runner.go` 中增加 `drl` 分支：当 `decision_mode=drl` 时创建 DRL engine 替代 chanlun engine。
- [ ] 确保 DRL 引擎在回测模式下使用确定性推理（无随机性）。
- [ ] 在回测报告中增加 `DRLMetrics *DRLBacktestMetrics` 字段，计算 Sharpe/Sortino/方向准确性。
- [ ] 实现 `backtest/drl_metrics.go`：从权益曲线计算年化收益率、夏普比率、索提诺比率、最大回撤、胜率、盈亏比。
- [ ] 验证：使用 mock 模型进行端到端回测，`go test ./backtest/...`。

## Phase 9: 蒙特卡洛模拟与压力测试

- [ ] 实现 `backtest/monte_carlo.go`：`MonteCarloSimulator` 结构体和 `Simulate()` 方法。
- [ ] 实现几何布朗运动路径生成：S(t+dt) = S(t) × exp((μ - σ²/2)dt + σ√dt × Z)。
- [ ] 从历史数据估计漂移率 μ 和波动率 σ。
- [ ] 在模拟路径上运行 DRL 策略，收集终端资产价值分布。
- [ ] 计算 VaR_95、VaR_99、CVaR（Expected Shortfall）、亏损概率。
- [ ] 实现 `backtest/stress_test.go`：`StressTester` 结构体和 `Test()` 方法。
- [ ] 在历史 K 线数据中注入随机时间点的 ±δ 价格冲击。
- [ ] 评估冲击下的策略最大回撤、存活率和夏普比率变化。
- [ ] 实现 `backtest/hedge_comparison.go`：对比纯策略 vs 策略+稳定币的组合回撤。
- [ ] 在 `backtest/report.go` 中集成 DRL 扩展指标的 JSON 序列化和输出。
- [ ] 验证：`go test ./backtest/...`。

## Phase 10: Python 训练环境

- [ ] 创建 `training/drl/` 目录结构：`env/`、`agents/`、`data/`、`backtest/`、`scripts/`。
- [ ] 实现 `training/drl/requirements.txt`：列出 stable-baselines3、gymnasium、torch、onnx、onnxruntime、numpy、pandas 依赖。
- [ ] 实现 `training/drl/env/trading_env.py`：Gymnasium 兼容的 `CryptoTradingEnv`，含 observation_space、action_space、step()、reset()。
- [ ] 实现 `training/drl/env/features.py`：与 Go 侧 `FeatureBuilder` 对齐的 Python 特征工程（OHLCV + MACD + EMA + RSI + ATR + CCI + Bollinger + 账户状态）。
- [ ] 实现 `training/drl/env/rewards.py`：奖励函数（资产价值变化率 + 方向切换惩罚 + 回撤惩罚）。
- [ ] 实现 `training/drl/data/loader.py`：从 SQLite historydb 加载 K 线数据。
- [ ] 实现 `training/drl/data/preprocessor.py`：数据清洗、缺失值处理、时间对齐。
- [ ] 验证：`cd training/drl && python -m pytest env/ data/`。

## Phase 11: PPO 训练与模型导出

- [ ] 实现 `training/drl/agents/ppo_agent.py`：封装 Stable-Baselines3 PPO 训练，含超参配置和 callback。
- [ ] 实现滚动窗口训练逻辑：按配置窗口大小切分数据，逐窗口训练并验证。
- [ ] 实现方向准确性验证：在验证集上评估模型的方向预测准确率。
- [ ] 实现 `training/drl/agents/export.py`：将 PyTorch 模型导出为 ONNX 格式（opset 17，动态 batch）。
- [ ] 实现 `training/drl/scripts/train.py`：训练入口脚本（CLI 参数：数据路径、符号、时间范围、输出路径）。
- [ ] 实现 `training/drl/scripts/evaluate.py`：评估脚本（加载模型、在测试数据上运行、输出指标报告）。
- [ ] 实现 `training/drl/scripts/export_model.py`：模型导出脚本（PyTorch → ONNX）。
- [ ] 实现 `training/drl/backtest/metrics.py`：Python 侧回测指标计算（Sharpe/Sortino/MaxDD/WinRate），用于快速训练验证。
- [ ] 实现 `training/drl/backtest/monte_carlo.py`：Python 侧蒙特卡洛模拟，用于训练阶段快速风险评估。
- [ ] 编写 `training/drl/README.md`：训练流程文档（环境安装、数据准备、训练命令、导出步骤）。
- [ ] 验证：使用小规模历史数据完成一次完整训练→导出→验证流程。

## Phase 12: 模型生命周期管理

> **设计对应**：对应 design.md §11 模型生命周期管理组件。该组件管理 DRL 模型的自动重训练调度、验证、热更新原子切换和故障回滚，确保推理服务在更新全过程中不中断。

- [ ] 在 `strategy/drl/lifecycle.go` 中实现 `ModelLifecycleManager` 结构体：持有 `*Engine` 引用、`currentModel`（`*ONNXModel`）、`modelVersion`、`sync.RWMutex` 读写锁、`lastRetrainAt`/`lastValidateAt` 时间戳。
- [ ] 实现 `NewModelLifecycleManager(engine *Engine, cfg *DRLEngineConfig) *ModelLifecycleManager` 构造函数。
- [ ] 实现 `GetModelForInference() *ONNXModel`：使用 `mu.RLock()` 读锁保护，确保推理线程在热更新过程中不阻塞。
- [ ] 实现 `RetrainScheduler` 结构体：持有重训练间隔、脚本路径、输出目录，使用 `time.Ticker` 驱动定时检查。
- [ ] 实现 `RetrainScheduler.Start()` 和 `Stop()`：在后台 goroutine 中按 `retrain_interval_hours` 周期触发训练。
- [ ] 实现重训练触发逻辑：通过 `os/exec.CommandContext` 调用 Python 训练脚本，设置 30 分钟超时上下文，超时则 kill 进程并记录中文错误 "DRL 自动重训练超时（30分钟）"。
- [ ] 实现 `ValidateCandidate(candidatePath string) (*ModelValidationResult, error)`：加载候选模型，在最近验证窗口数据上逐步推理，计算方向准确性 DA。
- [ ] 实现原子切换逻辑 `HotSwap(candidatePath string) error`：
  - 加载候选模型 → 验证 DA ≥ `validation_min_da` → `mu.Lock()` → 替换 `currentModel` 引用 → `mu.Unlock()` → 旧模型 `Close()` → 归档旧模型到 `models/drl/archive/`。
- [ ] 实现验证不通过处理：`candidateModel.Close()` → 删除候选文件 → 记录中文警告 "候选模型验证不通过: DA={da:.4f} < 阈值{min_da}, 保留当前模型"。
- [ ] 实现 `Rollback(reason string) error`：从 `models/drl/archive/` 扫描最近备份 → 加载 → 原子替换 → 记录 "模型已回滚: 原因={reason}, 恢复到 v{version}"。
- [ ] 实现回滚失败降级逻辑：当归档目录无可用模型时，设置引擎为纯 wait 模式（所有推理返回 0.0），记录严重错误日志。
- [ ] 实现连续推理异常检测：在 `Engine.GetFullDecision()` 中维护 `consecutiveInferErrors` 计数器，连续 3 次推理错误触发 `Rollback()`。
- [ ] 实现与 Engine 的集成：`Engine.activeModel()` 方法优先通过 `Lifecycle.GetModelForInference()` 获取模型（当 `auto_retrain=true` 时），否则直接返回 `e.Model`。
- [ ] 在 `lifecycle_test.go` 中编写测试：热更新成功/失败、回滚成功/失败、并发读取安全、超时处理。
- [ ] 验证：`go test ./strategy/drl/...`。

## Phase 13: API 端点

- [ ] 在 `api/server.go` 中注册 DRL 策略相关路由。
- [ ] 实现 `GET /api/strategy/drl/status`：返回模型版本、最后推理时间、累计推理次数、平均推理耗时、模型文件路径。
- [ ] 实现 `GET /api/strategy/drl/features`：返回最近一次观测向量的完整特征值（标准化前后）和维度信息。
- [ ] 实现 `POST /api/strategy/drl/backtest/monte-carlo`：接收参数，触发蒙特卡洛模拟并返回结果。
- [ ] 实现 `POST /api/strategy/drl/backtest/stress-test`：接收参数，触发压力测试并返回结果。
- [ ] 在 `api/server_test.go` 中增加 DRL API 路由测试。
- [ ] 验证：`go test ./api/...`。

## Phase 14: 端到端验证与文档

- [ ] 编写 `docs/drl_strategy.md`：DRL 策略使用指南（配置说明、训练流程、回测方法、生产部署注意事项）。
- [ ] 创建 `models/drl/.gitkeep` 和 `models/drl/README.md`：模型文件存放说明。
- [ ] 编写端到端集成测试：使用 mock ONNX 模型，验证 config → engine → decision → validation → report 完整链路。
- [ ] 在 Makefile 中增加 DRL 相关命令：`make drl-train`、`make drl-backtest`、`make drl-export`。
- [ ] 验证全量测试：`go test ./...`（排除需要真实 ONNX Runtime 的测试使用 build tag `//go:build drl`）。
- [ ] 更新项目 README.md，在功能列表中添加 DRL 策略模式说明。
