package main

import "testing"

func TestBuildCalibrationReport_ExtractsFiltersAndRecommendations(t *testing.T) {
	data := []byte(`{
		"symbols": [
			{
				"symbol": "BTCUSDT",
				"status": "TRADING",
				"filters": [
					{"filterType": "PRICE_FILTER", "tickSize": "0.10"},
					{"filterType": "LOT_SIZE", "minQty": "0.001", "stepSize": "0.001"},
					{"filterType": "MIN_NOTIONAL", "notional": "12"}
				]
			}
		]
	}`)

	report, err := buildCalibrationReport("aster", "fixture", data, []string{"BTCUSDT"}, 10, 5)
	if err != nil {
		t.Fatalf("buildCalibrationReport failed: %v", err)
	}
	if len(report.Symbols) != 1 {
		t.Fatalf("symbol count mismatch: %+v", report)
	}
	got := report.Symbols[0]
	if got.TickSize != "0.10" || got.StepSize != "0.001" || got.MinQty != "0.001" {
		t.Fatalf("filter extraction mismatch: %+v", got)
	}
	if got.RecommendedMinOrderValueUSDT != 12 || got.RecommendedPartialCloseUSDT != 12 {
		t.Fatalf("recommendation mismatch: %+v", got)
	}
}

func TestBuildCalibrationReport_MissingSymbol(t *testing.T) {
	report, err := buildCalibrationReport("aster", "fixture", []byte(`{"symbols":[]}`), []string{"ETHUSDT"}, 10, 5)
	if err != nil {
		t.Fatalf("buildCalibrationReport failed: %v", err)
	}
	if len(report.Symbols) != 1 || len(report.Symbols[0].Findings) == 0 {
		t.Fatalf("missing symbol should produce finding: %+v", report)
	}
}
