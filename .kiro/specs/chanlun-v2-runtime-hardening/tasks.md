# 缠论 V2 运行态修正与部署验证任务

## Phase 1: 需求与设计确认

- [x] 确认 `requirements.md` 覆盖跨重启 no-open 继承、loosen 风控边界、native 依赖验证和远端上线保护。
- [x] 确认 `design.md` 的实现边界不改变交易所接口、不绕过 open gate/position sizing/exchange preflight。
- [x] 确认本规格不修改真实 `config.json`，不提交 `data/`、`decision_logs/`、`coin_pool_cache/`。

## Phase 2: 频率状态数据结构

- [x] 在 `decision/types.go` 扩展 `FrequencyState`，新增 `InactivityMinutes`、`InactivitySource`、`NoOpenSince`、`LogWindowStart`、`LogWindowEnd`、`InactivityWarning`。
- [x] 在 `logger/decision_logger.go` 扩展 `RiskStateSnapshot` 与 `FrequencyStateSnapshot`，保持字段 `omitempty` 兼容旧日志。
- [x] 更新 `copyFrequencyStateSnapshot()`，同步 no-open/inactivity 新字段到嵌套 `frequency_state`。
- [x] 更新 `buildRiskStateSnapshot()`，同步 no-open/inactivity 新字段到顶层 `risk_state`。
- [x] 若 `InactivityWarning` 非空，在 `risk_state.warnings` 中追加可观测 warning。

## Phase 3: 跨重启 no-open 推导

- [x] 在 `trader/auto_trader.go` 新增 `noOpenState` 与 `deriveNoOpenState(records, traderID, now, runtimeMinutes)`。
- [x] 实现 `last_successful_open` 来源：有成功开仓时从最近成功开仓时间计算 no-open 分钟数。
- [x] 实现 `log_window_start` 来源：日志可读但无成功开仓时从最早有效日志时间计算 no-open 分钟数。
- [x] 实现 `runtime_fallback` 来源：日志为空或不可用时回退当前进程运行时长，并设置 warning。
- [x] 新增 `buildFrequencyStateAt(records, accountEquity, now)`，让测试可注入固定时间。
- [x] 调整 `buildFrequencyState()` 调用 `buildFrequencyStateAt(..., time.Now())`，保留原调用方兼容。
- [x] 新增 `frequencyRecordLimit()`，按 `loosen_mode.inactivity_window_minutes` 与 `scan_interval_minutes` 计算有上限的日志读取数量。
- [x] 将构建上下文和状态接口里的固定 `loadRecentDecisionRecords(500)` 替换为 `frequencyRecordLimit()`，确保 12 小时以上窗口可覆盖。

## Phase 4: Loosen mode 接入

- [x] 在 `strategy/chanlunv2/loosen_mode.go` 新增 `inactivityDurationForLoosen(ctx)`。
- [x] 修改 `loosenModeController()`，优先使用 `ctx.FrequencyState.InactivityMinutes` 判断是否进入 loosen。
- [x] 保留现有退出条件：loss mode active、safe/loss mode、`OpenCount24h > 0`。
- [x] 保留现有 loosen 调整范围：trigger confidence、signal-type min RR、chase ratio。
- [x] 确认 freshness guard、open gate、position sizing、final limit、exchange preflight 仍在 loosen 后继续执行。
- [x] 在 `effectiveEntryTimingDiagnostics()` 中增加 `inactivity_minutes`、`inactivity_source`、`no_open_since` 等只读诊断字段。

## Phase 5: Replay 版本诊断改进

- [x] 在 `logger/replay.go` 新增 version diagnostic status 结构，统计全窗口缺字段和当前重启窗口缺字段。
- [x] 识别当前重启窗口：按时间排序后，取最后一个 `cycle_number` 回落点之后的 Chanlun V2 记录。
- [x] 扩展 `OpenRejectionDailyReport`，新增当前窗口 version diagnostic 字段和 missing count。
- [x] 调整 notes：历史旧格式缺字段但当前窗口正常时，提示历史旧格式；当前窗口仍缺字段时，提示运行进程可能未部署 HEAD。
- [x] 保持旧 `version_diagnostic_missing` 字段语义兼容，不破坏已有 replay 输出。

## Phase 6: Native 依赖脚本与 Makefile

- [x] 新增 `scripts/check-chanlun-v2-native.sh`，只读检查 `cargo`、`/usr/local/lib/libchanlun_v2.a`、`chanlun_v2/target/release/libchanlun_v2.a` 和 CGO 状态。
- [x] 新增 `scripts/prepare-chanlun-v2-native.sh`，在存在 `cargo` 时构建 `chanlun_v2/target/release/libchanlun_v2.a`。
- [x] 在 prepare 脚本缺少 `cargo` 时输出明确安装 Rust toolchain 或 Docker 构建路径提示。
- [x] 支持 prepare 脚本输出 `CGO_LDFLAGS="-L.../chanlun_v2/target/release" go test ./strategy/chanlunv2` 验证命令。
- [x] 可选支持 `--install-local`，仅在明确传参时提示/执行安装到 `/usr/local/lib`，避免隐式 sudo。
- [x] 更新 `Makefile`，新增 `check-native-chanlunv2`、`native-chanlunv2`、`test-chanlunv2`、`test-chanlunv2-go` targets。
- [x] 运行 `bash -n scripts/check-chanlun-v2-native.sh scripts/prepare-chanlun-v2-native.sh`。

