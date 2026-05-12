# 开仓频率优化 Requirements

## 背景

本规格基于本机 `nofx-trading` 服务与 `decision_logs/aster_deepseek/` 的最近运行记录。当前可用日志覆盖 `2026-05-10 22:18:26 +0800` 至 `2026-05-12 10:09:14 +0800`，共 718 个决策周期；服务运行在 Docker 容器 `nofx-trading` 中，容器状态为 Up/healthy。

最近记录显示，当前只有 `aster_deepseek` trader 启用，账户净值约 59 USDT，扫描周期为 3 分钟，AI 新机会分析间隔为 15 分钟，单笔风险预算约 2%，总风险预算约 8%。该窗口内实际开仓 3 次、平仓 3 次、开仓建议被本地风控拒绝 76 次，未发现已执行开仓的交易所拒单或保护单失败。

不开仓的主要原因如下：

- 471 个周期因未满 15 分钟新机会分析间隔而跳过 AI 开仓搜索。
- 约 48 次 AI 实际分析后主动输出 `wait`，近期典型原因为 BTC 低 ADX 震荡、ETH 弱势、山寨币受 BTC 压制或 ADX 过高。
- 76 条 `open_rejected` 中，主要拒绝类别为高 ADX/置信度门槛 35 次、风险回报比低于 2.5 计 31 次、历史滚动表现偏弱 28 次、同向高 beta 暴露 12 次、单笔风险超过当前有效上限 3 次。
- 最近 3 笔闭合交易为 1 胜 2 负，统计文件总 PnL 为负，Profit Factor 约 0.20；因此任何提高开仓数量的修改必须保留硬风控，并以小步灰度和可回滚方式执行。

## 目标

1. 将当前“不开仓”的可观测原因转化为可配置、可回放、可验证的策略参数。
2. 在不移除硬风控的前提下，提高有效开仓候选进入执行层的数量。
3. 优先减少无效拒绝和过度保守的机会搜索节流，而不是简单放大仓位或绕过亏损保护。
4. 默认上线 `balanced` 档，使用 12 分钟新机会分析间隔和 10 个 prompt 候选；`active` 档仅作为显式灰度选择。
5. 为后续实现提供清晰的设计边界、测试口径和灰度回滚标准。

## 非目标

- 不直接修改 API key、私钥、真实账户凭证或启用当前禁用的 trader。
- 不取消最大持仓 3 个、单笔风险、总风险预算、最大回撤、保护单失败阻断、AI 失败退避等硬安全约束。
- 不通过扩大杠杆或无条件降低止损要求来提高交易数量。
- 不把历史负期望问题归因于单一参数；方案必须同时考虑交易质量与开仓频率。

## 术语

- **新机会分析间隔**：`AnalysisIntervalMin`，当前默认 15 分钟；扫描周期仍为 3 分钟。
- **有效开仓候选**：AI 输出 `open_long` 或 `open_short` 后，经过参数补全、本地风控和最终限制后可进入执行层的决策。
- **硬风控**：触发后必须拒绝开仓的约束，如回撤硬停、风险预算用尽、交易所保护单失败、重复持仓、止损止盈方向错误。
- **软风控**：触发后可以降仓、提高置信度、延迟或记录警告的约束，如中等 ADX、BTC 多周期冲突、近期滚动表现偏弱。
- **灰度档位**：以配置控制的开仓频率策略档位，例如 `safe`、`balanced`、`active`。本规格默认推荐 `balanced`。
- **report-only**：只记录“如果放宽会怎样”的诊断结果，不改变实盘执行决策。

## Requirements

### Requirement 1: 建立开仓频率诊断基线

**User Story:** 作为交易系统维护者，我希望系统能持续量化“为什么没有开仓”，以便调参前后可以对比真实影响。

#### Acceptance Criteria

1. WHEN 读取最近 N 小时 `decision_*.json` 日志 THEN 诊断 SHALL 输出周期数、实际开仓数、平仓数、AI wait 数、间隔跳过数、开仓拒绝数和拒绝原因分布。
2. WHEN 存在 `open_rejected` THEN 诊断 SHALL 按 symbol、side、主因和 gate reason 汇总，不得只输出原始文本。
3. WHEN 日志时间戳包含 Go RFC3339Nano 纳秒精度 THEN 诊断 SHALL 正确解析，不得漏算记录。
4. WHEN 生成调参方案 THEN SHALL 明确当前日志覆盖范围和样本限制。
5. IF 诊断发现交易所拒单、保护单失败或 AI 调用失败 THEN 方案 SHALL 将其列为优先阻断项，而不是继续提高开仓频率。

### Requirement 2: 配置化 AI 新机会分析间隔

**User Story:** 作为交易策略维护者，我希望把 15 分钟新机会分析间隔从硬编码默认值变成可配置参数，以便在不同风险档位下调整机会搜索频率。

#### Acceptance Criteria

