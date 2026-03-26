# 需求文档

## 简介

NOFX 是一个基于 AI 驱动的量化交易操作系统，采用 Go 语言后端 + React/TypeScript 前端架构。系统支持多 AI 模型（DeepSeek、Qwen、自定义 OpenAI 兼容 API）在多个交易所（Binance 期货、Hyperliquid、Aster DEX）上进行自动化加密货币合约交易。核心能力包括：多 Agent 竞赛框架、AI 自学习与策略优化、多时间框架市场数据分析、统一风险控制、低延迟执行引擎、以及专业级监控仪表盘。

## 术语表

- **交易系统 (Trading_System)**: NOFX 量化交易操作系统的整体，包含后端服务、前端仪表盘及所有子模块
- **自动交易器 (Auto_Trader)**: 单个 AI 驱动的交易实例，负责完整的交易周期（数据采集→AI决策→订单执行→风险管理）
- **交易管理器 (Trader_Manager)**: 管理多个 Auto_Trader 实例的组件，支持并发运行和统一监控
- **决策引擎 (Decision_Engine)**: AI 决策核心模块，负责构建交易上下文、调用 AI API、解析决策、验证参数
- **交易计划 (Trade_Plan)**: 每笔开仓交易对应的完整计划，包含入场价、止损、止盈、失效条件、分批止盈档位等
- **熔断机制 (Circuit_Breaker)**: 风险保护机制，在极端市场条件或连续亏损时自动暂停交易
- **MCP客户端 (MCP_Client)**: AI API 通信客户端，支持 DeepSeek、Qwen 和自定义 OpenAI 兼容 API
- **币种池 (Coin_Pool)**: 候选交易币种管理模块，支持 AI500 评分池和 OI Top 持仓量增长池的合并去重
- **持仓评估器 (Position_Evaluator)**: 对现有持仓进行多维度评估的组件，包含止损、止盈、移动止损、分批止盈、计划失效检查
- **市场数据模块 (Market_Data)**: 从 Binance API 获取多时间框架 K 线数据并计算技术指标的模块
- **订单追踪器 (Order_Tracker)**: 追踪交易所自动成交订单（止损/止盈触发）的组件
- **决策日志器 (Decision_Logger)**: 记录每个交易周期的完整决策过程，包括输入提示、AI 思维链、执行结果
- **前端仪表盘 (Web_Dashboard)**: React/TypeScript 构建的专业监控界面，提供实时数据展示和交互功能

## 需求

### 需求 1: 系统配置管理

**用户故事:** 作为量化交易系统管理员，我想要通过 JSON 配置文件灵活配置多个交易者实例、交易所连接、AI 模型和风险参数，以便快速部署和调整交易策略。

#### 验收标准

1. THE Trading_System SHALL 从 JSON 配置文件加载所有系统配置，包括交易者列表、杠杆配置、币种池设置和 API 服务器端口
2. WHEN 配置文件中包含多个交易者配置时，THE Trading_System SHALL 为每个启用的交易者创建独立的 Auto_Trader 实例
3. THE Trading_System SHALL 验证每个交易者配置的完整性，包括唯一 ID、名称、AI 模型类型、交易所类型和对应的 API 密钥
4. WHEN AI 模型设置为 "custom" 时，THE Trading_System SHALL 要求配置 custom_api_url、custom_api_key 和 custom_model_name 三个字段
5. WHEN 交易所设置为 "binance" 时，THE Trading_System SHALL 要求配置 binance_api_key 和 binance_secret_key
6. WHEN 交易所设置为 "hyperliquid" 时，THE Trading_System SHALL 要求配置 hyperliquid_private_key
7. WHEN 交易所设置为 "aster" 时，THE Trading_System SHALL 要求配置 aster_user、aster_signer 和 aster_private_key
8. WHEN use_default_coins 未设置且 coin_pool_api_url 为空时，THE Trading_System SHALL 自动启用默认主流币种列表
9. THE Trading_System SHALL 为杠杆配置设置默认值（BTC/ETH 和山寨币均为 5 倍），WHEN 杠杆超过 5 倍时 SHALL 输出警告信息
10. IF 配置文件不存在或格式错误，THEN THE Trading_System SHALL 返回描述性错误信息并终止启动

