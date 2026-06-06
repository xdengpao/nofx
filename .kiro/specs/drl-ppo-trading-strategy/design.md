# 深度强化学习（DRL-PPO）自动化交易策略 Design

## Overview

本设计为 NOFX 新增基于 PPO（Proximal Policy Optimization）的深度强化学习决策模式。当 trader 配置 `decision_mode=drl` 时，系统加载预训练的 ONNX 模型，将多维市场观测向量输入 PPO Agent，获取连续动作输出并映射为标准 `decision.Decision`。现有公共风控层继续作为安全约束保留。

系统分为两大子系统：
1. **推理子系统（Go）**：运行时加载 ONNX 模型，构建观测向量，执行推理，输出交易决策。
2. **训练子系统（Python）**：基于 OpenAI Gym 兼容环境和 Stable-Baselines3 进行 PPO 训练，输出 ONNX 模型文件。

两个子系统通过 ONNX 模型文件解耦，训练侧可独立迭代而不影响运行时。

## Design Principles

1. **公共风控优先**：熔断、账户回撤硬停、交易计划失效、保护单同步、open gate、仓位 sizing、最小名义额和 preflight 可覆盖 DRL 策略输出。
2. **策略只产出决策，不执行订单**：DRL 引擎返回 `decision.Decision` 和诊断信息，实际执行仍由 `trader.AutoTrader` 通过现有 `trader.Trader` 交易所抽象负责，不引入 CCXT 执行层。
3. **训练推理解耦**：训练使用 Python 生态（Stable-Baselines3 + PyTorch），Go 侧通过 `InferenceBackend` 接口推理；默认构建使用 mock/stub 后端不依赖系统 `libonnxruntime`，真实 ONNX Runtime 后端使用 `//go:build drl` 隔离。
4. **增量集成**：复用现有 `backtest.Runner`、`decision.Context`、`market.Data` 和公共验证管线，最小化对已有代码的侵入。
5. **确定性推理**：相同观测输入下，ONNX 模型推理输出确定性（无随机采样），保证回测可复现。
6. **可观测性**：每次推理记录完整特征、原始输出、映射动作和耗时，支持事后分析。

## Architecture

```mermaid
flowchart TD
    Config[config.json traders[].decision_mode=drl / drl_strategy] --> Normalize[Config.Validate / config.NormalizeDRLStrategy]
    Normalize --> Split[显式分流DRL / 不进入programmatic归一化]
    Split --> Manager[manager.TraderManager]
    Manager --> AT[trader.AutoTrader]

    AT --> Context[buildTradingContext]
    AT --> Mode{decision_mode}
    Mode -->|ai| AI[decision.GetFullDecision AI path]
    Mode -->|programmatic| Chanlun[strategy/chanlun.Engine]
    Mode -->|drl| DRL[strategy/drl.Engine]

    Context --> DRL
    DRL --> Prep[decision.PrepareCycleContext(MarketHistoryDepth)]
    Prep --> Market[market.GetWithHistory]
    Prep --> PublicRisk[熔断 / 账户硬停 / 交易计划 / 持仓评估]
    PublicRisk --> DRL
    Market --> DRL

    subgraph DRL Engine
        DRL --> FE[FeatureBuilder 特征工程]
        FE --> Obs[Observation Vector 观测向量]
        Obs --> Backend[InferenceBackend]
        Backend --> RawAction[Raw Action ∈ [-1, +1]]
        RawAction --> Mapper[ActionMapper 动作映射]
        Mapper --> SL_TP[StopLoss/TakeProfit 计算]
    end

    SL_TP --> Decisions[[]decision.Decision]
    Decisions --> ValidateOpen[ValidateStrategyDecisions open/add]
    Decisions --> ValidateRisk[ValidateRiskReducingStrategyDecisions close/risk-reducing]
    ValidateOpen --> Merge[decision.MergePublicAndStrategyDecisions]
    ValidateRisk --> Merge
    AI --> Merge
    Chanlun --> Merge
    Merge --> Execute[executeDecisionWithRecord]
    Execute --> Exchange[trader.Trader implementations]

    DRL --> Diag[StrategyDiagnostics]
    Diag --> Logs[decision_logs]
    Logs --> API[/api/strategy/drl/*?trader_id=...]

    subgraph Training Subsystem (Python)
        HistDB[(历史数据 SQLite)] --> Env[TradingEnv Gym]
        Env --> PPO_Train[PPO Agent Stable-Baselines3]
        PPO_Train --> Export[ONNX Export]
        Export --> ModelFile[models/drl/*.onnx]
    end

    ModelFile --> ONNX
```

## Component Design

### 1. Configuration

#### Files

- `config/config.go` — 新增 `DRLStrategyConfig` 结构
- `config/programmatic.go` — 注册 `drl` 到 `normalizeDecisionMode`，并确保 programmatic 归一化显式跳过 DRL trader
- `config/config_test.go` — DRL 配置校验测试
- `config.json.example` — DRL trader 示例

#### Config Shape

