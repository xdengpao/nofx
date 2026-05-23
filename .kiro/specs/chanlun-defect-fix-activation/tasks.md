# 程序化缠论策略第二轮优化 Tasks

## 关联

- `requirements.md`
- `design.md`

---

## Task 1 — normalize 逻辑修复（Req A1 + A5）

- [ ] `strategy/chanlun/engine.go` `normalizeRuntimePreviewSignals`：当 `defectFixPackEnabled=true` 且 `PilotMinConfidence > 70` 时强制设为 70。
- [ ] `strategy/chanlun/engine.go` `normalizeRuntimeEntryTiming`：当 `defectFixPackEnabled=true` 时强制 `DirectStructureOpen=true`。
- [ ] `strategy/chanlun/engine.go` `normalizeRuntimeEntryTiming`：当 `defectFixPackEnabled=true` 且 `MinRemainingNetRR > 2.0` 时强制设为 2.0。
- [ ] `strategy/chanlun/engine.go` `normalizeRuntimePreviewSignals`：当 `defectFixPackEnabled=true` 时强制 `PilotMinConfidenceUseP75=true`。
- [ ] 验证：`go build ./...` 通过。

## Task 2 — governor 执行顺序修复（Req A3）

- [ ] 确认 `StrategySymbol` 是否有 `FilterReason` 字段或等效标记。
- [ ] `strategy/chanlun/engine.go` `evaluateMainSignals`：遍历 universe 时跳过已被 governor 剔除的 symbol。
- [ ] 确认 `evaluatePreviewSignals` 的调用路径中也跳过被剔除 symbol。
- [ ] 验证：`go build ./...` 通过。

## Task 3 — config.json 更新（Req A1 + A2）

- [ ] 远程 config.json `traders[aster_deepseek].programmatic_strategy` 新增 `"defect_fix_pack_enabled": true`。
- [ ] 远程 config.json `preview_signals.pilot_min_confidence` 从 90 改为 70。
- [ ] 远程 config.json `entry_timing.direct_structure_open` 从 false 改为 true。
- [ ] 远程 config.json `entry_timing.require_fresh_trigger` 从 true 改为 false。
- [ ] 远程 config.json `entry_timing.entry_zone.min_remaining_net_rr` 从 2.5 改为 2.0。
- [ ] 远程 config.json `trading_frequency.loosen_mode` 设为完整启用配置。
- [ ] 备份当前 config.json。

## Task 4 — 构建与部署

- [ ] 远程 `go build -o nofx`。
- [ ] 停止当前进程。
- [ ] 启动新进程。
- [ ] `curl localhost:8080/health` 确认健康。

## Task 5 — 部署后验证（Req A4）

- [ ] 部署后 30min 检查日志：确认 `active_mode` 字段出现、`direct_structure` entry_path 被命中。
- [ ] 部署后 6h 检查：`no_trigger` 占比 ≤50%。
- [ ] 部署后 12h 检查：≥1 次真实开仓。
- [ ] 确认 XAG/XAU 不再出现在 pilot 跳过日志中。

---

## 验收度量

| 指标 | 当前 | 目标 |
|---|---|---|
| 真实开仓 / 日 | 0 | ≥1 |
| `no_trigger` 占比 | 94% | ≤30% |
| pilot 阈值生效值 | 90 | 70（或 P75 动态） |
| `direct_structure` 路径命中 | 0 | >0 |
| loosen_mode 激活 | 否 | 12h 无开仓后自动激活 |
