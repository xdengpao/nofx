package historydb

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"nofx/market"
)

type RateLimitProfile struct {
	RequestsPerMinute int `json:"requests_per_minute,omitempty"`
	Concurrency       int `json:"concurrency,omitempty"`
	PageLimit         int `json:"page_limit,omitempty"`
	MaxRetries        int `json:"max_retries,omitempty"`
	InitialBackoffMS  int `json:"initial_backoff_ms,omitempty"`
	MaxBackoffMS      int `json:"max_backoff_ms,omitempty"`
}

type FetchOptions struct {
	Source     KlineSource
	Store      *Store
	Symbols    []string
	Timeframes []string
	DataFrom   time.Time
	DataTo     time.Time
	RateLimit  RateLimitProfile
}

type FetchSummary struct {
	ID                 string            `json:"id"`
	Source             string            `json:"source"`
	Symbols            []string          `json:"symbols"`
	Timeframes         []string          `json:"timeframes"`
	DataFrom           time.Time         `json:"data_from"`
	DataTo             time.Time         `json:"data_to"`
	Status             string            `json:"status"`
	RequestCount       int               `json:"request_count"`
	RateWaitCount      int               `json:"rate_wait_count"`
	RetryCount         int               `json:"retry_count"`
	InsertedCount      int               `json:"inserted_count"`
	DuplicateCount     int               `json:"duplicate_count"`
	Failed             map[string]string `json:"failed,omitempty"`
	StartedAt          time.Time         `json:"started_at"`
	FinishedAt         time.Time         `json:"finished_at,omitempty"`
	LastFetchedCloseMS int64             `json:"last_fetched_close_ms,omitempty"`
}

func NormalizeRateLimitProfile(profile RateLimitProfile) RateLimitProfile {
	if profile.RequestsPerMinute <= 0 {
		profile.RequestsPerMinute = 120
	}
	if profile.Concurrency <= 0 {
		profile.Concurrency = 1
	}
	if profile.PageLimit <= 0 {
		profile.PageLimit = 1000
	}
	if profile.MaxRetries <= 0 {
		profile.MaxRetries = 3
	}
	if profile.InitialBackoffMS <= 0 {
		profile.InitialBackoffMS = 500
	}
	if profile.MaxBackoffMS <= 0 {
		profile.MaxBackoffMS = 5000
	}
	return profile
}

func FetchToStore(ctx context.Context, opts FetchOptions) (FetchSummary, error) {
	if opts.Source == nil {
		opts.Source = NewBinanceFuturesKlineSource()
	}
	if opts.Store == nil {
		return FetchSummary{}, fmt.Errorf("历史数据库未打开")
	}
	if !opts.DataFrom.Before(opts.DataTo) {
		return FetchSummary{}, fmt.Errorf("data_from必须早于data_to")
	}
	opts.RateLimit = NormalizeRateLimitProfile(opts.RateLimit)
	summary := FetchSummary{
		ID:         fmt.Sprintf("fetch_%d", time.Now().UnixNano()),
		Source:     opts.Source.Name(),
		Symbols:    normalizeFetchSymbols(opts.Symbols),
		Timeframes: normalizeFetchTimeframes(opts.Timeframes),
		DataFrom:   opts.DataFrom,
		DataTo:     opts.DataTo,
		Status:     "running",
		Failed:     map[string]string{},
		StartedAt:  time.Now().UTC(),
	}
	if len(summary.Symbols) == 0 || len(summary.Timeframes) == 0 {
		return summary, fmt.Errorf("symbols/timeframes不能为空")
	}

	limiter := newRequestLimiter(opts.RateLimit.RequestsPerMinute)
	type job struct {
		symbol    string
		timeframe string
	}
	jobs := make(chan job)
	var mu sync.Mutex
	var wg sync.WaitGroup
	workerCount := opts.RateLimit.Concurrency
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				inserted, dup, req, waits, retries, lastClose, err := fetchPartition(ctx, opts, limiter, j.symbol, j.timeframe)
				mu.Lock()
				summary.InsertedCount += inserted
				summary.DuplicateCount += dup
				summary.RequestCount += req
				summary.RateWaitCount += waits
				summary.RetryCount += retries
				if lastClose > summary.LastFetchedCloseMS {
					summary.LastFetchedCloseMS = lastClose
				}
				if err != nil {
					summary.Failed[j.symbol+"|"+j.timeframe] = err.Error()
				}
				mu.Unlock()
			}
		}()
	}
	for _, symbol := range summary.Symbols {
		for _, timeframe := range summary.Timeframes {
			select {
			case <-ctx.Done():
				close(jobs)
				wg.Wait()
				summary.Status = "cancelled"
				summary.FinishedAt = time.Now().UTC()
				return summary, ctx.Err()
			case jobs <- job{symbol: symbol, timeframe: timeframe}:
			}
		}
	}
	close(jobs)
	wg.Wait()
	summary.FinishedAt = time.Now().UTC()
	if len(summary.Failed) > 0 {
		summary.Status = "failed"
		return summary, fmt.Errorf("部分历史数据抓取失败")
	}
	summary.Status = "completed"
	return summary, nil
}

