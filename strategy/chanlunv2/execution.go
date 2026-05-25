package chanlunv2

import (
	"nofx/decision"
	"strings"
	"time"
)

type ExecutionResult struct {
	TraderID         string            `json:"trader_id,omitempty"`
	Decision         decision.Decision `json:"decision"`
	Success          bool              `json:"success"`
	FinalAction      string            `json:"final_action,omitempty"`
	ExecutedQuantity float64           `json:"executed_quantity,omitempty"`
	Price            float64           `json:"price,omitempty"`
	Error            string            `json:"error,omitempty"`
	ExecutedAt       time.Time         `json:"executed_at,omitempty"`
}

func (e *Engine) OnExecutionResult(result ExecutionResult) {
	if e == nil || !result.Success {
		return
	}
	finalAction := strings.TrimSpace(result.FinalAction)
	if finalAction == "" {
		finalAction = result.Decision.Action
	}
	if !decision.IsOpenLikeAction(finalAction) {
		return
	}
	d := result.Decision
	d.Action = finalAction
	e.markSignalExecuted(result.TraderID, d, result.ExecutedAt)
}
