# 熔断保护自动恢复 Bugfix 设计

## 概述

本设计修复熔断保护机制（CircuitBreaker）的三个核心缺陷：
1. **熔断状态跨周期丢失**：`buildTradingContext()` 每次创建全新 `Context`，`CircuitBreaker` 字段始终为 `nil`，导致 `checkCircuitBreakerState` 无法识别冷却状态
2. **持久化缺失**：`PersistentData` 未包含 `CircuitBreakerState`，系统重启后熔断状态完全丢失
3. **连续亏损计数器重置不充分**：冷却结束时仅将 `ConsecutiveLosses` 减 1（从 5→4），而非重置为 0，导致下一次亏损立即再次触发熔断

修复策略：引入全局熔断状态管理器（内存单例 + 持久化），使熔断状态在交易周期间和系统重启间保持一致。

## 术语表

- **Bug_Condition (C)**：导致熔断无法自动恢复的条件集合——熔断触发后，下一个交易周期因状态丢失而无法识别冷却中状态
- **Property (P)**：冷却期内系统应识别冷却状态并返回等待决策；冷却结束后应正确重置状态并恢复交易
- **Preservation**：未触发熔断时的正常交易流程、熔断首次触发逻辑、统计更新逻辑、现有持久化行为均不受影响
- **CircuitBreakerState**：`decision/types.go` 中定义的熔断状态结构体，包含 `IsTriggered`、`TriggerReason`、`TriggerTime`、`CooldownMinutes` 等字段
- **CheckCircuitBreaker**：`decision/risk.go` 中的熔断检查函数，评估四种熔断条件（BTC暴跌、账户回撤、连续亏损、保证金过高）
- **checkCircuitBreakerState**：`decision/decision.go` 中的冷却状态检查函数，在 `GetFullDecision` 入口处检查是否处于冷却期
- **buildTradingContext**：`trader/auto_trader.go` 中构建交易上下文的函数，每个周期创建新的 `Context`
- **PersistentData**：`decision/types.go` 中的持久化数据结构，当前包含 Plans、Statistics、Returns、ClosedTrades

## Bug 详情

### Bug 条件

Bug 在以下条件同时满足时触发：熔断已被触发（任意四种条件之一），且系统进入下一个交易周期。此时 `buildTradingContext()` 创建的新 `Context` 中 `CircuitBreaker` 为 `nil`，导致：
- `checkCircuitBreakerState` 因 `ctx.CircuitBreaker != nil` 为 `false` 直接返回 `nil`，跳过冷却检查
- `CheckCircuitBreaker` 重新评估熔断条件，若条件仍满足则再次触发，重置冷却计时器

**形式化规约：**
```
FUNCTION isBugCondition(state)
  INPUT: state 包含 {previousCycleTriggered: bool, currentCtxCircuitBreaker: *CircuitBreakerState, cooldownExpired: bool, systemRestarted: bool, persistedCircuitBreaker: *CircuitBreakerState, consecutiveLosses: int}
  OUTPUT: boolean

  // Bug 1: 熔断状态跨周期丢失
  IF state.previousCycleTriggered == true
     AND state.currentCtxCircuitBreaker == nil
  THEN RETURN true

  // Bug 2: 持久化缺失（重启场景）
  IF state.systemRestarted == true
     AND state.previousCycleTriggered == true
     AND state.persistedCircuitBreaker == nil
  THEN RETURN true

  // Bug 3: 连续亏损计数器重置不充分
  IF state.cooldownExpired == true
     AND state.consecutiveLosses >= MaxConsecutiveLosses - 1
  THEN RETURN true

  RETURN false
END FUNCTION
```

### 示例

- **示例 1（跨周期丢失）**：连续亏损 5 次触发熔断（冷却 30 分钟），3 分钟后下一周期 `ctx.CircuitBreaker == nil`，`CheckCircuitBreaker` 发现 `ConsecutiveLosses` 仍为 5，再次触发熔断，冷却计时器重置为 30 分钟。如此循环，冷却永远无法结束。
- **示例 2（持久化缺失）**：BTC 1h 暴跌 6% 触发熔断（冷却 120 分钟），60 分钟后系统重启，`PersistentData` 中无熔断状态，重启后系统不知道正在冷却中，若 BTC 仍在下跌则立即再次触发。
- **示例 3（计数器重置不充分）**：连续亏损 5 次触发熔断，假设冷却正常结束（理想情况），`ConsecutiveLosses` 从 5 减为 4，下一次交易若亏损则 `ConsecutiveLosses` 变为 5，立即再次触发熔断。
- **示例 4（边界情况）**：保证金使用率 91% 触发熔断，冷却 30 分钟后保证金已降至 80%，系统应正常恢复交易。