func fetchPartition(ctx context.Context, opts FetchOptions, limiter *requestLimiter, symbol, timeframe string) (inserted, duplicates, requestCount, waitCount, retryCount int, lastCloseMS int64, err error) {
	step := TimeframeDuration(timeframe)
	if step <= 0 {
		return 0, 0, 0, 0, 0, 0, fmt.Errorf("不支持timeframe: %s", timeframe)
	}
	start := opts.DataFrom
	if maxClose, ok, err := opts.Store.MaxCloseTime(ctx, opts.Source.Name(), symbol, timeframe); err != nil {
		return 0, 0, 0, 0, 0, 0, err
	} else if ok && maxClose.After(start) {
		start = maxClose.Add(time.Millisecond)
	}
	for start.Before(opts.DataTo) {
		if err := ctx.Err(); err != nil {
			return inserted, duplicates, requestCount, waitCount, retryCount, lastCloseMS, err
		}
		if limiter.Wait(ctx) {
			waitCount++
		}
		req := FetchKlineRequest{Symbol: symbol, Timeframe: timeframe, Start: start, End: opts.DataTo, Limit: opts.RateLimit.PageLimit}
		var klines []market.Kline
		var fetchErr error
		for attempt := 0; attempt <= opts.RateLimit.MaxRetries; attempt++ {
			klines, fetchErr = opts.Source.FetchKlines(ctx, req)
			requestCount++
			if fetchErr == nil {
				break
			}
			if attempt < opts.RateLimit.MaxRetries {
				retryCount++
				sleep := backoffDuration(opts.RateLimit, attempt)
				select {
				case <-ctx.Done():
					return inserted, duplicates, requestCount, waitCount, retryCount, lastCloseMS, ctx.Err()
				case <-time.After(sleep):
				}
			}
		}
		if fetchErr != nil {
			return inserted, duplicates, requestCount, waitCount, retryCount, lastCloseMS, fetchErr
		}
		if len(klines) == 0 {
			return inserted, duplicates, requestCount, waitCount, retryCount, lastCloseMS, nil
		}
		wrote, dup, err := opts.Store.UpsertKlines(ctx, opts.Source.Name(), symbol, timeframe, klines)
		if err != nil {
			return inserted, duplicates, requestCount, waitCount, retryCount, lastCloseMS, err
		}
		inserted += wrote
		duplicates += dup
		last := klines[len(klines)-1]
		lastCloseMS = last.CloseTime
		next := time.UnixMilli(last.CloseTime).Add(time.Millisecond)
		if !next.After(start) {
			next = start.Add(step)
		}
		start = next
		if len(klines) < opts.RateLimit.PageLimit {
			return inserted, duplicates, requestCount, waitCount, retryCount, lastCloseMS, nil
		}
	}
	return inserted, duplicates, requestCount, waitCount, retryCount, lastCloseMS, nil
}

type requestLimiter struct {
	interval time.Duration
	lastMu   sync.Mutex
	last     time.Time
}

func newRequestLimiter(requestsPerMinute int) *requestLimiter {
	if requestsPerMinute <= 0 {
		requestsPerMinute = 120
	}
	interval := time.Minute / time.Duration(requestsPerMinute)
	return &requestLimiter{interval: interval}
}

func (l *requestLimiter) Wait(ctx context.Context) bool {
	l.lastMu.Lock()
	defer l.lastMu.Unlock()
	now := time.Now()
	next := l.last.Add(l.interval)
	if l.last.IsZero() || !next.After(now) {
		l.last = now
		return false
	}
	wait := next.Sub(now)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		l.last = time.Now()
		return true
	}
}

func backoffDuration(profile RateLimitProfile, attempt int) time.Duration {
	ms := profile.InitialBackoffMS
	for i := 0; i < attempt; i++ {
		ms *= 2
	}
	if ms > profile.MaxBackoffMS {
		ms = profile.MaxBackoffMS
	}
	return time.Duration(ms) * time.Millisecond
}

func normalizeFetchSymbols(symbols []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		symbol = market.Normalize(symbol)
		if symbol == "" || seen[symbol] {
			continue
		}
		seen[symbol] = true
		out = append(out, symbol)
	}
	return out
}

func normalizeFetchTimeframes(timeframes []string) []string {
	if len(timeframes) == 0 {
		timeframes = []string{"3m", "15m", "1h", "4h"}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(timeframes))
	for _, timeframe := range timeframes {
		timeframe = strings.ToLower(strings.TrimSpace(timeframe))
		if TimeframeDuration(timeframe) <= 0 || seen[timeframe] {
			continue
		}
		seen[timeframe] = true
		out = append(out, timeframe)
	}
	return out
}
