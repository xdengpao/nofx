package historydb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nofx/market"

	_ "modernc.org/sqlite"
)

const SchemaVersion = 1

type Store struct {
	db     *sql.DB
	path   string
	source string
}

type KlineRecord struct {
	Source      string
	Symbol      string
	Timeframe   string
	Kline       market.Kline
	QuoteVolume float64
	TradeCount  int64
	Quality     string
	FetchedAt   time.Time
	RawHash     string
}

type Coverage struct {
	Source    string `json:"source"`
	Symbol    string `json:"symbol"`
	Timeframe string `json:"timeframe"`
	Count     int64  `json:"count"`
	FromMS    int64  `json:"from_ms"`
	ToMS      int64  `json:"to_ms"`
	DataHash  string `json:"data_hash,omitempty"`
}

type QualityIssue struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Symbol      string `json:"symbol"`
	Timeframe   string `json:"timeframe"`
	IssueType   string `json:"issue_type"`
	StartTimeMS int64  `json:"start_time_ms"`
	EndTimeMS   int64  `json:"end_time_ms"`
	Detail      string `json:"detail"`
}

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("历史数据库路径不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("创建历史数据库目录失败: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开历史数据库失败: %w", err)
	}
	store := &Store{db: db, path: path, source: "binance-futures"}
	if err := store.InitSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func OpenWithDB(db *sql.DB) *Store {
	return &Store{db: db, source: "binance-futures"}
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) DB() *sql.DB  { return s.db }
func (s *Store) Path() string { return s.path }

func (s *Store) InitSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("历史数据库未打开")
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS klines (
			source TEXT NOT NULL,
			symbol TEXT NOT NULL,
			timeframe TEXT NOT NULL,
			open_time_ms INTEGER NOT NULL,
			close_time_ms INTEGER NOT NULL,
			open REAL NOT NULL,
			high REAL NOT NULL,
			low REAL NOT NULL,
			close REAL NOT NULL,
			volume REAL NOT NULL,
			quote_volume REAL NOT NULL DEFAULT 0,
			trade_count INTEGER NOT NULL DEFAULT 0,
			quality TEXT NOT NULL DEFAULT 'ok',
			fetched_at TEXT NOT NULL,
			raw_hash TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (source, symbol, timeframe, open_time_ms)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_klines_lookup
			ON klines(source, symbol, timeframe, close_time_ms)`,
		`CREATE TABLE IF NOT EXISTS fetch_runs (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			symbols TEXT NOT NULL,
			timeframes TEXT NOT NULL,
			data_from TEXT NOT NULL,
			data_to TEXT NOT NULL,
			status TEXT NOT NULL,
			request_count INTEGER NOT NULL DEFAULT 0,
			inserted_count INTEGER NOT NULL DEFAULT 0,
			duplicate_count INTEGER NOT NULL DEFAULT 0,
			retry_count INTEGER NOT NULL DEFAULT 0,
			rate_wait_count INTEGER NOT NULL DEFAULT 0,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS quality_issues (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			symbol TEXT NOT NULL,
			timeframe TEXT NOT NULL,
			issue_type TEXT NOT NULL,
			start_time_ms INTEGER NOT NULL,
			end_time_ms INTEGER NOT NULL,
			detail TEXT NOT NULL,
			detected_at TEXT NOT NULL
		)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, ?);`,
	}
	for i, stmt := range statements {
		if i == len(statements)-1 {
			if _, err := s.db.ExecContext(ctx, stmt, time.Now().UTC().Format(time.RFC3339)); err != nil {
				return fmt.Errorf("记录schema版本失败: %w", err)
			}
			continue
		}
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("初始化历史库schema失败: %w", err)
		}
	}
	return nil
}

func (s *Store) UpsertKlines(ctx context.Context, source, symbol, timeframe string, klines []market.Kline) (inserted int, duplicates int, err error) {
	source, symbol, timeframe, err = normalizeKey(source, symbol, timeframe)
	if err != nil {
		return 0, 0, err
	}
	if len(klines) == 0 {
		return 0, 0, nil
	}
	if issues := ValidateKlines(source, symbol, timeframe, klines); len(issues) > 0 {
		for _, issue := range issues {
			if issue.IssueType != "gap" {
				return 0, 0, fmt.Errorf("%s %s 数据质量错误: %s", symbol, timeframe, issue.Detail)
			}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO klines (
		source, symbol, timeframe, open_time_ms, close_time_ms,
		open, high, low, close, volume, quote_volume, trade_count,
		quality, fetched_at, raw_hash, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(source, symbol, timeframe, open_time_ms) DO UPDATE SET
		close_time_ms=excluded.close_time_ms,
		open=excluded.open,
		high=excluded.high,
		low=excluded.low,
		close=excluded.close,
		volume=excluded.volume,
		quote_volume=excluded.quote_volume,
		trade_count=excluded.trade_count,
		quality=excluded.quality,
		fetched_at=excluded.fetched_at,
		raw_hash=excluded.raw_hash,
		updated_at=excluded.updated_at`)
	if err != nil {
		return 0, 0, err
	}
	defer stmt.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, k := range klines {
		exists, err := s.exists(ctx, tx, source, symbol, timeframe, k.OpenTime)
		if err != nil {
			return 0, 0, err
		}
		if exists {
			duplicates++
		} else {
			inserted++
		}
		hash := hashKline(k)
		if _, err = stmt.ExecContext(ctx, source, symbol, timeframe, k.OpenTime, k.CloseTime, k.Open, k.High, k.Low, k.Close, k.Volume, 0, 0, "ok", now, hash, now, now); err != nil {
			return 0, 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, err
	}
	return inserted, duplicates, nil
}

