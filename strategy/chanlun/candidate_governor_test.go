package chanlun

import (
	"nofx/decision"
	"testing"
)

func TestCandidateGovernorFiltersSpreadAndEnsuresCore(t *testing.T) {
	engine := &Engine{Policy: decision.ProgrammaticStrategyPolicy{
		DefectFixPackEnabled: true,
		CandidateGovernor: decision.ProgrammaticCandidateGovernorPolicy{
			Enabled:               true,
			MaxQuoteSpreadBps:     20,
			CoreSymbolsMustAppear: []string{"BTCUSDT"},
		},
	}}
	ctx := &decision.Context{
		CandidateCoins: []decision.CandidateCoin{
			{Symbol: "XAUUSDT", Sources: []string{"test"}},
			{Symbol: "SOLUSDT", Sources: []string{"test"}},
			{Symbol: "ETHUSDT", Sources: []string{"test"}},
		},
		QuoteSpreadProvider: func(symbol string) (float64, float64, error) {
			if symbol == "SOLUSDT" {
				return 100, 101, nil
			}
			return 100, 100.05, nil
		},
	}
	diagnostics := engine.applyCandidateGovernor(ctx)
	if len(diagnostics) < 2 {
		t.Fatalf("应记录剔除诊断: %+v", diagnostics)
	}
	if len(ctx.CandidateCoins) != 4 {
		t.Fatalf("应保留过滤诊断并强制补BTC: %+v", ctx.CandidateCoins)
	}
	if ctx.CandidateCoins[0].Symbol != "XAUUSDT" || ctx.CandidateCoins[0].FilterReason != "non_crypto_symbol" ||
		ctx.CandidateCoins[1].Symbol != "SOLUSDT" || ctx.CandidateCoins[1].FilterReason != "quote_spread_too_high" ||
		ctx.CandidateCoins[2].Symbol != "ETHUSDT" || ctx.CandidateCoins[3].Symbol != "BTCUSDT" {
		t.Fatalf("候选治理输出错误: %+v", ctx.CandidateCoins)
	}
	universe := ResolveProgrammaticSymbols(ctx.CandidateCoins, nil, engine.Policy)
	if len(universe) != 2 || universe[0].Symbol != "BTCUSDT" || universe[1].Symbol != "ETHUSDT" {
		t.Fatalf("程序化候选 universe 应跳过过滤项并保留core: %+v", universe)
	}
}

func TestIsCryptoUSDT(t *testing.T) {
	if !isCryptoUSDT("SOLUSDT") || !isCryptoUSDT("ETHUSDC") {
		t.Fatal("常规加密USDT/USDC应通过")
	}
	if isCryptoUSDT("XAUUSDT") || isCryptoUSDT("CLUSDT") {
		t.Fatal("贵金属/大宗商品前缀应被剔除")
	}
}
