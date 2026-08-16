package api

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
)

const (
	defaultUpgradeConcurrency = 2
	maxUpgradeConcurrency     = 16
)

var upgradeStartMu sync.Mutex

// upgradeResumeDelay 是 Server 重启后重新启动 pending 升级任务的宽限期，
// 让 Agent 有时间重连，避免启动瞬间把所有目标标记为 offline。
const defaultUpgradeResumeDelay = 30 * time.Second

// InitializeUpgradeJobs repairs jobs left by an unclean restart:
//   - in-flight per-machine results are marked "interrupted";
//   - jobs that were "running" fall back to "pending" so their unfinished
//     targets are picked up again (finished targets are not re-run);
//   - every "pending" job is re-queued after a short grace period.
func (s *Server) InitializeUpgradeJobs() error {
	now := time.Now()
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.UpgradeResult{}).Where("status = ?", "running").Updates(map[string]any{
			"status": "interrupted", "error": "server restarted during upgrade", "finished_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&model.UpgradeJob{}).Where("status = ?", "running").Updates(map[string]any{
			"status": "pending", "started_at": nil, "finished_at": nil,
		}).Error
	}); err != nil {
		return err
	}
	var pending []model.UpgradeJob
	if err := s.DB.Where("status = ?", "pending").Order("id").Find(&pending).Error; err != nil {
		return err
	}
	delay := s.upgradeResumeDelay
	if delay <= 0 {
		delay = defaultUpgradeResumeDelay
	}
	for i := range pending {
		id := pending[i].ID
		time.AfterFunc(delay, func() { s.runUpgradeJob(id) })
	}
	return nil
}

func (s *Server) listUpgradeJobs(c *gin.Context) {
	var jobs []model.UpgradeJob
	if err := s.DB.Preload("Results", func(db *gorm.DB) *gorm.DB { return db.Order("id") }).Order("id DESC").Limit(100).Find(&jobs).Error; err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"jobs": jobs})
}

func (s *Server) createUpgradeJob(c *gin.Context) {
	p := principalFromContext(c)
	var req struct {
		ServerIDs   []int64 `json:"server_ids"`
		URL         string  `json:"url"`
		SHA256      string  `json:"sha256"`
		Version     string  `json:"version"`
		Concurrency int     `json:"concurrency"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.ServerIDs) == 0 {
		fail(c, http.StatusBadRequest, "server_ids required")
		return
	}
	req.URL, req.SHA256 = strings.TrimSpace(req.URL), strings.ToLower(strings.TrimSpace(req.SHA256))
	if req.URL == "" || len(req.SHA256) != 64 {
		fail(c, http.StatusBadRequest, "url and 64-character sha256 required")
		return
	}
	if _, err := hex.DecodeString(req.SHA256); err != nil {
		fail(c, http.StatusBadRequest, "sha256 must be hexadecimal")
		return
	}
	u, err := url.ParseRequestURI(req.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		fail(c, http.StatusBadRequest, "url must be an absolute http(s) URL")
		return
	}
	if req.Concurrency <= 0 {
		req.Concurrency = defaultUpgradeConcurrency
	}
	if req.Concurrency > maxUpgradeConcurrency {
		fail(c, http.StatusBadRequest, fmt.Sprintf("concurrency must not exceed %d", maxUpgradeConcurrency))
		return
	}

	seen := make(map[int64]struct{}, len(req.ServerIDs))
	ids := make([]int64, 0, len(req.ServerIDs))
	for _, id := range req.ServerIDs {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; !exists {
			seen[id], ids = struct{}{}, append(ids, id)
		}
	}
	if len(ids) == 0 {
		fail(c, http.StatusBadRequest, "valid server_ids required")
		return
	}
	job := model.UpgradeJob{URL: req.URL, SHA256: req.SHA256, Version: strings.TrimSpace(req.Version), Status: "pending", Concurrency: req.Concurrency, TargetCount: len(ids), CreatedBy: p.UserID}
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&job).Error; err != nil {
			return err
		}
		results := make([]model.UpgradeResult, 0, len(ids))
		for _, id := range ids {
			var server model.Server
			name := "?"
			status, message := "pending", ""
			finished := (*time.Time)(nil)
			if err := tx.First(&server, id).Error; err != nil {
				// 目标不存在：立即失败并打上完成时间，避免结果停留在 pending。
				now := time.Now()
				status, message, finished = "failure", "server not found", &now
			} else {
				name = server.Name
			}
			results = append(results, model.UpgradeResult{JobID: job.ID, ServerID: id, ServerName: name, Status: status, Error: message, FinishedAt: finished})
		}
		return tx.Create(&results).Error
	}); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	s.auditLog(c, "server.upgrade.create", fmt.Sprintf("job=%d targets=%d sha256=%s", job.ID, len(ids), job.SHA256))
	_ = s.DB.Preload("Results").First(&job, job.ID).Error
	go s.runUpgradeJob(job.ID)
	c.JSON(http.StatusAccepted, job)
}

func (s *Server) runUpgradeJob(jobID int64) {
	// Prevent startup recovery and request creation from starting the same pending job twice.
	upgradeStartMu.Lock()
	res := s.DB.Model(&model.UpgradeJob{}).Where("id = ? AND status = ?", jobID, "pending").Updates(map[string]any{"status": "running", "started_at": time.Now()})
	upgradeStartMu.Unlock()
	if res.Error != nil || res.RowsAffected == 0 {
		return
	}
	var job model.UpgradeJob
	if s.DB.First(&job, jobID).Error != nil {
		return
	}
	var targets []model.UpgradeResult
	if s.DB.Where("job_id = ? AND status = ?", jobID, "pending").Order("id").Find(&targets).Error != nil {
		return
	}
	workers := job.Concurrency
	if workers < 1 {
		workers = defaultUpgradeConcurrency
	}
	if workers > len(targets) {
		workers = len(targets)
	}
	queue := make(chan model.UpgradeResult)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for target := range queue {
				s.runUpgradeTarget(&job, &target)
			}
		}()
	}
	for _, target := range targets {
		queue <- target
	}
	close(queue)
	wg.Wait()
	now := time.Now()
	_ = s.DB.Model(&model.UpgradeJob{}).Where("id = ? AND status = ?", jobID, "running").Updates(map[string]any{"status": "completed", "finished_at": now}).Error
}

func (s *Server) runUpgradeTarget(job *model.UpgradeJob, target *model.UpgradeResult) {
	now := time.Now()
	if s.DB.Model(target).Where("status = ?", "pending").Updates(map[string]any{"status": "running", "started_at": now}).RowsAffected == 0 {
		return
	}
	status, message := "success", ""
	peer := s.Agents.Peer(target.ServerID)
	if peer == nil {
		status = "offline"
	} else {
		resp, err := peer.Call(protocol.MethodUpgrade, protocol.UpgradeParams{URL: job.URL, SHA256: job.SHA256, Version: job.Version}, 6*time.Minute)
		if err != nil {
			status, message = "failure", err.Error()
		} else if resp.Error != nil {
			status, message = "failure", resp.Error.Message
		}
	}
	finished := time.Now()
	_ = s.DB.Model(target).Updates(map[string]any{"status": status, "error": message, "finished_at": finished}).Error
	_ = s.DB.Create(&model.AuditLog{UserID: job.CreatedBy, Action: "server.upgrade.result", Detail: fmt.Sprintf("job=%d server=%d status=%s", job.ID, target.ServerID, status), CreatedAt: finished}).Error
}