1. WHEN 配置包含新机会分析间隔 THEN `AutoTraderConfig.AnalysisIntervalMin` SHALL 使用该配置值，而不是固定使用 `trader.DefaultAnalysisInterval`。
2. WHEN 旧配置完全没有 `trading_frequency` 配置块 THEN 系统 SHALL 保持当前 15 分钟默认行为，保证兼容。
3. WHEN 存在 `trading_frequency` 配置块但未显式提供分析间隔 THEN 系统 SHALL 按档位派生间隔。
4. WHEN 配置值低于安全下限 THEN 系统 SHALL 拒绝启动或归一化到安全下限，并记录原因。
5. IF 档位为 `balanced` THEN 初始建议 SHALL 将间隔调整到 12 分钟，作为默认上线值。
6. IF 档位为 `active` THEN 初始建议 SHALL 将间隔调整到 9 分钟；不得直接调整为每 3 分钟都调用 AI。
7. WHEN 间隔调整生效 THEN 决策日志的 wait reason SHALL 显示实际间隔值，便于回放验证。

### Requirement 3: 引入开仓频率灰度档位

**User Story:** 作为操盘者，我希望用一个清晰档位切换开仓频率策略，而不是同时手改多个分散参数。

#### Acceptance Criteria

1. SHALL 支持至少 `safe`、`balanced`、`active` 三个档位。
2. WHEN 档位为 `safe` THEN 系统 SHALL 保持或接近当前保守行为。
3. WHEN 存在 `trading_frequency` 配置块但未显式配置档位 THEN 系统 SHALL 使用 `balanced` 作为推荐上线档位，但 SHALL 支持通过配置回退到 `safe`。
4. WHEN 档位为 `balanced` THEN 系统 SHALL 使用 12 分钟新机会分析间隔和 10 个 prompt 候选，并保持当前核心开仓闸门。
5. WHEN 档位为 `active` THEN 系统 MAY 使用 9 分钟新机会分析间隔和更多候选，但 SHALL 保留硬风控、最大持仓和风险预算。
6. WHEN 档位为 `active` THEN 系统 SHALL 启用每日新增开仓上限和 24 小时自动回滚条件。
7. WHEN `active` 触发 24 小时自动回滚 THEN 回滚 SHALL 只影响运行时开仓行为、AI 新机会搜索节流和风险闸门，不得在同一运行周期内改写或动态回退已配置的候选池 prompt limit。
8. WHEN 档位切换 THEN 日志和 API 状态 SHALL 能展示当前档位和关键派生参数。

### Requirement 4: 扩大候选机会覆盖面

**User Story:** 作为交易策略维护者，我希望 AI 每次分析能看到更多质量合格的候选币，以提高找到可执行机会的概率。

#### Acceptance Criteria

1. WHEN 动态候选池快照可用且数量大于 prompt limit THEN 系统 SHALL 允许通过配置提高 `prompt_candidate_limit`。
2. IF 当前档位为 `balanced` THEN prompt 候选值 SHALL 为 10，除非显式配置更保守的值。
3. IF 当前档位为 `active` THEN prompt 候选建议值 SHOULD 为 12。
4. WHEN 候选数量增加 THEN 每个候选仍 SHALL 保留来源、score、market state、data quality 和 included_in_prompt 记录。
5. IF OI Top API 未配置 THEN 系统 SHALL 在诊断中标记“缺少 OI 增量来源”，但不得因此中断交易循环。
6. WHEN `active` 因 24 小时表现触发 runtime rollback THEN 候选池 SHALL 继续使用启动时派生或显式配置的 prompt candidate limit，除非人工修改配置并重启。

### Requirement 5: 校准高 ADX 软闸门

**User Story:** 作为交易策略维护者，我希望系统区分“强趋势可交易”和“趋势末端追高”，避免 ADX>50 一律把门槛抬到 90 后错过部分可控机会。

#### Acceptance Criteria

1. WHEN 高 beta 多单目标 `CurrentADX` 介于 50 到 60 之间 THEN 系统 SHALL 支持按档位计算“假设放宽后”的最低置信度和风险乘数。
2. IF 档位为 `balanced` THEN 高 ADX 规则 SHALL 保持现有实盘拦截/降权行为，仅额外记录 report-only 结果。
3. IF 档位为 `active` AND 多时间框架同向 AND 存在回踩确认 THEN 最低置信度 MAY 在 report-only 中从 90 下调到 85，但默认不得影响实盘执行。
4. IF `CurrentADX > 60` AND 短期涨幅过大 AND 无回踩确认 THEN 系统 SHALL 继续硬拒绝开仓。
5. WHEN 高 ADX 仅触发软降权 THEN report-only 结果 SHALL 计算缩小仓位后的可行性，而不是建议无条件开仓。
6. WHEN report-only 认为候选可通过 THEN 决策记录 SHALL 保存原始 gate reason、模拟后的 min confidence、effective risk 和 adjusted size。
7. SHALL NOT 在本规格默认上线阶段把高 ADX 放宽接入实盘执行路径。

### Requirement 6: 改善 RR 拒绝的可执行性

**User Story:** 作为交易策略维护者，我希望减少 AI 反复输出 RR 不达标方案的无效拒绝，同时不降低整体盈亏比约束。

#### Acceptance Criteria

