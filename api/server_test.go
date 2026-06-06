package api

// 任务 20.2: HTTP API 服务集成测试
// 覆盖需求: 12.1, 12.2, 12.3, 12.4, 12.5

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"nofx/config"
	"nofx/decision"
	"nofx/drltrain"
	"nofx/historydb"
	"nofx/logger"
	"nofx/manager"
	"nofx/market"
	"nofx/storage"
	"nofx/trader"
	"os"
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

func newDRLTestServer(t *testing.T) *Server {
	t.Helper()
	tm := manager.NewTraderManager()
	modelPath := t.TempDir() + "/ppo.onnx"
	if err := os.WriteFile(modelPath, []byte("stub"), 0o600); err != nil {
		t.Fatalf("创建DRL模型fixture失败: %v", err)
	}
	cfg := config.TraderConfig{
		ID:                  "drl-trader",
		Name:                "DRL Trader",
		DecisionMode:        config.DecisionModeDRL,
		Exchange:            "binance",
		BinanceAPIKey:       "fake-api-key-for-test",
		BinanceSecretKey:    "fake-secret-key-for-test",
		InitialBalance:      10000.0,
		ScanIntervalMinutes: 3,
		DRLStrategy: config.DRLStrategyConfig{
			ModelPath:         modelPath,
			ModelVersion:      "test-v1",
			ObservationWindow: 10,
			Timeframe:         "4h",
			Symbols:           []string{"ETHUSDT"},
		},
	}
	root := &config.Config{Traders: []config.TraderConfig{cfg}}
	if err := root.Validate(); err != nil {
		t.Fatalf("DRL配置校验失败: %v", err)
	}
	if err := tm.AddTrader(root.Traders[0], "", 10, 20, 60, config.LeverageConfig{BTCETHLeverage: 5, AltcoinLeverage: 5}); err != nil {
		t.Fatalf("添加DRL trader失败: %v", err)
	}
	return NewServer(tm, 8080)
}

