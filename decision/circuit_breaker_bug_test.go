package decision

// Feature: circuit-breaker-auto-recovery, Property 1: Bug Condition
// 冷却期内熔断状态保持 + 冷却结束正确重置 + 持久化恢复
//
// CRITICAL: 此测试必须在未修复代码上 FAIL — 失败即确认 Bug 存在
// NOTE: 此测试编码了期望行为 — 修复后通过即验证修复正确性

import (
	"encoding/json"
	"fmt"
	"nofx/market"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// ============================================================================
// 属性 1a — 跨周期状态丢失
// ============================================================================
//
// Bug Condition 伪代码:
//   previousCycleTriggered == true AND currentCtxCircuitBreaker == nil
//
// 期望行为: checkCircuitBreakerState 应识别冷却状态并返回 wait 决策
// 实际行为(Bug): ctx.CircuitBreaker == nil → 直接返回 nil，跳过冷却检查

func TestProperty1a_CrossCycleStateLoss(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	triggerReasons := []string{
		"BTC 1小时暴跌 -6.50%",
		"账户回撤 -12.00% 超过10%",
		"连续亏损 5 次",
		"保证金使用率 92.00% 过高",
	}

	properties.Property("跨周期: 冷却期内应返回 wait 决策", prop.ForAll(
		func(reasonIdx int, cooldownMin int, elapsedMin int) bool {
			reason := triggerReasons[reasonIdx%len(triggerReasons)]

			// 模拟: 上一周期触发了熔断，设置全局状态
			triggerTime := time.Now().Add(-time.Duration(elapsedMin) * time.Minute)
			SetCircuitBreakerState(&CircuitBreakerState{
				IsTriggered:     true,
				TriggerReason:   reason,
				TriggerTime:     triggerTime,
				CooldownMinutes: cooldownMin,
			})
			defer SetCircuitBreakerState(nil) // 清理全局状态

			// 模拟 buildTradingContext 创建新 Context（CircuitBreaker == nil）
			ctx := &Context{
				CircuitBreaker: nil, // 这就是 Bug: 每个周期都是 nil
			}

			// 期望: checkCircuitBreakerState 应该能从全局状态识别冷却状态
			result := checkCircuitBreakerState(ctx)

			if result == nil {
				t.Logf("反例: reason=%s, cooldown=%dmin, elapsed=%dmin → result=nil (Bug: 冷却期内未返回 wait)",
					reason, cooldownMin, elapsedMin)
				return false
			}

			// 验证返回的是 wait 决策
			if len(result.Decisions) == 0 || result.Decisions[0].Action != "wait" {
				return false
			}

			return true
		},
		gen.IntRange(0, 3),    // triggerReason 索引
		gen.IntRange(30, 120), // cooldownMinutes
		gen.IntRange(1, 29),   // elapsedMinutes（在冷却期内）
	))

	properties.TestingRun(t)
}

// ============================================================================
// 属性 1b — 冷却结束计数器重置不充分
// ============================================================================
//
// Bug Condition 伪代码:
//   cooldownExpired == true AND consecutiveLosses >= MaxConsecutiveLosses - 1
//
// 期望行为: 冷却结束后 ConsecutiveLosses 重置为 0
// 实际行为(Bug): ConsecutiveLosses 仅减 1（如 5→4），下次亏损立即再触发

func TestProperty1b_CooldownCounterReset(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	properties.Property("冷却结束: ConsecutiveLosses 应重置为 0", prop.ForAll(
		func(consecutiveLosses int) bool {
			// 模拟: 熔断已触发，冷却已过期
			cooldownMinutes := 30
			triggerTime := time.Now().Add(-time.Duration(cooldownMinutes+1) * time.Minute)

			ctx := &Context{
				CircuitBreaker: &CircuitBreakerState{
					IsTriggered:     true,
					TriggerReason:   fmt.Sprintf("连续亏损 %d 次", consecutiveLosses),
					TriggerTime:     triggerTime,
					CooldownMinutes: cooldownMinutes,
				},
				Account: AccountInfo{
					TotalEquity:   10000,
					TotalPnLPct:   -3.0,  // 未超过 -10% 回撤阈值
					MarginUsedPct: 50.0,   // 未超过 90% 保证金阈值
				},
				// 提供 BTC 数据，PriceChange1h 正常（不触发 BTC 暴跌熔断）
				MarketDataMap: map[string]*market.Data{
					"BTCUSDT": {PriceChange1h: -1.0},
				},
			}

			stats := &TradeStatistics{
				ConsecutiveLosses: consecutiveLosses,
			}

			// 调用 CheckCircuitBreaker，冷却已过期应触发重置
			CheckCircuitBreaker(ctx, stats)

			// 期望: ConsecutiveLosses 重置为 0
			// 实际(Bug): ConsecutiveLosses = consecutiveLosses - 1
			if stats.ConsecutiveLosses != 0 {
				t.Logf("反例: ConsecutiveLosses 初始=%d, 冷却后=%d (期望=0)",
					consecutiveLosses, stats.ConsecutiveLosses)
				return false
			}

			return true
		},
		gen.IntRange(5, 10), // consecutiveLosses (>= MaxConsecutiveLosses)
	))

	properties.TestingRun(t)
}

// ============================================================================
// 属性 1c — 持久化缺失
// ============================================================================
//
// Bug Condition 伪代码:
//   systemRestarted == true AND persistedCircuitBreaker == nil
//
// 期望行为: PersistentData 序列化后应包含 CircuitBreakerState
// 实际行为(Bug): PersistentData 结构体无 CircuitBreaker 字段

func TestProperty1c_PersistenceMissing(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(42)
	properties := gopter.NewProperties(parameters)

	triggerReasons := []string{
		"BTC 1小时暴跌 -6.50%",
		"账户回撤 -12.00% 超过10%",
		"连续亏损 5 次",
		"保证金使用率 92.00% 过高",
	}

	properties.Property("持久化: PersistentData 应包含 CircuitBreakerState", prop.ForAll(
		func(reasonIdx int, cooldownMin int, losses int) bool {
			reason := triggerReasons[reasonIdx%len(triggerReasons)]

			// 构造 PersistentData（含熔断状态）并序列化
			persistentData := PersistentData{
				Plans:      make(map[string]*TradePlan),
				Statistics: &TradeStatistics{ConsecutiveLosses: losses},
				Returns:    []float64{-1.5, 2.0, -0.5},
				CircuitBreaker: &CircuitBreakerState{
					IsTriggered:     true,
					TriggerReason:   reason,
					TriggerTime:     time.Now(),
					CooldownMinutes: cooldownMin,
				},
				UpdatedAt: time.Now(),
			}

			data, err := json.Marshal(persistentData)
			if err != nil {
				t.Logf("序列化失败: %v", err)
				return false
			}

			// 检查 JSON 中是否存在 circuit_breaker 键
			var rawMap map[string]json.RawMessage
			if err := json.Unmarshal(data, &rawMap); err != nil {
				t.Logf("解析 JSON map 失败: %v", err)
				return false
			}

			if _, exists := rawMap["circuit_breaker"]; !exists {
				t.Logf("反例: PersistentData JSON 不包含 circuit_breaker 字段 (reason=%s, cooldown=%d, losses=%d)",
					triggerReasons[reasonIdx%len(triggerReasons)], cooldownMin, losses)
				return false
			}

			return true
		},
		gen.IntRange(0, 3),    // triggerReason 索引
		gen.IntRange(30, 120), // cooldownMinutes
		gen.IntRange(5, 10),   // consecutiveLosses
	))

	properties.TestingRun(t)
}