### 需求 2: 多交易所统一接口

**用户故事:** 作为量化交易开发者，我想要通过统一的交易接口支持多个交易所，以便在不修改核心逻辑的情况下切换交易平台。

#### 验收标准

1. THE Trading_System SHALL 定义统一的 Trader 接口，包含获取余额、获取持仓、开多/开空、平多/平空、设置杠杆、获取市价、设置止损/止盈、取消订单、格式化数量等方法
2. THE Trading_System SHALL 实现 Binance 期货交易器，支持通过 Binance Futures API 执行所有交易操作
3. THE Trading_System SHALL 实现 Hyperliquid 交易器，支持通过以太坊私钥认证和 IOC 限价单模拟市价单
4. THE Trading_System SHALL 实现 Aster DEX 交易器，支持通过 API 钱包签名认证和限价单模拟市价单
5. WHEN 执行开仓操作时，THE Trading_System SHALL 自动处理数量和价格的精度格式化，确保符合各交易所的精度要求
6. WHEN 设置杠杆时，THE Trading_System SHALL 检查当前杠杆是否已为目标值，避免不必要的 API 调用
7. THE Trading_System SHALL 支持独立取消止损单和止盈单，避免调整止损时误删止盈单
8. WHEN 平仓数量参数为 0 时，THE Trading_System SHALL 自动获取当前持仓数量并全部平仓
9. THE Trading_System SHALL 为 Binance 交易器实现 15 秒缓存机制，减少重复 API 调用
10. THE Trading_System SHALL 支持查询订单历史、成交历史和单个订单状态，用于订单追踪功能

### 需求 3: AI 决策引擎

**用户故事:** 作为量化交易系统，我想要通过 AI 模型分析市场数据并生成交易决策，以便实现智能化的自动交易。

#### 验收标准

1. THE Decision_Engine SHALL 在每个交易周期构建完整的交易上下文，包含账户信息、持仓信息、候选币种、市场数据和历史表现
2. WHEN 调用 AI 获取决策时，THE Decision_Engine SHALL 构建包含系统提示和用户提示的消息，系统提示定义交易规则，用户提示包含实时市场数据
3. THE Decision_Engine SHALL 支持解析 AI 返回的 JSON 格式决策，包括标准 JSON 解析、模糊 JSON 解析、文本提取和默认等待决策四种降级策略
4. WHEN AI 返回开仓决策时，THE Decision_Engine SHALL 验证并补充缺失参数（杠杆、仓位大小、止损、止盈、置信度、最小持仓时间）
5. THE Decision_Engine SHALL 验证开仓决策的合法性，包括市场数据可用性、重复持仓检查、风险预算检查、杠杆范围检查、仓位大小检查、止损止盈方向检查和风险回报比检查（最低 2.5:1）
6. WHEN 风险预算已用尽（剩余低于 1%）或持仓已满（3 个）时，THE Decision_Engine SHALL 跳过新机会搜索
7. THE Decision_Engine SHALL 合并持仓评估决策和 AI 新开仓决策，持仓评估决策优先级高于 AI 新开仓决策
8. THE Decision_Engine SHALL 在开仓前执行失效条件预检查，包括失效价格检查和结构化失效条件检查（EMA 交叉、价格突破、RSI、ADX、MACD、趋势反转）
9. WHEN AI API 调用失败时，THE Decision_Engine SHALL 记录警告日志并继续处理已有的持仓评估决策
10. THE Decision_Engine SHALL 控制 AI 调用频率，距离上次分析不足配置间隔时间时跳过 AI 调用

### 需求 4: 持仓评估与止盈止损管理