## 期望行为

### 保留要求

**不变行为：**
- 鼠标/API 触发的正常交易流程（无熔断状态时）必须与修复前完全一致
- 熔断条件首次被满足时的触发逻辑和冷却时间设置必须与修复前一致（BTC暴跌/账户回撤: 120 分钟，连续亏损/保证金过高: 30 分钟）
- `UpdateStatistics` 对盈利/亏损交易的统计更新逻辑不受影响
- 现有 `PersistentData` 中 Plans、Statistics、Returns、ClosedTrades 的保存和加载行为不受影响
- `ConsecutiveLosses` 和 `ConsecutiveWins` 在正常交易中的更新逻辑不受影响

**范围：**
所有不涉及熔断状态管理的输入和流程应完全不受此修复影响，包括：
- 正常交易周期中的持仓评估、AI 调用、决策验证
- 开仓/平仓/部分平仓/更新止损等操作
- 市场数据获取和相关性计算
- 风险预算计算

## 假设根因

基于代码分析，三个 Bug 的根因如下：

1. **熔断状态无全局存储**：`CircuitBreakerState` 仅作为 `Context` 的字段存在，而 `buildTradingContext()` 每次创建新 `Context` 时不设置该字段（默认 `nil`）。没有全局变量或单例来跨周期保持熔断状态。
   - 位置：`trader/auto_trader.go` 的 `buildTradingContext()` 函数
   - 位置：`decision/decision.go` 的 `checkCircuitBreakerState()` 函数

2. **持久化结构缺少熔断字段**：`PersistentData` 结构体（`decision/types.go`）仅包含 `Plans`、`Statistics`、`Returns`、`ClosedTrades`，不包含 `CircuitBreakerState`。`saveToFile()` 和 `loadFromFile()` 均未处理熔断状态。
   - 位置：`decision/types.go` 的 `PersistentData` 结构体
   - 位置：`decision/persistence.go` 的 `saveToFile()` 和 `loadFromFile()` 方法

3. **冷却结束重置逻辑错误**：`CheckCircuitBreaker()` 中冷却结束时执行 `stats.ConsecutiveLosses = stats.ConsecutiveLosses - 1`，仅减 1 而非重置为 0。当 `MaxConsecutiveLosses` 为 5 时，减 1 后为 4，下一次亏损即变为 5 再次触发。
   - 位置：`decision/risk.go` 的 `CheckCircuitBreaker()` 函数，冷却结束分支

4. **`checkCircuitBreakerState` 的空指针短路**：该函数首先检查 `ctx.CircuitBreaker != nil`，由于 `buildTradingContext` 不设置该字段，条件永远为 `false`，冷却检查被完全跳过。
   - 位置：`decision/decision.go` 的 `checkCircuitBreakerState()` 函数

## 正确性属性

Property 1: Bug Condition - 冷却期内熔断状态保持

_For any_ 已触发熔断的状态（`IsTriggered == true` 且冷却未过期），在后续交易周期中调用决策流程时，系统 SHALL 识别冷却状态，返回 `wait` 决策，且不重新评估熔断条件、不重置冷却计时器。`TriggerTime` 和 `CooldownMinutes` 应与首次触发时一致。

**Validates: Requirements 2.1**

Property 2: Bug Condition - 冷却结束后正确重置

_For any_ 已触发熔断的状态（`IsTriggered == true` 且冷却已过期），系统 SHALL 将 `IsTriggered` 重置为 `false`，并将 `ConsecutiveLosses` 重置为 `0`（而非仅减 1），使系统能够正常恢复交易决策流程。

**Validates: Requirements 2.2, 2.3**

Property 3: Bug Condition - 持久化与恢复

_For any_ 已触发熔断的状态，系统 SHALL 将 `CircuitBreakerState` 包含在持久化数据中。系统重启后加载持久化数据时，若冷却未过期，SHALL 恢复冷却状态并继续执行剩余冷却时间；若冷却已过期，SHALL 识别冷却已结束并正常恢复交易。

**Validates: Requirements 2.5, 2.6**

Property 4: Preservation - 正常交易流程不受影响

_For any_ 输入中熔断条件均不满足（BTC 未暴跌、账户回撤正常、连续亏损 <5、保证金使用率 <90%），修复后的系统 SHALL 产生与修复前完全相同的行为：不触发熔断，正常执行 AI 决策流程。

**Validates: Requirements 3.1, 3.3**

Property 5: Preservation - 熔断触发逻辑不变

