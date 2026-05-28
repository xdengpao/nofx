# Chanlun V2 Short Signals Consistency Review

## Review Scope

本轮交叉验证覆盖：

- Spec: `requirements.md`、`design.md`、`tasks.md`
- Rust code: `chanlun_v2/src/signal.rs`、`segment.rs`、`kline.rs`、`center.rs`、`trend.rs`、`divergence.rs`、`analyzer.rs`
- Go code: `strategy/chanlunv2/engine.go`、`entry_timing.go`、`report.go`、`config/config.go`、相关测试
- 217 运维边界：实际服务目录为 `/home/ubuntu/appai2/nofx`，当前运行容器为 `nofx-trading` 和 `nofx-frontend`

## Verdict

交叉验证未发现 Go 路由或 RR 默认配置阻塞。Rust 侧确实缺少 `Sell2/Sell3` 产出，且 `Sell1` 仅限 `UpTrend`。Spec 可以进入实现阶段，但执行前已修正 5 个文档缺口：

1. `requirements.md` 状态仍写 `requirements 阶段`，与已有 design/tasks 不一致。
2. `requirements.md` 的 Sell2 条件未包含 `last_seg.direction == Direction::Down`，与 `design.md` 交叉验证修正冲突。
3. `design.md` 主实现片段仍是旧 Sell2 方案，缺少方向约束和 TP 保护。
4. `tasks.md` 没有要求新增 Rust 单测，只有笼统 `cargo test`，无法防止 Sell2/Sell3 误触发或 Buy2/Sell2 同时触发。
5. 部署任务未明确保留 217 本地 Aster 账户和测试资金配置。

本次只修正 spec 文档，没有修改业务代码。

## Code Facts

### Rust

- `chanlun_v2/src/signal.rs` 已定义 `SignalType::Sell1/Sell2/Sell3`，serde 名称分别为 `sell1/sell2/sell3`。
- `detect_signals()` 当前只会产出 `Sell1`、`Buy2`、`Buy3`。
- `Sell1` 当前条件为 `DivergenceType::Top && trend == TrendType::UpTrend`。
- `Segment` 已有 `direction: Direction`，`Direction` 位于 `crate::kline`。
- `Center` 已有 `zg/zd/high/low`，足够计算 Sell2/Sell3 的 SL/TP。

### Go

- `signalToDecision()` 已将 `sell*` 和 `quasi_sell*` 映射为 `open_short`。
- `signalActionForDirection("short")` 返回 `open_short`。
- `invalidStopTakeProfit()` 对 short 使用 `take_profit < current_price < stop_loss`，方向正确。
- `NormalizeChanlunV2EntryZone()` 默认包含 `sell1:2.0`、`sell2:1.5`、`sell3:1.2`。
- `multiLevelJudgment()` 对 higher timeframe 为 `up_trend` 的 short 信号会记录 countertrend suppression 并丢弃，对 `down_trend` 的 short 信号加 15 置信度。

## Requirement Cross-Check

### S1 Sell2

Status: consistent after spec correction.

Sell2 必须绑定 `Direction::Down`，否则只靠 `last_seg.high` 落在中枢内，容易在震荡段和 Buy2 条件附近产生对称噪声。需求已补充：

- `last_seg.direction == Direction::Down`
- `ZD < last_seg.high < ZG`
- `current_price < last_seg.high`
- `take_profit` 使用中枢下方等距目标，并加 `current_price * 0.9` 保护
- 不允许同一最后线段同时产出 `buy2` 和 `sell2`

### S2 Sell3

Status: consistent after spec correction.

Sell3 只要求 `last_seg.high < center.zd` 还不够。若当前价格已回到 ZD 上方，继续产出 `sell3` 会在 Go entry timing 中变成无效父结构噪声。需求已补充：

- `last_seg.direction == Direction::Down`
- `last_seg.high < center.zd`
- `current_price < center.zd`

### S3 Sell1

Status: consistent.

`TrendType::Consolidation` 已存在，`DivergenceType::Top` 已存在。实现时只需把 match guard 扩展为 `UpTrend || Consolidation`，并对 consolidation 置信度减 15 后 clamp 到 `[0,100]`。

### S4 Go Compatibility

Status: already supported by current code.

Go 层无需为基础路由修改代码，但仍需要跑聚焦测试：

- `go test ./config ./strategy/chanlunv2`
- 如触及通用 decision 逻辑，再补跑 `go test ./decision ./trader`

## Design Cross-Check

### Import Boundary

`signal.rs` 当前没有引入 `Direction`。若按修正后的 Sell2/Sell3 条件实现，需要新增：

```rust
use crate::kline::Direction;
```

或使用全限定名 `crate::kline::Direction::Down`。任务已补充这一点，避免编译失败。

### Test Boundary

`signal.rs` 当前没有测试模块。仅执行 `cargo test` 不足以证明新增信号正确，任务已补充 Rust 单测：

- `sell2` 能产出 `short`
- `sell3` 能产出 `short`
- `sell1` 在 `Consolidation` 中可产出且扣减置信度
- 同一最后线段不同时产出 `buy2` 和 `sell2`
- 当前价格回到 ZD 上方时不产出 `sell3`

### Deployment Boundary

217 实际目录为 `/home/ubuntu/appai2/nofx`，不是早期评估记录中的 `appai3`。部署任务必须保留：

- 217 本地 Aster API 凭证
- `capital_allocation.enabled = true`
- `capital_allocation.allocated_balance = 100`

## Residual Risks

- `open_short >= 1` 是活体市场结果，受 open gate、RR、仓位、交易所最小名义额、行情环境影响。实现验收应先以“sell 信号产出、entry timing 可评估、open_short 或结构化拒绝记录出现”为确定性验证，再观察 24h 实盘是否达到目标。
- 清空 Docker build cache 后，217 下一次构建会较慢，这是已知运维影响。

## Validation Checklist

- `cd chanlun_v2 && cargo test`
- `go test ./config ./strategy/chanlunv2`
- `docker compose build && docker compose up -d` on 217
- 等待至少一个 1h K 线闭合后检查 `缠论V2策略分析摘要` 是否出现 `sell1/sell2/sell3`
- 检查是否产生 `open_short` 决策、或可解释的 open gate/RR/entry zone 结构化拒绝
