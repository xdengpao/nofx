# 缠论 V2 运行态修正与部署验证需求

## 背景

2026-05-31 对 217 远端环境完成验证后确认：

- 远端仓库 `/home/ubuntu/appai2/nofx` 已同步到 `jzhbnofxdev`，HEAD 为 `0778b9453 feat: add chanlun v2 no-open diagnostics`。
- `aster_chanlun_v2` 已在 2026-05-31 19:18:38 +08:00 重启，重启后日志从 `cycle1` 重新开始，并包含 `strategy_diagnostics.active_mode` 与 `strategy_diagnostics.effective_entry_timing`。
- 48 小时 replay 复现旧问题：5 个历史 `sell2` freshness RR 拒绝样本在新配置 `sell2=1.1` 下都会通过 freshness 层，旧日志里的阈值为 `2.5`。
- 重启后的新窗口不再出现 freshness RR 误杀，唯一开仓拒绝落在 open gate：`DOGEUSDT 1h ADX 14.0` 与置信度 `62 < 65`。
- 远端 `loosen_mode` 仍处于 `balanced`，因为 `inactivity_minutes` 使用进程启动后的运行时间；重启后历史 50 小时无成功开仓没有被继承。
- 本机和 217 远端默认 `go test ./strategy/chanlunv2` 均因缺少 `libchanlun_v2.a` 失败；`CGO_ENABLED=0 go test ./strategy/chanlunv2` 通过，`go test ./logger ./decision ./cmd/replay` 通过。

因此当前不需要紧急修改 freshness RR 交易逻辑，但需要修正两个运行保障点：

1. `loosen_mode` 的无成功开仓时长应能跨进程重启继承，避免重启后重新等待 720 分钟。
2. 直接部署/验证环境应能稳定构建或验证 Chanlun V2 native 依赖，避免生产二进制可运行但远端测试不可重复。

## 目标

1. 让 Chanlun V2 的 inactivity/no-open 判定基于 trader 历史决策日志或持久化频率状态，而不是只基于当前进程运行时间。
2. 在保持账户硬风控、亏损模式、open gate、position sizing 和交易所 preflight 不变的前提下，使长时间无成功开仓后 `loosen_mode` 能按配置生效。
3. 让部署与验证链路能明确检查 `libchanlun_v2.a`，并提供可执行的本地/远端修复命令或脚本。
4. 保证所有新增验证不触发真实下单，不提交 `data/`、`decision_logs/`、`coin_pool_cache/` 运行时内容，不提交真实密钥或账户配置。

## 非目标

- 不进一步放宽 `sell2/buy3/sell3` 的 RR 阈值。
- 不绕过 freshness guard、open gate、position sizing、final limit 或 exchange preflight。
- 不改变 Aster、Binance、Hyperliquid 的交易所接口语义。
- 不把 Rust 静态库二进制作为普通源码提交，除非后续设计明确采用受控发布资产方案。
- 不自动重启 217 远端服务；部署动作应由明确任务或人工确认触发。

## 术语

- **Inactivity / no-open 时长**：某 trader 自最近一次成功开仓以来经过的时间；若日志中从未成功开仓，则为可确认日志窗口起点到当前周期的时间。
- **运行时重启边界**：同一 trader 的决策日志 cycle 从较大编号变为 `cycle1` 或进程启动时间变化的边界。
- **Native 依赖**：`strategy/chanlunv2/ffi.go` 通过 CGO 链接的 `libchanlun_v2.a`。
- **CGO 验证**：默认 `go test ./strategy/chanlunv2` 或 `go build` 走 CGO 链接 Rust 静态库的验证。
- **纯 Go 验证**：`CGO_ENABLED=0 go test ./strategy/chanlunv2`，只验证 Go 层逻辑与 stub 路径。

## 需求

### 1. 跨重启 no-open 时长继承

**用户故事：** 作为 NOFX 操作者，我希望 `loosen_mode` 能识别重启前已经持续很久没有成功开仓，以免服务重启后重新等待 12 小时才进入保守放宽状态。

#### 验收标准

1. WHEN `aster_chanlun_v2` 最近一次成功开仓不存在，且最近决策日志窗口已经超过 `trading_frequency.loosen_mode.inactivity_window_minutes` THEN 新进程首个或后续周期 SHALL 将 inactivity/no-open 时长计算为历史日志窗口时长，而不是当前进程运行时长。
2. WHEN 历史日志中存在最近一次成功开仓 THEN inactivity/no-open 时长 SHALL 从该成功开仓时间计算到当前周期时间。
3. WHEN 历史日志为空或不可读取 THEN 系统 SHALL 回退到当前进程 `RuntimeMinutes`，并在 risk state 或 strategy diagnostics 中标记 inactivity 来源为 runtime fallback。
4. WHEN trader ID 不同 THEN no-open 时长 SHALL 按 trader scope 独立计算，不得混用其他 trader 的日志。
5. IF 日志里存在重启边界 THEN no-open 时长 SHALL 跨边界连续计算，除非边界后出现成功开仓。

### 2. Loosen mode 触发条件与风控边界

**用户故事：** 作为策略负责人，我希望跨重启继承的 no-open 时长只影响进入 loosen 的时机，不削弱后续确定性风控。

