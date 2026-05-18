package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nofx/backtest"
	"nofx/historydb"

	"github.com/gin-gonic/gin"
)

type backtestJob struct {
	RunID     string            `json:"run_id"`
	Type      string            `json:"type"`
	Status    string            `json:"status"`
	Progress  backtest.Progress `json:"progress,omitempty"`
	StartedAt time.Time         `json:"started_at"`
	EndedAt   time.Time         `json:"ended_at,omitempty"`
	Error     string            `json:"error,omitempty"`
	cancel    context.CancelFunc
}

type backtestJobManager struct {
	mu   sync.RWMutex
	jobs map[string]*backtestJob
}

var localBacktestJobs = &backtestJobManager{jobs: map[string]*backtestJob{}}

func backtestAPIEnabled() bool {
	return strings.EqualFold(os.Getenv("NOFX_BACKTEST_API_ENABLED"), "true")
}

func (s *Server) registerBacktestRoutes(group *gin.RouterGroup) {
	group.GET("/health", s.handleBacktestHealth)
	group.GET("/history/inspect", s.handleBacktestHistoryInspect)
	group.GET("/history/klines", s.handleBacktestHistoryKlines)
	group.POST("/history/fetch", s.handleBacktestHistoryFetch)
	group.POST("/history/gaps", s.handleBacktestHistoryGaps)
	group.GET("/runs", s.handleBacktestRuns)
	group.POST("/runs", s.handleBacktestRun)
	group.POST("/runs/batch", s.handleBacktestBatch)
	group.GET("/runs/:run_id", s.handleBacktestRunStatus)
	group.POST("/runs/:run_id/cancel", s.handleBacktestCancel)
	group.GET("/reports/:run_id", s.handleBacktestReport)
	group.GET("/reports/:run_id/files/*file", s.handleBacktestReportFile)
}

func (s *Server) handleBacktestHistoryKlines(c *gin.Context) {
	store, err := historydb.Open(c.DefaultQuery("db", backtest.DefaultHistoryDBPath))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer store.Close()
	loc, _ := time.LoadLocation(c.DefaultQuery("timezone", backtest.DefaultTimezone))
	from, err := backtest.ParseConfigTime(c.DefaultQuery("from", "1970-01-01"), loc)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	toRaw := c.DefaultQuery("to", time.Now().Format(time.RFC3339))
	to, err := backtest.ParseConfigTime(toRaw, loc)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	klines, err := store.QueryKlines(c.Request.Context(), c.DefaultQuery("source", backtest.DefaultSource), c.Query("symbol"), c.DefaultQuery("timeframe", "1h"), from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	type klineDTO struct {
		OpenTime  int64   `json:"open_time"`
		CloseTime int64   `json:"close_time"`
		Open      float64 `json:"open"`
		High      float64 `json:"high"`
		Low       float64 `json:"low"`
		Close     float64 `json:"close"`
		Volume    float64 `json:"volume"`
	}
	out := make([]klineDTO, 0, len(klines))
	for _, k := range klines {
		out = append(out, klineDTO{OpenTime: k.OpenTime, CloseTime: k.CloseTime, Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume})
	}
	c.JSON(http.StatusOK, gin.H{
		"symbol":    c.Query("symbol"),
		"timeframe": c.DefaultQuery("timeframe", "1h"),
		"limit":     len(out),
		"klines":    out,
	})
}

func (s *Server) handleBacktestHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"enabled": true, "dry_run": true, "live_trading": false, "scope": "local_dev"})
}

func (s *Server) handleBacktestHistoryInspect(c *gin.Context) {
	store, err := historydb.Open(c.DefaultQuery("db", backtest.DefaultHistoryDBPath))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer store.Close()
	coverage, err := store.Inspect(c.Request.Context(), c.DefaultQuery("source", backtest.DefaultSource))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, coverage)
}

