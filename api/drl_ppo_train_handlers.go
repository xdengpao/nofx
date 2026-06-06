package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"nofx/backtest"
	"nofx/drltrain"
	"nofx/historydb"
	"nofx/storage"

	"github.com/gin-gonic/gin"
)

var drlPPOMarketCatalogCache = struct {
	mu        sync.Mutex
	expiresAt time.Time
	value     historydb.MarketCatalog
}{}

func (s *Server) registerDRLPPOTrainRoutes(group *gin.RouterGroup) {
	group.GET("/health", s.handleDRLPPOHealth)
	group.GET("/storage", s.handleStorageDiagnostics)
	group.GET("/markets", s.handleDRLPPOMarkets)
	group.GET("/history/coverage", s.handleDRLPPOHistoryCoverage)
	group.POST("/history/gaps", s.handleDRLPPOHistoryGaps)
	group.POST("/history/fetch", s.handleDRLPPOHistoryFetch)
	group.GET("/history/fetch/:run_id", s.handleDRLPPOHistoryFetchStatus)
	group.GET("/jobs", s.handleDRLPPOJobs)
	group.POST("/jobs", s.handleDRLPPOCreateJob)
	group.GET("/jobs/:job_id", s.handleDRLPPOJob)
	group.POST("/jobs/:job_id/cancel", s.handleDRLPPOCancelJob)
	group.GET("/jobs/:job_id/logs", s.handleDRLPPOJobLogs)
	group.GET("/models", s.handleDRLPPOModels)
	group.POST("/models/:model_id/evaluate", s.handleDRLPPOEvaluateModel)
	group.GET("/models/:model_id/evaluation", s.handleDRLPPOModelEvaluation)
	group.POST("/models/:model_id/staging-config", s.handleDRLPPOStagingConfig)
}

func (s *Server) handleDRLPPOHealth(c *gin.Context) {
	env := drltrain.EnvironmentStatus{}
	if s != nil && s.drlTrain != nil {
		env = s.drlTrain.EnvironmentStatus()
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled":                   s != nil && s.drlTrain != nil,
		"python_available":          env.PythonAvailable,
		"python_bin":                env.PythonBin,
		"python_path":               env.PythonPath,
		"train_script":              env.TrainScript,
		"train_script_available":    env.TrainScriptAvailable,
		"evaluate_script":           env.EvaluateScript,
		"evaluate_script_available": env.EvaluateScriptAvailable,
		"training_ready":            env.Ready,
		"environment_errors":        env.Errors,
		"storage_root":              s.storageRootForAPI(),
		"onnx_runtime":              "stub",
		"message":                   "当前默认DRL推理后端为stub，真实ONNX Runtime依赖build tag",
	})
}

func (s *Server) handleDRLPPOMarkets(c *gin.Context) {
	refresh := strings.EqualFold(c.Query("refresh"), "true")
	now := time.Now()
	drlPPOMarketCatalogCache.mu.Lock()
	if !refresh && now.Before(drlPPOMarketCatalogCache.expiresAt) && len(drlPPOMarketCatalogCache.value.Sources) > 0 {
		value := drlPPOMarketCatalogCache.value
		drlPPOMarketCatalogCache.mu.Unlock()
		c.JSON(http.StatusOK, value)
		return
	}
	drlPPOMarketCatalogCache.mu.Unlock()

	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()
	catalog, err := historydb.FetchMarketCatalog(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "同步交易所标的失败: " + err.Error()})
		return
	}
	drlPPOMarketCatalogCache.mu.Lock()
	drlPPOMarketCatalogCache.value = catalog
	drlPPOMarketCatalogCache.expiresAt = now.Add(10 * time.Minute)
	drlPPOMarketCatalogCache.mu.Unlock()
	c.JSON(http.StatusOK, catalog)
}