```go
// DRLStrategyConfig 深度强化学习策略配置
type DRLStrategyConfig struct {
    // 模型配置
    ModelPath          string `json:"model_path"`                     // ONNX 模型文件路径（必须）
    ModelVersion       string `json:"model_version,omitempty"`        // 模型版本标记
    
    // 观测空间配置
    ObservationWindow  int      `json:"observation_window,omitempty"`  // 观测窗口长度，默认 60
    Timeframe          string   `json:"timeframe,omitempty"`           // 主时间框架，默认 "4h"
    Symbols            []string `json:"symbols,omitempty"`             // 交易币种列表
    Features           DRLFeatureConfig `json:"features,omitempty"`    // 特征工程配置
    
    // 动作空间配置
    ActionThreshold    float64 `json:"action_threshold,omitempty"`    // 动作阈值，默认 0.1
    MaxPositionPct     float64 `json:"max_position_pct,omitempty"`    // 最大仓位比例，默认 0.3 (30%)
    DefaultLeverage    int     `json:"default_leverage,omitempty"`    // 默认杠杆，默认 5
    
    // 风控配置
    StopLossATRMult    float64 `json:"stop_loss_atr_mult,omitempty"`  // 止损 ATR 倍数，默认 2.0
    TakeProfitATRMult  float64 `json:"take_profit_atr_mult,omitempty"` // 止盈 ATR 倍数，默认 3.0
    MaxDrawdownPct     float64 `json:"max_drawdown_pct,omitempty"`    // 最大回撤阈值，默认 20%
    
    // 模型生命周期
    AutoRetrain        bool   `json:"auto_retrain,omitempty"`         // 是否自动重训练
    RetrainIntervalH   int    `json:"retrain_interval_hours,omitempty"` // 重训练间隔（小时），默认 168 (7天)
    ValidationMinDA    float64 `json:"validation_min_da,omitempty"`   // 验证最低方向准确性，默认 0.55
    
    // 回测增强配置
    MonteCarloEnabled  bool    `json:"monte_carlo_enabled,omitempty"` // 启用蒙特卡洛模拟
    MonteCarloPaths    int     `json:"monte_carlo_paths,omitempty"`   // 模拟路径数，默认 2000
    StressTestEnabled  bool    `json:"stress_test_enabled,omitempty"` // 启用波动率压力测试
    StressTestDelta    float64 `json:"stress_test_delta,omitempty"`   // 波动冲击因子，默认 0.3
    StablecoinHedge    bool    `json:"stablecoin_hedge,omitempty"`    // 启用稳定币避险模拟
    StablecoinRatio    float64 `json:"stablecoin_ratio,omitempty"`    // 稳定币比例，默认 0.3
    
    // 预测增强（预留）
    PredictionEnhancement bool `json:"prediction_enhancement,omitempty"` // 启用外部预测增强
}

// DRLFeatureConfig 特征工程配置
type DRLFeatureConfig struct {
    IncludeMACD       bool `json:"include_macd,omitempty"`       // 默认 true
    IncludeEMA        bool `json:"include_ema,omitempty"`        // 默认 true
    IncludeRSI        bool `json:"include_rsi,omitempty"`        // 默认 true
    IncludeATR        bool `json:"include_atr,omitempty"`        // 默认 true
    IncludeCCI        bool `json:"include_cci,omitempty"`        // 默认 true
    IncludeBollinger  bool `json:"include_bollinger,omitempty"`  // 默认 true
    EMAShortPeriod    int  `json:"ema_short_period,omitempty"`   // 默认 12
    EMALongPeriod     int  `json:"ema_long_period,omitempty"`    // 默认 26
    RSIPeriod         int  `json:"rsi_period,omitempty"`         // 默认 14
    ATRPeriod         int  `json:"atr_period,omitempty"`         // 默认 14
    CCIPeriod         int  `json:"cci_period,omitempty"`         // 默认 20
    BollingerPeriod   int  `json:"bollinger_period,omitempty"`   // 默认 20
    BollingerStdDev   float64 `json:"bollinger_std_dev,omitempty"` // 默认 2.0
}
```

#### TraderConfig 扩展

```go
type TraderConfig struct {
    // existing fields...
    DRLStrategy DRLStrategyConfig `json:"drl_strategy,omitempty"`
}
```

#### 配置归一化流程

`Config.Validate()` 是 `config.LoadConfig()` 的早期校验入口，因此 DRL 模式必须在配置阶段完成显式分流：

1. `normalizeDecisionMode()` 返回 `DecisionModeDRL`。
2. `Config.Validate()` 或 `NormalizeDRLStrategy()` 在 `decision_mode=drl` 时归一化 `DRLStrategy`，校验 `model_path`、`timeframe`、`observation_window`、`action_threshold`、`max_position_pct` 等字段。
3. `decision_mode=drl` 时运行时 `AIModel` 标识设置为 `drl`，并跳过 Qwen/DeepSeek/custom AI key 校验。
4. `NormalizeProgrammaticStrategies()` 对 `DecisionModeDRL` 返回仅包含 `DecisionMode: "drl"` 的占位 profile 或由新的统一策略 profile 显式分流，不能调用 `normalizeProgrammaticStrategyConfig()`，不能把 DRL 改写成 `programmatic`。
5. `manager.TraderManager` 将 `config.TraderConfig.DRLStrategy` 透传到 `trader.AutoTraderConfig.DRLStrategyConfig`。

