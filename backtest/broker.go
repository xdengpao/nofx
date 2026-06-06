package backtest

import (
	"fmt"
	"math"
	"strings"
	"time"

	"nofx/decision"
	"nofx/market"
	"nofx/trader"
)

type PaperAccount struct {
	InitialEquity float64 `json:"initial_equity"`
	Cash          float64 `json:"cash"`
	Equity        float64 `json:"equity"`
	RealizedPnL   float64 `json:"realized_pnl"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	FeePaid       float64 `json:"fee_paid"`
	SlippagePaid  float64 `json:"slippage_paid"`
	MarginUsed    float64 `json:"margin_used"`
}

type PaperPosition struct {
	Symbol       string    `json:"symbol"`
	Side         string    `json:"side"`
	Quantity     float64   `json:"quantity"`
	AverageEntry float64   `json:"average_entry"`
	MarkPrice    float64   `json:"mark_price"`
	Leverage     int       `json:"leverage"`
	StopLoss     float64   `json:"stop_loss,omitempty"`
	TakeProfit   float64   `json:"take_profit,omitempty"`
	OpenedAt     time.Time `json:"opened_at"`
	LifecycleID  string    `json:"lifecycle_id"`
	InitialRisk  float64   `json:"initial_risk"`
	PeakPrice    float64   `json:"peak_price"`
	TroughPrice  float64   `json:"trough_price"`
	EntryReason  string    `json:"entry_reason,omitempty"`
	SignalID     string    `json:"signal_id,omitempty"`
	SignalType   string    `json:"signal_type,omitempty"`
}

type PendingOrder struct {
	Decision decision.Decision `json:"decision"`
	QueuedAt time.Time         `json:"queued_at"`
}

type PaperBroker struct {
	Account        PaperAccount
	Positions      map[string]*PaperPosition
	Pending        []PendingOrder
	Executions     []ExecutionEvent
	Lifecycles     map[string]*TradeLifecycle
	Costs          CostConfig
	Execution      ExecutionConfig
	Exchange       string
	OpenRejections []decision.OpenRejection
}

func NewPaperBroker(initialEquity float64, costs CostConfig, execution ExecutionConfig) *PaperBroker {
	return NewPaperBrokerWithExchange(initialEquity, costs, execution, "backtest")
}

func NewPaperBrokerWithExchange(initialEquity float64, costs CostConfig, execution ExecutionConfig, exchange string) *PaperBroker {
	return &PaperBroker{
		Account: PaperAccount{
			InitialEquity: initialEquity,
			Cash:          initialEquity,
			Equity:        initialEquity,
		},
		Positions:  map[string]*PaperPosition{},
		Lifecycles: map[string]*TradeLifecycle{},
		Costs:      normalizeCostConfig(costs),
		Execution:  normalizeExecutionConfig(execution),
		Exchange:   exchange,
	}
}

func (b *PaperBroker) SubmitDecision(d decision.Decision, now time.Time) {
	switch d.Action {
	case "open_long", "open_short", "add_long", "add_short", "partial_close", "close_long", "close_short":
		b.Pending = append(b.Pending, PendingOrder{Decision: d, QueuedAt: now})
	case "update_stop_loss":
		b.updateStopLoss(d, now)
	}
}

func (b *PaperBroker) ProcessBar(symbol string, bar market.Kline, now time.Time) {
	b.fillPending(symbol, bar, now)
	b.checkProtection(symbol, bar, now)
	b.MarkToMarket(symbol, bar.Close, now)
}

func (b *PaperBroker) fillPending(symbol string, bar market.Kline, now time.Time) {
	var remaining []PendingOrder
	for _, order := range b.Pending {
		d := order.Decision
		if market.Normalize(d.Symbol) != market.Normalize(symbol) {
			remaining = append(remaining, order)
			continue
		}
		if err := b.executeDecision(d, bar.Open, now, "strategy"); err != nil {
			b.Executions = append(b.Executions, executionFromDecision(d, now, "rejected", 0, 0, 0, err.Error()))
			if isRiskIncreaseAction(d.Action) {
				b.OpenRejections = append(b.OpenRejections, decision.NewOpenRejectionFromDecision(d, err.Error()))
			}
		}
	}
	b.Pending = remaining
}

func (b *PaperBroker) executeDecision(d decision.Decision, rawPrice float64, now time.Time, reason string) error {
	symbol := market.Normalize(d.Symbol)
	switch d.Action {
	case "open_long", "open_short":
		side := sideFromAction(d.Action)
		if existing := b.Positions[symbol]; existing != nil && existing.Quantity > 0 {
			if existing.Side != side {
				return fmt.Errorf("%s 已有%s持仓，不允许双向持仓", symbol, existing.Side)
			}
			return fmt.Errorf("%s 已有%s持仓，open动作被拒绝", symbol, existing.Side)
		}
		return b.openPosition(d, side, rawPrice, now)
	case "add_long", "add_short":
		return b.addPosition(d, sideFromAction(d.Action), rawPrice, now)
	case "partial_close":
		return b.closePosition(d, rawPrice, now, false)
	case "close_long", "close_short":
		return b.closePosition(d, rawPrice, now, true)
	default:
		return nil
	}
}

func (b *PaperBroker) openPosition(d decision.Decision, side string, rawPrice float64, now time.Time) error {
	notional := d.PositionSizeUSD
	if notional <= 0 {
		notional = math.Max(10, b.Account.Equity*0.1)
	}
	leverage := d.Leverage
	if leverage <= 0 {
		leverage = 1
	}
	price := b.slippedPrice(side, "entry", rawPrice)
	qty := notional / price
	if result := b.evaluatePreflight(d, side, qty, price, leverage, "open"); !result.Allowed {
		return fmt.Errorf("preflight失败: %s", strings.Join(result.Reasons, "; "))
	}
	fee := b.fee(notional)
	margin := notional / float64(leverage)
	if b.Account.Cash-margin-fee < -1e-9 {
		return fmt.Errorf("虚拟账户可用余额不足")
	}
	b.Account.Cash -= fee
	b.Account.FeePaid += fee
	b.Account.SlippagePaid += math.Abs(price-rawPrice) * qty
	lifecycleID := fmt.Sprintf("%s_%s_%d", market.Normalize(d.Symbol), side, now.UnixMilli())
	initialRisk := math.Abs(price - effectiveStopLoss(d))
	pos := &PaperPosition{
		Symbol:       market.Normalize(d.Symbol),
		Side:         side,
		Quantity:     qty,
		AverageEntry: price,
		MarkPrice:    rawPrice,
		Leverage:     leverage,
		StopLoss:     effectiveStopLoss(d),
		TakeProfit:   effectiveTakeProfit(d),
		OpenedAt:     now,
		LifecycleID:  lifecycleID,
		InitialRisk:  initialRisk,
		PeakPrice:    rawPrice,
		TroughPrice:  rawPrice,
		EntryReason:  d.Reasoning,
		SignalID:     d.SignalID,
		SignalType:   d.SignalType,
	}
	b.Positions[pos.Symbol] = pos
	b.Lifecycles[lifecycleID] = &TradeLifecycle{
		LifecycleID: lifecycleID,
		Symbol:      pos.Symbol,
		Side:        side,
		EntryTime:   now,
		EntryPrice:  price,
		EntryReason: d.Reasoning,
		SignalID:    d.SignalID,
		SignalType:  d.SignalType,
		MFE:         0,
		MAE:         0,
	}
	b.Executions = append(b.Executions, executionFromDecision(d, now, "filled", price, qty, fee, "open"))
	b.recomputeAccount()
	return nil
}

func (b *PaperBroker) addPosition(d decision.Decision, side string, rawPrice float64, now time.Time) error {
	pos := b.Positions[market.Normalize(d.Symbol)]
	if pos == nil || pos.Quantity <= 0 {
		return fmt.Errorf("无可加仓持仓")
	}
	if pos.Side != side {
		return fmt.Errorf("加仓方向与持仓方向不一致")
	}
	notional := d.PositionSizeUSD
	if notional <= 0 {
		notional = pos.Quantity * pos.MarkPrice * 0.5
	}
	price := b.slippedPrice(side, "entry", rawPrice)
	qty := notional / price
	leverage := d.Leverage
	if leverage <= 0 {
		leverage = pos.Leverage
	}
	if result := b.evaluatePreflight(d, side, qty, price, leverage, "add"); !result.Allowed {
		return fmt.Errorf("preflight失败: %s", strings.Join(result.Reasons, "; "))
	}
	fee := b.fee(notional)
	newQty := pos.Quantity + qty
	pos.AverageEntry = (pos.AverageEntry*pos.Quantity + price*qty) / newQty
	pos.Quantity = newQty
	pos.MarkPrice = rawPrice
	if sl := effectiveStopLoss(d); sl > 0 {
		pos.StopLoss = sl
	}
	if tp := effectiveTakeProfit(d); tp > 0 {
		pos.TakeProfit = tp
	}
	b.Account.Cash -= fee
	b.Account.FeePaid += fee
	b.Account.SlippagePaid += math.Abs(price-rawPrice) * qty
	b.Executions = append(b.Executions, executionFromDecision(d, now, "filled", price, qty, fee, "add"))
	b.recomputeAccount()
	return nil
}

func (b *PaperBroker) closePosition(d decision.Decision, rawPrice float64, now time.Time, full bool) error {
	symbol := market.Normalize(d.Symbol)
	pos := b.Positions[symbol]
	if pos == nil || pos.Quantity <= 0 {
		return fmt.Errorf("无可平仓持仓")
	}
	if !full && d.ClosePercentage <= 0 {
		d.ClosePercentage = 50
	}
	qty := pos.Quantity
	if !full {
		qty = pos.Quantity * math.Min(100, d.ClosePercentage) / 100
	}
	if qty <= 0 {
		return fmt.Errorf("平仓数量为0")
	}
	price := b.slippedPrice(pos.Side, "exit", rawPrice)
	notional := qty * price
	if !full {
		minPartialCloseValue := trader.CalibratedPartialCloseMinValueUSDT(b.Exchange, d.Symbol)
		if notional < minPartialCloseValue {
			return fmt.Errorf("部分平仓名义额 %.4f USDT 低于最小值 %.2f USDT", notional, minPartialCloseValue)
		}
	}
	fee := b.fee(notional)
	pnl := pnlFor(pos.Side, pos.AverageEntry, price, qty)
	b.Account.Cash += pnl - fee
	b.Account.RealizedPnL += pnl
	b.Account.FeePaid += fee
	b.Account.SlippagePaid += math.Abs(price-rawPrice) * qty
	pos.Quantity -= qty
	if lifecycle := b.Lifecycles[pos.LifecycleID]; lifecycle != nil {
		lifecycle.Fees += fee
		lifecycle.ExitReason = d.Reasoning
		lifecycle.RealizedPnL += pnl
		lifecycle.ExecutionCount++
		if pos.InitialRisk > 0 && qty > 0 {
			lifecycle.RMultiple = lifecycle.RealizedPnL / (pos.InitialRisk * qty)
			lifecycle.FinalRMultiple = lifecycle.RMultiple
		}
		if pos.Quantity <= 1e-12 {
			lifecycle.ExitTime = now
			lifecycle.ExitPrice = price
			lifecycle.Closed = true
			lifecycle.DurationMinutes = now.Sub(lifecycle.EntryTime).Minutes()
			if lifecycle.RealizedPnL >= 0 {
				lifecycle.RecoveryMinutes = lifecycle.DurationMinutes
			}
		}
	}
	status := "filled"
	if !full {
		status = "partial_filled"
	}
	b.Executions = append(b.Executions, executionFromDecision(d, now, status, price, qty, fee, "close"))
	if pos.Quantity <= 1e-12 {
		delete(b.Positions, symbol)
	}
	b.recomputeAccount()
	return nil
}

func (b *PaperBroker) updateStopLoss(d decision.Decision, now time.Time) {
	pos := b.Positions[market.Normalize(d.Symbol)]
	if pos == nil {
		b.Executions = append(b.Executions, executionFromDecision(d, now, "rejected", 0, 0, 0, "无持仓可更新止损"))
		return
	}
	if d.NewStopLoss <= 0 {
		b.Executions = append(b.Executions, executionFromDecision(d, now, "rejected", 0, 0, 0, "new_stop_loss无效"))
		return
	}
	pos.StopLoss = d.NewStopLoss
	b.Executions = append(b.Executions, executionFromDecision(d, now, "updated", d.NewStopLoss, 0, 0, d.Reasoning))
}

func (b *PaperBroker) checkProtection(symbol string, bar market.Kline, now time.Time) {
	pos := b.Positions[market.Normalize(symbol)]
	if pos == nil {
		return
	}
	stopHit := false
	tpHit := false
	if pos.StopLoss > 0 {
		stopHit = (pos.Side == "long" && bar.Low <= pos.StopLoss) || (pos.Side == "short" && bar.High >= pos.StopLoss)
	}
	if pos.TakeProfit > 0 {
		tpHit = (pos.Side == "long" && bar.High >= pos.TakeProfit) || (pos.Side == "short" && bar.Low <= pos.TakeProfit)
	}
	if !stopHit && !tpHit {
		return
	}
	price := pos.StopLoss
	reason := "virtual_stop_loss"
	if tpHit && !stopHit {
		price = pos.TakeProfit
		reason = "virtual_take_profit"
	}
	d := decision.Decision{
		Symbol:    pos.Symbol,
		Action:    closeAction(pos.Side),
		Reasoning: reason,
	}
	_ = b.closePosition(d, price, now, true)
}

func (b *PaperBroker) MarkToMarket(symbol string, price float64, _ time.Time) {
	pos := b.Positions[market.Normalize(symbol)]
	if pos == nil {
		b.recomputeAccount()
		return
	}
	pos.MarkPrice = price
	if price > pos.PeakPrice || pos.PeakPrice == 0 {
		pos.PeakPrice = price
	}
	if price < pos.TroughPrice || pos.TroughPrice == 0 {
		pos.TroughPrice = price
	}
	b.updateLifecycleExcursions(pos, price)
	b.recomputeAccount()
}

func (b *PaperBroker) evaluatePreflight(d decision.Decision, side string, quantity float64, price float64, leverage int, intent string) trader.ExecutionPreflightResult {
	return trader.EvaluateExecutionPreflight(trader.ExecutionPreflightInput{
		Symbol:        market.Normalize(d.Symbol),
		Side:          side,
		Quantity:      quantity,
		Price:         price,
		Leverage:      leverage,
		MinOrderValue: trader.CalibratedOpenMinOrderValueUSDT(b.Exchange, d.Symbol),
		Positions:     b.preflightPositions(),
		Intent:        intent,
	})
}

func (b *PaperBroker) preflightPositions() []map[string]interface{} {
	positions := make([]map[string]interface{}, 0, len(b.Positions))
	for _, pos := range b.Positions {
		amount := pos.Quantity
		if pos.Side == "short" {
			amount = -amount
		}
		positions = append(positions, map[string]interface{}{
			"symbol":      pos.Symbol,
			"side":        pos.Side,
			"positionAmt": amount,
		})
	}
	return positions
}

func (b *PaperBroker) updateLifecycleExcursions(pos *PaperPosition, price float64) {
	if pos == nil || pos.InitialRisk <= 0 {
		return
	}
	lifecycle := b.Lifecycles[pos.LifecycleID]
	if lifecycle == nil {
		return
	}
	favorable := 0.0
	adverse := 0.0
	if pos.Side == "short" {
		favorable = pos.AverageEntry - price
		adverse = price - pos.AverageEntry
	} else {
		favorable = price - pos.AverageEntry
		adverse = pos.AverageEntry - price
	}
	if favorable > 0 {
		lifecycle.MFE = math.Max(lifecycle.MFE, favorable/pos.InitialRisk)
	}
	if adverse > 0 {
		lifecycle.MAE = math.Max(lifecycle.MAE, adverse/pos.InitialRisk)
		lifecycle.MaxDrawdownPct = math.Max(lifecycle.MaxDrawdownPct, adverse/pos.AverageEntry*100)
	}
}

func (b *PaperBroker) DecisionPositions(now time.Time) []decision.PositionInfo {
	positions := make([]decision.PositionInfo, 0, len(b.Positions))
	for _, pos := range b.Positions {
		notional := pos.Quantity * pos.MarkPrice
		margin := notional / float64(maxInt(pos.Leverage, 1))
		unrealized := pnlFor(pos.Side, pos.AverageEntry, pos.MarkPrice, pos.Quantity)
		pct := 0.0
		if margin > 0 {
			pct = unrealized / margin * 100
		}
		positions = append(positions, decision.PositionInfo{
			Symbol:           pos.Symbol,
			Side:             pos.Side,
			EntryPrice:       pos.AverageEntry,
			MarkPrice:        pos.MarkPrice,
			Quantity:         pos.Quantity,
			Leverage:         pos.Leverage,
			UnrealizedPnL:    unrealized,
			UnrealizedPnLPct: pct,
			MarginUsed:       margin,
			UpdateTime:       now.UnixMilli(),
			StopLoss:         pos.StopLoss,
			TakeProfit:       pos.TakeProfit,
		})
	}
	return positions
}

func (b *PaperBroker) AccountInfo() decision.AccountInfo {
	b.recomputeAccount()
	marginPct := 0.0
	if b.Account.Equity > 0 {
		marginPct = b.Account.MarginUsed / b.Account.Equity * 100
	}
	return decision.AccountInfo{
		TotalEquity:      b.Account.Equity,
		AvailableBalance: b.Account.Equity - b.Account.MarginUsed,
		TotalPnL:         b.Account.Equity - b.Account.InitialEquity,
		TotalPnLPct:      (b.Account.Equity - b.Account.InitialEquity) / b.Account.InitialEquity * 100,
		MarginUsed:       b.Account.MarginUsed,
		MarginUsedPct:    marginPct,
		PositionCount:    len(b.Positions),
	}
}

func (b *PaperBroker) recomputeAccount() {
	unrealized := 0.0
	margin := 0.0
	for _, pos := range b.Positions {
		unrealized += pnlFor(pos.Side, pos.AverageEntry, pos.MarkPrice, pos.Quantity)
		margin += pos.Quantity * pos.MarkPrice / float64(maxInt(pos.Leverage, 1))
	}
	b.Account.UnrealizedPnL = unrealized
	b.Account.MarginUsed = margin
	b.Account.Equity = b.Account.Cash + unrealized
}

func (b *PaperBroker) fee(notional float64) float64 {
	return notional * b.Costs.TakerFeeBPS / 10000
}

func (b *PaperBroker) slippedPrice(side, phase string, price float64) float64 {
	bps := b.Costs.SlippageBPS / 10000
	if bps <= 0 {
		return price
	}
	if (side == "long" && phase == "entry") || (side == "short" && phase == "exit") {
		return price * (1 + bps)
	}
	return price * (1 - bps)
}

func sideFromAction(action string) string {
	if strings.Contains(action, "short") {
		return "short"
	}
	return "long"
}

func closeAction(side string) string {
	if side == "short" {
		return "close_short"
	}
	return "close_long"
}

func isRiskIncreaseAction(action string) bool {
	return action == "open_long" || action == "open_short" || action == "add_long" || action == "add_short"
}

func effectiveStopLoss(d decision.Decision) float64 {
	if d.EffectiveStopLoss > 0 {
		return d.EffectiveStopLoss
	}
	return d.StopLoss
}

func effectiveTakeProfit(d decision.Decision) float64 {
	if d.EffectiveTakeProfit > 0 {
		return d.EffectiveTakeProfit
	}
	return d.TakeProfit
}

func pnlFor(side string, entry, exit, qty float64) float64 {
	if side == "short" {
		return (entry - exit) * qty
	}
	return (exit - entry) * qty
}

func executionFromDecision(d decision.Decision, now time.Time, status string, price, qty, fee float64, reason string) ExecutionEvent {
	return ExecutionEvent{
		Timestamp:         now,
		Symbol:            market.Normalize(d.Symbol),
		Action:            d.Action,
		Side:              sideFromAction(d.Action),
		Status:            status,
		Price:             price,
		Quantity:          qty,
		Fee:               fee,
		Reason:            reason,
		SignalID:          d.SignalID,
		SignalType:        d.SignalType,
		SignalCloseTime:   metadataInt64(d.StrategyMetadata, "signal_close_time"),
		DecisionCloseTime: metadataInt64(d.StrategyMetadata, "decision_close_time"),
	}
}

func metadataInt64(values map[string]any, key string) int64 {
	if len(values) == 0 {
		return 0
	}
	switch value := values[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
