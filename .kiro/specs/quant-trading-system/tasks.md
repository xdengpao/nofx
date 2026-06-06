# 实施计划: NOFX 量化交易系统

## 概述

本实施计划将设计文档中的架构和 50 条正确性属性转化为可执行的编码任务。任务按模块从底层到上层组织，每个模块包含核心实现和对应的属性基测试（PBT）。后端使用 Go 语言 `github.com/leanovate/gopter` 库进行属性基测试，前端使用 TypeScript。

## 任务

- [x] 1. 项目测试基础设施搭建
  - 在 `go.mod` 中添加 `github.com/leanovate/gopter` 测试依赖
  - 创建 `testutil/generators.go`，定义通用的 gopter 生成器（随机 Config、TraderConfig、Decision、PositionInfo、TradePlan、市场数据等）
  - 创建 `testutil/helpers.go`，定义测试辅助函数（浮点数近似比较、mock 数据构造等）
  - _需求: 全局_

- [x] 2. 配置管理模块 (config/)
  - [x] 2.1 实现配置加载、验证和默认值逻辑的测试覆盖
    - 确保 `LoadConfig` 和 `Validate` 函数的现有实现与需求一致
    - 补充缺失的验证逻辑（如有）
    - _需求: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 1.10_

  - [x] 2.2 属性测试: 配置序列化往返
    - **Property 1: 配置序列化往返**
    - 对任意有效 Config，序列化为 JSON 再反序列化应产生等价结构体
    - **验证: 需求 1.1**

  - [x] 2.3 属性测试: 配置类型必填字段验证
    - **Property 2: 配置类型必填字段验证**
    - 对任意缺少必填字段的 TraderConfig，Validate() 应返回错误
    - **验证: 需求 1.3, 1.4, 1.5, 1.6, 1.7**

  - [x] 2.4 属性测试: 启用交易者计数
    - **Property 3: 启用交易者计数**
    - 对任意包含 M 个 Enabled=true 的配置，应创建恰好 M 个 AutoTrader 实例
    - **验证: 需求 1.2**

  - [x] 2.5 属性测试: 默认币种池自动启用
    - **Property 4: 默认币种池自动启用**
    - 当 UseDefaultCoins=false 且 CoinPoolAPIURL 为空时，加载后 UseDefaultCoins 应为 true
    - **验证: 需求 1.8**

  - [x] 2.6 属性测试: 杠杆默认值
    - **Property 5: 杠杆默认值**
    - 当杠杆值 ≤0 时，验证后应被设置为默认值 5
    - **验证: 需求 1.9**

  - [x] 2.7 属性测试: 无效配置文件拒绝
    - **Property 6: 无效配置文件拒绝**
    - 对任意非法 JSON 或缺少必填字段的内容，LoadConfig 应返回错误
    - **验证: 需求 1.10**

- [x] 3. 检查点 - 配置模块测试通过
  - 确保所有配置模块测试通过，如有问题请咨询用户。

- [x] 4. 币种池管理模块 (pool/)
  - [x] 4.1 实现币种池核心逻辑的测试覆盖
    - 确保 `normalizeSymbol`、`GetTopRatedCoins`、`GetMergedCoinPool` 的实现与需求一致
    - 补充单元测试覆盖重试、缓存降级、默认币种回退逻辑
    - _需求: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7, 7.8, 7.9, 7.10_

  - [x] 4.2 属性测试: 币种评分排序
    - **Property 30: 币种评分排序**
    - 对任意 N > limit 的币种池，GetTopRatedCoins(limit) 应返回恰好 limit 个币种且按评分降序
    - **验证: 需求 7.2**

  - [x] 4.3 属性测试: 币种池合并去重
    - **Property 31: 币种池合并去重**
    - 对任意两个符号列表 A 和 B，合并后 AllSymbols 应包含 A∪B 所有唯一元素
    - **验证: 需求 7.4**

  - [x] 4.4 属性测试: 币种符号标准化
    - **Property 32: 币种符号标准化**
    - 对任意输入字符串，normalizeSymbol 应产生全大写且以 "USDT" 结尾的字符串
    - **验证: 需求 7.9**