### 2. DRL Strategy Engine（Go 推理侧）

#### Package: `strategy/drl`

#### Files

- `strategy/drl/engine.go` — 主引擎，实现 `GetFullDecision(ctx)`
- `strategy/drl/feature_builder.go` — 特征工程与观测向量构建
- `strategy/drl/action_mapper.go` — 动作映射与决策生成
- `strategy/drl/model.go` — ONNX 模型加载与推理封装
- `strategy/drl/risk.go` — ATR 基止损/止盈计算
- `strategy/drl/diagnostics.go` — 诊断信息生成
- `strategy/drl/types.go` — 内部类型定义
- `strategy/drl/engine_test.go` — 单元测试

#### Engine 接口

```go
package drl

import "nofx/decision"

// Engine DRL 策略引擎
type Engine struct {
    Config      *DRLEngineConfig
    Backend     InferenceBackend
    Features    *FeatureBuilder
    Mapper      *ActionMapper
    Clock       func() time.Time
    Diagnostics *DiagnosticsCollector
}

// NewEngine 创建 DRL 引擎
func NewEngine(cfg *DRLEngineConfig) (*Engine, error)

// GetFullDecision 生成完整交易决策
func (e *Engine) GetFullDecision(ctx *decision.Context) (*decision.FullDecision, error)

// Close 释放模型资源
func (e *Engine) Close() error
```

#### 推理流程

```
GetFullDecision(ctx):
  1. 根据 cfg.Symbols、当前持仓和候选币生成本周期 symbol universe
  2. 调用 decision.PrepareCycleContext(ctx, CyclePreparationOptions{MarketSymbols, MarketHistoryDepth, ClosedKlinesOnly:true, AllowRiskReducingOnHalt:true})
  3. 从 ctx.MarketDataMap 获取各 symbol 的 K 线数据
  4. 对每个 symbol:
     a. FeatureBuilder.Build(klines, account) → observation []float32
     b. Backend.Infer(observation) → rawAction float32
     c. ActionMapper.Map(rawAction, currentPosition, ctx) → []decision.Decision
  5. 将 open/add 动作交给 decision.ValidateStrategyDecisions()
  6. 将 close_long/close_short/partial_close/update_stop_loss 等风险降低动作交给 decision.ValidateRiskReducingStrategyDecisions()
  7. 使用 decision.MergePublicAndStrategyDecisionsWithContext() 合并公共持仓管理和 DRL 策略输出
  8. 填充 DecisionMode/StrategyName/StrategyVersion/ConfigHash、StrategyDiagnostics 和 CoTTrace
  9. 返回 *decision.FullDecision
```

### 3. Feature Builder（特征工程）

#### 观测向量结构

每个时间步包含以下特征（每个 symbol 独立）：

| 特征组 | 维度 | 说明 |
|--------|------|------|
| OHLCV | 5 | 标准化开高低收量 |
| MACD | 3 | MACD 线、信号线、柱状图 |
| EMA | 2 | 短期 EMA、长期 EMA（标准化） |
| RSI | 1 | RSI 值 / 100 |
| ATR | 1 | ATR / 收盘价 |
| CCI | 1 | CCI / 200（截断到 [-1, 1]） |
| Bollinger | 3 | 上轨、中轨、下轨（标准化） |
| **合计/时间步** | **16** | |

账户状态特征（全局，不随时间步变化）：

| 特征 | 维度 | 说明 |
|------|------|------|
| 仓位比例 | 1 | 当前仓位 / 最大仓位，带方向符号 |
| 未实现盈亏比例 | 1 | unrealizedPnL / equity |
| 可用保证金比例 | 1 | availableBalance / totalEquity |
| **合计** | **3** | |

**总观测维度** = `observation_window × 16 + 3` = 默认 `60 × 16 + 3 = 963`

#### 标准化方法

```go
// ZScoreNormalizer 滚动窗口 Z-Score 标准化器
type ZScoreNormalizer struct {
    WindowSize int
    means      []float64
    stds       []float64
}

func (n *ZScoreNormalizer) Normalize(raw []float64) []float32 {
    // z = (x - mean) / (std + epsilon)
    // mean 和 std 基于最近 WindowSize 个时间步计算
}
```

### 4. Inference Backend / ONNX Model Wrapper

#### 默认构建策略

Go 侧先定义推理接口并提供 mock/stub 后端，确保默认 `go test ./...` 和非 DRL 模式构建不需要系统安装 `libonnxruntime`。真实 ONNX Runtime 后端放在带 `//go:build drl` 的文件中，只有显式启用 DRL build tag 时才编译。

```go
// InferenceBackend 是 DRL 推理后端抽象。默认构建使用 mock/stub，真实 ONNX 后端使用 //go:build drl。
type InferenceBackend interface {
    Load(modelPath string, inputShape []int64) error
    Infer(observation []float32) (float32, error)
    Close() error
}

// StubBackend 默认构建后端：用于配置、动作映射、诊断和回测测试，不链接 libonnxruntime。
type StubBackend struct {
    FixedOutput float32
}

// ONNXRuntimeBackend ONNX Runtime 后端（//go:build drl）
type ONNXRuntimeBackend struct {
    session    *ort.Session
    inputName  string
    outputName string
    inputShape []int64
    mu         sync.Mutex // 推理线程安全
}
```