_For any_ 输入中熔断条件首次被满足（且当前无活跃熔断），修复后的系统 SHALL 与修复前一致地触发熔断，设置相同的冷却时间（BTC暴跌/账户回撤: 120 分钟，连续亏损/保证金过高: 30 分钟）。

**Validates: Requirements 3.2**

Property 6: Preservation - 统计更新逻辑不变

_For any_ 交易结果（盈利或亏损），`UpdateStatistics` 函数 SHALL 与修复前一致地更新 `ConsecutiveLosses`、`ConsecutiveWins`、`WinRate`、`ProfitFactor` 等统计字段。

**Validates: Requirements 3.4, 3.5**

Property 7: Preservation - 现有持久化行为不变

_For any_ 持久化操作，修复后的 `saveToFile` 和 `loadFromFile` SHALL 继续正确保存和恢复 Plans、Statistics、Returns、ClosedTrades 字段，现有数据格式向后兼容。

**Validates: Requirements 3.6**

## 修复实现

### 所需变更

假设根因分析正确：

**文件**: `decision/risk.go`

**函数**: `CheckCircuitBreaker`、新增全局熔断状态管理

**具体变更**:

1. **引入全局熔断状态单例**：在 `decision/risk.go` 中新增全局变量 `globalCircuitBreakerState` 和对应的 `sync.RWMutex`，用于跨周期保持熔断状态。
   - 新增 `var globalCircuitBreakerState *CircuitBreakerState` 和 `var cbStateLock sync.RWMutex`
   - 新增 `GetCircuitBreakerState() *CircuitBreakerState` 函数返回当前状态的深拷贝
   - 新增 `SetCircuitBreakerState(state *CircuitBreakerState)` 函数更新全局状态

2. **修改 `CheckCircuitBreaker` 冷却结束重置逻辑**：将 `stats.ConsecutiveLosses = stats.ConsecutiveLosses - 1` 改为 `stats.ConsecutiveLosses = 0`，确保冷却结束后连续亏损计数器完全重置。

3. **修改 `CheckCircuitBreaker` 使用全局状态**：函数开头从全局状态读取当前熔断信息，触发或重置时同步更新全局状态并触发持久化。

---

**文件**: `decision/types.go`

**结构体**: `PersistentData`

**具体变更**:

4. **扩展 `PersistentData` 结构体**：新增 `CircuitBreaker *CircuitBreakerState` 字段（JSON tag: `"circuit_breaker,omitempty"`），使熔断状态可被持久化。

---

**文件**: `decision/persistence.go`

**函数**: `loadFromFile`、`saveToFile`

**具体变更**:

5. **`saveToFile` 中保存熔断状态**：在构建 `PersistentData` 时，调用 `GetCircuitBreakerState()` 获取当前熔断状态并写入 `CircuitBreaker` 字段。
6. **`loadFromFile` 中恢复熔断状态**：加载 `PersistentData` 后，若包含 `CircuitBreaker` 字段且 `IsTriggered == true`，检查冷却是否已过期：若未过期则调用 `SetCircuitBreakerState` 恢复；若已过期则忽略（状态保持默认未触发）。

---

**文件**: `decision/decision.go`

**函数**: `checkCircuitBreakerState`、`GetFullDecision`

**具体变更**:

7. **修改 `checkCircuitBreakerState`**：不再依赖 `ctx.CircuitBreaker`，改为调用 `GetCircuitBreakerState()` 从全局状态读取。若全局状态显示 `IsTriggered == true` 且冷却未过期，返回包含剩余冷却时间的等待决策。
8. **修改 `GetFullDecision` 中熔断触发后的状态同步**：`CheckCircuitBreaker` 返回触发状态后，调用 `SetCircuitBreakerState` 更新全局状态，确保下一周期可读取。

---

**文件**: `trader/auto_trader.go`

**函数**: `buildTradingContext`

**具体变更**:

9. **在 `buildTradingContext` 中注入全局熔断状态**：构建 `Context` 后，调用 `decision.GetCircuitBreakerState()` 设置 `ctx.CircuitBreaker` 字段，确保后续流程（如日志输出）可访问当前熔断状态。

## 测试策略

### 验证方法

测试策略分两阶段：首先在未修复代码上运行探索性测试以确认 Bug 存在，然后在修复后验证正确性和行为保留。

### 探索性 Bug 条件检查

**目标**：在实施修复前，通过测试用例复现 Bug，确认或否定根因分析。若否定，需重新假设根因。

**测试计划**：构造模拟场景，验证熔断触发后下一周期的行为。在未修复代码上运行，预期观察到 Bug 行为。