- [x] 5. 市场数据模块 (market/)
  - [x] 5.1 实现技术指标计算的测试覆盖
    - 确保 EMA、MACD、RSI、ATR、ADX、布林带计算函数的正确性
    - 确保 `calculateIntradaySeriesEnhanced` 等序列生成函数的正确性
    - 确保 `Format` 格式化输出包含所有关键指标
    - _需求: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7, 6.8, 6.9, 6.10_

  - [x] 5.2 属性测试: 技术指标值域
    - **Property 27: 技术指标值域**
    - 对任意有效 K 线数据，RSI ∈ [0,100]，ATR ≥ 0，EMA > 0
    - **验证: 需求 6.2**

  - [x] 5.3 属性测试: 指标序列长度上限
    - **Property 28: 指标序列长度上限**
    - 对任意足够长的 K 线数据，各指标序列长度应 ≤ 10
    - **验证: 需求 6.3**

  - [x] 5.4 属性测试: 市场数据格式化完整性
    - **Property 29: 市场数据格式化完整性**
    - 对任意有效 market.Data，Format() 应产生包含价格、EMA、MACD、RSI、ADX 关键字的非空字符串
    - **验证: 需求 6.9**

- [x] 6. 检查点 - 基础数据模块测试通过
  - 确保币种池和市场数据模块所有测试通过，如有问题请咨询用户。

- [ ] 7. MCP 客户端模块 (mcp/)
  - [ ] 7.1 实现 MCP 客户端的测试覆盖
    - 确保 `SetCustomAPI` 的 URL 处理逻辑（# 后缀）正确
    - 确保空 API 密钥时 `CallWithMessages` 返回错误
    - 确保重试机制仅对网络错误重试
    - _需求: 9.1, 9.2, 9.3, 9.4, 9.5, 9.6, 9.7_

  - [ ]* 7.2 属性测试: 自定义 API URL 处理
    - **Property 34: 自定义 API URL 处理**
    - 对任意以 "#" 结尾的 URL，SetCustomAPI 后 BaseURL 应去掉 "#" 且 UseFullURL=true；否则 UseFullURL=false
    - **验证: 需求 9.5**

  - [ ]* 7.3 属性测试: 空 API 密钥拒绝
    - **Property 35: 空 API 密钥拒绝**
    - 对任意 APIKey 为空的 Client，CallWithMessages 应返回错误
    - **验证: 需求 9.7**

- [x] 8. AI 响应解析器 (decision/parser.go)
  - [x] 8.1 实现 AI 响应解析器的测试覆盖
    - 确保四级降级解析策略（标准 JSON → 模糊 JSON → 文本提取 → 默认 wait）正确工作
    - 确保 `fixJSON` 能处理尾逗号、无引号 key、单引号、注释等
    - _需求: 3.3_

  - [x] 8.2 属性测试: AI 响应解析鲁棒性
    - **Property 8: AI 响应解析鲁棒性**
    - 对任意非空字符串，ExtractDecisionsRobust 永远不返回 nil 且不 panic
    - **验证: 需求 3.3**

- [x] 9. 失效条件解析器 (decision/parser.go)
  - [x] 9.1 实现失效条件解析器的测试覆盖
    - 确保 9 种结构化条件类型和 5 种时间框架的解析正确
    - 确保自然语言解析降级策略正确
    - 确保 `FormatInvalidationCondition` 格式化输出可读
    - _需求: 15.1, 15.2, 15.3, 15.4, 15.5, 15.6_

  - [x] 9.2 属性测试: 失效条件解析正确性
    - **Property 44: 失效条件解析正确性**
    - 对任意有效格式化条件字符串（9 种类型 × 5 种时间框架），ParseInvalidationCondition 应返回 IsValid=true
    - **验证: 需求 15.1, 15.2, 15.3**

  - [x] 9.3 属性测试: 失效条件格式化可读性
    - **Property 45: 失效条件格式化可读性**
    - 对任意有效失效条件，FormatInvalidationCondition 应产生非空且不包含 "未能解析" 的字符串
    - **验证: 需求 15.5**

  - [x] 9.4 属性测试: 失效条件解析-格式化往返
    - **Property 46: 失效条件解析-格式化往返**
    - 对任意有效条件字符串，解析→格式化→再解析应产生等价条件对象
    - **验证: 需求 15.6**

