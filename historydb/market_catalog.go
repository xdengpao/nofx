package historydb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type MarketCatalog struct {
	Sources  []MarketSource `json:"sources"`
	SyncedAt time.Time      `json:"synced_at"`
}

type MarketSource struct {
	Source      string         `json:"source"`
	DisplayName string         `json:"display_name"`
	Exchange    string         `json:"exchange"`
	Timeframes  []string       `json:"timeframes"`
	Symbols     []MarketSymbol `json:"symbols"`
	SyncedAt    time.Time      `json:"synced_at"`
}

type MarketSymbol struct {
	Symbol       string `json:"symbol"`
	BaseAsset    string `json:"base_asset,omitempty"`
	QuoteAsset   string `json:"quote_asset,omitempty"`
	Status       string `json:"status,omitempty"`
	ContractType string `json:"contract_type,omitempty"`
}

func FetchMarketCatalog(ctx context.Context) (MarketCatalog, error) {
	source := NewBinanceFuturesKlineSource()
	symbols, err := source.FetchSymbols(ctx)
	if err != nil {
		return MarketCatalog{}, err
	}
	now := time.Now().UTC()
	return MarketCatalog{
		SyncedAt: now,
		Sources: []MarketSource{
			{
				Source:      source.Name(),
				DisplayName: "Binance USD-M Futures",
				Exchange:    "binance",
				Timeframes:  source.SupportedTimeframes(),
				Symbols:     symbols,
				SyncedAt:    now,
			},
		},
	}, nil
}

func (s *BinanceFuturesKlineSource) FetchSymbols(ctx context.Context) ([]MarketSymbol, error) {
	if s.Client == nil {
		s.Client = &http.Client{Timeout: 15 * time.Second}
	}
	base := strings.TrimRight(s.BaseURL, "/")
	if base == "" {
		base = "https://fapi.binance.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/fapi/v1/exchangeInfo", nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Binance exchangeInfo接口失败: status=%d body=%s", resp.StatusCode, string(body))
	}
	var parsed struct {
		Symbols []struct {
			Symbol       string `json:"symbol"`
			Status       string `json:"status"`
			ContractType string `json:"contractType"`
			BaseAsset    string `json:"baseAsset"`
			QuoteAsset   string `json:"quoteAsset"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	out := make([]MarketSymbol, 0, len(parsed.Symbols))
	for _, item := range parsed.Symbols {
		symbol := strings.ToUpper(strings.TrimSpace(item.Symbol))
		status := strings.ToUpper(strings.TrimSpace(item.Status))
		contractType := strings.ToUpper(strings.TrimSpace(item.ContractType))
		quoteAsset := strings.ToUpper(strings.TrimSpace(item.QuoteAsset))
		if symbol == "" || status != "TRADING" || contractType != "PERPETUAL" || quoteAsset != "USDT" {
			continue
		}
		out = append(out, MarketSymbol{
			Symbol:       symbol,
			BaseAsset:    strings.ToUpper(strings.TrimSpace(item.BaseAsset)),
			QuoteAsset:   quoteAsset,
			Status:       status,
			ContractType: contractType,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return symbolRank(out[i].Symbol) < symbolRank(out[j].Symbol)
	})
	return out, nil
}

func symbolRank(symbol string) string {
	switch symbol {
	case "BTCUSDT":
		return "0000"
	case "ETHUSDT":
		return "0001"
	case "SOLUSDT":
		return "0002"
	case "BNBUSDT":
		return "0003"
	default:
		return "1000" + symbol
	}
}
