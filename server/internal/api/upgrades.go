package api

import (
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/protocol"
	"github.com/motao123/Argus/server/internal/model"
)

// ---- Agent 批量升级（借鉴 nezha force-update；逐机回执）----

type upgradeJob struct {
	ID        string                   `json:"id"`
	URL       string                   `json:"url"`
	SHA256    string                   `json:"sha256"`
	Version   string                   `json:"version"`
	CreatedAt time.Time                `json:"created_at"`
	Results   map[int64]upgradeReceipt `json:"results"`
}

type upgradeReceipt struct {
	ServerID int64  `json:"server_id"`
	Name     string `json:"name"`
	Status   string `json:"status"` // success / failure / offline
	Error    string `json:"error,omitempty"`
}

var (
	upgradeMu   sync.Mutex
	upgradeJobs = map[string]*upgradeJob{}
	upgradeSeq  int64
)

// listUpgradeJobs 升级任务列表与状态。
func (s *Server) listUpgradeJobs(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	upgradeMu.Lock()
	defer upgradeMu.Unlock()
	jobs := make([]*upgradeJob, 0, len(upgradeJobs))
	for _, j := range upgradeJobs {
		jobs = append(jobs, j)
	}
	ok(c, gin.H{"jobs": jobs})
}

// createUpgradeJob 发起批量升级（灰度：可选并发上限，逐机回执）。
func (s *Server) createUpgradeJob(c *gin.Context) {
	p := principalFromContext(c)
	if !p.IsAdmin {
		fail(c, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		ServerIDs []int64 `json:"server_ids"`
		URL       string  `json:"url"`
		SHA256    string  `json:"sha256"`
		Version   string  `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.ServerIDs) == 0 {
		fail(c, http.StatusBadRequest, "server_ids required")
		return
	}
	if req.URL == "" || req.SHA256 == "" {
		fail(c, http.StatusBadRequest, "url and sha256 required")
		return
	}
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		fail(c, http.StatusBadRequest, "url must be http(s)")
		return
	}

	upgradeMu.Lock()
	upgradeSeq++
	id := "up-" + strconv.FormatInt(upgradeSeq, 10)
	job := &upgradeJob{ID: id, URL: req.URL, SHA256: req.SHA256, Version: req.Version, CreatedAt: time.Now(), Results: map[int64]upgradeReceipt{}}
	upgradeJobs[id] = job
	upgradeMu.Unlock()

	// 逐机下发（同步等待，保留 30s 超时）
	for _, sid := range req.ServerIDs {
		var srv model.Server
		if err := s.DB.First(&srv, sid).Error; err != nil {
			job.Results[sid] = upgradeReceipt{ServerID: sid, Name: "?", Status: "failure", Error: "server not found"}
			continue
		}
		peer := s.Agents.Peer(sid)
		if peer == nil {
			job.Results[sid] = upgradeReceipt{ServerID: sid, Name: srv.Name, Status: "offline"}
			continue
		}
		resp, err := peer.Call(protocol.MethodUpgrade, protocol.UpgradeParams{URL: req.URL, SHA256: req.SHA256, Version: req.Version}, 30*time.Second)
		if err != nil {
			job.Results[sid] = upgradeReceipt{ServerID: sid, Name: srv.Name, Status: "failure", Error: err.Error()}
			continue
		}
		if resp.Error != nil {
			job.Results[sid] = upgradeReceipt{ServerID: sid, Name: srv.Name, Status: "failure", Error: resp.Error.Message}
			continue
		}
		job.Results[sid] = upgradeReceipt{ServerID: sid, Name: srv.Name, Status: "success"}
		s.auditLog(c, "server.upgrade", srv.Name)
	}
	ok(c, job)
}