- [x] 10. 检查点 - 解析器模块测试通过
  - 确保 MCP 客户端、AI 响应解析器和失效条件解析器所有测试通过，如有问题请咨询用户。

- [x] 11. 数据持久化模块 (decision/persistence.go)
  - [x] 11.1 实现持久化和统计逻辑的测试覆盖
    - 确保原子写入（临时文件+重命名）机制正确
    - 确保 `UpdateStatistics` 的胜率、盈亏因子计算正确
    - 确保 `CalculateSharpeRatio` 和 `CalculateSortinoRatio` 公式正确
    - 确保 `AddReturn` 的收益率序列长度上限为 1000
    - 确保 `ExportData` 和 `ImportData` 往返正确
    - _需求: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8, 10.9_

  - [x] 11.2 属性测试: 持久化数据往返
    - **Property 36: 持久化数据往返**
    - 对任意有效 PersistentData，序列化为 JSON 再反序列化应产生等价数据
    - **验证: 需求 10.1, 10.4**

  - [x] 11.3 属性测试: 统计指标正确性
    - **Property 37: 统计指标正确性**
    - 对任意交易结果序列，WinRate = WinningTrades / TotalTrades，ProfitFactor 公式正确
    - **验证: 需求 10.5**

  - [x] 11.4 属性测试: 夏普比率公式正确性
    - **Property 38: 夏普比率公式正确性**
    - 对任意收益率序列（长度 ≥ MinTradesForCalc），夏普比率公式应正确
    - **验证: 需求 10.6**

  - [x] 11.5 属性测试: 收益率序列长度上限
    - **Property 39: 收益率序列长度上限**
    - 对任意数量的 AddReturn 调用，returnsSeries 长度永远不超过 1000
    - **验证: 需求 10.7**

  - [x] 11.6 属性测试: 数据导出导入往返
    - **Property 40: 数据导出导入往返**
    - 对任意系统状态，ExportData 后 ImportData 应恢复等价状态
    - **验证: 需求 10.8**

- [x] 12. 风险管理模块 (decision/risk.go)
  - [x] 12.1 实现风险计算和熔断机制的测试覆盖
    - 确保 `CalculatePositionRisk` 的四级降级风险计算正确
    - 确保 `CalculateTotalRisk` 的安全边际计算正确
    - 确保 `CheckCircuitBreaker` 的四种熔断条件触发正确
    - 确保 `GetAdjustedRisk` 的动态调整范围在 ±50% 内
    - 确保 `CalculateCorrelationMatrix` 的相关性计算和权重调整正确
    - _需求: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 5.8, 5.9, 5.10, 5.11_

  - [x] 12.2 属性测试: 风险计算精确性
    - **Property 23: 风险计算精确性**
    - 对任意持仓和已知止损价，RiskUSD = positionValue × |markPrice - stopLoss| / markPrice
    - **验证: 需求 5.3**

  - [x] 12.3 属性测试: 不准确风险估算安全边际
    - **Property 24: 不准确风险估算安全边际**
    - 对任意包含 N 个不准确估算的持仓集合，总风险应包含 (1 + N×0.1) 安全边际
    - **验证: 需求 5.4**

  - [x] 12.4 属性测试: 熔断条件触发
    - **Property 25: 熔断条件触发**
    - 当 BTC 1h 跌幅 < -5%、账户回撤超限、连续亏损 ≥5、保证金 > 90% 时，应触发熔断
    - **验证: 需求 5.5, 5.6, 5.7, 5.8**

  - [x] 12.5 属性测试: 动态风险调整范围
    - **Property 26: 动态风险调整范围**
    - 对任意统计数据，GetAdjustedRisk 返回值应在 [BaseRisk×0.5, BaseRisk×1.5] 范围内
    - **验证: 需求 5.10**

- [x] 13. 检查点 - 持久化和风险模块测试通过
  - 确保持久化和风险管理模块所有测试通过，如有问题请咨询用户。

