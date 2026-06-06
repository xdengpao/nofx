package api

import (
	"net/http"
	"os"
	"sync"
	"time"

	"nofx/storage"

	"github.com/gin-gonic/gin"
)

type storageMigrationJob struct {
	MigrationID string                  `json:"migration_id"`
	Status      string                  `json:"status"`
	StartedAt   time.Time               `json:"started_at"`
	EndedAt     time.Time               `json:"ended_at,omitempty"`
	Error       string                  `json:"error,omitempty"`
	Report      storage.MigrationReport `json:"report,omitempty"`
}

type storageMigrationManager struct {
	mu   sync.RWMutex
	jobs map[string]*storageMigrationJob
}

var localStorageMigrations = &storageMigrationManager{jobs: map[string]*storageMigrationJob{}}

func (s *Server) registerStorageRoutes(group *gin.RouterGroup) {
	group.GET("/diagnostics", s.handleStorageDiagnostics)
	group.POST("/migrations", s.handleStorageMigrationStart)
	group.GET("/migrations/:migration_id", s.handleStorageMigrationStatus)
}

func (s *Server) handleStorageDiagnostics(c *gin.Context) {
	if s == nil || s.storageLayout == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Storage Layout未配置"})
		return
	}
	runtime := storage.RuntimeConfig{Root: s.storageLayout.Root}
	if s.storageRuntime != nil {
		runtime = *s.storageRuntime
	}
	diag, err := storage.DiskUsage(runtime, *s.storageLayout)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, diag)
}

func (s *Server) handleStorageMigrationStart(c *gin.Context) {
	if s == nil || s.storageLayout == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Storage Layout未配置"})
		return
	}
	var req struct {
		Sources        []string `json:"sources"`
		Overwrite      bool     `json:"overwrite"`
		DryRun         bool     `json:"dry_run"`
		CreateSymlinks bool     `json:"create_symlinks"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	sourceRoot, err := os.Getwd()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取源目录失败: " + err.Error()})
		return
	}
	job := localStorageMigrations.start()
	go func() {
		report, err := storage.Migrate(storage.MigrationOptions{
			SourceRoot: sourceRoot,
			Layout:     *s.storageLayout,
			Sources:    req.Sources,
			Overwrite:  req.Overwrite,
			DryRun:     req.DryRun,
		})
		if err != nil {
			localStorageMigrations.fail(job.MigrationID, report, err)
			return
		}
		localStorageMigrations.complete(job.MigrationID, report)
	}()
	c.JSON(http.StatusAccepted, job)
}

func (s *Server) handleStorageMigrationStatus(c *gin.Context) {
	job, ok := localStorageMigrations.get(c.Param("migration_id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "migration不存在"})
		return
	}
	c.JSON(http.StatusOK, job)
}

func (m *storageMigrationManager) start() *storageMigrationJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := "migration_" + time.Now().UTC().Format("20060102_150405_000000000")
	job := &storageMigrationJob{MigrationID: id, Status: "running", StartedAt: time.Now().UTC()}
	m.jobs[id] = job
	return job
}

func (m *storageMigrationManager) complete(id string, report storage.MigrationReport) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job := m.jobs[id]; job != nil {
		job.Status = "completed"
		job.Report = report
		job.EndedAt = time.Now().UTC()
	}
}

func (m *storageMigrationManager) fail(id string, report storage.MigrationReport, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if job := m.jobs[id]; job != nil {
		job.Status = "failed"
		job.Report = report
		job.Error = err.Error()
		job.EndedAt = time.Now().UTC()
	}
}

func (m *storageMigrationManager) get(id string) (*storageMigrationJob, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	return job, ok
}