**测试用例**:
1. **跨周期状态丢失测试**：触发连续亏损熔断，模拟下一周期创建新 Context，验证 `checkCircuitBreakerState` 是否跳过冷却检查（预期在未修复代码上失败）
2. **冷却计时器重置测试**：触发 BTC 暴跌熔断，模拟冷却期内下一周期，验证 `TriggerTime` 是否被重置（预期在未修复代码上失败）
3. **计数器重置不充分测试**：触发连续亏损熔断，模拟冷却结束，验证 `ConsecutiveLosses` 是否为 0（预期在未修复代码上失败，实际为 4）
4. **持久化缺失测试**：触发熔断后序列化 `PersistentData`，验证是否包含熔断状态（预期在未修复代码上失败）

**预期反例**:
- `checkCircuitBreakerState` 因 `ctx.CircuitBreaker == nil` 返回 `nil`，跳过冷却检查
- `CheckCircuitBreaker` 在冷却期内重新触发熔断，`TriggerTime` 被更新为当前时间
- 冷却结束后 `ConsecutiveLosses` 为 4 而非 0

### Fix 检查

**目标**：验证对于所有满足 Bug 条件的输入，修复后的函数产生期望行为。

**伪代码：**
```
FOR ALL state WHERE isBugCondition(state) DO
  result := GetFullDecision_fixed(state)
  IF state.inCooldown THEN
    ASSERT result.Action == "wait"
    ASSERT result.TriggerTime == state.originalTriggerTime
  END IF
  IF state.cooldownExpired THEN
    ASSERT state.consecutiveLosses == 0
    ASSERT state.isTriggered == false
  END IF
  IF state.systemRestarted AND state.inCooldown THEN
    ASSERT loadedState.isTriggered == true
    ASSERT loadedState.triggerTime == state.originalTriggerTime
  END IF
END FOR
```

### Preservation 检查

**目标**：验证对于所有不满足 Bug 条件的输入，修复后的函数与原始函数产生相同结果。

**伪代码：**
```
FOR ALL state WHERE NOT isBugCondition(state) DO
  ASSERT CheckCircuitBreaker_original(state) == CheckCircuitBreaker_fixed(state)
  ASSERT UpdateStatistics_original(pnl) == UpdateStatistics_fixed(pnl)
  ASSERT saveToFile_original(data) contains same Plans, Statistics, Returns, ClosedTrades as saveToFile_fixed(data)
END FOR
```

**测试方法**：推荐使用属性基测试（Property-Based Testing，使用 `github.com/leanovate/gopter`）进行保留检查，因为：
- 自动生成大量测试用例覆盖输入域
- 捕获手动单元测试可能遗漏的边界情况
- 对非 Bug 输入的行为不变性提供强保证

**测试计划**：先在未修复代码上观察正常输入的行为，然后编写属性基测试捕获该行为。

**测试用例**:
1. **正常交易流程保留**：验证无熔断条件时，`CheckCircuitBreaker` 返回未触发状态，与修复前一致
2. **熔断触发逻辑保留**：验证首次满足熔断条件时，触发逻辑和冷却时间与修复前一致
3. **统计更新保留**：验证 `UpdateStatistics` 对盈利/亏损交易的处理与修复前一致
4. **持久化兼容性保留**：验证修复后的 `saveToFile`/`loadFromFile` 正确处理现有数据格式

### 单元测试

- 测试全局熔断状态管理器的 Get/Set 线程安全性
- 测试 `CheckCircuitBreaker` 冷却期内不重新触发
- 测试 `CheckCircuitBreaker` 冷却结束后 `ConsecutiveLosses` 重置为 0
- 测试 `PersistentData` 序列化/反序列化包含熔断状态
- 测试系统重启后冷却状态恢复（未过期/已过期两种场景）
- 测试 `checkCircuitBreakerState` 从全局状态读取而非依赖 `ctx.CircuitBreaker`

### 属性基测试

- 生成随机熔断状态（触发原因、冷却时间、触发时间），验证冷却期内始终返回 wait 决策且不重置计时器
- 生成随机交易统计（ConsecutiveLosses 0-10），验证冷却结束后 ConsecutiveLosses 始终为 0
- 生成随机市场条件（BTC 价格变化、账户回撤、保证金使用率），验证未触发熔断时行为与修复前一致
- 生成随机持久化数据（含/不含熔断状态），验证加载后状态正确恢复

### 集成测试

- 模拟完整交易周期：触发熔断 → 冷却期内多个周期 → 冷却结束 → 恢复交易
- 模拟系统重启场景：触发熔断 → 序列化 → 反序列化 → 验证冷却继续
- 模拟连续亏损场景：5 次亏损触发熔断 → 冷却结束 → 1 次亏损 → 验证不再触发熔断