- [ ] 14. 持仓评估器模块 (decision/takeprofit.go)
  - [x] 14.1 实现持仓评估器的测试覆盖
    - 确保 `Evaluate` 的优先级链正确（硬性止损 → 固定止盈 → 最小持仓时间保护 → ...）
    - 确保止损/止盈触发逻辑对多头和空头均正确
    - 确保最小持仓时间保护逻辑正确（默认 30 分钟，极端亏损 -3% 例外）
    - 确保利润保护触发逻辑正确（峰值 ≥8%，回落至 50% 以下）
    - 确保移动止损的单调性（只升不降/只降不升）
    - 确保计划失效条件检查时机正确（≥60 分钟才检查）
    - _需求: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8, 4.9, 4.10_

  - [x] 14.2 属性测试: 止损触发立即平仓
    - **Property 15: 止损触发立即平仓**
    - 多头: 当前价格 ≤ 止损价 → action="close"；空头: 当前价格 ≥ 止损价 → action="close"
    - **验证: 需求 4.2**

  - [x] 14.3 属性测试: 止盈触发立即平仓
    - **Property 16: 止盈触发立即平仓**
    - 多头: 当前价格 ≥ 止盈价 → action="close"；空头: 当前价格 ≤ 止盈价 → action="close"
    - **验证: 需求 4.3**

  - [x] 14.4 属性测试: 最小持仓时间保护
    - **Property 17: 最小持仓时间保护**
    - 持仓时间未达最小值且盈亏 > -3% → hold；盈亏 < -3% → close
    - **验证: 需求 4.4**

  - [x] 14.5 属性测试: 利润保护触发
    - **Property 18: 利润保护触发**
    - 峰值盈利 ≥ 8% 且当前盈利 < 峰值×50% → 平仓
    - **验证: 需求 4.5**

  - [x] 14.6 属性测试: 移动止损单调性
    - **Property 19: 移动止损单调性**
    - 多头新止损 ≥ 当前止损；空头新止损 ≤ 当前止损
    - **验证: 需求 4.8**

  - [x] 14.7 属性测试: 计划失效条件检查时机
    - **Property 20: 计划失效条件检查时机**
    - 持仓 < 60 分钟不检查失效条件；≥ 60 分钟且条件触发 → 平仓
    - **验证: 需求 4.10**

- [ ] 15. AI 决策引擎模块 (decision/decision.go)
  - [x] 15.1 实现决策引擎核心逻辑的测试覆盖
    - 确保 `ValidateAndEnrichDecision` 能正确补充缺失参数
    - 确保 `validateOpenDecision` 的所有验证逻辑正确（风险回报比 ≥ 2.5:1、单笔风险上限、重复持仓检查等）
    - 确保 `shouldCallAIForNewOpportunities` 的频率控制和条件判断正确
    - 确保 `mergeDecisions` 的优先级合并逻辑正确
    - 确保 `validateFinalDecisions` 的持仓数量限制正确
    - 确保 `CheckPreOpenInvalidation` 的开仓前失效条件预检查正确
    - _需求: 3.1, 3.2, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10_

  - [x] 15.2 属性测试: 决策参数自动补充
    - **Property 9: 决策参数自动补充**
    - 对任意缺少杠杆/仓位/止损/止盈的开仓决策，补充后所有字段应为正值
    - **验证: 需求 3.4**

  - [x] 15.3 属性测试: 风险回报比验证
    - **Property 10: 风险回报比验证**
    - 当净风险回报比 < 2.5:1 时，validateOpenDecision 应返回错误
    - **验证: 需求 3.5**

  - [x] 15.4 属性测试: 满仓或预算不足时跳过 AI 调用
    - **Property 11: 满仓或预算不足时跳过 AI 调用**
    - 持仓 ≥ 3 或剩余风险预算 ≤ 1% → shouldCallAIForNewOpportunities 返回 false
    - **验证: 需求 3.6**

  - [x] 15.5 属性测试: 决策合并优先级
    - **Property 12: 决策合并优先级**
    - 同一币种的持仓评估决策（非 hold/wait）应优先于 AI 新开仓决策
    - **验证: 需求 3.7**

  - [x] 15.6 属性测试: 开仓前失效条件预检查
    - **Property 13: 开仓前失效条件预检查**
    - 当失效条件已触发时，CheckPreOpenInvalidation 应返回 (true, 非空原因)
    - **验证: 需求 3.8**

  - [x] 15.7 属性测试: AI 调用频率控制
    - **Property 14: AI 调用频率控制**
    - 距离上次分析不足配置间隔时，shouldCallAIForNewOpportunities 应返回 false
    - **验证: 需求 3.10**

  - [x] 15.8 属性测试: 单笔风险上限
    - **Property 21: 单笔风险上限**
    - 持仓风险超过账户净值 2% 时，validateOpenDecision 应返回错误
    - **验证: 需求 5.1**

  - [x] 15.9 属性测试: 最大持仓数量限制
    - **Property 22: 最大持仓数量限制**
    - 现有持仓 + 新开仓 > 3 时，validateFinalDecisions 应返回错误
    - **验证: 需求 5.2**

