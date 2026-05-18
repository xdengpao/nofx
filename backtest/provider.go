package backtest

import (
	"context"
	"fmt"
	"time"

	"nofx/decision"
	"nofx/historydb"
	"nofx/market"
)

type HistoricalMarketDataProvider struct {
	Store  *historydb.Store
	Source string
	Clock  func() time.Time
}

func (p *HistoricalMarketDataProvider) GetMarketData(symbol string, opts decision.CyclePreparationOptions) (*market.Data, error) {
	if p == nil || p.Store == nil {
		return nil, fmt.Errorf("历史行情provider未初始化")
	}
	now := time.Now()
	if p.Clock != nil {
		now = p.Clock()
	} else if opts.Clock != nil {
		now = opts.Clock()
	}
	source := p.Source
	if source == "" {
		source = DefaultSource
	}
	depth := map[string]int{
		"3m":  opts.MarketHistoryDepth["3m"],
		"15m": opts.MarketHistoryDepth["15m"],
		"1h":  opts.MarketHistoryDepth["1h"],
		"4h":  opts.MarketHistoryDepth["4h"],
	}
	for tf, value := range depth {
		if value <= 0 {
			switch tf {
			case "3m":
				depth[tf] = 240
			case "15m":
				depth[tf] = 192
			case "1h":
				depth[tf] = 240
			case "4h":
				depth[tf] = 180
			}
		}
	}
	ctx := context.Background()
	m3, err := p.Store.LastClosedKlines(ctx, source, symbol, "3m", depth["3m"], now)
	if err != nil {
		return nil, err
	}
	m15, err := p.Store.LastClosedKlines(ctx, source, symbol, "15m", depth["15m"], now)
	if err != nil {
		return nil, err
	}
	h1, err := p.Store.LastClosedKlines(ctx, source, symbol, "1h", depth["1h"], now)
	if err != nil {
		return nil, err
	}
	h4, err := p.Store.LastClosedKlines(ctx, source, symbol, "4h", depth["4h"], now)
	if err != nil {
		return nil, err
	}
	if len(m3) == 0 || len(m15) == 0 || len(h1) == 0 || len(h4) == 0 {
		return nil, fmt.Errorf("%s 历史K线样本不足: 3m=%d 15m=%d 1h=%d 4h=%d", symbol, len(m3), len(m15), len(h1), len(h4))
	}
	return market.BuildDataFromKlines(symbol, market.KlineBundle{M3: m3, M15: m15, H1: h1, H4: h4}, market.BuildDataOptions{
		IncludeMicroADX: opts.IncludeMicroADX,
		EnrichmentMode:  "disabled",
	})
}
