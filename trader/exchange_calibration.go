package trader

import "strings"

func calibratedExchangeMinNotionalUSDT(exchange, symbol string) float64 {
	return CalibratedExchangeMinNotionalUSDT(exchange, symbol)
}

func CalibratedExchangeMinNotionalUSDT(exchange, symbol string) float64 {
	exchange = strings.ToLower(strings.TrimSpace(exchange))
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	switch exchange {
	case "binance":
		switch symbol {
		case "BTCUSDT":
			return 50
		case "ETHUSDT":
			return 20
		default:
			return 5
		}
	case "aster":
		return 5
	default:
		return 5
	}
}

func calibratedOpenMinOrderValueUSDT(exchange, symbol string) float64 {
	return CalibratedOpenMinOrderValueUSDT(exchange, symbol)
}

func CalibratedOpenMinOrderValueUSDT(exchange, symbol string) float64 {
	return maxFloat(minPreflightOrderValueUSDT, CalibratedExchangeMinNotionalUSDT(exchange, symbol))
}

func calibratedPartialCloseMinValueUSDT(exchange, symbol string) float64 {
	return CalibratedPartialCloseMinValueUSDT(exchange, symbol)
}

func CalibratedPartialCloseMinValueUSDT(exchange, symbol string) float64 {
	return maxFloat(minPartialCloseOrderValueUSDT, CalibratedExchangeMinNotionalUSDT(exchange, symbol))
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