- [x] 16. 检查点 - 决策引擎和持仓评估器测试通过
  - 确保决策引擎和持仓评估器所有测试通过，如有问题请咨询用户。

- [ ] 17. 统一交易接口模块 (trader/)
  - [x] 17.1 实现交易接口的测试覆盖
    - 确保 `FormatQuantity` 的精度格式化往返正确
    - 确保 `sortDecisionsByPriority` 的排序逻辑正确（平仓优先于开仓）
    - 使用 mock Trader 接口测试 AutoTrader 的决策执行流程
    - _需求: 2.1, 2.5, 2.7, 8.1, 8.2, 8.3, 8.4, 8.5, 8.6_

  - [x] 17.2 属性测试: 数量精度格式化往返
    - **Property 7: 数量精度格式化往返**
    - 对任意正浮点数，FormatQuantity 产生的字符串解析回浮点数后差值不超过 stepSize
    - **验证: 需求 2.5**

  - [x] 17.3 属性测试: 决策执行排序
    - **Property 33: 决策执行排序**
    - 对任意包含开仓和平仓决策的列表，排序后所有平仓决策应在开仓决策之前
    - **验证: 需求 8.1**

- [x] 18. 决策日志模块 (logger/)
  - [x] 18.1 实现决策日志的测试覆盖
    - 确保 `LogDecision` 和 `GetLatestRecords` 的往返正确
    - 确保 `GetLatestRecords` 返回按时间戳升序排列的记录
    - 确保 `AnalyzePerformance` 的胜率计算正确
    - 确保 3 倍窗口预填充开仓记录逻辑正确
    - _需求: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7, 11.8_

  - [x] 18.2 属性测试: 决策日志往返
    - **Property 41: 决策日志往返**
    - 对任意 DecisionRecord，LogDecision 后 GetLatestRecords(1) 应返回包含该记录关键字段的记录
    - **验证: 需求 11.1**

  - [x] 18.3 属性测试: 日志时间正序
    - **Property 42: 日志时间正序**
    - 对任意 N 条按时间顺序记录的日志，GetLatestRecords(N) 应按时间戳升序排列
    - **验证: 需求 11.3**

  - [x] 18.4 属性测试: 表现分析胜率正确性
    - **Property 43: 表现分析胜率正确性**
    - 对任意已知开仓和平仓配对，AnalyzePerformance 的 WinRate = 盈利交易数 / 总交易数 × 100
    - **验证: 需求 11.6**

- [x] 19. 检查点 - 交易接口和日志模块测试通过
  - 确保交易接口和决策日志模块所有测试通过，如有问题请咨询用户。

- [x] 20. 系统集成与生命周期管理
  - [x] 20.1 实现系统入口和生命周期管理的测试覆盖
    - 确保 `main.go` 的启动流程正确（加载配置→创建数据目录→初始化决策模块→创建 TraderManager→启动 API→启动 trader）
    - 确保优雅退出流程正确（停止 trader→保存数据→打印统计，超时 10 秒）
    - 确保信号处理（SIGINT、SIGTERM、SIGQUIT）正确
    - _需求: 16.1, 16.2, 16.3, 16.4, 16.5, 16.6_

  - [x] 20.2 实现 HTTP API 服务的集成测试
    - 使用 httptest 测试所有 11 个 API 端点的响应格式
    - 确保 CORS 中间件正确配置
    - 确保 trader_id 参数缺失时默认返回第一个 trader 的数据
    - _需求: 12.1, 12.2, 12.3, 12.4, 12.5_

  - [x] 20.3 实现多 Trader 管理器的测试覆盖
    - 确保 TraderManager 能并发管理多个 AutoTrader 实例
    - 确保竞赛对比数据 API 返回所有 trader 的状态
    - _需求: 14.1, 14.2, 14.3, 14.4, 14.5, 14.6, 14.7_

