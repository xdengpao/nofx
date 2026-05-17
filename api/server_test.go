package api

// 任务 20.2: HTTP API 服务集成测试
// 覆盖需求: 12.1, 12.2, 12.3, 12.4, 12.5

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"nofx/config"
	"nofx/logger"
	"nofx/manager"
	"nofx/market"
	"nofx/trader"
	"testing"
	"time"
)

// ============================================================================
// 辅助函数
// ============================================================================

// newTestServer 创建一个用于测试的 Server（空 TraderManager）
func newTestServer() *Server {
	tm := manager.NewTraderManager()
	return NewServer(tm, 8080)
}

// newTestServerWithTrader 创建一个包含测试 trader 的 Server
func newTestServerWithTrader(t *testing.T) *Server {
	t.Helper()
	tm := manager.NewTraderManager()

	cfg := config.TraderConfig{
		ID:                  "test-trader-1",
		Name:                "Test Trader",
		AIModel:             "deepseek",
		Exchange:            "binance",
		BinanceAPIKey:       "fake-api-key-for-test",
		BinanceSecretKey:    "fake-secret-key-for-test",
		DeepSeekKey:         "fake-deepseek-key-for-test",
		InitialBalance:      10000.0,
		ScanIntervalMinutes: 3,
	}
	leverage := config.LeverageConfig{BTCETHLeverage: 5, AltcoinLeverage: 5}

	err := tm.AddTrader(cfg, "", 10.0, 20.0, 60, leverage)
	if err != nil {
		t.Fatalf("添加测试 trader 失败: %v", err)
	}

	return NewServer(tm, 8080)
}

func newProgrammaticTestServer(t *testing.T, configure ...func(*config.Config)) *Server {
	t.Helper()
	tm := manager.NewTraderManager()
	cfg := config.TraderConfig{
		ID:                  "programmatic-trader",
		Name:                "Programmatic Trader",
		DecisionMode:        config.DecisionModeProgrammatic,
		Exchange:            "binance",
		BinanceAPIKey:       "fake-api-key-for-test",
		BinanceSecretKey:    "fake-secret-key-for-test",
		InitialBalance:      10000.0,
		ScanIntervalMinutes: 3,
	}
	root := &config.Config{Traders: []config.TraderConfig{cfg}}
	root.Traders[0].ProgrammaticStrategy.Timeframes.Trade = "4h"
	root.Traders[0].ProgrammaticStrategy.State.Path = t.TempDir() + "/state.json"
	for _, fn := range configure {
		fn(root)
	}
	profiles, err := root.NormalizeProgrammaticStrategies()
	if err != nil {
		t.Fatalf("归一化程序化策略失败: %v", err)
	}
	if err := tm.AddTraderWithPolicies(root.Traders[0], "", 10, 20, 60,
		config.LeverageConfig{BTCETHLeverage: 5, AltcoinLeverage: 5},
		config.TradingFrequencyProfile{Legacy: true, Mode: config.TradingFrequencyModeLegacy, EffectiveMode: config.TradingFrequencyModeLegacy, AnalysisIntervalMinutes: 15, PromptCandidateLimit: 8},
		config.StrategyRiskProfile{Legacy: true, RollbackLegacyValidation: true, FeeSlippagePct: 0.002, DefaultMinNetRR: 2.5, ADXTimeframe: "1h"},
		profiles["programmatic-trader"],
	); err != nil {
		t.Fatalf("添加程序化trader失败: %v", err)
	}
	return NewServer(tm, 8080)
}

// doRequest 执行 HTTP 请求并返回 recorder
func doRequest(s *Server, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	return w
}

// parseJSON 解析 JSON 响应体为 map
func parseJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("解析 JSON 响应失败: %v, body=%s", err, w.Body.String())
	}
	return result
}