**用户故事:** 作为量化交易系统，我想要对现有持仓进行多维度智能评估，以便在合适的时机执行止盈、止损、移动止损和分批平仓操作。

#### 验收标准

1. THE Position_Evaluator SHALL 按优先级顺序评估持仓：硬性止损 → 固定止盈 → 最小持仓时间保护 → 利润保护 → ATR 跟踪止盈 → 智能分批止盈 → 移动止损 → 动态止盈调整 → 计划失效条件检查
2. WHEN 当前价格触及止损价时，THE Position_Evaluator SHALL 立即生成平仓决策
3. WHEN 当前价格触及止盈价时，THE Position_Evaluator SHALL 立即生成平仓决策
4. WHILE 持仓时间未达到最小持仓时间（默认 30 分钟），THE Position_Evaluator SHALL 仅在极端亏损（超过 -3%）时才生成平仓决策
5. WHEN 峰值盈利超过利润保护触发线（默认 8%）且当前盈利回落至峰值的保护比例（默认 50%）以下时，THE Position_Evaluator SHALL 生成平仓决策
6. THE Position_Evaluator SHALL 支持基于 ATR 的跟踪止盈，WHEN 盈利超过 5% 且价格从峰值回落超过 ATR 乘数（默认 2.5）时生成平仓决策
7. THE Position_Evaluator SHALL 支持自适应分批止盈，根据风险回报比（RR）和市场趋势强度（ADX）分 4 档执行部分平仓（20%→30%→30%→20%）
8. THE Position_Evaluator SHALL 支持移动止损，根据盈利百分比和 ADX 趋势强度分档锁定利润（15% 锁 30%、20% 锁 50%、30% 锁 70%），并确保止损价与当前价保持 ATR 安全距离
9. THE Position_Evaluator SHALL 支持动态止盈价格调整，根据市场状态（趋势/震荡）、波动率变化和持仓时间衰减因子自动调整止盈目标
10. WHEN 持仓超过 60 分钟时，THE Position_Evaluator SHALL 检查计划失效条件，包括结构化技术指标条件、4H 趋势反转、EMA 交叉和价格失效线

### 需求 5: 风险管理与熔断机制

**用户故事:** 作为量化交易系统管理员，我想要系统具备完善的风险管理和熔断保护机制，以便在极端市场条件下保护账户资金安全。

#### 验收标准

1. THE Trading_System SHALL 限制单笔交易风险不超过账户净值的 2%（MaxRiskPerTrade），总风险预算不超过账户净值的 8%（TotalRiskBudget）
2. THE Trading_System SHALL 限制最大同时持仓数量为 3 个
3. THE Trading_System SHALL 计算每个持仓的精确风险，按优先级使用交易计划止损、持仓止损单、ATR 估算和默认值四种方法
4. WHEN 风险估算不准确时，THE Trading_System SHALL 添加安全边际（每个不准确持仓增加 10%）
5. WHEN BTC 1 小时价格暴跌超过 5% 时，THE Circuit_Breaker SHALL 触发熔断，冷却时间为 120 分钟
6. WHEN 账户回撤超过配置的最大日亏损百分比（默认 10%）时，THE Circuit_Breaker SHALL 触发熔断，冷却时间为 120 分钟
7. WHEN 连续亏损次数达到 5 次时，THE Circuit_Breaker SHALL 触发熔断，冷却时间为 30 分钟
8. WHEN 保证金使用率超过 90% 时，THE Circuit_Breaker SHALL 触发熔断，冷却时间为 30 分钟
9. THE Trading_System SHALL 计算持仓间的 BTC 相关性矩阵，WHEN 相关性超过 0.8 时自动降低仓位权重至 70%
10. THE Trading_System SHALL 支持动态风险调整，根据夏普比率、胜率、连续盈亏次数自动调整单笔风险比例（调整幅度限制在 ±50%）
11. THE Trading_System SHALL 检测市场状态（趋势/震荡/高波动/崩盘），并根据市场状态提供交易建议

### 需求 6: 市场数据采集与技术指标计算