- [x] 21. 检查点 - 原有模块全部测试通过
  - 运行 `go test ./...` 确保原有 46 条正确性属性的测试全部通过
  - 如有问题请咨询用户。

- [x] 22. 后端: 成交记录完整字段与 /api/performance 端点增强
  - [x] 22.1 确保 ClosedTradeRecord 数据模型包含 EntryTime 字段
    - 检查 `decision/types.go` 中 `ClosedTradeRecord` 的 `EntryTime` 字段定义
    - 确保 `decision/persistence.go` 中 `OnPositionClosed` 正确从 TradePlan.CreatedAt 填充 EntryTime
    - 确保 `OnPositionClosed` 同时填充 `Quantity`、`Leverage`、`Side` 等完整字段
    - _需求: 13.14_

  - [x] 22.2 确保 AnalyzePerformance 返回的 TradeOutcome 包含完整字段
    - 检查 `logger/decision_logger.go` 中 `AnalyzePerformance` 方法
    - 确保每个 TradeOutcome 包含 symbol、side、quantity、leverage、open_price、close_price、position_value、margin_used、pnl、pnl_pct、duration、open_time、close_time、was_stop_loss 所有字段
    - 确保 open_time 来自开仓动作的时间戳，close_time 来自平仓动作的时间戳
    - _需求: 13.11, 13.14_

  - [x] 22.3 确保 /api/performance 端点响应中 open_time 和 close_time 正确序列化
    - 检查 `api/server.go` 中 `handlePerformance` 端点
    - 确保 TradeOutcome 的 OpenTime 和 CloseTime 以 ISO 8601 格式序列化到 JSON 响应
    - 验证 JSON 输出中 `open_time` 和 `close_time` 字段非空
    - _需求: 13.14_

  - [x] 22.4 属性测试: 成交记录字段完整性
    - **Property 47: 成交记录字段完整性**
    - 对任意包含有效开仓和平仓配对的决策记录集合，AnalyzePerformance 返回的每个 TradeOutcome 应包含非零的 open_time 和 close_time，且 close_time > open_time；同时 symbol、side、quantity、leverage、open_price、close_price、position_value、margin_used 字段均应为非零值
    - **验证: 需求 13.11, 13.14**

- [x] 23. 前端: 成交历史表格完整记录展示
  - [x] 23.1 更新前端 TradeOutcome TypeScript 接口
    - 确保 `web/src/types/index.ts` 或 `web/src/components/AILearning.tsx` 中的 TradeOutcome 接口包含 open_time、close_time、was_stop_loss 字段
    - _需求: 13.11_

  - [x] 23.2 更新 AILearning 组件成交历史表格
    - 修改 `web/src/components/AILearning.tsx` 中的成交历史区域
    - 将卡片式布局改为表格布局，展示所有字段：交易对、方向、开仓时间、平仓时间、开仓价格、平仓价格、数量、杠杆、持仓价值、已用保证金、盈亏金额、盈亏百分比
    - 盈亏金额和百分比使用绿色/红色区分盈利/亏损
    - _需求: 13.11_

  - [x] 23.3 实现时间格式化函数
    - 在 AILearning 组件或 utils 中添加 `formatDateTime` 函数
    - 将 ISO 8601 时间字符串格式化为 `YYYY-MM-DD HH:mm:ss` 格式
    - 开仓时间和平仓时间均使用此格式显示
    - _需求: 13.12_

  - [x] 23.4 属性测试: 时间格式化一致性
    - **Property 50: 时间格式化一致性**
    - 对任意有效的 ISO 8601 时间字符串，formatDateTime 应产生长度为 19 的字符串，包含 `-`、空格和 `:` 分隔符，格式为 `YYYY-MM-DD HH:mm:ss`
    - **验证: 需求 13.12**