// parseJSONArray 解析 JSON 响应体为数组
func parseJSONArray(t *testing.T, w *httptest.ResponseRecorder) []interface{} {
	t.Helper()
	var result []interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("解析 JSON 数组响应失败: %v, body=%s", err, w.Body.String())
	}
	return result
}

// ============================================================================
// 需求 12.1: CORS 中间件测试
// ============================================================================

func TestCORS_OptionsRequest(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "OPTIONS", "/health")

	if w.Code != http.StatusOK {
		t.Errorf("OPTIONS /health: 期望状态码 200, 实际=%d", w.Code)
	}

	origin := w.Header().Get("Access-Control-Allow-Origin")
	if origin != "*" {
		t.Errorf("CORS Allow-Origin: 期望 *, 实际=%s", origin)
	}

	methods := w.Header().Get("Access-Control-Allow-Methods")
	if methods == "" {
		t.Error("CORS Allow-Methods 不应为空")
	}

	headers := w.Header().Get("Access-Control-Allow-Headers")
	if headers == "" {
		t.Error("CORS Allow-Headers 不应为空")
	}
}

func TestCORS_HeadersOnGET(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "GET", "/health")

	origin := w.Header().Get("Access-Control-Allow-Origin")
	if origin != "*" {
		t.Errorf("GET 请求也应包含 CORS 头, Allow-Origin=%s", origin)
	}
}

// ============================================================================
// 需求 12.2: 端点 1 - GET /health
// ============================================================================

func TestHealth_ReturnsOK(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "GET", "/health")

	if w.Code != http.StatusOK {
		t.Errorf("GET /health: 期望状态码 200, 实际=%d", w.Code)
	}

	result := parseJSON(t, w)
	if result["status"] != "ok" {
		t.Errorf("GET /health: 期望 status=ok, 实际=%v", result["status"])
	}
}

// ============================================================================
// 需求 12.2: 端点 2 - GET /api/competition
// ============================================================================

func TestCompetition_EmptyManager(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "GET", "/api/competition")

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/competition: 期望状态码 200, 实际=%d", w.Code)
	}

	result := parseJSON(t, w)
	count, ok := result["count"].(float64)
	if !ok || count != 0 {
		t.Errorf("空 manager 的 competition count 应为 0, 实际=%v", result["count"])
	}
}

// ============================================================================
// 需求 12.2: 端点 3 - GET /api/traders
// ============================================================================

func TestTraders_EmptyManager(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "GET", "/api/traders")

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/traders: 期望状态码 200, 实际=%d", w.Code)
	}

	result := parseJSONArray(t, w)
	if len(result) != 0 {
		t.Errorf("空 manager 的 traders 列表应为空, 实际长度=%d", len(result))
	}
}

func TestTraders_WithTrader(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/traders")

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/traders: 期望状态码 200, 实际=%d", w.Code)
	}

	result := parseJSONArray(t, w)
	if len(result) != 1 {
		t.Fatalf("期望 1 个 trader, 实际=%d", len(result))
	}

	traderData := result[0].(map[string]interface{})
	if traderData["trader_id"] != "test-trader-1" {
		t.Errorf("trader_id 应为 test-trader-1, 实际=%v", traderData["trader_id"])
	}
	if traderData["trader_name"] != "Test Trader" {
		t.Errorf("trader_name 应为 Test Trader, 实际=%v", traderData["trader_name"])
	}
	if traderData["ai_model"] != "deepseek" {
		t.Errorf("ai_model 应为 deepseek, 实际=%v", traderData["ai_model"])
	}
}

// ============================================================================
// 需求 12.2: 端点 4 - GET /api/status
// ============================================================================

func TestStatus_NoTraders(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "GET", "/api/status")

	if w.Code != http.StatusBadRequest {
		t.Errorf("GET /api/status (无 trader): 期望状态码 400, 实际=%d", w.Code)
	}
}