**用户故事:** 作为量化交易系统，我想要从交易所获取多时间框架的市场数据并计算丰富的技术指标，以便为 AI 决策提供全面的市场分析依据。

#### 验收标准

1. THE Market_Data SHALL 并发获取 4 个时间框架的 K 线数据：3 分钟（50 根）、15 分钟（60 根）、1 小时（80 根）和 4 小时（80 根）
2. THE Market_Data SHALL 计算以下技术指标：EMA20、EMA50、MACD（线、信号线、柱状图）、RSI7、RSI14、ATR3、ATR14、ADX14（含 DI+/DI-）、布林带（上轨、下轨、宽度、价格位置）
3. THE Market_Data SHALL 为每个时间框架生成指标序列数据（最近 10 个数据点），用于趋势分析
4. THE Market_Data SHALL 获取持仓量（Open Interest）数据，包括当前值、1 小时变化率和 4 小时变化率
5. THE Market_Data SHALL 获取资金费率数据，并计算年化费率
6. THE Market_Data SHALL 实现 30 秒缓存机制，避免短时间内重复请求相同币种的数据
7. THE Market_Data SHALL 计算 1 小时和 4 小时价格变化百分比
8. THE Market_Data SHALL 计算成交量比率（当前成交量/平均成交量）和 ATR 比率（ATR3/ATR14）
9. THE Market_Data SHALL 提供格式化输出功能，将所有指标数据格式化为结构化文本，供 AI 分析使用
10. THE Market_Data SHALL 提供趋势判断辅助函数，包括 4H 趋势反转检测和市场状态检测

### 需求 7: 币种池管理

**用户故事:** 作为量化交易系统，我想要从多个数据源获取和管理候选交易币种，以便为 AI 提供高质量的交易机会。

#### 验收标准

1. THE Coin_Pool SHALL 支持三种币种来源：默认主流币种列表、AI500 评分 API 和 OI Top 持仓量增长 API
2. WHEN 配置了 AI500 API 时，THE Coin_Pool SHALL 获取评分最高的前 N 个币种（默认 20 个）
3. WHEN 配置了 OI Top API 时，THE Coin_Pool SHALL 获取持仓量增长 Top 20 的币种
4. THE Coin_Pool SHALL 合并 AI500 和 OI Top 的币种列表并去重，记录每个币种的来源信息
5. THE Coin_Pool SHALL 实现 3 次重试机制，每次重试间隔 2 秒
6. IF API 请求全部失败，THEN THE Coin_Pool SHALL 尝试使用本地缓存数据
7. IF 缓存数据也不可用，THEN THE Coin_Pool SHALL 回退到默认主流币种列表
8. THE Coin_Pool SHALL 将成功获取的数据保存到本地 JSON 缓存文件
9. THE Coin_Pool SHALL 标准化币种符号格式（大写 + USDT 后缀）
10. THE Coin_Pool SHALL 过滤 OI 价值低于 15M USD 的非持仓币种，避免低流动性交易

### 需求 8: 交易执行与订单管理

**用户故事:** 作为量化交易系统，我想要安全可靠地执行交易决策，以便确保每笔交易都按照 AI 决策的参数正确执行。

#### 验收标准

1. THE Auto_Trader SHALL 按优先级排序执行决策：先平仓后开仓，防止仓位叠加超限
2. WHEN 执行开仓决策时，THE Auto_Trader SHALL 检查是否已有同币种同方向持仓，如有则拒绝开仓
3. WHEN 执行开仓决策时，THE Auto_Trader SHALL 依次执行：取消旧委托单→设置杠杆→设置保证金模式→计算数量→下单→设置止损→设置止盈→创建交易计划
4. WHEN 执行平仓决策时，THE Auto_Trader SHALL 在平仓后自动取消该币种的所有挂单（清理孤儿止损/止盈单）
5. THE Auto_Trader SHALL 支持部分平仓操作，按指定百分比平仓并更新止损价
6. THE Auto_Trader SHALL 支持更新止损价操作，仅取消旧止损单并设置新止损单，不影响止盈单
7. THE Auto_Trader SHALL 在每个交易周期开始时检测自动成交的订单（止损/止盈触发），并更新统计数据和交易计划
8. WHEN 检测到持仓消失时，THE Auto_Trader SHALL 自动撤销该币种的所有委托单
9. THE Auto_Trader SHALL 在启动时同步现有持仓的交易计划，确保重启后能继续管理已有持仓
10. THE Auto_Trader SHALL 支持每日盈亏重置和风控暂停交易功能