func (s *Server) handleDRLPPOHistoryCoverage(c *gin.Context) {
	store, err := historydb.Open(s.defaultBacktestHistoryDBPath())
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

func (s *Server) handleDRLPPOHistoryGaps(c *gin.Context) {
	var req struct {
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
	store, err := historydb.Open(s.defaultBacktestHistoryDBPath())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer store.Close()
	ok, detail, gaps, err := store.CheckKlineCoverage(c.Request.Context(), defaultString(req.Source, backtest.DefaultSource), req.Symbol, req.Timeframe, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if gaps == nil {
		gaps = []historydb.QualityIssue{}
	}
	c.JSON(http.StatusOK, gin.H{"ok": ok, "detail": detail, "gaps": gaps})
}

func (s *Server) handleDRLPPOHistoryFetch(c *gin.Context) {
	var req struct {
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
	job := localBacktestJobs.start("drl_history_fetch", cancel)
	go func() {
		store, err := historydb.Open(s.defaultBacktestHistoryDBPath())
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
		job.FetchSummary = &summary
		job.Progress = backtest.Progress{RunID: summary.ID, Status: summary.Status, Executions: summary.InsertedCount, Rejections: len(summary.Failed)}
		if err != nil {
			localBacktestJobs.fail(job.RunID, err)
			return
		}
		localBacktestJobs.complete(job.RunID)
	}()
	c.JSON(http.StatusAccepted, job)
}

func (s *Server) handleDRLPPOHistoryFetchStatus(c *gin.Context) {
	job, ok := localBacktestJobs.get(c.Param("run_id"))
	if !ok || job.Type != "drl_history_fetch" {
		c.JSON(http.StatusNotFound, gin.H{"error": "补数据任务不存在"})
		return
	}
	c.JSON(http.StatusOK, job)
}

func (s *Server) handleDRLPPOJobs(c *gin.Context) {
	c.JSON(http.StatusOK, s.drlTrain.List())
}

func (s *Server) handleDRLPPOCreateJob(c *gin.Context) {
	var req drltrain.TrainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	job, err := s.drlTrain.Start(c.Request.Context(), req)
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "并发上限") {
			code = http.StatusConflict
		} else if strings.Contains(err.Error(), "训练环境不可用") {
			code = http.StatusServiceUnavailable
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (s *Server) handleDRLPPOJob(c *gin.Context) {
	job, ok := s.drlTrain.Get(c.Param("job_id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "训练任务不存在"})
		return
	}
	c.JSON(http.StatusOK, job)
}

func (s *Server) handleDRLPPOCancelJob(c *gin.Context) {
	if err := s.drlTrain.Cancel(c.Param("job_id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancel_requested"})
}

func (s *Server) handleDRLPPOJobLogs(c *gin.Context) {
	offset, _ := parseInt64Query(c, "offset", 0)
	tail, _ := parseInt64Query(c, "tail_bytes", 0)
	logs, err := s.drlTrain.Logs(c.Param("job_id"), c.DefaultQuery("stream", "stdout"), offset, tail)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, logs)
}

func (s *Server) handleDRLPPOModels(c *gin.Context) {
	models, err := s.drlTrain.Models()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, models)
}

func (s *Server) handleDRLPPOEvaluateModel(c *gin.Context) {
	var req drltrain.EvaluationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := s.drlTrain.Evaluate(c.Request.Context(), c.Param("model_id"), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) handleDRLPPOModelEvaluation(c *gin.Context) {
	modelID := c.Param("model_id")
	models, err := s.drlTrain.Models()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for _, model := range models {
		if model.ModelID == modelID && model.EvaluationPath != "" && s.storageLayout != nil {
			path, err := storage.ResolveUnderRoot(*s.storageLayout, model.EvaluationPath)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			c.File(path)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "评估报告不存在"})
}

func (s *Server) handleDRLPPOStagingConfig(c *gin.Context) {
	result, err := s.drlTrain.StagingConfig(c.Param("model_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) storageRootForAPI() string {
	if s != nil && s.storageLayout != nil {
		return s.storageLayout.Root
	}
	return ""
}

func parseInt64Query(c *gin.Context, key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback, err
	}
	return parsed, nil
}