func (s *Server) handleBacktestHistoryFetch(c *gin.Context) {
	var req struct {
		DB         string                     `json:"db"`
		Source     string                     `json:"source"`
		Symbols    []string                   `json:"symbols"`
		Timeframes []string                   `json:"timeframes"`
		DataFrom   string                     `json:"data_from"`
		DataTo     string                     `json:"data_to"`
		Timezone   string                     `json:"timezone"`
		RateLimit  historydb.RateLimitProfile `json:"rate_limit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	loc, err := time.LoadLocation(defaultString(req.Timezone, backtest.DefaultTimezone))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	from, err := backtest.ParseConfigTime(req.DataFrom, loc)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	to := time.Now().In(loc)
	if strings.TrimSpace(req.DataTo) != "" {
		to, err = backtest.ParseConfigTime(req.DataTo, loc)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := localBacktestJobs.start("history_fetch", cancel)
	go func() {
		store, err := historydb.Open(defaultString(req.DB, backtest.DefaultHistoryDBPath))
		if err != nil {
			localBacktestJobs.fail(job.RunID, err)
			return
		}
		defer store.Close()
		summary, err := historydb.FetchToStore(ctx, historydb.FetchOptions{
			Source:     historydb.NewBinanceFuturesKlineSource(),
			Store:      store,
			Symbols:    req.Symbols,
			Timeframes: req.Timeframes,
			DataFrom:   from,
			DataTo:     to,
			RateLimit:  req.RateLimit,
		})
		job.Progress = backtest.Progress{RunID: summary.ID, Status: summary.Status, Executions: summary.InsertedCount, Rejections: len(summary.Failed)}
		if err != nil {
			localBacktestJobs.fail(job.RunID, err)
			return
		}
		localBacktestJobs.complete(job.RunID)
	}()
	c.JSON(http.StatusAccepted, job)
}

func (s *Server) handleBacktestHistoryGaps(c *gin.Context) {
	var req struct {
		DB        string `json:"db"`
		Source    string `json:"source"`
		Symbol    string `json:"symbol"`
		Timeframe string `json:"timeframe"`
		From      string `json:"from"`
		To        string `json:"to"`
		Timezone  string `json:"timezone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	loc, _ := time.LoadLocation(defaultString(req.Timezone, backtest.DefaultTimezone))
	from, err := backtest.ParseConfigTime(req.From, loc)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	to, err := backtest.ParseConfigTime(req.To, loc)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	store, err := historydb.Open(defaultString(req.DB, backtest.DefaultHistoryDBPath))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer store.Close()
	gaps, err := store.DetectGaps(c.Request.Context(), defaultString(req.Source, backtest.DefaultSource), req.Symbol, req.Timeframe, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gaps)
}

func (s *Server) handleBacktestRuns(c *gin.Context) {
	c.JSON(http.StatusOK, localBacktestJobs.list())
}

func (s *Server) handleBacktestRun(c *gin.Context) {
	var cfg backtest.BacktestConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := localBacktestJobs.start("backtest_run", cancel)
	go func() {
		runner, err := backtest.NewRunner(&cfg, nil)
		if err != nil {
			localBacktestJobs.fail(job.RunID, err)
			return
		}
		defer runner.Store.Close()
		result, err := runner.Run(ctx)
		job.Progress = result.Progress
		if err != nil {
			localBacktestJobs.fail(job.RunID, err)
			return
		}
		localBacktestJobs.complete(job.RunID)
	}()
	c.JSON(http.StatusAccepted, job)
}

func (s *Server) handleBacktestBatch(c *gin.Context) {
	var cfg backtest.BatchConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := localBacktestJobs.start("backtest_batch", cancel)
	go func() {
		_, err := (&backtest.BatchRunner{Config: cfg}).Run(ctx)
		if err != nil {
			localBacktestJobs.fail(job.RunID, err)
			return
		}
		localBacktestJobs.complete(job.RunID)
	}()
	c.JSON(http.StatusAccepted, job)
}

func (s *Server) handleBacktestRunStatus(c *gin.Context) {
	job, ok := localBacktestJobs.get(c.Param("run_id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "run不存在"})
		return
	}
	c.JSON(http.StatusOK, job)
}

func (s *Server) handleBacktestCancel(c *gin.Context) {
	if !localBacktestJobs.cancel(c.Param("run_id")) {
		c.JSON(http.StatusNotFound, gin.H{"error": "run不存在或不可取消"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

func (s *Server) handleBacktestReport(c *gin.Context) {
	path := filepath.Join(backtest.DefaultOutputDir, filepath.Base(c.Param("run_id")), "report.json")
	c.File(path)
}

func (s *Server) handleBacktestReportFile(c *gin.Context) {
	runID := filepath.Base(c.Param("run_id"))
	file := strings.TrimPrefix(c.Param("file"), "/")
	clean := filepath.Clean(file)
	if strings.HasPrefix(clean, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法文件路径"})
		return
	}
	path := filepath.Join(backtest.DefaultOutputDir, runID, clean)
	if strings.HasSuffix(clean, ".json") {
		var raw json.RawMessage
		data, err := os.ReadFile(path)
		if err == nil && json.Unmarshal(data, &raw) == nil {
			c.Data(http.StatusOK, "application/json", data)
			return
		}
	}
	c.File(path)
}

func (m *backtestJobManager) start(kind string, cancel context.CancelFunc) *backtestJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := "job_" + time.Now().UTC().Format("20060102_150405_000000000")
	job := &backtestJob{RunID: id, Type: kind, Status: "running", StartedAt: time.Now().UTC(), cancel: cancel}
	m.jobs[id] = job
	return job
}

func (m *backtestJobManager) fail(id string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job := m.jobs[id]; job != nil {
		job.Status = "failed"
		job.Error = err.Error()
		job.EndedAt = time.Now().UTC()
	}
}

func (m *backtestJobManager) complete(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job := m.jobs[id]; job != nil {
		job.Status = "completed"
		job.EndedAt = time.Now().UTC()
	}
}

func (m *backtestJobManager) cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.jobs[id]
	if job == nil || job.cancel == nil {
		return false
	}
	job.cancel()
	job.Status = "cancelled"
	job.EndedAt = time.Now().UTC()
	return true
}

func (m *backtestJobManager) get(id string) (*backtestJob, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	return job, ok
}

func (m *backtestJobManager) list() []*backtestJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*backtestJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		out = append(out, job)
	}
	return out
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
