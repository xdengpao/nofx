# 缠论 V2 最近 48 小时无开仓诊断与优化任务

## Phase 1: 运行版本与日志确认

- [x] 记录分析基线：最近 48 小时 `aster_chanlun_v2` 961 条日志、成功开仓 0、`open_rejected` 3、主因 `freshness_gate.rr_invalid`。
- [ ] 确认当前运行进程是否已经重启到 HEAD `dd023df3b`。
- [ ] 重启或重新部署 NOFX 后，等待至少 1 个新周期。
- [ ] 检查新日志是否包含 `strategy_diagnostics.active_mode` 和 `strategy_diagnostics.effective_entry_timing`。
- [ ] 若新日志仍以 `sell2` 的 `min_remaining_net_rr=2.5` 拒绝，标记为运行版本未更新或配置未生效。

## Phase 2: Freshness RR 回归测试

- [x] 在 `strategy/chanlunv2/engine_test.go` 添加非 loosen 的 `sell2` entry trigger 使用信号类型 RR 阈值的测试，复现 `remaining_net_rr≈1.19`、`sell2=1.1`、`default_min_net_rr=2.5` 的日志样本。
- [x] 添加缺失信号类型时回退全局/default RR 阈值的测试。
- [x] 复核已有 loosen mode freshness RR 测试，避免重复实现。
- [x] 运行 `GOCACHE=/tmp/nofx-go-build-cache go test ./strategy/chanlunv2`。

## Phase 3: Replay no-open 诊断增强

- [x] 扩展 `logger.BuildOpenRejectionDailyReport()`，输出 no-open 时长、top no-open bucket 和 version diagnostic missing 标记。
- [x] 增加历史 `freshness_gate.rr_invalid` 兼容审计：通过 `cmd/replay -config` 可选读取运行配置，识别旧阈值拒绝但新 signal-type 阈值可通过的样本；未提供配置时在 notes 标记使用日志/默认阈值。
- [x] 在 `logger/replay_test.go` 添加最近 48 小时类似 fixture，覆盖 `sell2` RR near-miss 样本。
- [x] 运行 `GOCACHE=/tmp/nofx-go-build-cache go test ./logger`。

## Phase 4: 保守优化与配置验证

- [x] 验证 `loosen_mode` 在 12 小时无开仓、loss mode inactive、open_count_24h=0 时进入预期 active mode。
- [x] 验证 loosen mode 只影响 entry timing 和 signal-type RR，不绕过 open gate、position sizing、final limit。
- [x] 明确本规格不接入 `PilotRiskFraction` 真实缩仓；候选通过后仍由现有 position sizing 决定仓位。
- [ ] 检查 `entry_zone_chased` 39 次样本，评估是否需要 symbol-level `max_chase_ratio` override。
- [ ] 检查 `waiting_for_fresh_entry_trigger` 86 次样本，评估是否需要调整 trigger 类型或 watch window。
- [ ] 若调整配置，先做只读 replay 或纸面验证，不直接扩大实盘风险。

## Phase 5: 综合验证与交付

- [x] 运行 `GOCACHE=/tmp/nofx-go-build-cache go test ./strategy/chanlunv2 ./logger ./decision`。
- [ ] 运行最近 48 小时 replay，确认报告能明确主因和 near-miss 样本。
- [x] 检查 `git diff`，确保没有提交 `data/`、`decision_logs/`、`coin_pool_cache/` 运行时内容。
- [ ] 输出交付说明：无开仓主因、已验证修复、剩余风险、下一步是否执行实盘重启/配置调整。