### 需求 9: AI 通信与模型集成

**用户故事:** 作为量化交易系统，我想要支持多种 AI 模型提供商，以便灵活选择和对比不同 AI 模型的交易表现。

#### 验收标准

1. THE MCP_Client SHALL 支持三种 AI 提供商：DeepSeek、Qwen（阿里云）和自定义 OpenAI 兼容 API
2. THE MCP_Client SHALL 使用 system + user 双消息格式调用 AI API，system 消息定义交易规则，user 消息包含实时数据
3. THE MCP_Client SHALL 实现 3 次重试机制，仅对网络错误（超时、连接重置、EOF）进行重试
4. THE MCP_Client SHALL 设置 120 秒超时时间，适应 AI 分析大量数据的需求
5. WHEN 自定义 API URL 以 "#" 结尾时，THE MCP_Client SHALL 使用完整 URL 而不自动添加 /chat/completions 路径
6. THE MCP_Client SHALL 设置 temperature 为 0.5，max_tokens 为 2000，以提高 JSON 格式稳定性
7. IF AI API 密钥未设置，THEN THE MCP_Client SHALL 返回描述性错误信息

### 需求 10: 数据持久化与统计

**用户故事:** 作为量化交易系统，我想要持久化保存交易计划、统计数据和收益率序列，以便系统重启后能恢复状态并持续积累交易数据。

#### 验收标准

1. THE Trading_System SHALL 将交易计划、统计数据、收益率序列和已平仓交易记录持久化到 JSON 文件
2. THE Trading_System SHALL 使用临时文件+重命名的原子写入方式，防止写入过程中断导致数据损坏
3. THE Trading_System SHALL 在交易计划变更、统计更新和收益率记录时自动保存
4. THE Trading_System SHALL 在启动时从持久化文件恢复所有数据
5. THE Trading_System SHALL 计算并维护交易统计指标：总交易数、胜率、平均盈利、平均亏损、盈亏因子、夏普比率、索提诺比率、最大连续亏损、平均持仓时间
6. THE Trading_System SHALL 计算年化夏普比率和索提诺比率，使用 252 天年化因子
7. THE Trading_System SHALL 保留最近 1000 条收益率记录和最近 100 条已平仓交易记录
8. THE Trading_System SHALL 支持数据导出（JSON 格式）和导入功能
9. THE Trading_System SHALL 在优雅退出时强制保存所有数据

### 需求 11: 决策日志与表现分析

**用户故事:** 作为量化交易系统管理员，我想要完整记录每个交易周期的决策过程和交易表现，以便回溯分析和优化策略。

#### 验收标准

1. THE Decision_Logger SHALL 为每个交易周期记录完整的决策记录，包括时间戳、周期编号、输入提示、AI 思维链、决策 JSON、账户快照、持仓快照、候选币种、执行动作和执行日志
2. THE Decision_Logger SHALL 将每条记录保存为独立的 JSON 文件，文件名包含日期时间和周期编号
3. THE Decision_Logger SHALL 支持获取最近 N 条记录（按时间正序排列，用于图表显示）
4. THE Decision_Logger SHALL 支持按日期查询记录
5. THE Decision_Logger SHALL 支持清理 N 天前的旧记录
6. THE Decision_Logger SHALL 提供交易表现分析功能，计算胜率、平均盈利/亏损、盈亏因子、夏普比率和各币种表现统计
7. THE Decision_Logger SHALL 在分析表现时使用扩大 3 倍的窗口预填充开仓记录，避免开仓记录在分析窗口外导致匹配失败
8. THE Decision_Logger SHALL 为每个 Auto_Trader 创建独立的日志目录