1. WHEN AI 输出的止损止盈导致净 RR 低于本地阈值 THEN 系统 SHALL 在拒绝日志中记录实际 RR、阈值和 stop/take-profit 价格。
2. WHEN prompt 构建开仓说明 THEN SHALL 明确本地验证使用净 RR 阈值，避免 prompt 写 1:3 而代码按 2.5:1 校验造成歧义。
3. IF 档位为 `balanced` THEN RR 实盘阈值 SHALL 保持当前 2.5。
4. IF 档位为 `active` THEN 系统 MAY 对 BTC/ETH 或低相关低波动标的在 report-only 中模拟不低于 2.0 的配置化 RR 下限；高 beta 山寨默认 SHALL 继续使用 2.5。
5. IF 降低 RR 阈值 THEN 方案 SHALL 通过 replay 或 report-only 输出新增通过数量和理论风险，不得直接静默上线。
6. SHALL NOT 因提高开仓数量而允许 RR 小于 2.0 的新仓。

### Requirement 7: 将可缩仓的风险超限从拒绝改为调整

**User Story:** 作为交易策略维护者，我希望当 AI 仓位过大但缩小后仍可满足最小下单额时，系统自动缩仓执行，而不是直接拒绝。

#### Acceptance Criteria

1. WHEN AI 请求仓位超过单笔有效风险上限 THEN 系统 SHALL 计算风险、保证金、最小名义额共同约束下的最大可执行仓位。
2. IF 最大可执行仓位大于等于交易所最小名义额 THEN 系统 SHALL 将仓位缩小到该值并继续验证。
3. IF 缩仓后仍低于最小名义额或无法可靠设置保护单 THEN 系统 SHALL 拒绝开仓。
4. WHEN 自动缩仓发生 THEN 决策日志 SHALL 记录 requested size、adjusted size、risk cap 和原因。
5. SHALL 保持当前总风险预算和最大持仓数量限制。

### Requirement 8: 滚动表现降权需要样本和恢复机制

**User Story:** 作为交易系统维护者，我希望近期亏损后的保守机制仍然有效，但不要因为极小样本长期压制所有开仓。

#### Acceptance Criteria

1. WHEN rolling performance gate 触发 THEN 日志 SHALL 包含样本数、窗口、PnL、Profit Factor 和冷却截止时间。
2. IF 样本数低于配置下限 THEN rolling gate SHALL 只能降仓，不得提高最低置信度到导致长期拒绝。
3. WHEN 冷却期结束或新样本改善 THEN gate SHALL 自动恢复到较宽松状态。
4. WHEN 档位为 `balanced` THEN rolling gate SHALL 保持当前实盘行为，仅增强可观测性。
5. WHEN 档位为 `active` THEN rolling gate MAY 在 report-only 中优先使用风险乘数降仓，而不是提高置信度门槛。
6. IF 最近窗口出现连续亏损或 Profit Factor 明显低于 1 THEN 系统 SHALL 保留降权，不得完全关闭亏损保护。

### Requirement 9: 保持风控底线和回滚能力

**User Story:** 作为实盘账户负责人，我希望提高开仓数量的所有改动都有明确底线和回滚路径。

#### Acceptance Criteria

1. SHALL 保留最大 3 个持仓、单笔风险预算、总风险预算、最大回撤硬停、AI backoff 和执行质量 gate。
2. SHALL 保留保护单失败后的新开仓阻断逻辑。
3. WHEN 新档位上线 THEN SHALL 支持通过配置回退到当前保守行为。
4. WHEN 档位为 `active` THEN 系统 SHALL 限制每日新增开仓数量；初始上限 SHOULD 为 4 笔/24小时。
5. WHEN 最近 24 小时新增开仓胜率、Profit Factor 或回撤劣化超过阈值 THEN 系统 SHALL 建议或自动回退到 `safe` 的运行时开仓行为。
6. WHEN 自动回退到 `safe` 的运行时开仓行为 THEN 系统 SHALL 保持原候选池规则和启动时 prompt candidate limit，不得把候选池隐式改为 `safe` 档。
7. SHALL 不修改真实凭证、交易所账户配置或当前禁用 trader 的启用状态。

### Requirement 10: 验证与验收

**User Story:** 作为维护者，我希望每项提高开仓频率的修改都有聚焦测试和离线验证，避免把亏损交易数量也同步放大。

#### Acceptance Criteria

1. SHALL 为新配置字段、默认值和档位派生参数添加单元测试。
2. SHALL 为档位派生参数、自动缩仓、每日开仓上限、24h 回滚和 report-only 诊断添加单元测试。
3. SHALL 为高 ADX 软闸门、RR 阈值和 rolling gate 的 report-only 模拟添加单元测试，不要求默认接入实盘执行。
4. SHALL 增加最近日志 report-only 或 replay 检查，输出“新增可能通过的开仓候选数”和对应拒绝原因变化。
5. Validation SHALL 至少运行 `go test ./config ./decision ./trader ./manager`。
6. IF 修改 API 或前端展示字段 THEN 还 SHALL 运行对应 API 测试和 `cd web && npm run build`。