func TestStatus_WithTrader(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/status?trader_id=test-trader-1")

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/status: 期望状态码 200, 实际=%d", w.Code)
	}

	result := parseJSON(t, w)
	if result["trader_id"] != "test-trader-1" {
		t.Errorf("status trader_id 应为 test-trader-1, 实际=%v", result["trader_id"])
	}
	if result["trader_name"] != "Test Trader" {
		t.Errorf("status trader_name 应为 Test Trader, 实际=%v", result["trader_name"])
	}
	if _, ok := result["initial_balance"]; !ok {
		t.Error("status 应包含 initial_balance 字段")
	}
}

func TestStatus_InvalidTraderID(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/status?trader_id=nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("GET /api/status (无效 ID): 期望状态码 404, 实际=%d", w.Code)
	}
}

// ============================================================================
// 需求 12.3: trader_id 缺失时默认返回第一个 trader
// ============================================================================

func TestStatus_DefaultsToFirstTrader(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/status")

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/status (无 trader_id): 期望状态码 200, 实际=%d", w.Code)
	}

	result := parseJSON(t, w)
	if result["trader_id"] != "test-trader-1" {
		t.Errorf("默认应返回第一个 trader, 实际 trader_id=%v", result["trader_id"])
	}
}

// ============================================================================
// 需求 12.2: 端点 5 - GET /api/account
// ============================================================================

func TestAccount_NoTraders(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "GET", "/api/account")

	if w.Code != http.StatusBadRequest {
		t.Errorf("GET /api/account (无 trader): 期望状态码 400, 实际=%d", w.Code)
	}
}

func TestAccount_InvalidTraderID(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/account?trader_id=nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("GET /api/account (无效 ID): 期望状态码 404, 实际=%d", w.Code)
	}
}

func TestAccount_WithTrader_ReturnsErrorOrData(t *testing.T) {
	// 使用假密钥的 trader，GetAccountInfo 会调用交易所 API 失败
	// 预期返回 500（交易所 API 错误）
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/account?trader_id=test-trader-1")

	// 假密钥会导致交易所 API 调用失败，返回 500
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusOK {
		t.Errorf("GET /api/account: 期望状态码 500 或 200, 实际=%d", w.Code)
	}

	// 无论成功或失败，响应体应为有效 JSON
	result := parseJSON(t, w)
	if w.Code == http.StatusInternalServerError {
		if _, ok := result["error"]; !ok {
			t.Error("500 响应应包含 error 字段")
		}
	}
}

// ============================================================================
// 需求 12.2: 端点 6 - GET /api/positions
// ============================================================================

func TestPositions_NoTraders(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "GET", "/api/positions")

	if w.Code != http.StatusBadRequest {
		t.Errorf("GET /api/positions (无 trader): 期望状态码 400, 实际=%d", w.Code)
	}
}

func TestPositions_InvalidTraderID(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/positions?trader_id=nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("GET /api/positions (无效 ID): 期望状态码 404, 实际=%d", w.Code)
	}
}

// ============================================================================
// 需求 12.2: 端点 7 - GET /api/decisions
// ============================================================================

func TestDecisions_NoTraders(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "GET", "/api/decisions")

	if w.Code != http.StatusBadRequest {
		t.Errorf("GET /api/decisions (无 trader): 期望状态码 400, 实际=%d", w.Code)
	}
}

func TestDecisions_WithTrader(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/decisions?trader_id=test-trader-1")

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/decisions: 期望状态码 200, 实际=%d", w.Code)
	}

	// 新 trader 没有决策记录，应返回空数组或 null
	body := w.Body.String()
	if body != "null" && body != "[]" {
		var arr []interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &arr); err != nil {
			t.Errorf("GET /api/decisions: 响应应为 JSON 数组或 null, body=%s", body)
		}
	}
}

// ============================================================================
// 需求 12.2: 端点 8 - GET /api/decisions/latest
// ============================================================================

func TestDecisionsLatest_NoTraders(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "GET", "/api/decisions/latest")

	if w.Code != http.StatusBadRequest {
		t.Errorf("GET /api/decisions/latest (无 trader): 期望状态码 400, 实际=%d", w.Code)
	}
}

