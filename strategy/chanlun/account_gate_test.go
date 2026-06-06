package chanlun

import (
	"nofx/decision"
	"testing"
)

func TestAccountSizeGateHoldOnlyAndPilotReject(t *testing.T) {
	engine := &Engine{Policy: decision.ProgrammaticStrategyPolicy{
		DefectFixPackEnabled: true,
		MaxPilotNotionalPct:  0.6,
		MinPilotNotionalUSD:  30,
	}}
	hold := engine.accountSizeGate(&decision.Context{
		Account:         decision.AccountInfo{AvailableBalance: 5},
		AltcoinLeverage: 5,
	})
	if !hold.HoldOnly || !hold.AccountTooSmall {
		t.Fatalf("小账户应进入hold_only: %+v", hold)
	}
	reject := engine.accountSizeGate(&decision.Context{
		Account:         decision.AccountInfo{AvailableBalance: 8},
		AltcoinLeverage: 5,
	})
	if !reject.RejectPilot || reject.HoldOnly {
		t.Fatalf("max_allowed_notional低于最小名义额时应拒绝pilot但不hold_only: %+v", reject)
	}
	pass := engine.accountSizeGate(&decision.Context{
		Account:         decision.AccountInfo{AvailableBalance: 100},
		AltcoinLeverage: 5,
	})
	if pass.HoldOnly || pass.RejectPilot || pass.PilotPositionSizeUSD != 300 {
		t.Fatalf("正常账户gate错误: %+v", pass)
	}
}
