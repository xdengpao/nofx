# 实现计划

- [x] 1. 编写 Bug 条件探索性测试（修复前运行）
  - **Property 1: Bug Condition** - 冷却期内熔断状态保持 + 冷却结束正确重置 + 持久化恢复
  - **CRITICAL**: 此测试必须在未修复代码上 FAIL — 失败即确认 Bug 存在
  - **DO NOT** 在测试失败时尝试修复测试或代码
  - **NOTE**: 此测试编码了期望行为 — 修复后通过即验证修复正确性
  - **GOAL**: 通过反例证明 Bug 存在，理解根因
  - **Scoped PBT Approach**: 针对三个确定性 Bug 场景分别构造属性
  - 测试文件: `decision/circuit_breaker_bug_test.go`
  - 使用 `gopter` 属性基测试框架
  - **属性 1a — 跨周期状态丢失**: 生成随机熔断状态（触发原因、冷却时间 30-120 分钟、触发时间在冷却期内），模拟 `buildTradingContext` 创建新 Context（`CircuitBreaker == nil`），调用 `checkCircuitBreakerState`，断言应返回 wait 决策（从 Bug Condition 伪代码: `previousCycleTriggered == true AND currentCtxCircuitBreaker == nil`）
  - **属性 1b — 冷却结束计数器重置**: 生成随机 `ConsecutiveLosses` (5-10)，模拟冷却已过期，调用 `CheckCircuitBreaker`，断言 `ConsecutiveLosses` 应重置为 0（从 Bug Condition 伪代码: `cooldownExpired == true AND consecutiveLosses >= MaxConsecutiveLosses - 1`）
  - **属性 1c — 持久化缺失**: 触发熔断后序列化 `PersistentData`，断言反序列化后应包含 `CircuitBreakerState`（从 Bug Condition 伪代码: `systemRestarted == true AND persistedCircuitBreaker == nil`）
  - 在未修复代码上运行测试
  - **EXPECTED OUTCOME**: 测试 FAIL（确认 Bug 存在）
  - 记录反例: 如 `checkCircuitBreakerState` 因 `ctx.CircuitBreaker == nil` 返回 `nil`；`ConsecutiveLosses` 冷却后为 4 而非 0；`PersistentData` 不含熔断状态
  - 测试写完、运行完、失败已记录后标记任务完成
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6_

- [x] 2. 编写保留性属性测试（修复前运行）
  - **Property 2: Preservation** - 正常交易流程 + 熔断触发逻辑 + 统计更新 + 持久化兼容性
  - **IMPORTANT**: 遵循观察优先方法论
  - 测试文件: `decision/circuit_breaker_preservation_test.go`
  - 使用 `gopter` 属性基测试框架
  - **观察阶段**（在未修复代码上运行）:
    - 观察: 无熔断条件时 `CheckCircuitBreaker` 返回 `IsTriggered == false`
    - 观察: BTC 1h 跌 >5% 首次触发时 `CooldownMinutes == 120`
    - 观察: 连续亏损 >=5 首次触发时 `CooldownMinutes == 30`
    - 观察: `UpdateStatistics` 盈利时 `ConsecutiveLosses = 0, ConsecutiveWins++`
    - 观察: `UpdateStatistics` 亏损时 `ConsecutiveLosses++, ConsecutiveWins = 0`
    - 观察: `saveToFile`/`loadFromFile` 正确保存恢复 Plans、Statistics、Returns、ClosedTrades
  - **属性 2a — 正常交易流程不受影响**: 生成随机市场条件（BTC 价格变化 > -5%、账户回撤 > -10%、连续亏损 0-4、保证金使用率 0-89%），验证 `CheckCircuitBreaker` 返回 `IsTriggered == false`（Property 4）
  - **属性 2b — 熔断触发逻辑不变**: 生成随机首次触发场景（BTC 暴跌/账户回撤/连续亏损/保证金过高），验证触发后 `IsTriggered == true` 且冷却时间正确（BTC暴跌/账户回撤: 120 分钟，连续亏损/保证金过高: 30 分钟）（Property 5）
  - **属性 2c — 统计更新逻辑不变**: 生成随机交易结果（盈亏百分比 -50% 到 +100%），验证 `UpdateStatistics` 正确更新 `ConsecutiveLosses`、`ConsecutiveWins`、`WinRate`、`ProfitFactor`（Property 6）
  - **属性 2d — 现有持久化行为不变**: 生成随机 `PersistentData`（含 Plans、Statistics、Returns、ClosedTrades），验证 `saveToFile` 后 `loadFromFile` 正确恢复所有字段（Property 7）
  - 在未修复代码上运行测试
  - **EXPECTED OUTCOME**: 测试 PASS（确认基线行为）
  - 测试写完、运行完、通过后标记任务完成
  - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6_