func (s *Store) exists(ctx context.Context, tx *sql.Tx, source, symbol, timeframe string, openTime int64) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM klines WHERE source=? AND symbol=? AND timeframe=? AND open_time_ms=?`, source, symbol, timeframe, openTime).Scan(&count)
	return count > 0, err
}

func (s *Store) QueryKlines(ctx context.Context, source, symbol, timeframe string, from, to time.Time) ([]market.Kline, error) {
	source, symbol, timeframe, err := normalizeKey(source, symbol, timeframe)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT open_time_ms, open, high, low, close, volume, close_time_ms
		FROM klines
		WHERE source=? AND symbol=? AND timeframe=? AND close_time_ms >= ? AND close_time_ms < ?
		ORDER BY open_time_ms ASC`, source, symbol, timeframe, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanKlines(rows)
}

func (s *Store) LastClosedKlines(ctx context.Context, source, symbol, timeframe string, limit int, asOf time.Time) ([]market.Kline, error) {
	source, symbol, timeframe, err := normalizeKey(source, symbol, timeframe)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT open_time_ms, open, high, low, close, volume, close_time_ms
		FROM klines
		WHERE source=? AND symbol=? AND timeframe=? AND close_time_ms <= ?
		ORDER BY close_time_ms DESC LIMIT ?`, source, symbol, timeframe, asOf.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	klines, err := scanKlines(rows)
	if err != nil {
		return nil, err
	}
	return klines, nil
}

func (s *Store) MaxCloseTime(ctx context.Context, source, symbol, timeframe string) (time.Time, bool, error) {
	source, symbol, timeframe, err := normalizeKey(source, symbol, timeframe)
	if err != nil {
		return time.Time{}, false, err
	}
	var max sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(close_time_ms) FROM klines WHERE source=? AND symbol=? AND timeframe=?`, source, symbol, timeframe).Scan(&max); err != nil {
		return time.Time{}, false, err
	}
	if !max.Valid {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(max.Int64).UTC(), true, nil
}