#### 模型输入输出规范

- **输入**: `float32[1, obs_dim]` — 扁平化观测向量
- **输出**: `float32[1, 1]` — 连续动作值 ∈ [-1, +1]（tanh 激活）

### 5. Action Mapper（动作映射）

```go
// ActionMapper 将原始动作映射为交易决策
type ActionMapper struct {
    Threshold      float64
    MaxPositionPct float64
    DefaultLeverage int
}

// Map 映射动作到决策列表
func (m *ActionMapper) Map(
    rawAction float32,
    symbol string,
    currentPos *decision.PositionInfo,
    account decision.AccountInfo,
    atr float64,
    cfg *DRLEngineConfig,
) []decision.Decision
```

#### 映射逻辑

```
IF |rawAction| <= threshold:
    → wait

IF rawAction > threshold:
    IF 有空头持仓:
        → close_short + open_long（如果 rawAction 仍超阈值）
    ELIF 无持仓:
        → open_long, size = rawAction * maxPositionPct * equity
    ELIF 有多头持仓:
        → hold（已有同向仓位）

IF rawAction < -threshold:
    IF 有多头持仓:
        → close_long + open_short（如果 |rawAction| 仍超阈值）
    ELIF 无持仓:
        → open_short, size = |rawAction| * maxPositionPct * equity
    ELIF 有空头持仓:
        → hold（已有同向仓位）

止损 = entryPrice ∓ ATR * stopLossATRMult
止盈 = entryPrice ± ATR * takeProfitATRMult
Confidence = int(|rawAction| * 100)
```

#### 风控验证拆分

DRL 输出在 engine 内完成公共验证后才能返回给 `AutoTrader`：

- `open_long`、`open_short`、`add_long`、`add_short` 使用 `decision.ValidateStrategyDecisions(ctx, openLike, StrategyValidationOptions{Source:"drl"})`，继续复用 open gate、仓位 sizing、最小下单额、相关性和最终持仓限制。
- `close_long`、`close_short`、`partial_close`、`update_stop_loss` 等风险降低动作使用 `decision.ValidateRiskReducingStrategyDecisions(ctx, riskReducing, RiskReducingValidationOptions{Source:"drl"})`，校验已有持仓、方向和动作安全性。
- 验证后的 DRL 输出与 `PrepareCycleContext()` 产生的公共持仓管理动作通过 `decision.MergePublicAndStrategyDecisionsWithContext(ctx, prep.PositionDecisions, validDRL)` 合并，公共风险降低动作优先。

### 6. 回测集成

#### 与现有 Runner 集成

```go
// backtest/runner.go 扩展
func (r *Runner) Run(ctx context.Context) (RunResult, error) {
    // ...existing setup...
    
    var strategyEngine interface {
        GetFullDecision(*decision.Context) (*decision.FullDecision, error)
    }
    
    switch r.Config.Strategy.DecisionMode {
    case "programmatic", "chanlun_v2":
        engine, err := chanlun.NewEngine(policy)
        strategyEngine = engine
    case "drl":
        engine, err := drl.NewEngine(drlConfig)
        strategyEngine = engine
    }
    
    // ...existing loop using strategyEngine.GetFullDecision(decisionCtx)...
}
```

#### 扩展 Report

```go
// DRLBacktestMetrics DRL 策略回测扩展指标
type DRLBacktestMetrics struct {
    SharpeRatio        float64            `json:"sharpe_ratio"`
    SortinoRatio       float64            `json:"sortino_ratio"`
    AnnualizedReturn   float64            `json:"annualized_return"`
    DirectionalAccuracy float64           `json:"directional_accuracy"`
    AvgInferenceTimeMs float64            `json:"avg_inference_time_ms"`
    ModelVersion       string             `json:"model_version"`
    
    // 蒙特卡洛分析
    MonteCarlo         *MonteCarloResult  `json:"monte_carlo,omitempty"`
    
    // 压力测试
    StressTest         *StressTestResult  `json:"stress_test,omitempty"`
    
    // 稳定币避险对比
    HedgeComparison    *HedgeComparison   `json:"hedge_comparison,omitempty"`
}

// MonteCarloResult 蒙特卡洛模拟结果
type MonteCarloResult struct {
    PathCount          int     `json:"path_count"`
    VaR95              float64 `json:"var_95"`          // 95% VaR
    VaR99              float64 `json:"var_99"`          // 99% VaR
    ExpectedShortfall  float64 `json:"expected_shortfall"` // CVaR
    MedianReturn       float64 `json:"median_return"`
    P5Return           float64 `json:"p5_return"`       // 5th percentile
    P95Return          float64 `json:"p95_return"`      // 95th percentile
    LossProbability    float64 `json:"loss_probability"` // 亏损概率
}

// StressTestResult 波动率压力测试结果
type StressTestResult struct {
    ShockFactor        float64 `json:"shock_factor"`
    MaxDrawdownStress  float64 `json:"max_drawdown_stress"`
    ReturnStress       float64 `json:"return_stress"`
    SharpeStress       float64 `json:"sharpe_stress"`
    SurvivalRate       float64 `json:"survival_rate"`   // 未触发强平的比例
}

// HedgeComparison 稳定币避险对比
type HedgeComparison struct {
    PureStrategyReturn    float64 `json:"pure_strategy_return"`
    PureStrategyDrawdown  float64 `json:"pure_strategy_drawdown"`
    HedgedReturn          float64 `json:"hedged_return"`
    HedgedDrawdown        float64 `json:"hedged_drawdown"`
    StablecoinRatio       float64 `json:"stablecoin_ratio"`
}
```

