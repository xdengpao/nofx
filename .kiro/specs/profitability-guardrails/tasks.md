# 盈利导向策略防护优化 Tasks

## Phase 1: 开仓闸门

- [x] 1. 扩展 `decision/open_gate.go` 的高 beta 与市场结构 helper
  - 增加 `isHighBetaAltcoin`
  - 增加 `normalizePositionSide`，兼容 `long`/`short` 与旧路径的 `BUY`/`SELL`
  - 增加 BTC 1h/4h 空头结构判断
  - 增加 BTC 15m/1h 冲突判断
  - 增加高 ADX 回踩确认判断

- [x] 2. 实现 BTC 多周期开仓闸门
  - 高 beta `open_long` 遇到 BTC 1h/4h 明显空头时 block
  - 高 beta `open_long` 遇到短中期冲突时 penalize
  - 保持现有 BTC 闪崩硬阻断逻辑

- [x] 3. 实现同方向高 beta 集中度控制
  - 已有 2 个同向 long 时拒绝新增高 beta long
  - 已有 1 个同向高 beta long 时风险减半
  - 任一同向持仓 `UnrealizedPnLPct <= -4%` 时拒绝新增同向仓

- [x] 4. 实现高 ADX 追高过滤
  - `CurrentADX > 60` 且 1h/4h 已明显上涨、无回踩确认时 block
  - `CurrentADX > 50` 但未 block 时风险减半并提高最低置信度
  - 本地过滤优先于 AI reasoning

- [x] 5. 补充开仓闸门测试
  - 覆盖 BTC 1h/4h 空头阻断
  - 覆盖 BTC 多周期冲突降权
  - 覆盖第三个高 beta 多单阻断
  - 覆盖同向浮亏持仓阻断
  - 覆盖极高 ADX 追高阻断和高 ADX 降权

## Phase 2: 平仓与利润保护

- [x] 6. 收紧利润保护配置
  - 将基础利润保护比例提高到 60%
  - 峰值盈利 `>= 12%` 时使用不低于 65% 的保护线
  - 保留峰值、当前、保护线的原因输出

- [x] 7. 提前移动止损与锁盈
  - 浮盈 `>= 6%` 时允许保本或小幅盈利止损
  - 浮盈 `>= 10%` 时进入更早锁盈档
  - 调整移动止损档位，保持多头止损只升不降、空头只降不升

- [x] 8. 实现保护期后的软止损
  - 扩展 `PositionEvaluator`，从 `evaluateExistingPositions` 注入可选 BTC 市场数据
  - 保护期后 `UnrealizedPnLPct <= -5%` 平仓
  - 持仓超过 60 分钟、`max(MFE, 当前PnL) < +3%` 且当前 `<= -3%` 平仓
  - 曾经盈利后跌回开仓价以下且动量转弱时退出或减仓
  - 不改变保护期内 `< -3%` 极端亏损紧急平仓优先级

- [x] 9. 补充平仓规则测试
  - 覆盖 12% 峰值利润保护线
  - 覆盖 6% 保本/小幅盈利移动止损
  - 覆盖保护期后 -5% 软止损
  - 覆盖 60 分钟无动量失败退出
  - 回归保护期、硬止损、固定止盈、移动止损单调性

## Phase 3: 滚动亏损风险降档

- [x] 10. 扩展 `logger.RollingPerformanceSnapshot`
  - 增加 `recent_3`
  - 增加 `recent_loss_streak`
  - 增加 `recent_3_losses`
  - 保持 JSON 向后兼容

- [x] 11. 实现最近亏损风险降档
  - 最近两笔闭合交易均亏损时，下一笔单笔风险降至不高于 1%
  - 最近三笔亏损不少于两笔且总 PnL 为负时，对 long/short side gate 提高最低置信度并降权
  - 样本不足时不阻断交易

- [x] 12. 补充滚动绩效测试
  - 覆盖最近两笔连续亏损降风险
  - 覆盖最近三笔两亏提高置信度
  - 覆盖样本不足不 block

## Phase 4: Prompt 同步

- [x] 13. 更新 `decision/decision.go` 的开仓 prompt
  - 增加 BTC 多周期弱势禁止山寨多单说明
  - 增加同向高 beta 持仓限制说明
  - 增加 ADX `25-50` 与 `>60` 的不同解释
  - 增加亏损后自动降仓说明

- [x] 14. 检查 prompt 与本地规则一致性
  - 确认本地规则比 prompt 更严格或等价
  - 确认 JSON 输出格式不变
  - 确认中文文案清晰且不引入不可执行字段

## Phase 5: 验证与复盘

- [x] 15. 运行聚焦测试
  - `go test ./logger ./decision`
  - 如失败，修复后重跑

- [x] 16. 复盘 5.6/5.7 样例规则效果
  - 检查 HYPE/ZEC/SOL 类极高 ADX 追多是否会被拒绝或降权
  - 检查 ETH/XRP 峰值盈利后的保护线是否更早触发
  - 检查 HYPE 类持仓在保护期后是否会提前软止损

- [x] 17. 最终自检
  - 确认未修改交易所接口、凭证、真实账户配置
  - 确认没有改动无关 runtime 数据
  - 确认 `data/`、`decision_logs/` 的既有变更未被回退
  - 汇总代码变更、测试结果和剩余风险
