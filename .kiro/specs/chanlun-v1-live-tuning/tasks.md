# Chanlun V1 Live Tuning Tasks

## Phase 1 — Preflight

- [x] 1.1 连接 161 服务器并确认当前目录为 `/home/ubuntu/appai3/nofx`。
- [x] 1.2 记录当前 `git rev-parse --short HEAD`、`git status --short`、后端容器/进程、端口 8080/3000 状态。
- [x] 1.3 读取当前 `config.json` 的安全摘要，只输出目标字段，不输出 API key、私钥、钱包私钥或签名密钥。
- [x] 1.4 记录 `aster_deepseek` 最近 3 条决策日志的 `timestamp`、`candidate_coins`、`wait_reason_summary`、`risk_state.active_mode`，作为变更前基线。

## Phase 2 — Backup And Patch Config

- [x] 2.1 在远程创建时间戳备份：`config.json.bak.chanlun-v1-live-tuning-20260524_101823`。
- [x] 2.2 用 Python JSON parser 修改远程 `config.json`，避免手写 JSON。
- [x] 2.3 将顶层 `use_default_coins` 设置为 `true`。
- [x] 2.4 将顶层 `default_coins` 设置为 crypto-only 列表：`BTCUSDT`、`ETHUSDT`、`SOLUSDT`、`BNBUSDT`、`XRPUSDT`、`DOGEUSDT`、`LTCUSDT`、`ADAUSDT`、`HYPEUSDT`、`ASTERUSDT`。
- [x] 2.5 将顶层 `dynamic_candidate_pool.enabled` 设置为 `false`，其余字段保留。
- [x] 2.6 将 `aster_deepseek.programmatic_strategy.symbol_pool.mode` 设置为 `override`，并使用同一 crypto-only 列表。
- [x] 2.7 保留并规范化 `candidate_governor.enabled=true`、`allow_non_crypto_symbols=[]`、`max_quote_spread_bps=20`、`core_symbols_must_appear=["BTCUSDT","ETHUSDT"]`。
- [x] 2.8 保留 `timeframes.trade="1h"`、`sub="15m"`、`micro="3m"`、`higher="4h"`。
- [x] 2.9 设置结构门槛：`min_swing_pct=0.2`、`atr_multiplier=0.35`、`min_stroke_bars=4`，保留 `left_bars=2`、`right_bars=2`。
- [x] 2.10 保留 `entry_timing.direct_structure_open=true` 与 `direct_structure_min_confidence=70`。
- [x] 2.11 设置 `entry_timing.entry_zone.max_chase_atr_multiplier=0.8`。
- [x] 2.12 设置 `trading_frequency.loosen_mode.max_chase_ratio_bump=0.10`，并保留 loosen mode 其他字段。
- [x] 2.13 设置 `take_profit.min_net_rr=2.0` 与 `signal_freshness.min_remaining_net_rr=2.0`。
- [x] 2.14 设置 `entry_timing.entry_zone.signal_type_min_rr`：`buy1/sell1=1.8`、`buy2/sell2=1.4`、`buy3/sell3=1.2`，同时覆盖 `@1h` 和无 timeframe key。

## Phase 3 — Static Validation

- [x] 3.1 运行 `python3 -m json.tool config.json >/dev/null` 验证远程配置是严格 JSON。
- [x] 3.2 输出远程配置安全摘要，确认候选池、timeframes、structure、entry_zone、take_profit、signal_freshness、loosen mode 均已按设计生效。
- [x] 3.3 确认配置摘要中不包含任何密钥字段。
- [x] 3.4 静态验证通过，无需恢复备份。

## Phase 4 — Restart Backend

- [x] 4.1 使用 Docker/Compose 状态确认当前服务由容器管理，不在宿主机直接启动 `./nofx`。
- [x] 4.2 使用 `sudo docker compose restart nofx` 重启后端服务；如果 Compose plugin 不可用，再使用设计中的 fallback。
- [x] 4.3 验证 `curl -fsS http://localhost:8080/health` 成功。
- [x] 4.4 验证端口 8080 没有重复后端进程占用。
- [x] 4.5 健康检查通过，无需恢复备份。

## Phase 5 — Observe New Cycles

- [x] 5.1 记录重启完成时间，等待至少 3 个新的 `aster_deepseek` 决策周期。
- [x] 5.2 读取重启后的最新 3 条 V1 日志。
- [x] 5.3 汇总每条日志的 `timestamp`、`cycle_number`、`candidate_coins`、`wait_reason_summary`、`decisions`。
- [x] 5.4 汇总 `risk_state.active_mode`、`risk_state.frequency_policy.loosen_mode.max_chase_ratio_bump`、`risk_state.suppressions`。
- [x] 5.5 汇总 `strategy_diagnostics.messages` 与 `strategy_diagnostics.confidence_histogram`。
- [x] 5.6 验证最新 3 条 V1 日志中不再出现 `XAUUSDT`、`XAGUSDT`、`CLUSDT`。
- [x] 5.7 验证最新 3 条 V1 日志中没有 `candidate_details.filter_reason=non_crypto_symbol`。
- [x] 5.8 观察窗口未出现 `open_rejected`，无需记录拒绝详情。
- [x] 5.9 观察窗口未出现成功开仓，暂无保护单字段可检查。

## Phase 6 — Report And Handoff

- [x] 6.1 将实际备份文件名写回本任务或最终报告。
- [x] 6.2 报告修改后的关键配置值。
- [x] 6.3 报告 3 个新周期的观察结果。
- [x] 6.4 报告是否达到技术验收：crypto-only 候选、健康检查通过、新日志持续写入。
- [x] 6.5 报告策略层解释：仍在等待 1h 信号、已进入 open gate 被拒、或已成功开仓。
- [x] 6.6 无失败任务；回滚任务未触发。

## Rollback Tasks

- [ ] R.1 当触发回滚条件时，复制备份覆盖 `config.json`。
- [ ] R.2 验证恢复后的 `config.json` 是严格 JSON。
- [ ] R.3 重启 `nofx` 后端容器。
- [ ] R.4 验证 `/health` 成功。
- [ ] R.5 读取新日志确认恢复后服务继续写入决策记录。