### 7. 训练子系统（Python）

#### 目录结构

```
training/drl/
├── env/
│   ├── __init__.py
│   ├── trading_env.py      # OpenAI Gym 交易环境
│   ├── features.py         # 特征工程（与 Go 侧对齐）
│   └── rewards.py          # 奖励函数
├── agents/
│   ├── __init__.py
│   ├── ppo_agent.py        # PPO 训练封装
│   └── export.py           # ONNX 导出
├── data/
│   ├── __init__.py
│   ├── loader.py           # 从 SQLite 加载历史数据
│   └── preprocessor.py     # 数据预处理
├── backtest/
│   ├── __init__.py
│   ├── monte_carlo.py      # 蒙特卡洛模拟
│   ├── stress_test.py      # 波动率压力测试
│   └── metrics.py          # 指标计算
├── scripts/
│   ├── train.py            # 训练入口脚本
│   ├── evaluate.py         # 评估脚本
│   └── export_model.py     # 模型导出脚本
├── requirements.txt
└── README.md
```

#### 训练环境设计

> **手续费与滑点对齐说明**：Python 训练环境的交易成本参数必须与 Go 侧 `backtest.PaperBroker` 保持一致，以确保训练-回测-实盘三者的行为一致性。当前对齐关系如下：
> 
> | 参数 | Python 训练环境 | Go PaperBroker | 说明 |
> |------|----------------|----------------|------|
> | Taker 手续费 | `taker_fee=0.0005` (5bps) | `DefaultTakerFeeBPS=5` (5bps) | ✅ 一致 |
> | Maker 手续费 | `maker_fee=0.0002` (2bps) | `DefaultMakerFeeBPS=2` (2bps) | ✅ 一致 |
> | 滑点 | `slippage=0.0003` (3bps) | `DefaultSlippageBPS=3` (3bps) | ✅ 一致 |
> | 初始资金 | `initial_balance=10000` | `DefaultInitialEquity=10_000` | ✅ 一致 |
> 
> 如果回测配置中自定义了 `costs` 字段，训练脚本也应使用相同数值。训练脚本通过 `--taker-fee`、`--maker-fee`、`--slippage` CLI 参数支持覆盖默认值。

```python
class CryptoTradingEnv(gym.Env):
    """
    PPO 交易训练环境
    
    Observation Space: Box(low=-inf, high=inf, shape=(obs_dim,))
    Action Space: Box(low=-1, high=1, shape=(1,))
    """
    
    def __init__(self, config):
        self.observation_window = config.get("observation_window", 60)
        self.initial_balance = config.get("initial_balance", 10000)
        self.taker_fee = config.get("taker_fee", 0.0005)
        self.slippage = config.get("slippage", 0.0003)
        self.max_drawdown = config.get("max_drawdown", 0.20)
        
        obs_dim = self.observation_window * 16 + 3
        self.observation_space = spaces.Box(
            low=-np.inf, high=np.inf, shape=(obs_dim,), dtype=np.float32
        )
        self.action_space = spaces.Box(
            low=-1.0, high=1.0, shape=(1,), dtype=np.float32
        )
    
    def step(self, action):
        # 1. 执行交易动作（含手续费和滑点）
        # 2. 推进一根 K 线
        # 3. 计算奖励 = (new_gross_value - old_gross_value) / old_gross_value
        # 4. 检查终止条件（回撤超限、数据耗尽）
        # 5. 返回 obs, reward, done, truncated, info
        
    def reset(self, seed=None, options=None):
        # 重置环境到随机起始点（确保有足够历史计算指标）
        
    def _build_observation(self):
        # 构建观测向量（与 Go 侧 FeatureBuilder 对齐）
```

#### 奖励函数设计

```python
def compute_reward(old_value, new_value, action, position):
    """
    核心奖励：资产总价值变化率
    附加惩罚：
    - 过度交易惩罚（频繁切换方向）
    - 持仓过夜风险惩罚（可选）
    - 回撤惩罚（加速学习风险规避）
    """
    base_reward = (new_value - old_value) / old_value
    
    # 方向切换惩罚
    direction_change_penalty = -0.001 if direction_changed else 0
    
    # 回撤惩罚
    drawdown = (peak_value - new_value) / peak_value
    drawdown_penalty = -drawdown * 0.5 if drawdown > 0.05 else 0
    
    return base_reward + direction_change_penalty + drawdown_penalty
```

#### PPO 训练配置

