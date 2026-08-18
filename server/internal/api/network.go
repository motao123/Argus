package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/motao123/Argus/protocol"
)

// traceMaxHops 路由追踪的最大跳数与单跳超时上限（防滥用与过长时间占用）。
const (
	traceMaxHops      = 64
	tracePerHopMaxS   = 10
	traceGlobalMaxS   = 120
	bandwidthMaxSec   = 60
	meshMaxConcurrent = 4
)

// traceParams 单源路由追踪请求体。
type traceRequest struct {
	Target     string `json:"target"`
	Protocol   string `json:"protocol,omitempty"` // icmp/tcp/udp；空 = icmp
	MaxHops    int    `json:"max_hops,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

// traceResultItem 一次 trace 的完整结果（含来源标注）。
type traceResultItem struct {
	SourceID   int64                 `json:"source_id"`
	SourceName string                `json:"source_name"`
	Target     string                `json:"target"`
	Trace      *protocol.TraceResult `json:"trace"`
}

// validateTraceRequest 校验并规范化 trace 请求参数。
func validateTraceRequest(req *traceRequest) bool {
	if strings.TrimSpace(req.Target) == "" {
		return false
	}
	switch req.Protocol {
	case "", "icmp", "tcp", "udp":
	default:
		return false
	}
	if req.MaxHops > traceMaxHops {
		req.MaxHops = traceMaxHops
	}
	if req.TimeoutSec > tracePerHopMaxS {
		req.TimeoutSec = tracePerHopMaxS
	}
	return true
}

// serverTrace 对指定服务器执行单次路由追踪。
func (s *Server) serverTrace(c *gin.Context) {
	id := mustID(c)
	srv, authOK := s.authorizeServer(c, id, ScopeServerExec)
	if !authOK {
		return
	}
	var req traceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if !validateTraceRequest(&req) {
		fail(c, http.StatusBadRequest, "target required; protocol must be icmp/tcp/udp")
		return
	}
	peer := s.Agents.Peer(id)
	if peer == nil {
		fail(c, http.StatusGatewayTimeout, "server offline", "server.offline")
		return
	}
	hops := req.MaxHops
	if hops <= 0 {
		hops = 30
	}
	timeout := time.Duration(tracePerHopMaxS)*time.Duration(hops)*time.Second + 15*time.Second
	if timeout > traceGlobalMaxS*time.Second {
		timeout = traceGlobalMaxS * time.Second
	}
	resp, err := peer.Call(protocol.MethodTrace, protocol.TraceParams{
		Target: req.Target, Protocol: protocol.TraceProtocol(req.Protocol),
		MaxHops: hops, TimeoutSec: req.TimeoutSec,
	}, timeout)
	if err != nil {
		fail(c, http.StatusGatewayTimeout, err.Error(), "server.offline")
		return
	}
	if resp.Error != nil {
		fail(c, http.StatusBadGateway, resp.Error.Message)
		return
	}
	var tr protocol.TraceResult
	raw, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(raw, &tr); err != nil {
		fail(c, http.StatusInternalServerError, "bad agent response")
		return
	}
	ok(c, gin.H{"trace": tr, "server_id": id, "server_name": srv.Name, "target": req.Target})
}

// traceMeshRequest 多源路由追踪请求体。
// mode: all_to_all（每个源追踪每个目标）/ one_to_all（仅第一个源追踪全部目标）。
type traceMeshRequest struct {
	SourceIDs []int64  `json:"source_ids"`         // 源服务器 ID 列表
	Targets   []string `json:"targets"`            // 目标 host 列表
	Mode      string   `json:"mode,omitempty"`     // all_to_all / one_to_all
	Protocol  string   `json:"protocol,omitempty"` // icmp/tcp/udp
	MaxHops   int      `json:"max_hops,omitempty"`
}

// targetSrvIP 取目标服务器的上报 IP（优先 IPv4，回退 IPv6）；
// 无上报时返回空串（由 agent 端连接错误兜底）。
func targetSrvIP(s *Server, id int64) string {
	if st := s.Store.Get(id); st != nil {
		if st.Host.IPv4 != "" {
			return st.Host.IPv4
		}
		if st.Host.IP != "" {
			return st.Host.IP
		}
		return st.Host.IPv6
	}
	return ""
}

// traceMesh 多源×多目标路由追踪（同步聚合返回；并发上限 4）。
func (s *Server) traceMesh(c *gin.Context) {
	var req traceMeshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	if len(req.SourceIDs) == 0 || len(req.Targets) == 0 {
		fail(c, http.StatusBadRequest, "source_ids and targets required")
		return
	}
	if req.Mode != "one_to_all" {
		req.Mode = "all_to_all"
	}
	if len(req.SourceIDs) > 20 || len(req.Targets) > 20 {
		fail(c, http.StatusBadRequest, "at most 20 sources and 20 targets")
		return
	}
	// 预检来源归属与在线状态
	sources := make([]modelServerView, 0, len(req.SourceIDs))
	for _, id := range req.SourceIDs {
		srv, authOK := s.authorizeServer(c, id, ScopeServerExec)
		if !authOK {
			return
		}
		sources = append(sources, modelServerView{ID: srv.ID, Name: srv.Name})
	}
	sourceIDs := req.SourceIDs
	if req.Mode == "one_to_all" {
		sourceIDs = sourceIDs[:1]
	}
	type job struct {
		sourceID int64
		target   string
	}
	var jobs []job
	for _, sid := range sourceIDs {
		for _, target := range req.Targets {
			jobs = append(jobs, job{sid, target})
		}
	}
	results := make([]traceResultItem, 0, len(jobs))
	nameByID := make(map[int64]string, len(sources))
	for _, sv := range sources {
		nameByID[sv.ID] = sv.Name
	}
	// 并发上限内的并发执行
	sem := make(chan struct{}, meshMaxConcurrent)
	resCh := make(chan traceResultItem, len(jobs))
	for _, j := range jobs {
		sem <- struct{}{}
		go func(j job) {
			defer func() { <-sem }()
			resCh <- s.traceOne(j.sourceID, nameByID[j.sourceID], j.target, req.Protocol, req.MaxHops)
		}(j)
	}
	for i := 0; i < len(jobs); i++ {
		results = append(results, <-resCh)
	}
	ok(c, gin.H{"results": results, "mode": req.Mode})
}

type modelServerView struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// traceOne 对单台源服务器执行一次 trace 并返回带标注的结果。
func (s *Server) traceOne(sourceID int64, sourceName, target, proto string, maxHops int) traceResultItem {
	item := traceResultItem{SourceID: sourceID, SourceName: sourceName, Target: target}
	peer := s.Agents.Peer(sourceID)
	if peer == nil {
		item.Trace = &protocol.TraceResult{Error: "source server offline"}
		return item
	}
	hops := maxHops
	if hops <= 0 {
		hops = 30
	}
	if hops > traceMaxHops {
		hops = traceMaxHops
	}
	timeout := time.Duration(tracePerHopMaxS)*time.Duration(hops)*time.Second + 15*time.Second
	if timeout > traceGlobalMaxS*time.Second {
		timeout = traceGlobalMaxS * time.Second
	}
	resp, err := peer.Call(protocol.MethodTrace, protocol.TraceParams{
		Target: target, Protocol: protocol.TraceProtocol(proto), MaxHops: hops,
	}, timeout)
	if err != nil {
		item.Trace = &protocol.TraceResult{Error: err.Error()}
		return item
	}
	if resp.Error != nil {
		item.Trace = &protocol.TraceResult{Error: resp.Error.Message}
		return item
	}
	var tr protocol.TraceResult
	raw, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(raw, &tr); err != nil {
		item.Trace = &protocol.TraceResult{Error: "bad agent response"}
		return item
	}
	item.Trace = &tr
	return item
}

// bandwidthRequest 带宽测速请求体。
// target 可为 "host:port"（源 agent 直接测速）或服务器 ID（源 agent → 目标 agent）。
type bandwidthRequest struct {
	SourceID int64  `json:"source_id"`
	Target   string `json:"target"`             // host:port 或服务器 ID
	Duration int    `json:"duration,omitempty"` // 秒；默认 5，上限 60
	Parallel int    `json:"parallel,omitempty"` // 并发；默认 1，上限 8
}

// bandwidthTest 双 agent 带宽测速：先让目标 agent 监听端口，再让源 agent 向其发送数据。
func (s *Server) bandwidthTest(c *gin.Context) {
	var req bandwidthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "bad request")
		return
	}
	source, authOK := s.authorizeServer(c, req.SourceID, ScopeServerExec)
	if !authOK {
		return
	}
	if req.Target == "" {
		fail(c, http.StatusBadRequest, "target required (host:port or server id)")
		return
	}
	duration := req.Duration
	if duration <= 0 {
		duration = 5
	}
	if duration > bandwidthMaxSec {
		duration = bandwidthMaxSec
	}
	parallel := req.Parallel
	if parallel <= 0 {
		parallel = 1
	}
	if parallel > 8 {
		parallel = 8
	}

	// 目标解析：数字 = 服务器 ID（走目标 agent 监听）；否则视为 host:port
	var probeTarget string
	if targetID, err := strconv.ParseInt(req.Target, 10, 64); err == nil {
		if _, authOK := s.authorizeServer(c, targetID, ScopeServerExec); !authOK {
			return
		}
		targetPeer := s.Agents.Peer(targetID)
		if targetPeer == nil {
			fail(c, http.StatusGatewayTimeout, "target server offline", "server.offline")
			return
		}
		resp, err := targetPeer.Call(protocol.MethodBandwidthServe, protocol.BandwidthParams{
			Duration: duration,
		}, 30*time.Second)
		if err != nil {
			fail(c, http.StatusGatewayTimeout, err.Error(), "server.offline")
			return
		}
		if resp.Error != nil {
			fail(c, http.StatusBadGateway, resp.Error.Message)
			return
		}
		var serveRes protocol.BandwidthResult
		raw, _ := json.Marshal(resp.Result)
		if err := json.Unmarshal(raw, &serveRes); err != nil {
			fail(c, http.StatusInternalServerError, "bad agent response")
			return
		}
		if !serveRes.OK {
			fail(c, http.StatusBadGateway, serveRes.Error)
			return
		}
		probeTarget = fmt.Sprintf("%s:%d", targetSrvIP(s, targetID), serveRes.Port)
	} else {
		probeTarget = req.Target
	}

	// 源 agent 发起测速
	peer := s.Agents.Peer(req.SourceID)
	if peer == nil {
		fail(c, http.StatusGatewayTimeout, "source server offline", "server.offline")
		return
	}
	timeout := time.Duration(duration+15) * time.Second
	resp, err := peer.Call(protocol.MethodBandwidthProbe, protocol.BandwidthParams{
		Target: probeTarget, Duration: duration, Parallel: parallel,
	}, timeout)
	if err != nil {
		fail(c, http.StatusGatewayTimeout, err.Error(), "server.offline")
		return
	}
	if resp.Error != nil {
		fail(c, http.StatusBadGateway, resp.Error.Message)
		return
	}
	var bw protocol.BandwidthResult
	raw, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(raw, &bw); err != nil {
		fail(c, http.StatusInternalServerError, "bad agent response")
		return
	}
	ok(c, gin.H{"result": bw, "source_id": req.SourceID, "source_name": source.Name, "target": probeTarget})
}