func TestDecisionsLatest_WithTrader(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/decisions/latest?trader_id=test-trader-1")

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/decisions/latest: 期望状态码 200, 实际=%d", w.Code)
	}
}

func TestFilterDisplayableDecisionRecords_DropsEmptySuccessfulRecords(t *testing.T) {
	legacyEmpty := &logger.DecisionRecord{
		Timestamp:    time.Now().Add(-2 * time.Minute),
		CoTTrace:     "无决策输出",
		Success:      true,
		Decisions:    nil,
		DecisionJSON: "",
	}
	analysisOnly := &logger.DecisionRecord{
		Timestamp:    time.Now().Add(-90 * time.Second),
		CoTTrace:     "## 分析\n有分析但没有决策JSON",
		Success:      true,
		Decisions:    nil,
		DecisionJSON: "",
	}
	failedRecord := &logger.DecisionRecord{
		Timestamp:    time.Now().Add(-75 * time.Second),
		CoTTrace:     "AI调用失败",
		Success:      false,
		ErrorMessage: "AI调用失败",
		Decisions:    nil,
		DecisionJSON: "",
	}
	validWait := &logger.DecisionRecord{
		Timestamp:    time.Now().Add(-1 * time.Minute),
		CoTTrace:     "距离上次AI新机会分析2.8分钟，未满15分钟间隔",
		DecisionJSON: `[{"symbol":"ALL","action":"wait"}]`,
		Decisions: []logger.DecisionAction{{
			Symbol:  "ALL",
			Action:  "wait",
			Success: true,
		}},
		Success: true,
	}

	records := filterDisplayableDecisionRecords([]*logger.DecisionRecord{
		nil,
		legacyEmpty,
		analysisOnly,
		failedRecord,
		validWait,
	})

	if len(records) != 2 {
		t.Fatalf("过滤后应保留 2 条可展示记录，实际=%d", len(records))
	}
	if records[0] != failedRecord || records[1] != validWait {
		t.Fatalf("应保留失败排障记录和带 wait 原因的有效决策")
	}
}

func TestStrategySignalsEmptyReportIncludesTimeframes(t *testing.T) {
	s := newProgrammaticTestServer(t)
	w := doRequest(s, "GET", "/api/strategy/signals?trader_id=programmatic-trader&symbol=ETHUSDT")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/strategy/signals: 期望200，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	if body["trade_timeframe"] != "4h" || body["micro_timeframe"] != "3m" {
		t.Fatalf("空信号报告应返回timeframe元数据: %+v", body)
	}
}

func TestMarketKlinesRejectsUnsupportedTimeframe(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/market/klines?trader_id=test-trader-1&symbol=ETHUSDT&timeframe=5m")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法timeframe应返回400，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	if body["error"] == "" {
		t.Fatalf("应返回中文错误: %+v", body)
	}
}

func TestMarketKlinesUsesProgrammaticHistoryDepthWhenLimitMissing(t *testing.T) {
	var gotSymbol, gotTimeframe string
	var gotLimit int
	var gotClosedOnly bool
	restore := trader.SetMarketKlineFetcherForTest(func(symbol, timeframe string, limit int, closedOnly bool) ([]market.Kline, error) {
		gotSymbol = symbol
		gotTimeframe = timeframe
		gotLimit = limit
		gotClosedOnly = closedOnly
		return fakeMarketKlines(limit), nil
	})
	defer restore()

	s := newProgrammaticTestServer(t, func(root *config.Config) {
		root.Traders[0].ProgrammaticStrategy.Timeframes.Trade = "15m"
		root.Traders[0].ProgrammaticStrategy.HistoryDepth.M15 = 96
	})
	w := doRequest(s, "GET", "/api/market/klines?trader_id=programmatic-trader&symbol=ETHUSDT&timeframe=15m")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/market/klines: 期望200，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	if gotSymbol != "ETHUSDT" || gotTimeframe != "15m" || gotLimit != 96 || !gotClosedOnly {
		t.Fatalf("fetcher参数不符合配置深度: symbol=%s timeframe=%s limit=%d closed=%v", gotSymbol, gotTimeframe, gotLimit, gotClosedOnly)
	}
	if body["limit"] != float64(96) || body["configured_limit"] != float64(96) || body["limit_source"] != "programmatic_history_depth" {
		t.Fatalf("响应应说明使用程序化history_depth: %+v", body)
	}
	if klines, ok := body["klines"].([]interface{}); !ok || len(klines) != 96 {
		t.Fatalf("响应K线数量应等于配置深度: %+v", body["klines"])
	}
}

