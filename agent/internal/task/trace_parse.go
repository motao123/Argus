package task

import (
	"net"
	"strconv"
	"strings"

	"github.com/motao123/Argus/protocol"
)

// parseTraceHops 用统一的 token 解析器解析 traceroute/tracert 输出的一跳：
// 首 token 为跳数；后续 token 中 IP 记入 IP 字段、紧跟 "ms" 的数值记入 RTT、
// "*" 或无响应标记记入丢包。兼容 Linux/macOS/BSD 与 Windows 两种输出格式
// （两者 RTT 均为 "1.234 ms" 或 "1 ms" 两个 token）。
func parseTraceHops(text string) []protocol.TraceHop {
	var hops []protocol.TraceHop
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hopNum, err := strconv.Atoi(fields[0])
		if err != nil || hopNum <= 0 {
			continue
		}
		hop := protocol.TraceHop{Hop: hopNum}
		var rtts []float64
		unreachable := false
		for i := 1; i < len(fields); i++ {
			f := fields[i]
			switch {
			case f == "ms" && i > 1:
				if v, err := strconv.ParseFloat(fields[i-1], 64); err == nil {
					rtts = append(rtts, v)
				}
			case f == "*":
				unreachable = true
			case strings.EqualFold(f, "request") || strings.EqualFold(f, "timed"):
				unreachable = true
			case isIPAddr(f):
				hop.IP = f
			}
		}
		if len(rtts) > 0 {
			var sum float64
			for _, r := range rtts {
				sum += r
			}
			hop.RTTMs = sum / float64(len(rtts))
		}
		if unreachable || (hop.IP == "" && len(rtts) == 0) {
			hop.Loss = 100
		}
		hops = append(hops, hop)
	}
	return hops
}

// isIPAddr 判定 token 是否为 IPv4/IPv6 地址（带可选尾随冒号/括号）。
func isIPAddr(s string) bool {
	ip := net.ParseIP(strings.Trim(s, "[]:"))
	return ip != nil
}
