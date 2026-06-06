package decision

import (
	"nofx/market"
	"testing"
)

func programmaticDecision(symbol, action string) Decision {
	return Decision{
		Symbol:          symbol,
		Action:          action,
		StrategyMode:    "programmatic",
		StrategyName:    "chanlun_programmatic",
		StrategyVersion: "v1",
		ConfigHash:      "hash",
		SignalID:        symbol + "-" + action,
		StrategyMetadata: map[string]any{
			"layer": "position_management",
			"rule":  "test",
		},
	}
}

func TestValidateRiskReducingStrategyDecisions(t *testing.T) {
	ctx := &Context{
		TraderID: "t1",
		Positions: []PositionInfo{{
			Symbol:     "BTCUSDT",
			Side:       "long",
			EntryPrice: 100,
			MarkPrice:  110,
			StopLoss:   95,
			Quantity:   1,
		}},
		MarketDataMap: map[string]*market.Data{
			"BTCUSDT": {Symbol: "BTCUSDT", CurrentPrice: 110},
		},
	}

	validPartial := programmaticDecision("BTCUSDT", "partial_close")
	validPartial.ClosePercentage = 30
	valid, rejected := ValidateRiskReducingStrategyDecisions(ctx, []Decision{validPartial}, RiskReducingValidationOptions{Source: "programmatic"})
	if len(rejected) != 0 || len(valid) != 1 {
		t.Fatalf("合法partial_close应通过: valid=%+v rejected=%+v", valid, rejected)
	}

	badStop := programmaticDecision("BTCUSDT", "update_stop_loss")
	badStop.NewStopLoss = 94
	valid, rejected = ValidateRiskReducingStrategyDecisions(ctx, []Decision{badStop}, RiskReducingValidationOptions{Source: "programmatic"})
	if len(valid) != 0 || len(rejected) != 1 {
		t.Fatalf("降低保护效果的止损应被拒绝: valid=%+v rejected=%+v", valid, rejected)
	}

	noPosition := programmaticDecision("ETHUSDT", "partial_close")
	noPosition.ClosePercentage = 30
	valid, rejected = ValidateRiskReducingStrategyDecisions(ctx, []Decision{noPosition}, RiskReducingValidationOptions{Source: "programmatic"})
	if len(valid) != 0 || len(rejected) != 1 {
		t.Fatalf("无持仓partial_close应被拒绝: valid=%+v rejected=%+v", valid, rejected)
	}
}

func TestMergePublicAndStrategyDecisions_ProgrammaticRiskConflicts(t *testing.T) {
	ctx := &Context{
		Positions: []PositionInfo{{
			Symbol:     "BTCUSDT",
			Side:       "long",
			EntryPrice: 100,
			MarkPrice:  110,
			StopLoss:   95,
			Quantity:   1,
		}},
	}

	publicClose := []Decision{{Symbol: "BTCUSDT", Action: "close_long", Reasoning: "公共平仓"}}
	strategyPartial := programmaticDecision("BTCUSDT", "partial_close")
	strategyPartial.ClosePercentage = 30
	merged := MergePublicAndStrategyDecisionsWithContext(ctx, publicClose, []Decision{strategyPartial})
	if len(merged) != 1 || merged[0].Action != "close_long" {
		t.Fatalf("公共close应压制程序化partial: %+v", merged)
	}

	publicStop := []Decision{{Symbol: "BTCUSDT", Action: "update_stop_loss", NewStopLoss: 101, Reasoning: "公共止损"}}
	strategyStop := programmaticDecision("BTCUSDT", "update_stop_loss")
	strategyStop.NewStopLoss = 102
	merged = MergePublicAndStrategyDecisionsWithContext(ctx, publicStop, []Decision{strategyStop})
	if len(merged) != 1 || merged[0].NewStopLoss != 102 {
		t.Fatalf("多头双stop应保留保护更强的一条: %+v", merged)
	}

	strategyOpen := Decision{Symbol: "BTCUSDT", Action: "open_long", Reasoning: "开仓"}
	merged = MergePublicAndStrategyDecisionsWithContext(ctx, publicStop, []Decision{strategyOpen})
	if len(merged) != 1 || merged[0].Action != "update_stop_loss" {
		t.Fatalf("公共风险动作应阻断程序化open/add: %+v", merged)
	}
}
