package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/motao123/Argus/protocol"
)

var upgrader = websocket.Upgrader{
	// 校验 Origin/Referer 与 Host 一致（防跨站 WS 劫持）
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = r.Header.Get("Referer")
		}
		if origin == "" {
			return true // 非浏览器客户端
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return u.Host == r.Host
	},
}

// dashboardWS 每 2s 向所有前端推送服务器快照（单一 JSON 序列化后广播）。
func (s *Server) dashboardWS(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	p := principalFromContext(c)
	isGuest := p == nil
	if isGuest && s.GetSetting(SettingForceAuth, "0") == "1" {
		// 私有站点模式：游客 WS 拒绝（防止绕过登录墙）
		_ = conn.Close()
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			snap := s.Store.Snapshot()
			for id, st := range snap {
				if st.Server == nil {
					delete(snap, id)
					continue
				}
				if isGuest && st.Server.Hidden {
					delete(snap, id)
					continue
				}
				if p != nil && !p.IsAdmin && st.Server.OwnerID != p.UserID {
					delete(snap, id)
					continue
				}
				if p != nil && p.IsPAT && !p.canAccessServer(id) {
					delete(snap, id)
					continue
				}
			}
			out := make([]serverView, 0, len(snap))
			for _, st := range snap {
				if st.Server == nil {
					continue
				}
				v := serverView{Server: *st.Server}
				v.CPU = st.Last.CPU
				v.MemUsed = st.Last.MemUsed
				v.MemTotal = st.Last.MemTotal
				v.DiskUsed = st.Last.DiskUsed
				v.DiskTotal = st.Last.DiskTotal
				v.NetInSpeed = st.Last.NetInSpeed
				v.NetOutSpeed = st.Last.NetOutSpeed
				v.Load1 = st.Last.Load1
				v.Temperature = st.Last.Temperature
				v.GPUUtil = st.Last.GPUUtil
				v.GPU = st.Last.GPU
				v.ProcessCount = st.Last.ProcessCount
				v.TCPEstablished = st.Last.TCPEstablished
				v.TCPListen = st.Last.TCPListen
				v.UDPCount = st.Last.UDPCount
				v.DiskReadSpeed = st.Last.DiskReadSpeed
				v.DiskWriteSpeed = st.Last.DiskWriteSpeed
				v.DiskReadIOPS = st.Last.DiskReadIOPS
				v.DiskWriteIOPS = st.Last.DiskWriteIOPS
				v.DiskIOAvailability = st.Last.DiskIOAvailability
				v.SocketAvailability = st.Last.SocketAvailability
				v.ProcessAvailability = st.Last.ProcessAvailability
				v.TemperatureAvailability = st.Last.TemperatureAvailability
				v.Uptime = st.Last.Uptime
				v.Online = st.Online
				v.LastSeen = st.LastSeen
				if st.Host.Hostname != "" {
					country := ""
					if s.GeoIP != nil && st.Host.IP != "" {
						country = s.GeoIP.CountryCode(st.Host.IP)
					}
					v.Host = &hostView{
						Hostname:        st.Host.Hostname,
						Platform:        st.Host.Platform,
						PlatformVersion: st.Host.PlatformVersion,
						OS:              st.Host.OS, Arch: st.Host.Arch, KernelVersion: st.Host.KernelVersion,
						CPUModel:     st.Host.CPUModel,
						CPUCores:     st.Host.CPUCores,
						AgentVersion: st.Host.AgentVersion,
						IP:           st.Host.IP,
						IPv4:         st.Host.IPv4, IPv6: st.Host.IPv6,
						CountryCode: country,
					}
				}
				out = append(out, v)
			}
			msg, err := json.Marshal(gin.H{"type": "snapshot", "servers": out})
			if err != nil {
				continue
			}
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}
}

// terminalSession 浏览器 → Server → Agent 的终端中继。
type terminalSession struct {
	serverID  int64
	sessionID string
}

var (
	termMu    sync.Mutex
	termConns = map[string]*websocket.Conn{} // sessionID → 浏览器连接
)

// terminalWS 浏览器终端 WebSocket：建立时通知 Agent 开会话，双向转发字节。
func (s *Server) terminalWS(c *gin.Context) {
	serverID := mustIDParam(c, "serverId")
	if _, ok := s.authorizeServer(c, serverID, ScopeServerExec); !ok {
		fail(c, http.StatusForbidden, "server access denied")
		return
	}
	if s.Agents.Peer(serverID) == nil {
		fail(c, http.StatusConflict, "server offline")
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	sessionID := newSessionID()
	if err := s.Agents.OpenTerminal(serverID, sessionID, 80, 24); err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("terminal open failed: "+err.Error()))
		return
	}

	termMu.Lock()
	termConns[sessionID] = conn
	termMu.Unlock()
	defer func() {
		termMu.Lock()
		delete(termConns, sessionID)
		termMu.Unlock()
		s.Agents.CloseTerm(serverID, sessionID)
	}()

	// 浏览器输入 → Agent
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if msgType == websocket.TextMessage {
			var resize protocol.TerminalResize
			if json.Unmarshal(data, &resize) == nil && resize.Cols > 0 && resize.Rows > 0 {
				resize.SessionID = sessionID
				if err := s.Agents.ResizeTerm(serverID, resize); err != nil {
					return
				}
				continue
			}
		}
		if err := s.Agents.SendTermData(serverID, protocol.TerminalData{SessionID: sessionID, Data: data}); err != nil {
			return
		}
	}
}

// handleAgentTermData 由 agent.Hub 回调：Agent 输出 → 浏览器。
// HandleAgentTermData 由 agent.Hub 回调：Agent 输出 → 浏览器。
func (s *Server) HandleAgentTermData(serverID int64, data protocol.TerminalData) {
	termMu.Lock()
	conn := termConns[data.SessionID]
	termMu.Unlock()
	if conn == nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_ = conn.WriteMessage(websocket.BinaryMessage, data.Data)
}

// newSessionID 生成随机会话 ID（防猜测，借鉴 nezha idcodec 混淆思路）。
func newSessionID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
