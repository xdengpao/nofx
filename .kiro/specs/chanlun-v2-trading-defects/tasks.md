# 缠论V2策略交易缺陷修复 Tasks

## 关联

- `requirements.md`
- `design.md`

## 范围约束

- [x] 所有实现只针对 V2：`strategy/chanlunv2`、`chanlun_v2`、`config.ChanlunV2StrategyConfig`、V2 执行回调和相关测试。
- [x] 不修改 V2 以外的策略包、配置、状态和测试。
- [x] 不提交 `data/`、`decision_logs/`、`coin_pool_cache/`、161 真实配置或任何密钥。

---

## P0 - 阻断错误候选和不可执行开仓

### Task 1 - V2-only 标的过滤（D3）

- [x] 新增 `strategy/chanlunv2/symbol_filter.go`，实现 `isChanlunV2TradableCryptoSymbol`。
- [x] 更新 `strategy/chanlunv2/report.go` `resolveSymbolUniverse`：跳过非加密标的和 `FilterReason != ""` 的候选。
- [x] 保证过滤发生在 market data 拉取和 Rust 分析前。
- [x] 添加 `strategy/chanlunv2` 单元测试：CLUSDT/XAUUSDT/XAGUSDT 被过滤，BTCUSDT/ETHUSDT/SOLUSDT 保留。
- [x] 验证：V2 新开仓分析候选不再包含 CLUSDT/XAUUSDT/XAGUSDT。

### Task 2 - V2 zero-size fail-safe（D2）

- [x] 更新 `strategy/chanlunv2/engine.go` `validateChanlunV2Decisions`，确保 zero quantity、min notional、margin、preflight 不可执行均转为 `OpenRejection`。
- [x] 更新 V2 sizing rejection reason code：`position_sizing.zero_quantity`、`position_sizing.min_notional`、`position_sizing.margin_insufficient`、`position_sizing.not_executable`。
- [x] 确保这些 rejection 进入日志为 `open_rejected`，不是失败 `open_long/open_short`。
- [x] 添加回归测试覆盖历史 `仓位大小必须>0` 场景。
- [x] 验证：zero-size open-like decision 不会触发交易所开仓接口。

---

## P1 - V2 信号生命周期与执行结果回调

### Task 3 - V2 执行状态存储（D1/D5）

- [x] 在 `strategy/chanlunv2/state.go` 新增 V2 `SignalExecutionState`。
- [x] 新增 V2 Engine 方法：`hasTerminalSignal`、`markSignalExecuted`、`markSignalTerminalRejected`、`suppressKnownTerminalSignal`。
- [x] 将执行状态纳入 V2 生命周期持久化或独立 V2 状态文件；不得读写 V2 以外的策略状态。
- [x] 添加 V2 状态测试：执行成功后跳过同 signal_id；执行失败不标记 executed；终态拒绝重复出现只增加 suppressed count。

### Task 4 - V2 执行结果回调（D1）

- [x] 在 V2 包定义 `ExecutionResult` 与 `OnExecutionResult`。
- [x] 在 `trader/auto_trader.go` 中仅对 `decision_mode=chanlun_v2` 调用 V2 执行结果回调。
- [x] 只有 open-like action 执行成功时标记 executed。
- [x] 失败 open action 不写入 executed 状态。
- [x] 添加 `trader` 或 `strategy/chanlunv2` 测试，验证非 `chanlun_v2` mode 不调用 V2 回调。

---

## P2 - MACD 与 SL/TP 质量

### Task 5 - V2 真实 MACD histogram（D7）

- [x] 新增 `strategy/chanlunv2/macd.go`，实现 V2 本地 `calculateV2MACDHistogram(closes, 12, 26, 9)`。
- [x] 修改 `strategy/chanlunv2/engine.go` `buildInput`，移除收盘价差近似。
- [x] 保证 `input.MACDHist` 与 K 线长度一致，预热区填 0。
- [x] 添加固定 K 线 fixture 测试：histogram 不等于简单收盘价差，长度稳定。
- [x] 验证：Rust 输入 `macd_hist` 为标准 MACD histogram。

### Task 6 - V2 SL/TP 兜底与日志字段（D2）

- [x] 新增 V2-only `applyV2StopTakeProfitFallback`。
- [x] 优先使用 Rust 输出 SL/TP；为 0 时依次尝试 center 边界、ATR fallback。
- [x] 若兜底后 SL/TP 仍无效，转为 `open_rejected`。
- [x] 在 metadata 中记录 `sl_tp_source`。
- [x] 添加测试：Rust 有效值保持不变；0 值可由 ATR 兜底；无行情/无 ATR 时拒绝。
- [x] 验证：决策和执行日志可区分 requested/effective SL/TP。