```python
PPO_HYPERPARAMS = {
    "learning_rate": 3e-4,
    "n_steps": 2048,
    "batch_size": 64,
    "n_epochs": 10,
    "gamma": 0.99,
    "gae_lambda": 0.95,
    "clip_range": 0.2,
    "ent_coef": 0.01,
    "vf_coef": 0.5,
    "max_grad_norm": 0.5,
    "policy_kwargs": {
        "net_arch": [256, 256],  # Actor-Critic 共享网络
        "activation_fn": "tanh",
    },
}
```

#### 滚动窗口训练

```python
class RollingWindowTrainer:
    """
    滚动窗口训练器：
    - 训练窗口：最近 N 天（默认 90 天）
    - 验证窗口：训练窗口后 M 天（默认 14 天）
    - 步进周期：每 S 天重新训练（默认 7 天）
    """
    
    def train_rolling(self, data, window_size=90, val_size=14, step=7):
        for start in range(0, len(data) - window_size - val_size, step):
            train_data = data[start : start + window_size]
            val_data = data[start + window_size : start + window_size + val_size]
            
            model = self.train_single_window(train_data)
            metrics = self.validate(model, val_data)
            
            if metrics["directional_accuracy"] >= self.min_da:
                self.save_model(model, start + window_size)
```

### 8. 蒙特卡洛模拟与压力测试

#### 几何布朗运动模型

```go
// MonteCarloSimulator 蒙特卡洛模拟器
type MonteCarloSimulator struct {
    Paths     int       // 模拟路径数，默认 2000
    Steps     int       // 每条路径时间步数
    Mu        float64   // 漂移率（从历史数据估计）
    Sigma     float64   // 波动率（从历史数据估计）
    Dt        float64   // 时间步长
}

// Simulate 生成随机价格路径并评估策略
func (s *MonteCarloSimulator) Simulate(engine *Engine, initialPrice float64) *MonteCarloResult {
    // 对每条路径：
    // 1. S(t+dt) = S(t) * exp((mu - sigma²/2)*dt + sigma*sqrt(dt)*Z)
    //    其中 Z ~ N(0,1)
    // 2. 在模拟路径上运行 DRL 策略
    // 3. 收集终端资产价值
    // 4. 计算 VaR、CVaR、损失概率
}
```

#### 波动率压力测试

```go
// StressTester 波动率压力测试器
type StressTester struct {
    ShockFactor float64 // 波动冲击因子（如 0.3 = 30%）
}

// Test 在历史数据上注入波动冲击
func (t *StressTester) Test(engine *Engine, historicalData []market.Kline) *StressTestResult {
    // 对历史数据注入随机时间点的 ±ShockFactor 价格跳跃
    // 评估策略在冲击下的存活率和最大回撤
}
```

### 9. API 扩展

#### 新增端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/strategy/drl/status?trader_id={id}` | GET | 指定 DRL trader 的模型版本、推理统计 |
| `/api/strategy/drl/features?trader_id={id}` | GET | 指定 DRL trader 最近一次观测向量 |
| `/api/strategy/drl/backtest/monte-carlo?trader_id={id}` | POST | 指定 DRL trader 触发蒙特卡洛模拟 |
| `/api/strategy/drl/backtest/stress-test?trader_id={id}` | POST | 指定 DRL trader 触发压力测试 |

所有 DRL API 都必须解析 `trader_id` 并确认目标 trader 的 `decision_mode=="drl"`；若不是 DRL trader，返回 `{"error":"trader不是DRL策略模式: {trader_id}"}`。

### 10. 与 Backtest CLI 集成

```bash
# 使用 DRL 策略进行回测
go run cmd/backtest/main.go run -config backtest_drl.json

# backtest_drl.json 示例
{
  "backtest_from": "2024-01-01",
  "backtest_to": "2024-06-30",
  "symbols": ["ETHUSDT"],
  "initial_equity": 10000,
  "strategy": {
    "decision_mode": "drl",
    "drl_strategy": {
      "model_path": "models/drl/eth_ppo_v1.onnx",
      "observation_window": 60,
      "timeframe": "4h",
      "action_threshold": 0.1,
      "max_position_pct": 0.3,
      "monte_carlo_enabled": true,
      "stress_test_enabled": true
    }
  }
}
```

### 11. 模型生命周期管理

#### Package: `strategy/drl`

#### Files

- `strategy/drl/lifecycle.go` — 模型生命周期管理器主逻辑
- `strategy/drl/lifecycle_test.go` — 生命周期管理测试

#### 核心结构

```go
// ModelLifecycleManager 管理 DRL 模型的加载、验证、热更新与回滚
type ModelLifecycleManager struct {
    engine          *Engine
    config          *DRLEngineConfig
    scheduler       *RetrainScheduler
    currentBackend  InferenceBackend    // 当前活跃推理后端（原子引用）
    modelVersion    string              // 当前模型版本标记
    lastRetrainAt   time.Time           // 上次重训练时间
    lastValidateAt  time.Time           // 上次验证时间
    mu              sync.RWMutex        // 热更新读写锁
    logger          *log.Logger
}

// RetrainScheduler 重训练调度器
type RetrainScheduler struct {
    IntervalHours   int                 // 重训练间隔（小时）
    TrainScriptPath string              // Python 训练脚本路径
    DataPath        string              // 历史数据路径
    OutputDir       string              // 模型输出目录
    ticker          *time.Ticker
    stopCh          chan struct{}
}

// ModelValidationResult 模型验证结果
type ModelValidationResult struct {
    Passed              bool    `json:"passed"`
    DirectionalAccuracy float64 `json:"directional_accuracy"`
    MinRequired         float64 `json:"min_required"`
    SampleCount         int     `json:"sample_count"`
    ValidationPeriod    string  `json:"validation_period"`
    Reason              string  `json:"reason,omitempty"`
}
```