func newChanlunV2TestServer(t *testing.T, configure ...func(*config.TraderConfig)) *Server {
	t.Helper()
	tm := manager.NewTraderManager()
	cfg := config.TraderConfig{
		ID:                  "chanlun-v2-trader",
		Name:                "Chanlun V2 Trader",
		DecisionMode:        config.DecisionModeChanlunV2,
		Exchange:            "binance",
		BinanceAPIKey:       "fake-api-key-for-test",
		BinanceSecretKey:    "fake-secret-key-for-test",
		InitialBalance:      10000.0,
		ScanIntervalMinutes: 3,
		ChanlunV2Strategy: config.ChanlunV2StrategyConfig{
			Timeframes: map[string]string{"higher": "4h", "trade": "1h", "sub": "15m", "micro": "3m"},
			HistoryDepth: map[string]int{
				"1h": 240,
			},
		},
	}
	for _, fn := range configure {
		fn(&cfg)
	}
	if err := tm.AddTraderWithPolicies(cfg, "", 10, 20, 60,
		config.LeverageConfig{BTCETHLeverage: 5, AltcoinLeverage: 5},
		config.TradingFrequencyProfile{Legacy: true, Mode: config.TradingFrequencyModeLegacy, EffectiveMode: config.TradingFrequencyModeLegacy, AnalysisIntervalMinutes: 15, PromptCandidateLimit: 8},
		config.StrategyRiskProfile{Legacy: true, RollbackLegacyValidation: true, FeeSlippagePct: 0.002, DefaultMinNetRR: 2.5, ADXTimeframe: "1h"},
	); err != nil {
		t.Fatalf("添加缠论V2 trader失败: %v", err)
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

func doJSONRequest(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("序列化请求失败: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
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

func TestBuildEquityHistoryPoints_MarksLegacyMissingCostBasisUnreliable(t *testing.T) {
	records := []*logger.DecisionRecord{{
		Timestamp:   time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		CycleNumber: 1,
		AccountState: logger.AccountSnapshot{
			TotalBalance:          44.48,
			AvailableBalance:      44.47,
			TotalUnrealizedProfit: 34.48,
		},
	}}

	points := buildEquityHistoryPoints(records, 10)
	if len(points) != 1 {
		t.Fatalf("期望1个历史点，实际=%d", len(points))
	}
	if points[0].ReturnReliable {
		t.Fatalf("缺少cost_basis的旧记录不应标记为可靠: %+v", points[0])
	}
	if points[0].TotalPnLPct != 0 {
		t.Fatalf("不可靠旧记录不应发布误导性收益率: %+v", points[0])
	}
	if points[0].CostBasis != 10 {
		t.Fatalf("旧记录仍可保留兜底成本基准用于对账: %+v", points[0])
	}
}

func TestBuildEquityHistoryPoints_UsesBackendCostBasis(t *testing.T) {
	records := []*logger.DecisionRecord{{
		Timestamp:   time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		CycleNumber: 2,
		AccountState: logger.AccountSnapshot{
			TotalBalance:          44.48471397,
			AvailableBalance:      44.47581791,
			TotalUnrealizedProfit: 0.0717623067,
			CostBasis:             44.4129516633,
			StrategyBaseline:      44.4129516633,
			BaselineSource:        "trade_logs_plus_unrealized",
			EquitySource:          "exchange_balance",
		},
	}}

	points := buildEquityHistoryPoints(records, 10)
	if len(points) != 1 || !points[0].ReturnReliable {
		t.Fatalf("带cost_basis的记录应可靠: %+v", points)
	}
	if points[0].CostBasis != 44.4129516633 || points[0].StrategyBaseline != 44.4129516633 {
		t.Fatalf("应使用后端成本基准: %+v", points[0])
	}
	if points[0].TotalPnLPct < 0.16 || points[0].TotalPnLPct > 0.17 {
		t.Fatalf("收益率应按后端cost_basis计算: %+v", points[0])
	}
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

func TestDRLStatusEndpointSuccess(t *testing.T) {
	s := newDRLTestServer(t)
	w := doRequest(s, "GET", "/api/strategy/drl/status?trader_id=drl-trader")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/strategy/drl/status: 期望200，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	if body["trader_id"] != "drl-trader" || body["model_version"] != "test-v1" {
		t.Fatalf("DRL状态响应字段异常: %+v", body)
	}
	if body["model_path"] == "" {
		t.Fatalf("DRL状态应返回model_path: %+v", body)
	}
}

func TestDRLFeaturesEndpointReturnsRawAndNormalizedVectors(t *testing.T) {
	s := newDRLTestServer(t)
	tm := s.traderManager
	drlTrader, err := tm.GetTrader("drl-trader")
	if err != nil {
		t.Fatalf("获取DRL trader失败: %v", err)
	}
	ctx := &decision.Context{
		TraderID:     "drl-trader",
		DecisionMode: config.DecisionModeDRL,
		Account: decision.AccountInfo{
			TotalEquity:      10000,
			AvailableBalance: 8000,
			SizingEquity:     10000,
		},
	}
	restore := drlTrader.SetDRLMarketDataProviderForTest(func(symbol string, opts decision.CyclePreparationOptions) (*market.Data, error) {
		return makeAPITestMarketData(t, symbol, 120), nil
	})
	defer restore()
	if _, err := drlTrader.GetFullDecisionForTest(ctx); err != nil {
		t.Fatalf("生成DRL决策失败: %v", err)
	}

	w := doRequest(s, "GET", "/api/strategy/drl/features?trader_id=drl-trader")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/strategy/drl/features: 期望200，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	if body["trader_id"] != "drl-trader" || body["dimension"].(float64) == 0 {
		t.Fatalf("DRL features基础字段异常: %+v", body)
	}
	raw, ok := body["raw_features"].([]interface{})
	if !ok || len(raw) == 0 {
		t.Fatalf("DRL features应返回raw_features: %+v", body)
	}
	normalized, ok := body["normalized_features"].([]interface{})
	if !ok || len(normalized) != len(raw) {
		t.Fatalf("DRL features应返回同维度normalized_features: raw=%d body=%+v", len(raw), body)
	}
}

func makeAPITestMarketData(t *testing.T, symbol string, count int) *market.Data {
	t.Helper()
	klines := make([]market.Kline, count)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	for i := range klines {
		close := 1000 + float64(i)
		klines[i] = market.Kline{
			OpenTime:  base + int64(i)*60_000,
			Open:      close - 1,
			High:      close + 4,
			Low:       close - 4,
			Close:     close,
			Volume:    100 + float64(i),
			CloseTime: base + int64(i+1)*60_000 - 1,
		}
	}
	data, err := market.BuildDataFromKlines(symbol, market.KlineBundle{
		M3:  klines,
		M15: klines,
		H1:  klines,
		H4:  klines,
	}, market.BuildDataOptions{EnrichmentMode: "disabled"})
	if err != nil {
		t.Fatalf("构造API测试行情失败: %v", err)
	}
	return data
}

func TestDRLStatusEndpointRejectsNonDRLTrader(t *testing.T) {
	s := newTestServerWithTrader(t)
	w := doRequest(s, "GET", "/api/strategy/drl/status?trader_id=test-trader-1")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非DRL trader应返回400，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	if body["error"] != "trader不是DRL策略模式: test-trader-1" {
		t.Fatalf("非DRL trader错误信息不清晰: %+v", body)
	}
}

func TestDRLStatusEndpointMissingTrader(t *testing.T) {
	s := newDRLTestServer(t)
	w := doRequest(s, "GET", "/api/strategy/drl/status?trader_id=missing")
	if w.Code != http.StatusNotFound {
		t.Fatalf("缺失trader应返回404，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	if body["error"] == "" {
		t.Fatalf("缺失trader应返回error字段: %+v", body)
	}
}

func TestDRLMonteCarloEndpoint(t *testing.T) {
	s := newDRLTestServer(t)
	w := doJSONRequest(t, s, "POST", "/api/strategy/drl/backtest/monte-carlo?trader_id=drl-trader", map[string]any{
		"prices":        []float64{100, 101, 102, 103},
		"initial_value": 1000,
		"paths":         16,
		"horizon_steps": 4,
		"seed":          42,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("POST monte-carlo期望200，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	result, ok := body["result"].(map[string]interface{})
	if !ok || result["paths"].(float64) != 16 {
		t.Fatalf("蒙特卡洛响应异常: %+v", body)
	}
}

func TestDRLStressTestEndpoint(t *testing.T) {
	s := newDRLTestServer(t)
	w := doJSONRequest(t, s, "POST", "/api/strategy/drl/backtest/stress-test?trader_id=drl-trader", map[string]any{
		"prices":        []float64{100, 105, 110, 115},
		"initial_value": 1000,
		"delta":         0.2,
		"shock_index":   2,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("POST stress-test期望200，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	result, ok := body["result"].(map[string]interface{})
	if !ok || result["shock_terminal_value"].(float64) >= result["base_terminal_value"].(float64) {
		t.Fatalf("压力测试响应异常: %+v", body)
	}
}

func TestStrategySignalsParsesAuditViewOptions(t *testing.T) {
	s := newProgrammaticTestServer(t)
	w := doRequest(s, "GET", "/api/strategy/signals?trader_id=programmatic-trader&symbol=ETHUSDT&view=audit&layers=structure,preview_signal&statuses=invalidated&limit=25")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/strategy/signals audit: 期望200，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	if body["view"] != "audit" {
		t.Fatalf("应返回audit视图: %+v", body)
	}
	filters, ok := body["filters"].(map[string]interface{})
	if !ok || filters["limit"].(float64) != 25 {
		t.Fatalf("应返回过滤参数: %+v", body)
	}
}

func TestChanlunV2StrategySignalsEmptyReportIncludesTimeframes(t *testing.T) {
	s := newChanlunV2TestServer(t)
	w := doRequest(s, "GET", "/api/strategy/signals?trader_id=chanlun-v2-trader&symbol=ETHUSDT")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/strategy/signals: 期望200，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	if body["decision_mode"] != "chanlun_v2" || body["trade_timeframe"] != "1h" || body["micro_timeframe"] != "3m" {
		t.Fatalf("缠论V2空信号报告应返回模式和timeframe元数据: %+v", body)
	}
	if hash, ok := body["config_hash"].(string); !ok || hash == "" {
		t.Fatalf("缠论V2报告应返回config_hash: %+v", body)
	}
}

func TestChanlunV2StrategySignalsParsesAuditViewOptions(t *testing.T) {
	s := newChanlunV2TestServer(t)
	w := doRequest(s, "GET", "/api/strategy/signals?trader_id=chanlun-v2-trader&symbol=ETHUSDT&view=audit&layers=trade_action&statuses=ready&limit=25")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/strategy/signals audit: 期望200，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	if body["view"] != "audit" {
		t.Fatalf("应返回audit视图: %+v", body)
	}
	filters, ok := body["filters"].(map[string]interface{})
	if !ok || filters["limit"].(float64) != 25 {
		t.Fatalf("应返回过滤参数: %+v", body)
	}
}

func TestChanlunV2StrategySymbolsEmptyListBeforeCycle(t *testing.T) {
	s := newChanlunV2TestServer(t)
	w := doRequest(s, "GET", "/api/strategy/symbols?trader_id=chanlun-v2-trader")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/strategy/symbols: 期望200，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	symbols, ok := body["symbols"].([]interface{})
	if !ok {
		t.Fatalf("尚无策略周期时symbols应为数组而不是null: %+v", body)
	}
	if len(symbols) != 0 {
		t.Fatalf("尚无策略周期时symbols应为空数组: %+v", symbols)
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

func TestMarketKlinesUsesChanlunV2HistoryDepthWhenLimitMissing(t *testing.T) {
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

	s := newChanlunV2TestServer(t, func(cfg *config.TraderConfig) {
		cfg.ChanlunV2Strategy.HistoryDepth["15m"] = 123
	})
	w := doRequest(s, "GET", "/api/market/klines?trader_id=chanlun-v2-trader&symbol=ETHUSDT&timeframe=15m")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/market/klines: 期望200，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	if gotSymbol != "ETHUSDT" || gotTimeframe != "15m" || gotLimit != 123 || !gotClosedOnly {
		t.Fatalf("fetcher参数不符合v2配置深度: symbol=%s timeframe=%s limit=%d closed=%v", gotSymbol, gotTimeframe, gotLimit, gotClosedOnly)
	}
	if body["limit"] != float64(123) || body["configured_limit"] != float64(123) || body["limit_source"] != "chanlun_v2_history_depth" {
		t.Fatalf("响应应说明使用缠论V2 history_depth: %+v", body)
	}
}

func TestMarketKlinesCapsChanlunV2HistoryDepth(t *testing.T) {
	var gotLimit int
	restore := trader.SetMarketKlineFetcherForTest(func(symbol, timeframe string, limit int, closedOnly bool) ([]market.Kline, error) {
		gotLimit = limit
		return fakeMarketKlines(limit), nil
	})
	defer restore()

	s := newChanlunV2TestServer(t, func(cfg *config.TraderConfig) {
		cfg.ChanlunV2Strategy.HistoryDepth["1h"] = trader.MaxMarketKlineLimit + 50
	})
	w := doRequest(s, "GET", "/api/market/klines?trader_id=chanlun-v2-trader&symbol=ETHUSDT&timeframe=1h")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/market/klines: 期望200，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	if gotLimit != trader.MaxMarketKlineLimit {
		t.Fatalf("v2配置深度超过上限时应截断: got=%d", gotLimit)
	}
	if body["limit"] != float64(trader.MaxMarketKlineLimit) ||
		body["configured_limit"] != float64(trader.MaxMarketKlineLimit+50) ||
		body["limit_source"] != "chanlun_v2_history_depth_capped" {
		t.Fatalf("响应应说明缠论V2 history_depth 被截断: %+v", body)
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

func TestBacktestAPIRejectsExternalDBWhenStorageLayoutConfigured(t *testing.T) {
	t.Setenv("NOFX_BACKTEST_API_ENABLED", "true")
	root := t.TempDir()
	layout := storage.NewLayout(storage.RuntimeConfig{Root: root})
	if err := storage.EnsureLayout(layout); err != nil {
		t.Fatalf("初始化测试Storage Layout失败: %v", err)
	}
	s := NewServerWithOptions(manager.NewTraderManager(), 8080, ServerOptions{StorageLayout: &layout})
	outsideDB := t.TempDir() + "/outside.sqlite"

	w := doJSONRequest(t, s, "POST", "/api/backtest/history/gaps", map[string]any{
		"db":        outsideDB,
		"source":    "binance-futures",
		"symbol":    "BTCUSDT",
		"timeframe": "4h",
		"from":      "2025-01-01",
		"to":        "2025-01-02",
		"timezone":  "UTC",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("Root外db应返回400，实际=%d body=%s", w.Code, w.Body.String())
	}
}

func TestStorageDiagnosticsAndDryRunMigration(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(storage.RuntimeConfig{Root: root})
	if err := storage.EnsureLayout(layout); err != nil {
		t.Fatalf("初始化测试Storage Layout失败: %v", err)
	}
	runtime := storage.RuntimeConfig{Root: root, RootSource: storage.RootSourceConfig}
	s := NewServerWithOptions(manager.NewTraderManager(), 8080, ServerOptions{StorageLayout: &layout, StorageRuntime: &runtime})

	w := doRequest(s, "GET", "/api/storage/diagnostics")
	if w.Code != http.StatusOK {
		t.Fatalf("storage diagnostics应返回200，实际=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, s, "POST", "/api/storage/migrations", map[string]any{
		"sources": []string{"coin_pool_cache"},
		"dry_run": true,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("dry-run migration应返回202，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	migrationID, _ := body["migration_id"].(string)
	if migrationID == "" {
		t.Fatalf("migration_id不能为空: %+v", body)
	}
	var status *httptest.ResponseRecorder
	for i := 0; i < 20; i++ {
		status = doRequest(s, "GET", "/api/storage/migrations/"+migrationID)
		if status.Code != http.StatusOK {
			t.Fatalf("查询migration失败: %d body=%s", status.Code, status.Body.String())
		}
		parsed := parseJSON(t, status)
		if parsed["status"] != "running" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("migration dry-run未及时完成，最后响应=%s", status.Body.String())
}

func TestDRLPPOTrainAPIDisabledByDefault(t *testing.T) {
	s := newTestServer()
	w := doRequest(s, "GET", "/api/drl-ppo/health")
	if w.Code != http.StatusNotFound {
		t.Fatalf("训练API默认未启用时应404，实际=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDRLPPOTrainAPIHealthAndCreateJob(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(storage.RuntimeConfig{Root: root})
	if err := storage.EnsureLayout(layout); err != nil {
		t.Fatalf("初始化测试Storage Layout失败: %v", err)
	}
	trainManager, err := drltrain.NewManager(drltrain.Config{
		Layout:         layout,
		PythonBin:      "/bin/echo",
		TrainScript:    fakeDRLTrainScript(t),
		EvaluateScript: "fake-evaluate.py",
		MaxConcurrency: 1,
	})
	if err != nil {
		t.Fatalf("初始化训练manager失败: %v", err)
	}
	s := NewServerWithOptions(manager.NewTraderManager(), 8080, ServerOptions{StorageLayout: &layout, DRLTrain: trainManager})

	w := doRequest(s, "GET", "/api/drl-ppo/health")
	if w.Code != http.StatusOK {
		t.Fatalf("训练health应返回200，实际=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, s, "POST", "/api/drl-ppo/jobs", map[string]any{
		"symbol":                "BTCUSDT",
		"timeframe":             "4h",
		"start":                 "2025-01-01",
		"end":                   "2025-02-01",
		"total_timesteps":       1000,
		"observation_window":    60,
		"initial_balance":       10000,
		"taker_fee":             0.0005,
		"maker_fee":             0.0002,
		"slippage":              0.0003,
		"output_model_name":     "api_test_model",
		"allow_incomplete_data": true,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("创建训练job应返回202，实际=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDRLPPOTrainAPIMarketsUsesCachedCatalog(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(storage.RuntimeConfig{Root: root})
	if err := storage.EnsureLayout(layout); err != nil {
		t.Fatalf("初始化测试Storage Layout失败: %v", err)
	}
	trainManager, err := drltrain.NewManager(drltrain.Config{
		Layout:         layout,
		PythonBin:      "/bin/echo",
		TrainScript:    fakeDRLTrainScript(t),
		EvaluateScript: "fake-evaluate.py",
		MaxConcurrency: 1,
	})
	if err != nil {
		t.Fatalf("初始化训练manager失败: %v", err)
	}
	s := NewServerWithOptions(manager.NewTraderManager(), 8080, ServerOptions{StorageLayout: &layout, DRLTrain: trainManager})

	drlPPOMarketCatalogCache.mu.Lock()
	oldValue := drlPPOMarketCatalogCache.value
	oldExpires := drlPPOMarketCatalogCache.expiresAt
	drlPPOMarketCatalogCache.value = historydb.MarketCatalog{
		SyncedAt: time.Now().UTC(),
		Sources: []historydb.MarketSource{{
			Source:      "binance-futures",
			DisplayName: "Binance USD-M Futures",
			Exchange:    "binance",
			Timeframes:  []string{"1h", "4h"},
			Symbols:     []historydb.MarketSymbol{{Symbol: "BTCUSDT", BaseAsset: "BTC", QuoteAsset: "USDT"}},
			SyncedAt:    time.Now().UTC(),
		}},
	}
	drlPPOMarketCatalogCache.expiresAt = time.Now().Add(time.Hour)
	drlPPOMarketCatalogCache.mu.Unlock()
	defer func() {
		drlPPOMarketCatalogCache.mu.Lock()
		drlPPOMarketCatalogCache.value = oldValue
		drlPPOMarketCatalogCache.expiresAt = oldExpires
		drlPPOMarketCatalogCache.mu.Unlock()
	}()

	w := doRequest(s, "GET", "/api/drl-ppo/markets")
	if w.Code != http.StatusOK {
		t.Fatalf("markets应返回200，实际=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	sources, ok := body["sources"].([]interface{})
	if !ok || len(sources) != 1 {
		t.Fatalf("sources响应异常: %+v", body)
	}
}

func fakeDRLTrainScript(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/fake-train.py"
	if err := os.WriteFile(path, []byte("# fake train script\n"), 0o644); err != nil {
		t.Fatalf("写fake train script失败: %v", err)
	}
	return path
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
