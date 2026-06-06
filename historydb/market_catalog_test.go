package historydb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBinanceFuturesFetchSymbolsFiltersTradableUSDTPerpetuals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fapi/v1/exchangeInfo" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"symbols": [
				{"symbol":"ETHUSDT","status":"TRADING","contractType":"PERPETUAL","baseAsset":"ETH","quoteAsset":"USDT"},
				{"symbol":"BTCUSDT","status":"TRADING","contractType":"PERPETUAL","baseAsset":"BTC","quoteAsset":"USDT"},
				{"symbol":"BTCUSDC","status":"TRADING","contractType":"PERPETUAL","baseAsset":"BTC","quoteAsset":"USDC"},
				{"symbol":"OLDUSDT","status":"BREAK","contractType":"PERPETUAL","baseAsset":"OLD","quoteAsset":"USDT"},
				{"symbol":"DELIVERYUSDT","status":"TRADING","contractType":"CURRENT_QUARTER","baseAsset":"DELIVERY","quoteAsset":"USDT"}
			]
		}`))
	}))
	defer server.Close()

	source := NewBinanceFuturesKlineSource()
	source.BaseURL = server.URL
	source.Client = server.Client()

	symbols, err := source.FetchSymbols(context.Background())
	if err != nil {
		t.Fatalf("FetchSymbols失败: %v", err)
	}
	if len(symbols) != 2 {
		t.Fatalf("应只返回2个USDT永续TRADING symbol: %+v", symbols)
	}
	if symbols[0].Symbol != "BTCUSDT" || symbols[1].Symbol != "ETHUSDT" {
		t.Fatalf("核心symbol排序异常: %+v", symbols)
	}
}
