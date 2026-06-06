package chanlun

import (
	"nofx/decision"
	"testing"
)

func TestWaitReasonSummaryPriority(t *testing.T) {
	decisions := []decision.Decision{{Symbol: "ALL", Action: "wait"}}
	rejections := []decision.OpenRejection{{
		Reason:          "剩余净RR过低",
		GateReasons:     []string{"remaining_net_rr_too_low"},
		GateDiagnostics: map[string]any{"reason_code": "remaining_net_rr_too_low"},
	}}
	if got := waitReasonSummary([]string{"BTCUSDT suppressed_fast_skip: permanent_skip"}, rejections, decisions); got != "suppressed" {
		t.Fatalf("suppressed应优先于RR: got=%s", got)
	}
	if got := waitReasonSummary(nil, rejections, decisions); got != "structure_rr_too_low" {
		t.Fatalf("RR拒绝归因错误: got=%s", got)
	}
	if got := waitReasonSummary([]string{"ETHUSDT 无买卖点信号"}, nil, decisions); got != "no_signal" {
		t.Fatalf("无信号归因错误: got=%s", got)
	}
	if got := waitReasonSummary([]string{"pilot跳过: 置信度68低于70"}, nil, []decision.Decision{{Symbol: "BTCUSDT", Action: "open_long"}}); got != "" {
		t.Fatalf("非wait周期不应生成wait_reason_summary: got=%s", got)
	}
}
