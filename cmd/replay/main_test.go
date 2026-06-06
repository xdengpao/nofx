package main

import (
	"nofx/config"
	"testing"
)

func TestSelectReplayTraderConfigHonorsExplicitMissingID(t *testing.T) {
	traders := []config.TraderConfig{
		{ID: "aster_chanlun_v2", Enabled: true, DecisionMode: "chanlun_v2"},
		{ID: "binance_ai", Enabled: true, DecisionMode: "ai"},
	}

	if got := selectReplayTraderConfig(traders, "missing"); got != nil {
		t.Fatalf("显式trader不存在时不应回退到其他配置: %+v", got)
	}
}

func TestSelectReplayTraderConfigFallsBackToEnabledChanlunV2(t *testing.T) {
	traders := []config.TraderConfig{
		{ID: "binance_ai", Enabled: true, DecisionMode: "ai"},
		{ID: "aster_chanlun_v2", Enabled: true, DecisionMode: "chanlun_v2"},
	}

	got := selectReplayTraderConfig(traders, "")
	if got == nil || got.ID != "aster_chanlun_v2" {
		t.Fatalf("未指定trader时应选择启用的chanlun_v2配置: %+v", got)
	}
}
