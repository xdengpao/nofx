package trader

import (
	"nofx/decision"
	"nofx/logger"
	"nofx/strategy/chanlun"
	"nofx/strategy/chanlunv2"
	"testing"
	"time"
)

type fakeChanlunV2Reporter struct {
	calls   int
	results []chanlunv2.ExecutionResult
}

func (f *fakeChanlunV2Reporter) GetFullDecision(ctx *decision.Context) (*decision.FullDecision, error) {
	return nil, nil
}

func (f *fakeChanlunV2Reporter) SymbolUniverse(traderID string) []chanlun.StrategySymbol {
	return nil
}

func (f *fakeChanlunV2Reporter) LatestSignalsWithOptions(traderID, symbol string, opts chanlun.SignalReportOptions) (*chanlun.SignalReport, bool) {
	return nil, false
}

func (f *fakeChanlunV2Reporter) EmptySignalReportWithOptions(traderID, symbol string, opts chanlun.SignalReportOptions) *chanlun.SignalReport {
	return nil
}

func (f *fakeChanlunV2Reporter) OnExecutionResult(result chanlunv2.ExecutionResult) {
	f.calls++
	f.results = append(f.results, result)
}

func TestReportChanlunV2ExecutionResultOnlyForChanlunV2Mode(t *testing.T) {
	reporter := &fakeChanlunV2Reporter{}
	d := &decision.Decision{Symbol: "BNBUSDT", Action: "open_long", StrategyMode: "chanlun_v2", SignalID: "signal-1"}
	record := &logger.DecisionAction{Success: true, FinalAction: "open_long", Timestamp: time.Now()}

	aiTrader := &AutoTrader{id: "t1", config: AutoTraderConfig{DecisionMode: "ai"}, chanlunV2Engine: reporter}
	aiTrader.reportChanlunV2ExecutionResult(d, record)
	if reporter.calls != 0 {
		t.Fatalf("非chanlun_v2 mode不应调用V2回调: %d", reporter.calls)
	}

	v2Trader := &AutoTrader{id: "t1", config: AutoTraderConfig{DecisionMode: "chanlun_v2"}, chanlunV2Engine: reporter}
	v2Trader.reportChanlunV2ExecutionResult(d, record)
	if reporter.calls != 1 {
		t.Fatalf("chanlun_v2 mode应调用V2回调: %d", reporter.calls)
	}
	if reporter.results[0].TraderID != "t1" || reporter.results[0].FinalAction != "open_long" || !reporter.results[0].Success {
		t.Fatalf("V2回调内容错误: %+v", reporter.results[0])
	}
}
