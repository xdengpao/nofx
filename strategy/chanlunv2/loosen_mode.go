package chanlunv2

import (
	"nofx/config"
	"nofx/decision"
	"strings"
	"time"
)

func (e *Engine) loosenModeController(ctx *decision.Context) string {
	baseMode := "normal"
	mode := "normal"
	if ctx != nil && ctx.FrequencyPolicy != nil {
		baseMode = strings.ToLower(strings.TrimSpace(firstNonEmptyString(ctx.FrequencyPolicy.Mode, "normal")))
		mode = strings.ToLower(strings.TrimSpace(firstNonEmptyString(ctx.FrequencyPolicy.EffectiveMode, baseMode, "normal")))
	}
	if ctx == nil || ctx.FrequencyPolicy == nil || !ctx.FrequencyPolicy.LoosenMode.Enabled {
		e.setActiveMode(baseMode)
		return baseMode
	}
	lossActive := ctx.LossMode != nil && ctx.LossMode.Active
	safeActive := mode == "safe" || mode == "loss"
	if lossActive || safeActive || (ctx.FrequencyState != nil && ctx.FrequencyState.OpenCount24h > 0) {
		exitMode := baseMode
		if lossActive {
			exitMode = "loss"
		} else if safeActive {
			exitMode = mode
		}
		ctx.FrequencyPolicy.EffectiveMode = exitMode
		e.setActiveMode(exitMode)
		return exitMode
	}
	policy := normalizeV2LoosenPolicy(ctx.FrequencyPolicy.LoosenMode)
	inactiveFor := time.Duration(ctx.RuntimeMinutes) * time.Minute
	if ctx.FrequencyState != nil && !ctx.FrequencyState.LastOpenAt.IsZero() {
		inactiveFor = time.Since(ctx.FrequencyState.LastOpenAt)
	}
	if inactiveFor >= time.Duration(policy.InactivityWindowMinutes)*time.Minute {
		ctx.FrequencyPolicy.EffectiveMode = "loosen"
		e.setLoosenAdjustments(policy)
		e.setActiveMode("loosen")
		return "loosen"
	}
	e.setActiveMode(mode)
	return mode
}

func normalizeV2LoosenPolicy(policy decision.LoosenModePolicy) decision.LoosenModePolicy {
	if policy.InactivityWindowMinutes <= 0 {
		policy.InactivityWindowMinutes = 720
	}
	if policy.PilotConfidenceDrop <= 0 {
		policy.PilotConfidenceDrop = 10
	}
	if policy.MinNetRRDelta == 0 {
		policy.MinNetRRDelta = -0.4
	}
	if policy.MaxChaseRatioBump == 0 {
		policy.MaxChaseRatioBump = 0.05
	}
	if policy.MaxDurationHours <= 0 {
		policy.MaxDurationHours = 24
	}
	if policy.HardFloorPilotConfidence <= 0 {
		policy.HardFloorPilotConfidence = 60
	}
	return policy
}

func (e *Engine) setActiveMode(mode string) {
	if mode == "" {
		mode = "normal"
	}
	e.mu.Lock()
	e.activeMode = mode
	if mode != "loosen" {
		e.loosenMinRRDelta = 0
		e.loosenChaseBump = 0
		e.loosenConfidenceDrop = 0
		e.loosenConfidenceFloor = 0
	}
	e.mu.Unlock()
}

func (e *Engine) setLoosenAdjustments(policy decision.LoosenModePolicy) {
	policy = normalizeV2LoosenPolicy(policy)
	e.mu.Lock()
	e.loosenMinRRDelta = policy.MinNetRRDelta
	e.loosenChaseBump = policy.MaxChaseRatioBump
	e.loosenConfidenceDrop = policy.PilotConfidenceDrop
	e.loosenConfidenceFloor = policy.HardFloorPilotConfidence
	e.mu.Unlock()
}

func (e *Engine) activeRuntimeMode() string {
	e.mu.RLock()
	mode := e.activeMode
	e.mu.RUnlock()
	if mode == "" {
		return "normal"
	}
	return mode
}

func (e *Engine) activeLoosenAdjustments() (float64, float64, int, int) {
	e.mu.RLock()
	delta := e.loosenMinRRDelta
	bump := e.loosenChaseBump
	drop := e.loosenConfidenceDrop
	floor := e.loosenConfidenceFloor
	e.mu.RUnlock()
	if delta == 0 {
		delta = -0.4
	}
	if bump == 0 {
		bump = 0.05
	}
	if drop <= 0 {
		drop = 10
	}
	if floor <= 0 {
		floor = 60
	}
	return delta, bump, drop, floor
}

func (e *Engine) effectiveEntryTiming(ctx *decision.Context) config.ChanlunV2EntryTimingConfig {
	timing := config.NormalizeChanlunV2EntryTiming(e.Config.EntryTiming)
	if timing.EntryZone.SignalTypeMinRR != nil {
		copied := make(map[string]float64, len(timing.EntryZone.SignalTypeMinRR))
		for key, value := range timing.EntryZone.SignalTypeMinRR {
			copied[key] = value
		}
		timing.EntryZone.SignalTypeMinRR = copied
	}
	mode := e.activeRuntimeMode()
	if ctx != nil && ctx.FrequencyPolicy != nil {
		mode = strings.ToLower(strings.TrimSpace(firstNonEmptyString(ctx.FrequencyPolicy.EffectiveMode, mode)))
	}
	if mode != "loosen" {
		return timing
	}
	delta, bump, drop, floor := e.activeLoosenAdjustments()
	timing.EntryZone.MinRemainingNetRR = applyV2LoosenMinRR(timing.EntryZone.MinRemainingNetRR, delta)
	for key, value := range timing.EntryZone.SignalTypeMinRR {
		timing.EntryZone.SignalTypeMinRR[key] = applyV2LoosenMinRR(value, delta)
	}
	timing.EntryZone.MaxChaseRatio += bump
	if timing.EntryZone.MaxChaseRatio > 1 {
		timing.EntryZone.MaxChaseRatio = 1
	}
	timing.MinTriggerConfidence = max(timing.MinTriggerConfidence-drop, floor)
	return timing
}

func applyV2LoosenMinRR(value, delta float64) float64 {
	value += delta
	if value < 1 {
		return 1
	}
	return value
}

func (e *Engine) applyEffectiveV2MinRR(value float64) float64 {
	if e.activeRuntimeMode() != "loosen" {
		return value
	}
	delta, _, _, _ := e.activeLoosenAdjustments()
	return applyV2LoosenMinRR(value, delta)
}

func effectiveChanlunV2DecisionSignalType(d decision.Decision) string {
	return firstNonEmptyString(
		d.SignalType,
		metadataString(d.StrategyMetadata, "parent_signal_type"),
		metadataString(d.StrategyMetadata, "signal_type"),
	)
}

func (e *Engine) effectiveEntryTimingDiagnostics(ctx *decision.Context) map[string]any {
	timing := e.effectiveEntryTiming(ctx)
	return map[string]any{
		"active_mode":            e.activeRuntimeMode(),
		"min_trigger_confidence": timing.MinTriggerConfidence,
		"max_chase_ratio":        timing.EntryZone.MaxChaseRatio,
		"min_remaining_net_rr":   timing.EntryZone.MinRemainingNetRR,
		"signal_type_min_rr":     timing.EntryZone.SignalTypeMinRR,
	}
}
