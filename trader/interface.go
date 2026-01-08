package trader

import "time"

// Trader 交易器统一接口
// 支持多个交易平台（币安、Hyperliquid等）
type Trader interface {
	// GetBalance 获取账户余额
	GetBalance() (map[string]interface{}, error)

	// GetPositions 获取所有持仓
	GetPositions() ([]map[string]interface{}, error)

	// OpenLong 开多仓
	OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error)

	// OpenShort 开空仓
	OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error)

	// CloseLong 平多仓（quantity=0表示全部平仓）
	CloseLong(symbol string, quantity float64) (map[string]interface{}, error)

	// CloseShort 平空仓（quantity=0表示全部平仓）
	CloseShort(symbol string, quantity float64) (map[string]interface{}, error)

	// SetLeverage 设置杠杆
	SetLeverage(symbol string, leverage int) error

	// GetMarketPrice 获取市场价格
	GetMarketPrice(symbol string) (float64, error)

	// SetStopLoss 设置止损单
	SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error

	// SetTakeProfit 设置止盈单
	SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error

	// CancelStopOrders 取消该币种的止盈/止损单（已废弃：会同时删除止损和止盈）
	// 请使用 CancelStopLossOrders 或 CancelTakeProfitOrders
	CancelStopOrders(symbol string) error

	// CancelStopLossOrders 仅取消止损单（修复 BUG：调整止损时不删除止盈）
	CancelStopLossOrders(symbol string) error

	// CancelTakeProfitOrders 仅取消止盈单（修复 BUG：调整止盈时不删除止损）
	CancelTakeProfitOrders(symbol string) error

	// CancelAllOrders 取消该币种的所有挂单
	CancelAllOrders(symbol string) error

	// FormatQuantity 格式化数量到正确的精度
	FormatQuantity(symbol string, quantity float64) (string, error)

	// 🆕 新增：订单历史查询
	GetOrderHistory(symbol string, startTime, endTime int64, limit int) ([]OrderRecord, error)

	// 🆕 新增：成交历史查询
	GetTradeHistory(symbol string, startTime, endTime int64, limit int) ([]TradeRecord, error)

	// 🆕 新增：获取订单状态
	GetOrderStatus(symbol string, orderID int64) (*OrderRecord, error)
}

// OrderRecord 订单记录
type OrderRecord struct {
	OrderID      int64     `json:"order_id"`
	Symbol       string    `json:"symbol"`
	Side         string    `json:"side"`          // BUY/SELL
	PositionSide string    `json:"position_side"` // LONG/SHORT/BOTH
	Type         string    `json:"type"`          // MARKET/LIMIT/STOP_MARKET/TAKE_PROFIT_MARKET
	Status       string    `json:"status"`        // NEW/FILLED/CANCELED
	Price        float64   `json:"price"`
	AvgPrice     float64   `json:"avg_price"`
	OrigQty      float64   `json:"orig_qty"`
	ExecutedQty  float64   `json:"executed_qty"`
	StopPrice    float64   `json:"stop_price"`
	RealizedPnL  float64   `json:"realized_pnl"`
	Commission   float64   `json:"commission"`
	CreateTime   time.Time `json:"create_time"`
	UpdateTime   time.Time `json:"update_time"`
	IsAutoClose  bool      `json:"is_auto_close"` // 是否为自动平仓(止盈/止损)
	CloseReason  string    `json:"close_reason"`  // STOP_LOSS/TAKE_PROFIT/LIQUIDATION
}

// TradeRecord 成交记录
type TradeRecord struct {
	TradeID      int64     `json:"trade_id"`
	OrderID      int64     `json:"order_id"`
	Symbol       string    `json:"symbol"`
	Side         string    `json:"side"`
	Price        float64   `json:"price"`
	Qty          float64   `json:"qty"`
	QuoteQty     float64   `json:"quote_qty"`
	RealizedPnL  float64   `json:"realized_pnl"`
	Commission   float64   `json:"commission"`
	Time         time.Time `json:"time"`
	PositionSide string    `json:"position_side"`
	Buyer        bool      `json:"buyer"`
	Maker        bool      `json:"maker"`
}