#### 热更新流程

```
自动重训练调度（按 retrain_interval_hours 周期触发）:
  1. RetrainScheduler.tick()
  2. 调用 Python 训练脚本（os/exec）:
     python training/drl/scripts/train.py \
       --data-path <historydb_path> \
       --symbol <symbol> \
       --output <models/drl/candidate_<timestamp>.onnx>
  3. 等待训练完成（超时 30 分钟）
  4. IF 训练成功:
     → 进入验证流程
  5. ELIF 训练失败:
     → 记录中文错误日志 "DRL 自动重训练失败: {error}"
     → 保留当前模型，不做任何更改
```

#### 原子切换机制

```
模型热更新验证与切换:
  1. Load(candidateModelPath) → candidateBackend
  2. IF 加载失败:
     → 记录 "候选模型加载失败: {error}"
     → 回滚：保留 currentBackend，删除候选文件
     → 返回
  3. 在最近 validation_window 的历史数据上运行验证:
     a. 构建验证集观测向量序列
     b. 使用 candidateBackend 逐步推理
     c. 计算方向准确性 DA = correct_direction_count / total_count
  4. IF DA >= validation_min_da (默认 0.55):
     → mu.Lock()
     → oldBackend := currentBackend
     → currentBackend = candidateBackend  // 原子替换引用
     → modelVersion = new_version
     → mu.Unlock()
     → oldBackend.Close()  // 释放旧后端资源
     → 记录 "模型热更新成功: v{old} → v{new}, DA={da:.4f}"
     → 备份旧模型到 models/drl/archive/
  5. ELIF DA < validation_min_da:
     → candidateBackend.Close()
     → 记录 "候选模型验证不通过: DA={da:.4f} < 阈值{min_da}, 保留当前模型 v{current}"
     → 删除候选模型文件
```

#### 回滚机制

```go
// Rollback 回滚到上一个已知可用模型版本
func (m *ModelLifecycleManager) Rollback(reason string) error {
    // 1. 从 models/drl/archive/ 中查找最近一个备份模型
    // 2. 加载备份模型
    // 3. 原子替换 currentBackend
    // 4. 记录 "模型已回滚: 原因={reason}, 恢复到 v{archive_version}"
}

// GetBackendForInference 获取当前活跃推理后端（读锁保护，推理时不阻塞）
func (m *ModelLifecycleManager) GetBackendForInference() InferenceBackend {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.currentBackend
}
```

#### 生命周期状态机

```
                    ┌──────────────────────┐
                    │   LOADED (正常运行)    │
                    │ currentBackend 活跃   │
                    └──────────┬───────────┘
                               │ retrain_interval 到达
                               ▼
                    ┌──────────────────────┐
                    │  RETRAINING (训练中)  │
                    │ currentBackend 继续服务│
                    └──────────┬───────────┘
                               │ 训练完成
                    ┌──────────┴───────────┐
                    │                      │
                    ▼                      ▼
        ┌─────────────────┐    ┌─────────────────┐
        │ VALIDATING       │    │ RETRAIN_FAILED   │
        │ 验证候选模型     │    │ 保留当前模型     │
        └────────┬────────┘    └─────────────────┘
                 │
        ┌────────┴────────┐
        │                 │
        ▼                 ▼
┌──────────────┐  ┌──────────────┐
│ UPDATED      │  │ REJECTED     │
│ 原子切换成功  │  │ 验证不通过    │
│ 旧模型归档   │  │ 保留当前模型  │
└──────────────┘  └──────────────┘
```

#### 与 Engine 的集成

```go
// Engine 扩展
type Engine struct {
    // ...existing fields...
    Lifecycle *ModelLifecycleManager  // 生命周期管理器（auto_retrain=true 时初始化）
}

// GetFullDecision 中使用生命周期管理器获取推理后端
func (e *Engine) GetFullDecision(ctx *decision.Context) (*decision.FullDecision, error) {
    // 如果启用了生命周期管理，通过 Lifecycle.GetBackendForInference() 获取后端
    // 否则直接使用 e.Backend
    backend := e.activeBackend()
    // ...推理流程...
}

func (e *Engine) activeBackend() InferenceBackend {
    if e.Lifecycle != nil {
        return e.Lifecycle.GetBackendForInference()
    }
    return e.Backend
}
```

## Data Flow

### 实时交易模式