func (s *Store) Inspect(ctx context.Context, source string) ([]Coverage, error) {
	source = normalizeSource(source)
	rows, err := s.db.QueryContext(ctx, `SELECT source, symbol, timeframe, COUNT(1), MIN(close_time_ms), MAX(close_time_ms)
		FROM klines WHERE source=? GROUP BY source, symbol, timeframe ORDER BY symbol, timeframe`, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Coverage
	for rows.Next() {
		var c Coverage
		if err := rows.Scan(&c.Source, &c.Symbol, &c.Timeframe, &c.Count, &c.FromMS, &c.ToMS); err != nil {
			return nil, err
		}
		hash, _ := s.DataHash(ctx, c.Source, c.Symbol, c.Timeframe, time.UnixMilli(c.FromMS), time.UnixMilli(c.ToMS+1))
		c.DataHash = hash
		result = append(result, c)
	}
	return result, rows.Err()
}

func (s *Store) HasKlineCoverage(ctx context.Context, source, symbol, timeframe string, from, to time.Time) (bool, string, error) {
	klines, err := s.QueryKlines(ctx, source, symbol, timeframe, from, to)
	if err != nil {
		return false, "", err
	}
	if len(klines) == 0 {
		return false, "没有历史K线", nil
	}
	if klines[0].CloseTime > from.UnixMilli() {
		return false, fmt.Sprintf("起始覆盖不足: first_close=%s need_from=%s", time.UnixMilli(klines[0].CloseTime).Format(time.RFC3339), from.Format(time.RFC3339)), nil
	}
	last := klines[len(klines)-1]
	if last.CloseTime < to.Add(-TimeframeDuration(timeframe)).UnixMilli() {
		return false, fmt.Sprintf("结束覆盖不足: last_close=%s need_to=%s", time.UnixMilli(last.CloseTime).Format(time.RFC3339), to.Format(time.RFC3339)), nil
	}
	if gaps := FindGaps(source, symbol, timeframe, klines); len(gaps) > 0 {
		return false, gaps[0].Detail, nil
	}
	return true, "", nil
}

func (s *Store) DataHash(ctx context.Context, source, symbol, timeframe string, from, to time.Time) (string, error) {
	klines, err := s.QueryKlines(ctx, source, symbol, timeframe, from, to)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, k := range klines {
		_, _ = fmt.Fprintf(h, "%s|%s|%s|%d|%d|%.10f|%.10f|%.10f|%.10f|%.10f\n",
			source, symbol, timeframe, k.OpenTime, k.CloseTime, k.Open, k.High, k.Low, k.Close, k.Volume)
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

func (s *Store) DetectGaps(ctx context.Context, source, symbol, timeframe string, from, to time.Time) ([]QualityIssue, error) {
	klines, err := s.QueryKlines(ctx, source, symbol, timeframe, from, to)
	if err != nil {
		return nil, err
	}
	return FindGaps(source, symbol, timeframe, klines), nil
}

func (s *Store) RecordQualityIssues(ctx context.Context, issues []QualityIssue) error {
	if len(issues) == 0 {
		return nil
	}
	stmt, err := s.db.PrepareContext(ctx, `INSERT OR REPLACE INTO quality_issues(
		id, source, symbol, timeframe, issue_type, start_time_ms, end_time_ms, detail, detected_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, issue := range issues {
		if issue.ID == "" {
			issue.ID = qualityIssueID(issue)
		}
		if _, err := stmt.ExecContext(ctx, issue.ID, issue.Source, issue.Symbol, issue.Timeframe, issue.IssueType, issue.StartTimeMS, issue.EndTimeMS, issue.Detail, now); err != nil {
			return err
		}
	}
	return nil
}

func scanKlines(rows *sql.Rows) ([]market.Kline, error) {
	var klines []market.Kline
	for rows.Next() {
		var k market.Kline
		if err := rows.Scan(&k.OpenTime, &k.Open, &k.High, &k.Low, &k.Close, &k.Volume, &k.CloseTime); err != nil {
			return nil, err
		}
		klines = append(klines, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(klines, func(i, j int) bool { return klines[i].OpenTime < klines[j].OpenTime })
	return klines, nil
}

func normalizeKey(source, symbol, timeframe string) (string, string, string, error) {
	source = normalizeSource(source)
	symbol = market.Normalize(symbol)
	timeframe = strings.ToLower(strings.TrimSpace(timeframe))
	if source == "" {
		return "", "", "", fmt.Errorf("source不能为空")
	}
	if symbol == "" {
		return "", "", "", fmt.Errorf("symbol不能为空")
	}
	if TimeframeDuration(timeframe) <= 0 {
		return "", "", "", fmt.Errorf("不支持的K线周期: %s", timeframe)
	}
	return source, symbol, timeframe, nil
}

func normalizeSource(source string) string {
	source = strings.TrimSpace(strings.ToLower(source))
	if source == "" {
		return "binance-futures"
	}
	return source
}

func hashKline(k market.Kline) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%.10f|%.10f|%.10f|%.10f|%.10f", k.OpenTime, k.CloseTime, k.Open, k.High, k.Low, k.Close, k.Volume)))
	return hex.EncodeToString(sum[:])[:16]
}
