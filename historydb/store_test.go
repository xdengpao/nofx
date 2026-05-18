package historydb

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"nofx/market"
)

func TestStoreSchemaUpsertAndQuery(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()
	klines := fixtureKlines(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 5, 3*time.Minute)
	inserted, dup, err := store.UpsertKlines(ctx, "binance-futures", "btcusdt", "3m", klines)
	if err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if inserted != 5 || dup != 0 {
		t.Fatalf("首次写入计数错误 inserted=%d dup=%d", inserted, dup)
	}
	inserted, dup, err = store.UpsertKlines(ctx, "binance-futures", "BTCUSDT", "3m", klines)
	if err != nil {
		t.Fatalf("重复写入失败: %v", err)
	}
	if inserted != 0 || dup != 5 {
		t.Fatalf("重复写入应幂等 inserted=%d dup=%d", inserted, dup)
	}
	got, err := store.QueryKlines(ctx, "binance-futures", "BTCUSDT", "3m", time.UnixMilli(klines[0].CloseTime), time.UnixMilli(klines[4].CloseTime+1))
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("查询数量错误: %d", len(got))
	}
}

func TestLastClosedKlinesFiltersAsOf(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()
	klines := fixtureKlines(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 10, 3*time.Minute)
	if _, _, err := store.UpsertKlines(ctx, "binance-futures", "ETHUSDT", "3m", klines); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	asOf := time.UnixMilli(klines[4].CloseTime)
	got, err := store.LastClosedKlines(ctx, "binance-futures", "ETHUSDT", "3m", 3, asOf)
	if err != nil {
		t.Fatalf("LastClosedKlines失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("应返回3根，got %d", len(got))
	}
	if got[len(got)-1].CloseTime > asOf.UnixMilli() {
		t.Fatalf("返回了未来K线")
	}
	if got[0].OpenTime >= got[1].OpenTime {
		t.Fatalf("返回结果应按时间升序")
	}
}

func TestDetectGaps(t *testing.T) {
	klines := fixtureKlines(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 3, 3*time.Minute)
	klines = append(klines, fixtureKlines(time.Date(2026, 1, 1, 0, 18, 0, 0, time.UTC), 1, 3*time.Minute)...)
	gaps := FindGaps("binance-futures", "BTCUSDT", "3m", klines)
	if len(gaps) == 0 {
		t.Fatal("应识别gap")
	}
}

func TestFetchToStoreWithMockSource(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	source := &mockSource{pages: [][]market.Kline{
		fixtureKlines(start, 2, 3*time.Minute),
		fixtureKlines(start.Add(6*time.Minute), 1, 3*time.Minute),
	}}
	summary, err := FetchToStore(context.Background(), FetchOptions{
		Source:     source,
		Store:      store,
		Symbols:    []string{"BTCUSDT"},
		Timeframes: []string{"3m"},
		DataFrom:   start,
		DataTo:     start.Add(12 * time.Minute),
		RateLimit:  RateLimitProfile{RequestsPerMinute: 100000, PageLimit: 2, Concurrency: 1},
	})
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if summary.RequestCount != 2 || summary.InsertedCount != 3 {
		t.Fatalf("抓取摘要错误: %+v", summary)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "history.sqlite"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	return store
}

func fixtureKlines(start time.Time, n int, step time.Duration) []market.Kline {
	out := make([]market.Kline, 0, n)
	price := 100.0
	for i := 0; i < n; i++ {
		open := start.Add(time.Duration(i) * step)
		closeTime := open.Add(step).Add(-time.Millisecond)
		out = append(out, market.Kline{
			OpenTime:  open.UnixMilli(),
			CloseTime: closeTime.UnixMilli(),
			Open:      price,
			High:      price + 2,
			Low:       price - 1,
			Close:     price + 1,
			Volume:    1000,
		})
		price += 1
	}
	return out
}

type mockSource struct {
	pages [][]market.Kline
	calls int
}

func (m *mockSource) Name() string                  { return "binance-futures" }
func (m *mockSource) SupportedTimeframes() []string { return []string{"3m"} }
func (m *mockSource) MaxLimit(string) int           { return 2 }
func (m *mockSource) FetchKlines(context.Context, FetchKlineRequest) ([]market.Kline, error) {
	if m.calls >= len(m.pages) {
		return nil, nil
	}
	page := m.pages[m.calls]
	m.calls++
	if len(page) == 0 {
		return nil, fmt.Errorf("empty page")
	}
	return page, nil
}