```
每个扫描周期 (scan_interval_minutes):
  1. AutoTrader.runCycle()
  2. buildTradingContext() → decision.Context
  3. drl.Engine.GetFullDecision(ctx) 内调用 PrepareCycleContext(MarketHistoryDepth) → 公共风控前置检查 + 历史K线获取
  4. drl.Engine.GetFullDecision(ctx):
     a. 获取最近 60 根 K 线 + 指标
     b. 构建 963 维观测向量
     c. InferenceBackend 推理 → rawAction
     d. 映射为 Decision（含 SL/TP）
  5. ValidateStrategyDecisions(open/add) + ValidateRiskReducingStrategyDecisions(close/risk-reducing) → 风控过滤
  6. executeDecisionWithRecord() → 交易所执行
  7. 写入决策日志
```

### 训练模式

```
1. 从 historydb SQLite 加载历史 K 线
2. 构建 CryptoTradingEnv
3. PPO Agent 在环境中训练 N 个 episode
4. 每个 episode:
   a. env.reset() → 随机起始
   b. 循环: obs → agent.predict() → action → env.step()
   c. 收集 (obs, action, reward, done) tuples
   d. PPO update: clip surrogate loss + value loss
5. 验证方向准确性
6. 导出 ONNX 模型

```

## Dependencies

### Go 侧新增依赖

```
默认构建无新增必需依赖。
//go:build drl 后端引入 github.com/yalue/onnxruntime_go
```

#### ONNX Runtime 兼容性说明

`github.com/yalue/onnxruntime_go` 是 ONNX Runtime C API 的 Go 绑定库，只在 `//go:build drl` 文件中引用。兼容性注意事项：

| 项目 | 要求 |
|------|------|
| Go 版本 | 需要 Go 1.21+（当前项目 go.mod 声明 `go 1.25.0`，✅ 兼容） |
| ONNX Runtime | 需要系统安装 ONNX Runtime 共享库 (`libonnxruntime.so`)，版本 ≥ 1.16 |
| CGO | 需要启用 CGO（`CGO_ENABLED=1`），Linux 环境默认启用 |
| 平台 | 支持 Linux amd64/arm64、macOS、Windows |
| Docker | Dockerfile 需增加 ONNX Runtime 库安装步骤 |

**备选方案**（若 `yalue/onnxruntime_go` 出现兼容性问题）：

1. **`github.com/nicholasgasior/onnxruntime-go`**：另一个活跃的 Go ONNX Runtime 绑定，API 风格略有不同但功能等价。
2. **gRPC 推理服务**：将 ONNX 推理封装为独立 Python gRPC 微服务，Go 侧通过 RPC 调用。优点是完全消除 CGO 依赖，缺点是增加网络延迟（约 5-20ms）和运维复杂度。
3. **`github.com/owulveryck/onnx-go`**：纯 Go 实现的 ONNX 推理（无 CGO），但性能较低且算子覆盖不完整，仅作为最后手段。

**推荐实施策略**：先实现 `InferenceBackend` 接口、`StubBackend` 和测试注入点，保证默认构建不链接 `libonnxruntime`；真实推理优先使用 `yalue/onnxruntime_go`，放在 `model_onnx_drl.go` 等带 `//go:build drl` 的文件中，使后续可无侵入替换底层实现：

```go
// strategy/drl/inference.go
type InferenceBackend interface {
    Load(modelPath string, inputShape []int64) error
    Infer(observation []float32) (float32, error)
    Close() error
}

// 实现：StubBackend（默认）、ONNXRuntimeBackend（//go:build drl）、GRPCBackend（备选）
```

**Docker 部署补充**：在 `docker/Dockerfile.backend` 中需增加：

```dockerfile
# 安装 ONNX Runtime 共享库
RUN wget -q https://github.com/microsoft/onnxruntime/releases/download/v1.17.0/onnxruntime-linux-x64-1.17.0.tgz \
    && tar -xzf onnxruntime-linux-x64-1.17.0.tgz \
    && cp onnxruntime-linux-x64-1.17.0/lib/libonnxruntime.so* /usr/lib/ \
    && rm -rf onnxruntime-linux-x64-1.17.0*
```

### Python 侧依赖

```
stable-baselines3>=2.0
gymnasium>=0.29
torch>=2.0
onnx>=1.14
onnxruntime>=1.16
numpy>=1.24
pandas>=2.0
ta-lib>=0.4  # 技术指标（可选，也可手动实现）
```

## Error Handling

| 场景 | 处理 |
|------|------|
| ONNX 模型文件不存在 | 启动失败，中文错误："DRL 模型文件不存在: {path}" |
| ONNX 推理失败 | 输出 wait 决策，记录错误到诊断，不中断运行 |
| 观测数据不足 | 零填充 + `insufficient_data` 诊断标记 |
| 推理超时（>500ms）| 记录警告，仍使用结果 |
| 模型输出超出 [-1, 1] | clip 到有效范围，记录异常 |
| 自动重训练失败 | 保留当前模型，记录错误到日志 |
| 验证准确性不达标 | 拒绝新模型，保留旧版本，记录到日志 |

## Security Considerations

- 模型文件路径限制在项目目录内，不允许符号链接或相对路径逃逸。
- Python 训练脚本不持有交易所 API key，只处理历史数据。
- ONNX 模型为只读资源，推理时不修改模型状态。
- 自动重训练需要显式配置启用，默认关闭。