#### 验收标准

1. WHEN no-open 时长达到配置窗口、`loss_mode.active=false`、`open_count_24h=0`、频率模式不是 `safe/loss` THEN Chanlun V2 SHALL 将 effective mode 设为 `loosen`。
2. WHEN loosen 生效 THEN 系统 SHALL 只应用既有 loosen 调整：降低 trigger confidence、降低 signal-type min RR、放宽 chase ratio。
3. WHEN loosen 生效 THEN freshness guard、open gate、position sizing、final limit 和 exchange preflight SHALL 继续执行，且不得额外放大仓位。
4. IF 出现成功开仓、进入 loss mode、进入 safe/loss mode、或 `open_count_24h>0` THEN loosen SHALL 退出并恢复基础模式。
5. WHEN 输出决策日志 THEN `risk_state.inactivity_minutes` 与 `strategy_diagnostics.effective_entry_timing.active_mode` SHALL 能解释当前是否进入 loosen。

### 3. No-open 来源可观测性

**用户故事：** 作为维护者，我希望日志能说明 inactivity 是来自历史日志、成功开仓时间还是运行时 fallback，避免下次远程排查只看到 `balanced` 却不知道原因。

#### 验收标准

1. WHEN 构建 `decision.Context` THEN risk state SHALL 包含 no-open/inactivity 的来源，例如 `last_successful_open`、`log_window_start` 或 `runtime_fallback`。
2. WHEN 日志可读但没有成功开仓 THEN risk state SHALL 记录可确认日志窗口起点和当前 no-open 分钟数。
3. WHEN 日志中存在成功开仓 THEN risk state SHALL 记录最近成功开仓时间。
4. WHEN 日志不可读或解析失败 THEN 系统 SHALL 不中断交易周期，但 SHALL 记录可观测 warning。
5. WHEN replay 运行 no-open 报告 THEN 报告 SHOULD 能区分历史旧格式日志和当前 HEAD 日志，避免把重启边界旧日志误判为当前版本未部署。

### 4. Native 依赖构建与验证闭环

**用户故事：** 作为开发者/运维者，我希望在本机和 217 远端都能用明确命令完成 Chanlun V2 CGO 验证，而不是只能依赖已有二进制。

#### 验收标准

1. WHEN 运行默认 `go test ./strategy/chanlunv2` 且 `libchanlun_v2.a` 缺失 THEN 验证流程 SHALL 输出明确的缺依赖原因和修复指引。
2. WHEN 运行项目提供的 native 依赖准备命令 THEN 系统 SHALL 从 `chanlun_v2/` 构建或安装 `libchanlun_v2.a` 到 CGO 可发现路径，或生成可通过环境变量引用的构建产物。
3. WHEN native 依赖准备完成 THEN `go test ./strategy/chanlunv2` SHALL 通过 CGO 链接测试。
4. WHEN 只需要验证 Go 层逻辑 THEN 文档或脚本 SHALL 明确 `CGO_ENABLED=0 go test ./strategy/chanlunv2` 是降级验证，不能替代生产 native 链接验证。
5. IF 环境没有 `cargo` THEN 验证流程 SHALL 明确提示安装 Rust toolchain 或使用 Docker 构建路径，不得静默跳过 CGO 验证。

### 5. 远端验证与上线保护

**用户故事：** 作为操作者，我希望修正后能在 217 环境只读验证行为变化，再决定是否重启服务。

#### 验收标准

1. WHEN 执行远端验证 THEN SHALL 先检查 git HEAD、当前运行进程启动时间、最新日志诊断字段和 enabled trader，不直接修改运行配置。
2. WHEN 执行 replay 验证 THEN SHALL 使用只读命令，不写入仓库 tracked 文件，不修改 `data/`、`decision_logs/`、`coin_pool_cache/`。
3. WHEN 需要输出临时 replay JSON THEN SHALL 写入 `/tmp` 或其他临时路径，并在验证结束后清理。
4. WHEN 修正完成但服务尚未重启 THEN 交付说明 SHALL 明确“代码已修正”和“运行进程已生效”是否分别成立。
5. IF 需要重启 217 服务 THEN SHALL 在任务阶段列为单独人工确认步骤，不在代码验证中隐式执行。

## 验证要求

1. SHOULD 添加或更新 `trader`/`decision`/`strategy/chanlunv2` 单元测试，覆盖跨重启 no-open 计算、无日志 fallback、最近成功开仓优先、不同 trader 隔离。
2. SHOULD 添加或更新 replay/logger 测试，覆盖当前 HEAD 日志与旧格式重启边界共存时的版本诊断判断。
3. SHALL 运行：

```bash
CGO_ENABLED=0 GOCACHE=/tmp/nofx-go-build-cache go test ./strategy/chanlunv2 ./logger ./decision ./cmd/replay
GOCACHE=/tmp/nofx-go-build-cache go test ./logger ./decision ./cmd/replay ./trader
```

4. SHOULD 在 native 依赖准备完成后运行：

```bash
GOCACHE=/tmp/nofx-go-build-cache go test ./strategy/chanlunv2
```

5. SHALL 在交付中说明 CGO 验证是通过、跳过还是因环境缺失受阻。