func TestMarketKlinesExplicitLimitOverridesProgrammaticHistoryDepth(t *testing.T) {
	var gotLimit int
	restore := trader.SetMarketKlineFetcherForTest(func(symbol, timeframe string, limit int, closedOnly bool) ([]market.Kline, error) {
		gotLimit = limit
		return fakeMarketKlines(limit), nil
	})
	defer restore()

	s := newProgrammaticTestServer(t, func(root *config.Config) {
		root.Traders[0].ProgrammaticStrategy.Timeframes.Trade = "15m"
		root.Traders[0].ProgrammaticStrategy.HistoryDepth.M15 = 96
	})
	w := doRequest(s, "GET", "/api/market/klines?trader_id=programmatic-trader&symbol=ETHUSDT&timeframe=15m&limit=12")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/market/klines: 期望200，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	if gotLimit != 12 {
		t.Fatalf("显式query limit应优先: got=%d", gotLimit)
	}
	if body["limit"] != float64(12) || body["configured_limit"] != float64(12) || body["limit_source"] != "query" {
		t.Fatalf("响应应说明使用query limit: %+v", body)
	}
}

func fakeMarketKlines(count int) []market.Kline {
	klines := make([]market.Kline, 0, count)
	base := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	for i := 0; i < count; i++ {
		open := 100 + float64(i)*0.5
		klines = append(klines, market.Kline{
			OpenTime:  base.Add(time.Duration(i) * time.Minute).UnixMilli(),
			CloseTime: base.Add(time.Duration(i+1) * time.Minute).Add(-time.Millisecond).UnixMilli(),
			Open:      open,
			High:      open + 1,
			Low:       open - 1,
			Close:     open + 0.25,
			Volume:    10 + float64(i),
		})
	}
	return klines
}

// ============================================================================
// 需求 12.2: 端点 9 - GET /api/statistics
// ============================================================================

func TestStatistics_NoTraders(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "GET", "/api/statistics")

	if w.Code != http.StatusBadRequest {
		t.Errorf("GET /api/statistics (无 trader): 期望状态码 400, 实际=%d", w.Code)
	}
}

func TestStatistics_WithTrader(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/statistics?trader_id=test-trader-1")

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/statistics: 期望状态码 200, 实际=%d", w.Code)
	}

	result := parseJSON(t, w)
	if _, ok := result["total_cycles"]; !ok {
		t.Error("statistics 应包含 total_cycles 字段")
	}
}

// ============================================================================
// 需求 12.2: 端点 10 - GET /api/equity-history
// ============================================================================

func TestEquityHistory_NoTraders(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "GET", "/api/equity-history")

	if w.Code != http.StatusBadRequest {
		t.Errorf("GET /api/equity-history (无 trader): 期望状态码 400, 实际=%d", w.Code)
	}
}

func TestEquityHistory_WithTrader_NoRecords(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/equity-history?trader_id=test-trader-1")

	// 没有历史记录时，initialBalance 从 status 获取成功，但 records 为空
	// 代码中 initialBalance == 0 && len(records) > 0 的分支不会触发
	// 如果 records 为空且 status 返回 initial_balance，则 initialBalance > 0
	// 但 history 为空，返回 null
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("GET /api/equity-history: 期望状态码 200 或 500, 实际=%d", w.Code)
	}
}