### 需求 12: HTTP API 服务

**用户故事:** 作为前端仪表盘开发者，我想要通过 RESTful API 获取系统状态、账户信息、持仓数据和决策日志，以便构建实时监控界面。

#### 验收标准

1. THE Trading_System SHALL 提供基于 Gin 框架的 HTTP API 服务器，支持 CORS 跨域访问
2. THE Trading_System SHALL 提供以下 API 端点：
   - GET /health - 健康检查
   - GET /api/competition - 竞赛总览（对比所有 trader）
   - GET /api/traders - Trader 列表
   - GET /api/status?trader_id=xxx - 系统状态
   - GET /api/account?trader_id=xxx - 账户信息
   - GET /api/positions?trader_id=xxx - 持仓列表
   - GET /api/decisions?trader_id=xxx - 决策日志（全部）
   - GET /api/decisions/latest?trader_id=xxx - 最新决策（最近 5 条）
   - GET /api/statistics?trader_id=xxx - 统计信息
   - GET /api/equity-history?trader_id=xxx - 收益率历史数据
   - GET /api/performance?trader_id=xxx - AI 学习表现分析
3. WHEN trader_id 参数未指定时，THE Trading_System SHALL 默认返回第一个 trader 的数据
4. THE Trading_System SHALL 在收益率历史 API 中基于初始余额计算盈亏百分比
5. THE Trading_System SHALL 在竞赛 API 中返回所有 trader 的净值、盈亏、持仓数和运行状态

### 需求 13: 前端监控仪表盘

**用户故事:** 作为量化交易系统用户，我想要通过专业的 Web 界面实时监控交易状态、查看 AI 决策过程和分析交易表现，以便及时了解系统运行情况。

#### 验收标准

1. THE Web_Dashboard SHALL 采用 React 18 + TypeScript + Vite + Tailwind CSS 技术栈，使用 Binance 风格的深色主题
2. THE Web_Dashboard SHALL 提供竞赛页面，展示所有 trader 的排行榜、实时盈亏对比曲线和正面对决统计
3. THE Web_Dashboard SHALL 提供详情页面，展示选定 trader 的账户概览（净值、可用余额、总盈亏、持仓数）、净值曲线、当前持仓表格和最近决策列表
4. THE Web_Dashboard SHALL 使用 SWR 进行数据获取和缓存，账户数据 15 秒刷新，决策数据 30 秒刷新
5. THE Web_Dashboard SHALL 支持中英文双语切换
6. THE Web_Dashboard SHALL 在净值曲线图表中支持 USDT 绝对值和百分比两种显示模式切换
7. THE Web_Dashboard SHALL 在决策卡片中支持展开/折叠输入提示和 AI 思维链分析
8. THE Web_Dashboard SHALL 提供 AI 学习与反思模块，展示总交易数、胜率、平均盈利/亏损、夏普比率、盈亏因子、最佳/最差币种表现和历史成交记录
9. THE Web_Dashboard SHALL 在对比图表中使用统一的颜色分配逻辑，确保同一 trader 在不同图表中颜色一致
10. THE Web_Dashboard SHALL 限制图表最大显示 2000 个数据点，超出时只显示最近的数据
11. THE Web_Dashboard SHALL 在成交历史模块中展示所有已平仓交易的完整记录，包括交易对、方向、开仓价格、平仓价格、数量、杠杆、持仓价值、已用保证金、盈亏金额和盈亏百分比
12. THE Web_Dashboard SHALL 在成交历史记录中显示每笔交易的开仓时间和平仓时间，时间格式为 "YYYY-MM-DD HH:mm:ss"
13. THE Web_Dashboard SHALL 提供成交历史记录的 CSV 导出功能，WHEN 用户点击导出按钮时，生成包含所有成交历史字段（交易对、方向、开仓时间、平仓时间、开仓价格、平仓价格、数量、杠杆、持仓价值、已用保证金、盈亏金额、盈亏百分比、持仓时长、平仓原因）的 CSV 文件并触发浏览器下载
14. THE Trading_System SHALL 在 /api/performance 端点的响应中为每笔成交记录包含 open_time 和 close_time 字段，数据来源于 ClosedTradeRecord 的 EntryTime 和 ClosedAt 字段
15. WHEN 成交历史记录数量超过 50 条时，THE Web_Dashboard SHALL 支持分页显示，每页默认展示 20 条记录

