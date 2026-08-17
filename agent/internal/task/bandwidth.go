package task

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/motao123/Argus/protocol"
)

// maxBandwidthDurationSec / maxBandwidthParallel 限制测速时长与并发（防滥用）。
const (
	maxBandwidthDurationSec = 60
	maxBandwidthParallel    = 8
	bandwidthBufferSize     = 64 << 10 // 64KiB 写缓冲
)

// serveBandwidth 监听随机/指定 TCP 端口并立即返回实际端口；
// 后台接受单个连接并持续读取至 duration 结束（为源侧测速提供接收端）。
func serveBandwidth(p protocol.BandwidthParams) *protocol.BandwidthResult {
	listenAddr := p.ListenAddr
	if listenAddr == "" {
		listenAddr = "0.0.0.0:0"
	}
	duration := time.Duration(p.Duration) * time.Second
	if duration <= 0 {
		duration = 5 * time.Second
	}
	if duration > maxBandwidthDurationSec*time.Second {
		duration = maxBandwidthDurationSec * time.Second
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return &protocol.BandwidthResult{Error: err.Error()}
	}
	port := ln.Addr().(*net.TCPAddr).Port

	// 后台：接受单连接并读取（接收侧无需统计精确字节，测速以发送侧为准）
	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(duration + 5*time.Second))
		buf := make([]byte, bandwidthBufferSize)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()

	return &protocol.BandwidthResult{OK: true, Port: port}
}

// probeBandwidth 作为客户端向目标 host:port 建立 N 个并发连接，
// 持续发送数据 N 秒，统计总吞吐（bit/s）。
func probeBandwidth(ctx context.Context, p protocol.BandwidthParams) *protocol.BandwidthResult {
	if p.Target == "" {
		return &protocol.BandwidthResult{Error: "target required"}
	}
	duration := time.Duration(p.Duration) * time.Second
	if duration <= 0 {
		duration = 5 * time.Second
	}
	if duration > maxBandwidthDurationSec*time.Second {
		duration = maxBandwidthDurationSec * time.Second
	}
	parallel := p.Parallel
	if parallel <= 0 {
		parallel = 1
	}
	if parallel > maxBandwidthParallel {
		parallel = maxBandwidthParallel
	}

	ctx, cancel := context.WithTimeout(ctx, duration+10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var total uint64
	var mu sync.Mutex
	started := time.Now()
	errCh := make(chan error, parallel)
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", p.Target)
			if err != nil {
				errCh <- err
				return
			}
			defer conn.Close()
			// 设置发送缓冲，避免每包 syscall 开销
			if tc, ok := conn.(*net.TCPConn); ok {
				_ = tc.SetWriteBuffer(bandwidthBufferSize)
				_ = tc.SetNoDelay(false)
			}
			buf := make([]byte, bandwidthBufferSize)
			deadline := time.Now().Add(duration)
			_ = conn.SetWriteDeadline(deadline.Add(5 * time.Second))
			n, writeErr := conn.Write(buf)
			if n > 0 {
				mu.Lock()
				total += uint64(n)
				mu.Unlock()
			}
			if writeErr != nil {
				// 对端主动断开（如 server 端 duration 结束）属正常，记录但不报错
				return
			}
			// 持续写直到 duration 结束
			for time.Now().Before(deadline) {
				n, err := conn.Write(buf)
				if n > 0 {
					mu.Lock()
					total += uint64(n)
					mu.Unlock()
				}
				if err != nil {
					return
				}
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(started)
	select {
	case err := <-errCh:
		if total == 0 {
			return &protocol.BandwidthResult{Error: err.Error()}
		}
	default:
	}
	mu.Lock()
	sent := total
	mu.Unlock()
	return &protocol.BandwidthResult{
		OK:         sent > 0,
		BytesSent:  sent,
		DurationMs: elapsed.Milliseconds(),
		BitsPerSec: float64(sent) * 8 / elapsed.Seconds(),
	}
}
