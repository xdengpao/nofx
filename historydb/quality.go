package historydb

import (
	"fmt"
	"strings"
	"time"

	"nofx/market"
)

func TimeframeDuration(timeframe string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(timeframe)) {
	case "3m":
		return 3 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "1h":
		return time.Hour
	case "4h":
		return 4 * time.Hour
	default:
		return 0
	}
}

func ValidateKlines(source, symbol, timeframe string, klines []market.Kline) []QualityIssue {
	var issues []QualityIssue
	step := TimeframeDuration(timeframe)
	var prev *market.Kline
	for i := range klines {
		k := klines[i]
		switch {
		case k.OpenTime <= 0 || k.CloseTime <= 0 || k.OpenTime >= k.CloseTime:
			issues = append(issues, issue(source, symbol, timeframe, "invalid_time", k.OpenTime, k.CloseTime, "open_time/close_time非法"))
		case k.Open <= 0 || k.High <= 0 || k.Low <= 0 || k.Close <= 0:
			issues = append(issues, issue(source, symbol, timeframe, "invalid_price", k.OpenTime, k.CloseTime, "OHLC必须大于0"))
		case k.High < k.Low || k.High < k.Open || k.High < k.Close || k.Low > k.Open || k.Low > k.Close:
			issues = append(issues, issue(source, symbol, timeframe, "invalid_ohlc", k.OpenTime, k.CloseTime, "high/low与open/close不一致"))
		}
		if prev != nil {
			if k.OpenTime <= prev.OpenTime {
				issues = append(issues, issue(source, symbol, timeframe, "out_of_order", prev.OpenTime, k.OpenTime, "K线倒序或重复"))
			}
			if step > 0 && k.OpenTime-prev.OpenTime > step.Milliseconds() {
				issues = append(issues, issue(source, symbol, timeframe, "gap", prev.CloseTime, k.OpenTime, fmt.Sprintf("缺口 %.0f 分钟", float64(k.OpenTime-prev.OpenTime)/60000)))
			}
		}
		prev = &klines[i]
	}
	return issues
}

func FindGaps(source, symbol, timeframe string, klines []market.Kline) []QualityIssue {
	step := TimeframeDuration(timeframe)
	if step <= 0 || len(klines) < 2 {
		return nil
	}
	var issues []QualityIssue
	for i := 1; i < len(klines); i++ {
		delta := klines[i].OpenTime - klines[i-1].OpenTime
		if delta > step.Milliseconds() {
			issues = append(issues, issue(source, symbol, timeframe, "gap", klines[i-1].CloseTime, klines[i].OpenTime, fmt.Sprintf("期望间隔%dms，实际间隔%dms", step.Milliseconds(), delta)))
		}
		if delta <= 0 {
			issues = append(issues, issue(source, symbol, timeframe, "duplicate_or_order", klines[i-1].OpenTime, klines[i].OpenTime, "K线重复或倒序"))
		}
	}
	return issues
}

func issue(source, symbol, timeframe, issueType string, start, end int64, detail string) QualityIssue {
	item := QualityIssue{
		Source:      normalizeSource(source),
		Symbol:      market.Normalize(symbol),
		Timeframe:   strings.ToLower(strings.TrimSpace(timeframe)),
		IssueType:   issueType,
		StartTimeMS: start,
		EndTimeMS:   end,
		Detail:      detail,
	}
	item.ID = qualityIssueID(item)
	return item
}

func qualityIssueID(item QualityIssue) string {
	return fmt.Sprintf("%s:%s:%s:%s:%d:%d", item.Source, item.Symbol, item.Timeframe, item.IssueType, item.StartTimeMS, item.EndTimeMS)
}