### 需求 14: 多 Trader 管理与竞赛框架

**用户故事:** 作为量化交易系统管理员，我想要同时运行多个 AI trader 进行竞赛对比，以便评估不同 AI 模型的交易表现。

#### 验收标准

1. THE Trader_Manager SHALL 支持并发管理多个 Auto_Trader 实例，每个实例拥有独立的交易账户、AI 模型和决策日志
2. THE Trader_Manager SHALL 为每个 trader 创建独立的 Order_Tracker 实例
3. THE Trader_Manager SHALL 提供竞赛对比数据 API，返回所有 trader 的净值、盈亏、持仓数、保证金使用率和运行状态
4. THE Trader_Manager SHALL 支持设置自动平仓回调函数，在检测到止损/止盈触发时更新统计数据
5. THE Trader_Manager SHALL 支持启动和停止所有 trader，启动时同时启动订单追踪服务
6. THE Trader_Manager SHALL 支持动态添加和移除 trader
7. THE Trader_Manager SHALL 提供订单追踪摘要 API，返回所有 trader 的追踪订单信息

### 需求 15: 失效条件解析器

**用户故事:** 作为量化交易系统，我想要解析和评估 AI 生成的交易失效条件，以便在市场条件变化时自动使交易计划失效并平仓。

#### 验收标准

1. THE Decision_Engine SHALL 支持解析 9 种结构化失效条件类型：EMA 死叉、EMA 金叉、价格跌破、价格突破、RSI 超买、RSI 超卖、ADX 减弱、MACD 交叉、趋势反转
2. THE Decision_Engine SHALL 支持解析格式化条件字符串（如 "4H:EMA_CROSS_DOWN:EMA20:EMA50"）和自然语言条件（如 "4H EMA20 死叉 EMA50"）
3. THE Decision_Engine SHALL 支持 5 种时间框架：4H、1H、30M、15M、1D
4. WHEN 结构化解析失败时，THE Decision_Engine SHALL 尝试自然语言解析作为降级策略
5. THE Decision_Engine SHALL 提供失效条件的格式化显示功能，将条件转换为人类可读的中文描述
6. FOR ALL 有效的失效条件字符串，解析后格式化再解析 SHALL 产生等价的条件对象（往返属性）

### 需求 16: 系统生命周期管理

**用户故事:** 作为量化交易系统管理员，我想要系统支持优雅启动和关闭，以便确保数据完整性和交易安全。

#### 验收标准

1. THE Trading_System SHALL 在启动时依次执行：加载配置→创建数据目录→初始化决策模块→创建 TraderManager→启动 API 服务器→启动所有 trader
2. THE Trading_System SHALL 监听 SIGINT、SIGTERM 和 SIGQUIT 信号，收到信号后执行优雅退出
3. WHEN 执行优雅退出时，THE Trading_System SHALL 依次停止所有 trader、保存决策数据、打印最终统计，超时时间为 10 秒
4. THE Trading_System SHALL 在启动时打印系统信息（版本、Go 版本、OS、CPU 核心数）和参赛者信息
5. THE Trading_System SHALL 支持通过命令行参数指定配置文件路径，默认使用 config.json
6. THE Trading_System SHALL 支持 Docker 部署，提供独立的后端和前端 Dockerfile 以及 docker-compose 配置