## Phase 7: 单元测试

- [x] 在 `trader` 测试中覆盖：有最近成功开仓时 `InactivitySource=last_successful_open`。
- [x] 在 `trader` 测试中覆盖：无成功开仓但有日志窗口时 `InactivitySource=log_window_start`，并跨 12 小时。
- [x] 在 `trader` 测试中覆盖：空日志时 `InactivitySource=runtime_fallback` 且 warning 可观测。
- [x] 在 `trader` 测试中覆盖：混入其他 trader 记录时 no-open 推导仍按当前 trader scope 计算。
- [x] 在 `trader` 测试中覆盖：成功开仓判定使用 `final_action` fallback 到 `action`，与 replay 口径一致。
- [x] 在 `trader` 测试中覆盖：`buildFrequencyStateAt()` 正确填充 `InactivityMinutes`、`NoOpenSince`、`LogWindowStart`、`LogWindowEnd`。
- [x] 在 `strategy/chanlunv2` 测试中覆盖：`RuntimeMinutes` 不足但 `FrequencyState.InactivityMinutes` 足够时进入 loosen。
- [x] 在 `strategy/chanlunv2` 测试中覆盖：`OpenCount24h>0`、loss mode、safe/loss mode 仍退出 loosen。
- [x] 在 `logger` replay 测试中覆盖：历史旧格式 + 当前窗口新格式时不再误判当前进程缺诊断字段。
- [x] 在 `logger` replay 测试中覆盖：当前窗口仍缺 `active_mode/effective_entry_timing` 时输出当前窗口缺失警告。

## Phase 8: 本地验证

- [x] 运行 `CGO_ENABLED=0 GOCACHE=/tmp/nofx-go-build-cache go test ./strategy/chanlunv2 ./logger ./decision ./cmd/replay`。
- [!] 运行 `GOCACHE=/tmp/nofx-go-build-cache go test ./logger ./decision ./cmd/replay ./trader`：`trader` 默认 CGO 链接失败，`ld: library 'chanlun_v2' not found`，需先补 native 库。
- [x] 运行 `make check-native-chanlunv2`，确认 native 依赖状态提示清晰。
- [x] 若本机已有 `cargo`，运行 `make native-chanlunv2` 并尝试 `make test-chanlunv2`：本机无 `cargo`，条件不满足；已运行 `make native-chanlunv2` 确认提示清晰。
- [x] 若本机无 `cargo` 或缺 native 库，记录 CGO 验证受阻原因，并确认 `test-chanlunv2-go` 不是生产链接替代品。
- [x] 交付说明中区分标准 native 检查入口 `make check-native-chanlunv2` 与裸 `go test ./strategy/chanlunv2` 的系统链接器错误。
- [x] 检查 `git diff`，确认没有运行时目录或真实配置内容进入提交。

## Phase 9: 远端只读验证准备

- [x] 同步代码到 217 前，确认本地分支和远端分支状态。
- [x] 在 217 上只读检查 git HEAD、当前运行进程启动时间、enabled trader 和最新日志诊断字段。
- [x] 在 217 上运行 `make check-native-chanlunv2`，记录 `cargo` 与 `libchanlun_v2.a` 状态：在 `/tmp/nofx-verify` 临时目录验证，未修改服务目录。
- [x] 在 217 上运行纯 Go 验证：`CGO_ENABLED=0 GOCACHE=/tmp/nofx-go-build-cache go test ./strategy/chanlunv2 ./logger ./decision ./cmd/replay`。
- [x] 若 217 具备 native 依赖，运行 `GOCACHE=/tmp/nofx-go-build-cache go test ./strategy/chanlunv2`；否则记录明确阻塞原因：217 同样缺少 `cargo` 与 `libchanlun_v2.a`。
- [x] 使用 replay 只读验证历史 no-open 报告，临时 JSON 写入 `/tmp`，验证后清理。

## Phase 10: 运行进程生效验证

- [ ] 若用户确认重启 217 服务，则先记录当前进程 PID、启动时间、HEAD、最新日志文件。（未收到重启确认，未执行）
- [ ] 重启后等待至少 1 个 `aster_chanlun_v2` 新周期。
- [ ] 检查新日志 `risk_state.inactivity_source`，确认不再意外回退到 `runtime_fallback`。
- [ ] 若历史 no-open 已超过 720 分钟且风险健康，确认 `strategy_diagnostics.active_mode=loosen` 或 `risk_state.active_mode=loosen`。
- [ ] 若仍为 `balanced`，从 risk state / diagnostics 输出原因，例如 loss mode、safe mode、open_count_24h 或日志窗口不足。
- [ ] 确认新周期没有 freshness RR 旧阈值误杀，若有 open rejection，应明确落在 freshness/open gate/sizing/final limit/exchange execution 之一。

## Phase 11: 交付说明

- [x] 汇总代码变更：no-open 继承、loosen 接入、可观测字段、replay 当前窗口诊断、native 脚本。
- [x] 汇总本地测试结果，包括 CGO 验证是否通过或受阻。
- [x] 汇总 217 远端只读验证结果。
- [x] 明确服务是否已经重启并运行新逻辑；若未重启，说明“代码已修正但运行进程未生效”。
- [x] 列出剩余风险和后续建议，例如是否安装 Rust toolchain、是否采用 Docker 构建作为标准发布路径。
