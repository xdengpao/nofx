# 缠论 V2 策略检查交叉验证

## 结论

Requirements、Design、Tasks 与当前实现已对齐。交叉验证中发现 1 个契约偏差：`/api/strategy/symbols` 在尚无策略周期时可能返回 `symbols: null`，与“空标的列表”要求不一致；已修复为稳定返回 `[]`，并补充回归测试。

## Requirements Traceability

| Requirement | 实现位置 | 测试/验证 | 结论 |
| --- | --- | --- | --- |
| 1. v2 trader 显示策略检查入口 | `web/src/App.tsx` 使用 `isStrategyDecisionMode()` 同时识别 `programmatic` 与 `chanlun_v2` | `cd web && npm run build` | 通过 |
| 2. v2 引擎暴露策略标的池 | `strategy/chanlunv2.Engine.SymbolUniverse()`；`trader.AutoTrader.GetStrategySymbols()` 分支 v2；`api.handleStrategySymbols()` nil slice 归一化为 `[]` | `TestChanlunV2StrategySymbolsEmptyListBeforeCycle` | 通过 |
| 3. v2 引擎返回信号报告 | `strategy/chanlunv2/report.go` 生成 v1 兼容 `SignalReport`、`ChanlunSignal`、`SignalMarker`，支持 `view/layers/statuses/from/to/limit` | `TestLatestSignalsWithOptionsReturnsV2ReportMarkers`、`TestLatestSignalsWithOptionsFiltersV2Markers`、`TestChanlunV2StrategySignals*` | 通过 |
| 4. v2 K 线深度使用 v2 配置 | `manager.AddTraderWithPolicies()` 传递 `cfg.ChanlunV2Strategy`；`AutoTrader.ResolveMarketKlineLimit()` 读取 `ChanlunV2StrategyConfig.HistoryDepth`，支持 query 覆盖和上限截断 | `TestMarketKlinesUsesChanlunV2HistoryDepthWhenLimitMissing`、`TestMarketKlinesCapsChanlunV2HistoryDepth` | 通过 |
| 5. 保持 v1 兼容 | v1 `programmatic` 分支保留；通用 symbols 空数组归一化向前兼容前端；v2 空报告不触发 Rust FFI | 既有 v1 strategy/API 测试继续通过 | 通过 |

## Design Traceability

- `chanlunv2.Engine` 已增加 mutex 保护的 `latestSignals`、`symbolUniverse`、`configHash`。
- v2 报告只复用 `strategy/chanlun` DTO 类型，不复用 v1 状态机。
- `GetFullDecision()` 会缓存标的池，并为每个已分析 symbol 写入空报告或信号报告。
- v2 决策增加 `StrategyMode`、`StrategyName`、`StrategyVersion`、`ConfigHash`、`SignalID`、`SignalTimeframe` 和 `StrategyMetadata`。
- API 路径未新增，仍为 `/api/strategy/symbols`、`/api/strategy/signals`、`/api/market/klines`。
- 前端复用现有 `StrategyCandlestickChart`、信号筛选和诊断布局。

## 修复项

1. `api.handleStrategySymbols()` 将 nil symbols 归一化为空数组，满足“尚无策略周期结果时返回空列表而不是错误/null”。
2. 增加 v2 symbols 空列表测试。
3. 增加 v2 `history_depth` 超过 `MaxMarketKlineLimit` 时的截断测试。

## Validation

- `CGO_ENABLED=0 go test ./api ./strategy/chanlunv2 ./manager ./trader`
- `CGO_ENABLED=0 go build ./...`
- `cd web && npm run build`

说明：前端构建仍有 browserslist 数据过期和 chunk size 的提示，为既有构建提示，不影响本功能验证。
