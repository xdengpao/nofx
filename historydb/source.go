package historydb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"nofx/market"
)

type KlineSource interface {
	Name() string
	SupportedTimeframes() []string
	MaxLimit(timeframe string) int
	FetchKlines(ctx context.Context, req FetchKlineRequest) ([]market.Kline, error)
}

type FetchKlineRequest struct {
	Symbol    string
	Timeframe string
	Start     time.Time
	End       time.Time
	Limit     int
}

type BinanceFuturesKlineSource struct {
	BaseURL string
	Client  *http.Client
}

func NewBinanceFuturesKlineSource() *BinanceFuturesKlineSource {
	return &BinanceFuturesKlineSource{
		BaseURL: "https://fapi.binance.com",
		Client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *BinanceFuturesKlineSource) Name() string { return "binance-futures" }

func (s *BinanceFuturesKlineSource) SupportedTimeframes() []string {
	return []string{"3m", "15m", "1h", "4h"}
}

func (s *BinanceFuturesKlineSource) MaxLimit(_ string) int { return 1500 }

func (s *BinanceFuturesKlineSource) FetchKlines(ctx context.Context, req FetchKlineRequest) ([]market.Kline, error) {
	if s.Client == nil {
		s.Client = &http.Client{Timeout: 15 * time.Second}
	}
	base := strings.TrimRight(s.BaseURL, "/")
	if base == "" {
		base = "https://fapi.binance.com"
	}
	limit := req.Limit
	if limit <= 0 || limit > s.MaxLimit(req.Timeframe) {
		limit = s.MaxLimit(req.Timeframe)
	}
	params := url.Values{}
	params.Set("symbol", market.Normalize(req.Symbol))
	params.Set("interval", strings.ToLower(strings.TrimSpace(req.Timeframe)))
	params.Set("limit", strconv.Itoa(limit))
	if !req.Start.IsZero() {
		params.Set("startTime", strconv.FormatInt(req.Start.UnixMilli(), 10))
	}
	if !req.End.IsZero() {
		params.Set("endTime", strconv.FormatInt(req.End.UnixMilli()-1, 10))
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/fapi/v1/klines?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.Client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusTeapot {
		return nil, fmt.Errorf("数据源限频: status=%d body=%s", resp.StatusCode, string(body))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Binance K线接口失败: status=%d body=%s", resp.StatusCode, string(body))
	}
	var raw [][]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	klines := make([]market.Kline, 0, len(raw))
	for _, item := range raw {
		if len(item) < 7 {
			continue
		}
		openTime, _ := anyToInt64(item[0])
		open, _ := anyToFloat(item[1])
		high, _ := anyToFloat(item[2])
		low, _ := anyToFloat(item[3])
		closePrice, _ := anyToFloat(item[4])
		volume, _ := anyToFloat(item[5])
		closeTime, _ := anyToInt64(item[6])
		klines = append(klines, market.Kline{
			OpenTime:  openTime,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closePrice,
			Volume:    volume,
			CloseTime: closeTime,
		})
	}
	return market.FilterClosedKlines(klines, time.Now().UTC()), nil
}

func anyToFloat(value any) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("不能解析float: %T", value)
	}
}

func anyToInt64(value any) (int64, error) {
	switch v := value.(type) {
	case float64:
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, fmt.Errorf("不能解析int64: %T", value)
	}
}