- [x] 24. 前端: CSV 导出功能
  - [x] 24.1 实现 exportTradeHistoryCSV 函数
    - 在 `web/src/components/AILearning.tsx` 或 `web/src/utils/` 中实现纯前端 CSV 导出
    - CSV 包含 14 列：交易对、方向、开仓时间、平仓时间、开仓价格、平仓价格、数量、杠杆、持仓价值、已用保证金、盈亏金额、盈亏百分比、持仓时长、平仓原因
    - 使用 `Blob` + `URL.createObjectURL` + 临时 `<a>` 标签触发下载
    - 文件名格式: `trade_history_{traderId}_{YYYYMMDD}.csv`
    - CSV 首行为表头，数据行按平仓时间降序排列
    - 数值字段保留合理精度（价格 4 位小数，盈亏 2 位小数）
    - _需求: 13.13_

  - [x] 24.2 在成交历史区域添加导出按钮
    - 在成交历史表格标题栏添加 CSV 导出按钮
    - 无成交记录时禁用导出按钮
    - 按钮文案支持中英文 i18n
    - _需求: 13.13_

  - [x] 24.3 属性测试: CSV 导出字段完整性
    - **Property 48: CSV 导出字段完整性**
    - 对任意非空的 TradeOutcome 列表，exportTradeHistoryCSV 生成的 CSV 字符串应包含 14 列表头，且数据行数等于输入列表长度
    - **验证: 需求 13.13**

- [x] 25. 前端: 分页功能
  - [x] 25.1 实现成交历史分页逻辑
    - 在 AILearning 组件中添加分页状态: `currentPage`（从 1 开始）、`pageSize`（默认 20）
    - 总页数: `Math.ceil(totalRecords / pageSize)`
    - 当记录数 ≤ 50 时不显示分页控件，直接展示全部记录
    - 当记录数 > 50 时启用分页，表格仅显示当前页的 20 条记录
    - _需求: 13.15_

  - [x] 25.2 实现分页控件 UI
    - 添加上一页/下一页按钮 + 当前页码/总页数显示
    - 第一页时禁用上一页按钮，最后一页时禁用下一页按钮
    - 分页控件样式与 Binance 深色主题一致
    - _需求: 13.15_

  - [x] 25.3 属性测试: 分页正确性
    - **Property 49: 分页正确性**
    - 对任意长度为 N（N > 50）的成交记录列表和页码 P（1 ≤ P ≤ ceil(N/20)），分页后当前页应包含最多 20 条记录，且所有页的记录总数等于 N
    - **验证: 需求 13.15**

- [x] 26. 前端: i18n 翻译更新
  - [x] 26.1 添加成交历史相关的中英文翻译
    - 在 `web/src/i18n/translations.ts` 中添加新的翻译键值对
    - 英文: openTime, closeTime, exportCSV, noTradeData, page, of, previousPage, nextPage, positionValue, marginUsed, pnlAmount, pnlPercent, closedReason 等
    - 中文: 开仓时间, 平仓时间, 导出CSV, 暂无成交数据, 第, 页/共, 上一页, 下一页, 持仓价值, 已用保证金, 盈亏金额, 盈亏百分比, 平仓原因 等
    - _需求: 13.11, 13.12, 13.13, 13.15_

- [x] 27. 检查点 - 成交历史模块功能验证
  - 确保后端 /api/performance 端点返回的 TradeOutcome 包含 open_time 和 close_time
  - 确保前端成交历史表格正确展示所有字段
  - 确保 CSV 导出功能正常工作
  - 确保分页功能在记录数 > 50 时正确启用
  - 确保中英文翻译完整
  - 如有问题请咨询用户。

- [x] 28. 最终检查点 - 全部测试通过
  - 运行 `go test ./...` 确保所有测试通过
  - 确保所有 50 条正确性属性均有对应的属性基测试覆盖
  - 如有问题请咨询用户。

## 备注

- 标记 `*` 的任务为可选的属性基测试任务，可跳过以加速 MVP 开发
- 每个任务引用了具体的需求编号，确保可追溯性
- 检查点任务确保增量验证，及早发现问题
- 属性基测试使用 `github.com/leanovate/gopter` 库，每个属性至少运行 100 次迭代
- 属性基测试注释格式: `// Feature: quant-trading-system, Property N: [属性标题]`
- 单元测试和属性基测试互补：属性测试验证通用不变量，单元测试覆盖具体边界情况
- 前端属性测试（Property 48-50）可使用 Vitest + fast-check 或手动验证逻辑函数