- [x] 3. 实现熔断保护自动恢复修复

  - [x] 3.1 引入全局熔断状态管理器（`decision/risk.go`）
    - 新增 `var globalCircuitBreakerState *CircuitBreakerState` 和 `var cbStateLock sync.RWMutex`
    - 新增 `GetCircuitBreakerState() *CircuitBreakerState` 返回深拷贝
    - 新增 `SetCircuitBreakerState(state *CircuitBreakerState)` 更新全局状态
    - _Bug_Condition: isBugCondition(state) where state.previousCycleTriggered == true AND state.currentCtxCircuitBreaker == nil_
    - _Expected_Behavior: 熔断状态跨周期保持，冷却期内返回 wait 决策_
    - _Preservation: 不影响现有 CheckCircuitBreaker 触发逻辑_
    - _Requirements: 2.1_

  - [x] 3.2 修改 `CheckCircuitBreaker` 冷却结束重置逻辑（`decision/risk.go`）
    - 将 `stats.ConsecutiveLosses = stats.ConsecutiveLosses - 1` 改为 `stats.ConsecutiveLosses = 0`
    - 触发或重置时调用 `SetCircuitBreakerState` 同步全局状态并触发持久化
    - _Bug_Condition: isBugCondition(state) where state.cooldownExpired == true AND state.consecutiveLosses >= MaxConsecutiveLosses - 1_
    - _Expected_Behavior: 冷却结束后 ConsecutiveLosses 重置为 0，IsTriggered 重置为 false_
    - _Preservation: 首次触发逻辑和冷却时间不变_
    - _Requirements: 2.2, 2.3, 2.4_

  - [x] 3.3 扩展 `PersistentData` 结构体（`decision/types.go`）
    - 新增 `CircuitBreaker *CircuitBreakerState` 字段（JSON tag: `"circuit_breaker,omitempty"`）
    - _Bug_Condition: isBugCondition(state) where state.systemRestarted == true AND state.persistedCircuitBreaker == nil_
    - _Expected_Behavior: PersistentData 包含熔断状态字段_
    - _Preservation: 现有字段 Plans、Statistics、Returns、ClosedTrades 不受影响，向后兼容_
    - _Requirements: 2.5, 2.6_

  - [x] 3.4 修改 `saveToFile` 和 `loadFromFile`（`decision/persistence.go`）
    - `saveToFile`: 调用 `GetCircuitBreakerState()` 获取当前熔断状态写入 `CircuitBreaker` 字段
    - `loadFromFile`: 加载后若 `CircuitBreaker` 不为 nil 且 `IsTriggered == true`，检查冷却是否过期：未过期则调用 `SetCircuitBreakerState` 恢复；已过期则忽略
    - _Bug_Condition: isBugCondition(state) where state.systemRestarted == true AND state.persistedCircuitBreaker == nil_
    - _Expected_Behavior: 重启后冷却状态正确恢复或正确识别已过期_
    - _Preservation: 现有 Plans、Statistics、Returns、ClosedTrades 保存加载行为不变_
    - _Requirements: 2.5, 2.6, 3.6_

  - [x] 3.5 修改 `checkCircuitBreakerState`（`decision/decision.go`）
    - 不再依赖 `ctx.CircuitBreaker`，改为调用 `GetCircuitBreakerState()` 从全局状态读取
    - 若全局状态 `IsTriggered == true` 且冷却未过期，返回包含剩余冷却时间的 wait 决策
    - _Bug_Condition: isBugCondition(state) where state.previousCycleTriggered == true AND state.currentCtxCircuitBreaker == nil_
    - _Expected_Behavior: 从全局状态读取熔断信息，冷却期内返回 wait 决策_
    - _Preservation: 冷却已过期时正常放行，不影响后续决策流程_
    - _Requirements: 2.1_

  - [x] 3.6 修改 `GetFullDecision` 中熔断触发后状态同步（`decision/decision.go`）
    - `CheckCircuitBreaker` 返回触发状态后，调用 `SetCircuitBreakerState` 更新全局状态
    - _Expected_Behavior: 触发后全局状态立即更新，下一周期可读取_
    - _Preservation: 不影响 AI 决策调用和持仓评估流程_
    - _Requirements: 2.1, 2.2_

  - [x] 3.7 在 `buildTradingContext` 中注入全局熔断状态（`trader/auto_trader.go`）
    - 构建 Context 后调用 `decision.GetCircuitBreakerState()` 设置 `ctx.CircuitBreaker`
    - _Expected_Behavior: 每个周期的 Context 包含当前熔断状态_
    - _Preservation: 不影响账户信息、持仓信息、候选币种等其他上下文构建_
    - _Requirements: 2.1_

  - [x] 3.8 验证 Bug 条件探索性测试现在通过
    - **Property 1: Expected Behavior** - 冷却期内熔断状态保持 + 冷却结束正确重置 + 持久化恢复
    - **IMPORTANT**: 重新运行任务 1 中的同一测试 — 不要编写新测试
    - 任务 1 的测试编码了期望行为，通过即确认修复正确
    - 运行 `decision/circuit_breaker_bug_test.go`
    - **EXPECTED OUTCOME**: 测试 PASS（确认 Bug 已修复）
    - _Requirements: 2.1, 2.2, 2.3, 2.5, 2.6_

  - [x] 3.9 验证保留性属性测试仍然通过
    - **Property 2: Preservation** - 正常交易流程 + 熔断触发逻辑 + 统计更新 + 持久化兼容性
    - **IMPORTANT**: 重新运行任务 2 中的同一测试 — 不要编写新测试
    - 运行 `decision/circuit_breaker_preservation_test.go`
    - **EXPECTED OUTCOME**: 测试 PASS（确认无回归）
    - 确认修复后所有保留性测试仍然通过
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6_

- [x] 4. 检查点 — 确保所有测试通过
  - 运行全部测试: `go test ./decision/... -v -run "CircuitBreaker"`
  - 确保 Bug 条件测试（Property 1）通过
  - 确保保留性测试（Property 2）通过
  - 确保现有测试无回归
  - 如有问题，询问用户