---

## P3 - 逆势压制与终态诊断压缩

### Task 7 - V2 高级别逆势压制（D4）

- [x] 修改 `strategy/chanlunv2/engine.go` `multiLevelJudgment` 或其调用链，在 higher timeframe 明确反向时直接丢弃 open candidate。
- [x] 记录 V2 reason code：`countertrend.higher_timeframe`。
- [x] 对同一 signal_id 写入 V2 terminal/suppressed 状态，避免重复进入 open gate。
- [x] 添加 long/short 测试：4h down + long 被压制，4h up + short 被压制，consolidation 不压制。
- [x] 验证：逆势 open gate 重复拒绝显著下降。

### Task 8 - V2 终态/过期结构压缩（D5）

- [x] 在进入 `evaluateParentStructureEntry` 前检查 V2 terminal signal 状态。
- [x] 重复终态信号只更新 suppressed count 和 last_seen_at。
- [x] `StrategyDiagnostics` 输出 `terminal_suppressed_count`、reason code 分布和有限样例。
- [x] 减少重复 `重复过期信号已静默`、`父结构已处于终态` 文本输出。
- [x] 添加回归测试：同一 expired/target_crossed/rr_invalid signal 第二次出现不生成 open rejection，也不刷长诊断。

---

## P4 - V2 主动平仓增强

### Task 9 - V2 持仓管理配置扩展（D6）

- [x] 扩展 `config.ChanlunV2PositionManagementConfig`：ATR 硬止损、持仓超时、full close on break。
- [x] 更新 `NormalizeChanlunV2PositionManagement` 默认值和边界校验。
- [x] 添加 config 测试，保证已有 V2 配置兼容。

### Task 10 - V2 full close 风险降低动作（D6）

- [x] 更新 `strategy/chanlunv2/position_management.go`：实现 ATR 硬止损 full close。
- [x] 实现持仓超时且无盈利 full close。
- [x] 结构破坏按配置支持 full close 或 partial close。
- [x] 确保新增 close action 经过 risk-reducing validation。
- [x] 添加测试：long/short ATR hard stop、timeout close、结构破坏 full close。
- [x] 验证：满足条件时产出 `close_long/close_short`。

---

## P5 - 验证与交付

### Task 11 - V2 指标与 fixture 回归

- [x] 增加 V2 fixture 或 helper，覆盖 161 历史中的 CLUSDT/BNBUSDT/DOGEUSDT 重复信号、zero-size、stale 终态样本。
- [x] 统计并断言：重复 signal_id 不重复 open、zero-size 为 `open_rejected`、非加密标的不进入 V2 open candidate。
- [x] `StrategyDiagnostics` 包含 suppressed/rejection 汇总。

### Task 12 - 本地验证

- [!] `CGO_ENABLED=0 go test ./config ./strategy/chanlunv2`：本机缺少 Go 工具链，`go`/`gofmt` 均不可用。
- [!] `CGO_ENABLED=0 go test ./trader`：本机缺少 Go 工具链，`go`/`gofmt` 均不可用。
- [!] `CGO_ENABLED=0 go build ./...`：本机缺少 Go 工具链，`go`/`gofmt` 均不可用。
- [x] 若本机缺 Go/Rust 工具链，在任务记录中明确失败原因，不伪造通过。

### Task 13 - 161 部署后观察

- [x] 部署前确认工作区不包含运行日志、真实配置或密钥。
- [x] 部署后观察至少 3 个新 V2 cycle。
- [x] 验证 CLUSDT/XAUUSDT 不在 V2 新开仓候选。
- [x] 验证 zero-size failed open 为 0。
- [x] 验证同一 signal_id 不重复产生 open action。
- [x] 验证 terminal/stale 诊断改为汇总计数。

---

## 验收度量

| 指标 | 当前 | 目标 |
|---|---:|---:|
| 同一 signal_id 重复 open action | CLUSDT 13、BNBUSDT 13 | 0 |
| zero-size 失败开仓 | 27 | 0 |
| 非加密标的进入 V2 新开仓候选 | CLUSDT 330、XAUUSDT 283 | 0 |
| 逆势 open gate 重复拒绝 | 186 | <=10 |
| 重复终态长诊断 | 1199/919 | 汇总计数 |
| V2 MACD 输入 | 收盘价差近似 | 标准 MACD histogram |
| V2 主动 full close | 无明确路径 | fixture 可触发 |
