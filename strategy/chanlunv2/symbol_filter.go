package chanlunv2

import (
	"nofx/market"
	"strings"
)

func isChanlunV2TradableCryptoSymbol(symbol string) bool {
	normalized := market.Normalize(symbol)
	if normalized == "" {
		return false
	}
	if !(strings.HasSuffix(normalized, "USDT") || strings.HasSuffix(normalized, "USDC")) {
		return false
	}
	nonCryptoPrefixes := []string{"XAU", "XAG", "CL", "COPPER", "NG", "SI"}
	for _, prefix := range nonCryptoPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return false
		}
	}
	return true
}
