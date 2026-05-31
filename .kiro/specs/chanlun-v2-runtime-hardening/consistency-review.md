# 缠论 V2 运行态修正与部署验证一致性审查

## 审查结论

结论：`requirements.md`、`design.md`、`tasks.md` 的核心目标与当前代码边界一致，可以进入实现阶段；执行前建议处理 1 个中等设计澄清点和 2 个轻微任务补强点。

## 审查范围

- 规格文档：
  - `.kiro/specs/chanlun-v2-runtime-hardening/requirements.md`
  - `.kiro/specs/chanlun-v2-runtime-hardening/design.md`
  - `.kiro/specs/chanlun-v2-runtime-hardening/tasks.md`
- 相关代码：
  - `decision/types.go`
  - `logger/decision_logger.go`
  - `trader/auto_trader.go`
  - `strategy/chanlunv2/loosen_mode.go`
  - `logger/replay.go`
  - `strategy/chanlunv2/ffi.go`
  - `docker/Dockerfile.backend`
  - `Makefile`
  - `scripts/`

## 已确认一致

### 1. no-open 跨重启问题与当前代码一致

当前 `strategy/chanlunv2/loosen_mode.go` 中 loosen 进入逻辑优先使用 `FrequencyState.LastOpenAt`，否则回退 `ctx.RuntimeMinutes`。这与远端验证里“重启后 inactivity 从进程启动时间重新计算”的现象一致。

当前 `trader/auto_trader.go` 在构建 risk state 时也使用 `LastOpenAt` 或 `RuntimeMinutes` 计算 `risk_state.inactivity_minutes`，尚无 inactivity 来源、日志窗口起点或 warning 字段。

因此设计中扩展 `FrequencyState`、`RiskStateSnapshot`、`FrequencyStateSnapshot` 并让 loosen 使用 `FrequencyState.InactivityMinutes`，方向与现有代码匹配。

### 2. 决策日志天然按 trader 目录隔离

`NewAutoTrader()` 使用 `decision_logs/{trader_id}` 创建独立 `DecisionLogger`，`buildFrequencyState()` 读取的是该 AutoTrader 自己的 logger。

因此在正常运行链路里，no-open 统计不会混用其他 trader 的日志；需求中的 trader scope 隔离与现有目录设计一致。

### 3. replay 版本诊断问题与当前实现一致

当前 `logger/replay.go` 的 `chanlunV2VersionDiagnosticMissing(records)` 对全窗口任一旧格式 Chanlun V2 记录返回 true，并在 notes 中提示确认当前 HEAD。

这与远端验证观察到的情况一致：19:18 重启边界前旧日志缺字段，19:19 后新窗口正常，但全窗口 replay 仍提示 `version_diagnostic_missing=true`。

设计中新增当前重启窗口诊断状态，能满足需求 3.5。

### 4. native 依赖修复方向与代码一致

当前 `strategy/chanlunv2/ffi.go` 固定通过 CGO 链接 `/usr/local/lib/libchanlun_v2.a`，而 `Makefile` 没有裸机 native 准备或检查目标，`scripts/` 也没有对应脚本。

Docker backend 已包含 Rust 构建阶段并复制 `libchanlun_v2.a`，说明设计中的“裸机脚本 + Makefile target，不提交静态库二进制”与现有部署结构兼容。

### 5. 风控边界保持一致

设计只改变 no-open/inactivity 的来源与 loosen 进入时机，不改 `ValidateStrategyDecisions()`、open gate、position sizing、final limit 或交易所 preflight。

任务 Phase 4 明确保留 loss/safe/open count 退出条件，并确认后续风控继续执行，符合需求非目标。

## 发现项

### Medium: trader scope 隔离在设计 helper 中不够显式

需求 1.4 要求不同 trader 的 no-open 时长独立计算。当前运行链路依赖 `decision_logs/{trader_id}` 保证输入记录已 scoped，但设计里的 `deriveNoOpenState(records, now, runtimeMinutes)` 没有 `traderID` 参数，也没有在函数契约中明确“records 必须来自当前 trader 的 DecisionLogger”。

风险：未来若该 helper 被 replay、manager 或测试用混合记录调用，可能违反 trader scope 要求。

建议二选一：

1. 将 helper 设计改为 `deriveNoOpenState(records, traderID, now, runtimeMinutes)`，并在 trader 包内按 `RiskState.TraderID`、action metadata 或日志来源过滤。
2. 保持 helper 无 traderID，但在 design/tasks 中明确输入必须已 scoped，并新增测试证明 `AutoTrader` 使用独立 `DecisionLogger` 读取当前 trader 日志。

### Low: 成功开仓判定应统一 action/final_action 口径

当前 `trader.lastSuccessfulOpenAt()` 和 `logger.CountSuccessfulOpens()` 使用 `action.Action` 判断 open-like action；`logger.noSuccessfulOpenHours()` 使用 `FinalAction` fallback 到 `Action`。

设计未明确新 no-open 推导采用哪一种口径。

建议：在 `deriveNoOpenState()` 设计中明确使用 `FinalAction` fallback 到 `Action`，与 replay 的 no-open 报告口径保持一致；若实现继续使用 `Action`，需要说明真实执行日志的成功开仓 action 一定写为 `open_long/open_short/add_long/add_short`。

### Low: native “默认 go test 友好错误”依赖验证流程而非原生命令

需求 4.1 写的是运行默认 `go test ./strategy/chanlunv2` 且缺库时验证流程应输出明确修复指引。设计通过 `check-native-chanlunv2`、`native-chanlunv2` 和 Makefile target 解决；原生 `go test` 仍会输出链接器错误。

这在语义上可接受，因为需求写的是“验证流程”，不是要求改 CGO 链接错误本身。建议在 tasks 或交付说明中强调：标准验证入口应先运行 `make check-native-chanlunv2`，裸 `go test ./strategy/chanlunv2` 的系统链接器错误不做包装。

## 建议的最小修订

1. 在 `design.md` 的 `deriveNoOpenState` 小节明确 trader scope 策略：要么增加 `traderID` 参数，要么声明 records 必须已由 `DecisionLogger` 按 trader scoped。
2. 在 `tasks.md` Phase 7 增加一条：覆盖成功开仓判定使用 `final_action` fallback 到 `action` 或明确 action-only 口径。
3. 在 `tasks.md` Phase 8 或 Phase 11 增加一条：交付说明中区分 `make check-native-chanlunv2` 标准验证流程与裸 `go test ./strategy/chanlunv2` 的链接器错误。

## 可进入执行的条件

满足以下任一条件即可进入执行：

- 先接受上述 3 个最小修订并更新 spec。
- 或确认当前运行链路的 trader-scoped logger 与 action-only 成功开仓口径足够，不需要再修订文档。

从实现风险看，本规格主要是运行态可观测和 loosen 进入时机修正，不触碰交易所接口和真实下单路径；执行时仍需重点跑 `trader`、`strategy/chanlunv2`、`logger`、`decision`、`cmd/replay` 的定向测试。