// ============================================================================
// 需求 12.2: 端点 11 - GET /api/performance
// ============================================================================

func TestPerformance_NoTraders(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "GET", "/api/performance")

	if w.Code != http.StatusBadRequest {
		t.Errorf("GET /api/performance (无 trader): 期望状态码 400, 实际=%d", w.Code)
	}
}

func TestPerformance_WithTrader(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/performance?trader_id=test-trader-1")

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/performance: 期望状态码 200, 实际=%d", w.Code)
	}

	result := parseJSON(t, w)
	if _, ok := result["total_trades"]; !ok {
		t.Error("performance 应包含 total_trades 字段")
	}
	if _, ok := result["win_rate"]; !ok {
		t.Error("performance 应包含 win_rate 字段")
	}
	if _, ok := result["recent_trade_events"]; !ok {
		t.Error("performance 应包含 recent_trade_events 字段")
	}
	if _, ok := result["trade_event_stats"]; !ok {
		t.Error("performance 应包含 trade_event_stats 字段")
	}
	if execution, ok := result["execution_quality"].(map[string]interface{}); !ok {
		t.Error("performance 应包含 execution_quality 字段")
	} else {
		for _, field := range []string{"protection_order_failures", "high_risk_execution_failures", "open_rejected_count"} {
			if _, ok := execution[field]; !ok {
				t.Errorf("execution_quality 应包含 %s 字段", field)
			}
		}
	}
}

// ============================================================================
// 需求 12.3: 所有需要 trader_id 的端点默认行为测试
// ============================================================================

func TestDefaultTraderID_Decisions(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/decisions")

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/decisions (无 trader_id): 期望状态码 200, 实际=%d", w.Code)
	}
}

func TestDefaultTraderID_Statistics(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/statistics")

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/statistics (无 trader_id): 期望状态码 200, 实际=%d", w.Code)
	}
}

func TestDefaultTraderID_Performance(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/performance")

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/performance (无 trader_id): 期望状态码 200, 实际=%d", w.Code)
	}
}

func TestDefaultTraderID_DecisionsLatest(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/decisions/latest")

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/decisions/latest (无 trader_id): 期望状态码 200, 实际=%d", w.Code)
	}
}

// ============================================================================
// 需求 12.5: 竞赛 API 包含所有 trader 信息
// ============================================================================

func TestCompetition_WithTrader(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/competition")

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/competition: 期望状态码 200, 实际=%d", w.Code)
	}

	result := parseJSON(t, w)

	// competition 调用 GetAccountInfo 获取账户数据，假密钥会导致失败
	// GetComparisonData 内部 continue 跳过失败的 trader
	traders, ok := result["traders"].([]interface{})
	if !ok {
		t.Fatal("competition 应包含 traders 数组")
	}

	// 如果 GetAccountInfo 成功，验证字段
	if len(traders) > 0 {
		traderData := traders[0].(map[string]interface{})
		requiredFields := []string{"trader_id", "trader_name", "ai_model", "is_running"}
		for _, field := range requiredFields {
			if _, ok := traderData[field]; !ok {
				t.Errorf("competition trader 应包含 %s 字段", field)
			}
		}
	}
}

// ============================================================================
// 端点不存在时返回 404
// ============================================================================

func TestNotFound_UnknownEndpoint(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "GET", "/api/nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("GET /api/nonexistent: 期望状态码 404, 实际=%d", w.Code)
	}
}

// ============================================================================
// /health 支持 Any 方法
// ============================================================================

func TestHealth_POST(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "POST", "/health")

	if w.Code != http.StatusOK {
		t.Errorf("POST /health: 期望状态码 200, 实际=%d", w.Code)
	}

	result := parseJSON(t, w)
	if result["status"] != "ok" {
		t.Errorf("POST /health: 期望 status=ok, 实际=%v", result["status"])
	}
}
