# DRL PPO 策略使用指南

本文档说明 NOFX 的 `decision_mode=drl` 策略模式。DRL 运行时只负责生成标准 `decision.Decision`，订单执行仍走现有 `trader.AutoTrader`、公共风控和交易所抽象。

## 配置

在 `config.json` 的 trader 中设置：

```json
{
  "id": "binance_drl_ppo",
  "name": "Binance DRL PPO Trader",
  "enabled": false,
  "decision_mode": "drl",
  "exchange": "binance",
  "binance_api_key": "your_binance_api_key",
  "binance_secret_key": "your_binance_secret_key",
  "initial_balance": 1000,
  "scan_interval_minutes": 3,
  "drl_strategy": {
    "model_path": "models/drl/ppo_v1.onnx",
    "model_version": "ppo_v1",
    "observation_window": 60,
    "timeframe": "4h",
    "symbols": ["BTCUSDT", "ETHUSDT"],
    "action_threshold": 0.1,
    "max_position_pct": 0.3,
    "default_leverage": 5,
    "stop_loss_atr_mult": 2.0,
    "take_profit_atr_mult": 3.0
  }
}
```

关键约束：

- `model_path` 必须非空；默认 stub 后端也会在启动时检查文件存在。
- `observation_window` 范围为 `[10, 200]`。
- `timeframe` 支持 `3m`、`15m`、`1h`、`4h`。
- `action_threshold` 范围为 `(0, 1)`。
- `max_position_pct` 范围为 `(0, 1]`。

## 运行时流程

DRL trader 启动后：

1. `Config.Validate()` 将 `AIModel` 设置为 `drl`，跳过 Qwen/DeepSeek/custom key 校验。
2. `manager.TraderManager` 透传 `drl_strategy` 到 `trader.AutoTrader`。
3. `AutoTrader` 初始化 `strategy/drl.Engine`，不创建 `mcp.Client`。
4. 每个周期由 DRL engine 内部调用 `decision.PrepareCycleContext()`，使用 `MarketHistoryDepth` 拉取闭合 K 线。
5. FeatureBuilder 生成观测向量，InferenceBackend 输出 `raw_action ∈ [-1, 1]`。
6. ActionMapper 将动作映射为 `open_long`、`open_short`、`close_*`、`hold` 或 `wait`。
7. open/add 动作走 `decision.ValidateStrategyDecisions()`；close/risk-reducing 动作走 `decision.ValidateRiskReducingStrategyDecisions()`。
8. 最终决策通过现有 merge、排序、执行、日志链路处理。

## 推理后端

默认构建使用 `StubBackend`，不链接系统 `libonnxruntime`。真实 ONNX Runtime 后端必须放在 `//go:build drl` 文件中，并通过显式 build tag 编译：

```bash
go test ./strategy/drl/...
go test -tags drl ./strategy/drl/...
```

第二条命令需要本机已安装并可链接 ONNX Runtime。

## 回测

DRL 回测配置位于 backtest 配置的 `strategy` 块：

```json
{
  "strategy": {
    "decision_mode": "drl",
    "drl_strategy": {
      "model_path": "models/drl/ppo_v1.onnx",
      "model_version": "ppo_v1",
      "observation_window": 60,
      "timeframe": "4h",
      "symbols": ["BTCUSDT"]
    }
  }
}
```

当前默认回测使用 deterministic stub 推理，适合验证 config → engine → decision → validation → report 链路。真实模型回测应在 ONNX 后端完成后使用 `-tags drl` 环境验证。

## API

查询 DRL trader 状态：

```bash
curl "http://localhost:8080/api/strategy/drl/status?trader_id=binance_drl_ppo"
```

查询最近一次特征快照：

```bash
curl "http://localhost:8080/api/strategy/drl/features?trader_id=binance_drl_ppo"
```

非 DRL trader 会返回：

```json
{"error":"trader不是DRL策略模式: trader_id"}
```

## 生产注意事项

- 先用小额账户或 paper/backtest 验证模型输出。
- 确认模型文件版本、训练数据时间范围和实盘交易标的一致。
- 保留公共风控，不要绕过 `decision` 校验直接执行 DRL 输出。
- 默认 stub 后端只用于链路验证，不代表真实 PPO 推理。
