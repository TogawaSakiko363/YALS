package agent

import (
	"YALS/internal/plugin"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TCPingPlugin implements the TCP ping plugin
type TCPingPlugin struct{}

type tcpStatistics struct {
	sync.Mutex
	sentCount      int64
	respondedCount int64
	minTime        float64
	maxTime        float64
	totalTime      float64
}

func init() {
	plugin.RegisterAgentPlugin("tcping", func() plugin.Plugin {
		return &TCPingPlugin{}
	})
}

// GetName returns the plugin name
func (p *TCPingPlugin) GetName() string {
	return "tcping"
}

// GetDescription returns the plugin description
func (p *TCPingPlugin) GetDescription() string {
	return "TCP connectivity test to target host and port"
}

// GetIgnoreTarget returns whether this plugin ignores target parameter
func (p *TCPingPlugin) GetIgnoreTarget() bool {
	return false
}

// GetMaximumQueue returns the maximum queue size (0 = unlimited)
func (p *TCPingPlugin) GetMaximumQueue() int {
	return 10
}

// Execute runs the TCP ping test
func (p *TCPingPlugin) Execute(target string) (string, error) {
	var output string
	err := p.ExecuteStreaming(target, func(data string, isError bool, isComplete bool) {
		if !isError && !isComplete {
			output += data
		}
	})
	return output, err
}

// ExecuteStreaming runs the TCP ping test with streaming output
func (p *TCPingPlugin) ExecuteStreaming(target string, callback plugin.StreamingCallback) error {
	return p.ExecuteStreamingWithID(target, "", callback)
}

// ExecuteStreamingWithID runs the TCP ping test with command ID
func (p *TCPingPlugin) ExecuteStreamingWithID(target, commandID string, callback plugin.StreamingCallback) error {
	// Parse target format: IP:port
	host, port, err := parseTCPTarget(target)
	if err != nil {
		callback(fmt.Sprintf("Invalid target format: %v\nExpected format: IP:port (e.g., 192.168.1.1:80)\n", err), true, true)
		return err
	}

	// Validate port
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		callback("Invalid port number. Port must be between 1 and 65535\n", true, true)
		return fmt.Errorf("invalid port: %s", port)
	}

	// Validate that host is an IP address (not a domain)
	ip := net.ParseIP(host)
	if ip == nil {
		callback("Invalid IP address. Target must be a resolved IP address, not a domain name.\n", true, true)
		return fmt.Errorf("invalid IP address: %s", host)
	}

	// Determine IP version
	ipVersion := "IPv4"
	address := host
	if ip.To4() == nil {
		ipVersion = "IPv6"
		address = "[" + host + "]"
	}

	// Build output with initial message
	var output strings.Builder
	output.WriteString(fmt.Sprintf("TCPing %s [%s] port %s\n", host, ipVersion, port))
	callback(output.String(), false, false)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Statistics
	stats := &tcpStatistics{}

	// Run 4 pings
	const pingCount = 4
	for i := 0; i < pingCount; i++ {
		select {
		case <-ctx.Done():
			output.WriteString("\nOperation interrupted\n")
			callback(output.String(), false, true)
			return nil
		default:
		}

		// Perform ping
		elapsed, success := tcpPingOnce(ctx, address, port, 1000)
		stats.update(elapsed, success)

		if success {
			output.WriteString(fmt.Sprintf("Response from %s:%s seq=%d time=%.2fms\n", host, port, i, elapsed))
		} else {
			output.WriteString(fmt.Sprintf("TCP connection failed %s:%s: seq=%d timeout\n", host, port, i))
		}
		callback(output.String(), false, false)

		// Wait interval before next ping (except for last one)
		if i < pingCount-1 {
			time.Sleep(1 * time.Second)
		}
	}

	// Print statistics
	sent, responded, min, max, avg := stats.getStats()
	output.WriteString(fmt.Sprintf("\n--- TCP ping statistics for %s port %s ---\n", host, port))

	if sent > 0 {
		lossRate := float64(sent-responded) / float64(sent) * 100
		output.WriteString(fmt.Sprintf("Sent = %d, Received = %d, Lost = %d (%.1f%% loss)\n",
			sent, responded, sent-responded, lossRate))

		if responded > 0 {
			output.WriteString(fmt.Sprintf("RTT: min = %.2fms, max = %.2fms, avg = %.2fms\n",
				min, max, avg))
		}
	}

	callback(output.String(), false, true)
	return nil
}

// tcpPingOnce performs a single TCP ping
func tcpPingOnce(ctx context.Context, address, port string, timeoutMs int) (float64, bool) {
	dialCtx, dialCancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer dialCancel()

	start := time.Now()
	var d net.Dialer
	conn, err := d.DialContext(dialCtx, "tcp", address+":"+port)
	elapsed := float64(time.Since(start).Microseconds()) / 1000.0

	if err != nil {
		return elapsed, false
	}

	conn.Close()
	return elapsed, true
}

// update updates statistics
func (s *tcpStatistics) update(elapsed float64, success bool) {
	s.Lock()
	defer s.Unlock()

	s.sentCount++

	if !success {
		return
	}

	s.respondedCount++
	s.totalTime += elapsed

	if s.respondedCount == 1 {
		s.minTime = elapsed
		s.maxTime = elapsed
		return
	}

	if elapsed < s.minTime {
		s.minTime = elapsed
	}
	if elapsed > s.maxTime {
		s.maxTime = elapsed
	}
}

// getStats returns statistics
func (s *tcpStatistics) getStats() (sent, responded int64, min, max, avg float64) {
	s.Lock()
	defer s.Unlock()

	avg = 0.0
	if s.respondedCount > 0 {
		avg = s.totalTime / float64(s.respondedCount)
	}

	return s.sentCount, s.respondedCount, s.minTime, s.maxTime, avg
}

// defaultTCPPort is used when the target omits a port.
const defaultTCPPort = "80"

// parseTCPTarget parses the target string in format "host:port". The port is
// optional — when omitted it defaults to defaultTCPPort (80).
func parseTCPTarget(target string) (host, port string, err error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", fmt.Errorf("empty host")
	}

	// IPv6 in brackets: [host] or [host]:port
	if strings.HasPrefix(target, "[") {
		end := strings.Index(target, "]")
		if end == -1 {
			return "", "", fmt.Errorf("invalid IPv6 format")
		}
		host = target[1:end]
		rest := target[end+1:]
		if strings.HasPrefix(rest, ":") && len(rest) > 1 {
			port = rest[1:]
		} else {
			port = defaultTCPPort
		}
		return host, port, nil
	}

	// Bare IPv6 (multiple colons, no brackets): only valid as a host with the
	// default port; otherwise it's ambiguous and must use brackets.
	if strings.Count(target, ":") > 1 {
		if net.ParseIP(target) != nil {
			return target, defaultTCPPort, nil
		}
		return "", "", fmt.Errorf("IPv6 address must be enclosed in brackets [host]:port")
	}

	// host or host:port
	idx := strings.LastIndex(target, ":")
	if idx == -1 {
		return target, defaultTCPPort, nil // no port → default
	}
	host = target[:idx]
	port = target[idx+1:]
	if host == "" {
		return "", "", fmt.Errorf("empty host")
	}
	if port == "" {
		port = defaultTCPPort // trailing colon → default
	}
	return host, port, nil
}
