package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type exchangeInfo struct {
	Symbols []exchangeSymbol `json:"symbols"`
}

type exchangeSymbol struct {
	Symbol  string                   `json:"symbol"`
	Status  string                   `json:"status"`
	Filters []map[string]interface{} `json:"filters"`
}

type calibrationReport struct {
	GeneratedAt             time.Time           `json:"generated_at"`
	Exchange                string              `json:"exchange"`
	SourceURL               string              `json:"source_url,omitempty"`
	SystemMinOrderValueUSDT float64             `json:"system_min_order_value_usdt"`
	SystemPartialCloseUSDT  float64             `json:"system_partial_close_min_usdt"`
	Symbols                 []symbolCalibration `json:"symbols"`
}

type symbolCalibration struct {
	Symbol                       string   `json:"symbol"`
	Status                       string   `json:"status,omitempty"`
	StepSize                     string   `json:"step_size,omitempty"`
	TickSize                     string   `json:"tick_size,omitempty"`
	MinQty                       string   `json:"min_qty,omitempty"`
	MinNotional                  float64  `json:"min_notional_usdt,omitempty"`
	RecommendedMinOrderValueUSDT float64  `json:"recommended_min_order_value_usdt"`
	RecommendedPartialCloseUSDT  float64  `json:"recommended_partial_close_min_usdt"`
	Findings                     []string `json:"findings,omitempty"`
}

func main() {
	exchange := flag.String("exchange", "aster", "exchange name: aster or binance")
	sourceURL := flag.String("source-url", "", "optional exchangeInfo URL")
	inputFile := flag.String("input", "", "optional local exchangeInfo JSON file")
	output := flag.String("output", "", "optional output JSON path")
	symbolsText := flag.String("symbols", "BTCUSDT,ETHUSDT,SOLUSDT,BNBUSDT,XRPUSDT,DOGEUSDT,ASTERUSDT,HYPEUSDT", "comma separated symbols")
	systemMinOrder := flag.Float64("system-min-order", 10, "system preflight min order value in USDT")
	systemPartialMin := flag.Float64("system-partial-min", 5, "system partial close min value in USDT")
	timeout := flag.Duration("timeout", 15*time.Second, "HTTP timeout")
	flag.Parse()

	url := strings.TrimSpace(*sourceURL)
	if url == "" {
		url = defaultExchangeInfoURL(*exchange)
	}

	data, err := loadExchangeInfo(url, *inputFile, *timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "外部校准失败: %v\n", err)
		os.Exit(1)
	}

	report, err := buildCalibrationReport(*exchange, url, data, splitSymbols(*symbolsText), *systemMinOrder, *systemPartialMin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "生成校准报告失败: %v\n", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "序列化校准报告失败: %v\n", err)
		os.Exit(1)
	}
	if *output != "" {
		if err := os.WriteFile(*output, out, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入校准报告失败: %v\n", err)
			os.Exit(1)
		}
		return
	}
	fmt.Println(string(out))
}

func defaultExchangeInfoURL(exchange string) string {
	switch strings.ToLower(strings.TrimSpace(exchange)) {
	case "binance":
		return "https://fapi.binance.com/fapi/v1/exchangeInfo"
	default:
		return "https://fapi.asterdex.com/fapi/v3/exchangeInfo"
	}
}

func loadExchangeInfo(url, inputFile string, timeout time.Duration) ([]byte, error) {
	if inputFile != "" {
		return os.ReadFile(inputFile)
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("请求exchangeInfo失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("exchangeInfo HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

func buildCalibrationReport(exchange, sourceURL string, data []byte, symbols []string, systemMinOrder, systemPartialMin float64) (calibrationReport, error) {
	var info exchangeInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return calibrationReport{}, fmt.Errorf("解析exchangeInfo失败: %w", err)
	}
	index := make(map[string]exchangeSymbol)
	for _, s := range info.Symbols {
		index[strings.ToUpper(s.Symbol)] = s
	}

	report := calibrationReport{
		GeneratedAt:             time.Now(),
		Exchange:                strings.ToLower(strings.TrimSpace(exchange)),
		SourceURL:               sourceURL,
		SystemMinOrderValueUSDT: systemMinOrder,
		SystemPartialCloseUSDT:  systemPartialMin,
	}
	for _, symbol := range symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			continue
		}
		calibration := symbolCalibration{
			Symbol:                       symbol,
			RecommendedMinOrderValueUSDT: systemMinOrder,
			RecommendedPartialCloseUSDT:  systemPartialMin,
		}
		info, ok := index[symbol]
		if !ok {
			calibration.Findings = append(calibration.Findings, "交易所exchangeInfo未返回该symbol")
			report.Symbols = append(report.Symbols, calibration)
			continue
		}
		calibration.Status = info.Status
		applySymbolFilters(&calibration, info.Filters)
		if calibration.MinNotional > calibration.RecommendedMinOrderValueUSDT {
			calibration.RecommendedMinOrderValueUSDT = calibration.MinNotional
			calibration.Findings = append(calibration.Findings, fmt.Sprintf("系统开仓最小名义额应至少提高到 %.2f USDT", calibration.MinNotional))
		}
		if calibration.MinNotional > calibration.RecommendedPartialCloseUSDT {
			calibration.RecommendedPartialCloseUSDT = calibration.MinNotional
			calibration.Findings = append(calibration.Findings, fmt.Sprintf("partial_close最小名义额应至少提高到 %.2f USDT", calibration.MinNotional))
		}
		if calibration.Status != "" && calibration.Status != "TRADING" {
			calibration.Findings = append(calibration.Findings, "symbol非TRADING状态")
		}
		report.Symbols = append(report.Symbols, calibration)
	}
	sort.Slice(report.Symbols, func(i, j int) bool {
		return report.Symbols[i].Symbol < report.Symbols[j].Symbol
	})
	return report, nil
}

func applySymbolFilters(calibration *symbolCalibration, filters []map[string]interface{}) {
	for _, filter := range filters {
		filterType := getString(filter, "filterType")
		switch filterType {
		case "LOT_SIZE":
			calibration.StepSize = getString(filter, "stepSize")
			calibration.MinQty = getString(filter, "minQty")
		case "MARKET_LOT_SIZE":
			if calibration.StepSize == "" {
				calibration.StepSize = getString(filter, "stepSize")
			}
			if calibration.MinQty == "" {
				calibration.MinQty = getString(filter, "minQty")
			}
		case "PRICE_FILTER":
			calibration.TickSize = getString(filter, "tickSize")
		case "MIN_NOTIONAL", "NOTIONAL":
			if value := getFloat(filter, "notional"); value > calibration.MinNotional {
				calibration.MinNotional = value
			}
			if value := getFloat(filter, "minNotional"); value > calibration.MinNotional {
				calibration.MinNotional = value
			}
		}
	}
}

func getString(values map[string]interface{}, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func getFloat(values map[string]interface{}, key string) float64 {
	raw := getString(values, key)
	value, _ := strconv.ParseFloat(raw, 64)
	return value
}

func splitSymbols(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToUpper(strings.TrimSpace(part))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
