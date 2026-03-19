package agent

import (
	"YALS/internal/plugin"
	"context"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UDPingPlugin implements the UDP ping plugin
type UDPingPlugin struct{}

type udpStatistics struct {
	sync.Mutex
	sentCount      int64
	respondedCount int64
	minTime        float64
	maxTime        float64
	totalTime      float64
}

func init() {
	plugin.RegisterAgentPlugin("udping", func() plugin.Plugin {
		return &UDPingPlugin{}
	})
}

// GetName returns the plugin name
func (p *UDPingPlugin) GetName() string {
	return "udping"
}

// GetDescription returns the plugin description
func (p *UDPingPlugin) GetDescription() string {
	return "UDP connectivity test to target host and port"
}

// GetIgnoreTarget returns whether this plugin ignores target parameter
func (p *UDPingPlugin) GetIgnoreTarget() bool {
	return false
}

// GetMaximumQueue returns the maximum queue size (0 = unlimited)
func (p *UDPingPlugin) GetMaximumQueue() int {
	return 10
}

// Execute runs the UDP ping test
func (p *UDPingPlugin) Execute(target string) (string, error) {
	var output string
	err := p.ExecuteStreaming(target, func(data string, isError bool, isComplete bool) {
		if !isError && !isComplete {
			output += data
		}
	})
	return output, err
}

// ExecuteStreaming runs the UDP ping test with streaming output
func (p *UDPingPlugin) ExecuteStreaming(target string, callback plugin.StreamingCallback) error {
	return p.ExecuteStreamingWithID(target, "", callback)
}

// ExecuteStreamingWithID runs the UDP ping test with command ID
func (p *UDPingPlugin) ExecuteStreamingWithID(target, commandID string, callback plugin.StreamingCallback) error {
	// Parse target format: IP:port
	host, port, err := parseUDPTarget(target)
	if err != nil {
		callback(fmt.Sprintf("Invalid target format: %v\nExpected format: IP:port (e.g., 192.168.1.1:53)\n", err), true, true)
		return err
	}

	// Validate port
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		callback("Invalid port number. Port must be between 1 and 65535\n", true, true)
		return fmt.Errorf("invalid port: %s", port)
	}

	// Validate that host is an IP address (not a domain)
	targetIP := net.ParseIP(host)
	if targetIP == nil {
		callback("Invalid IP address. Target must be a resolved IP address, not a domain name.\n", true, true)
		return fmt.Errorf("invalid IP address: %s", host)
	}

	// Determine IP version
	ipVersion := "IPv4"
	if targetIP.To4() == nil {
		ipVersion = "IPv6"
	}

	// Create UDP connection
	var conn *net.UDPConn
	if ipVersion == "IPv6" {
		conn, err = net.DialUDP("udp6", nil, &net.UDPAddr{
			IP:   targetIP,
			Port: portNum,
		})
	} else {
		conn, err = net.DialUDP("udp4", nil, &net.UDPAddr{
			IP:   targetIP,
			Port: portNum,
		})
	}

	if err != nil {
		callback(fmt.Sprintf("Failed to create UDP connection: %v\n", err), true, true)
		return err
	}
	defer conn.Close()

	// Build output with initial message
	var output strings.Builder
	const payloadLen = 64
	output.WriteString(fmt.Sprintf("UDPping %s [%s] via port %s with %d bytes of payload\n", host, ipVersion, port, payloadLen))
	callback(output.String(), false, false)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Statistics
	stats := &udpStatistics{}

	// Run 4 pings
	const pingCount = 4
	const intervalMs = 1000

	for i := 0; i < pingCount; i++ {
		select {
		case <-ctx.Done():
			output.WriteString("\nOperation interrupted\n")
			callback(output.String(), false, true)
			return nil
		default:
		}

		// Perform ping
		elapsed, success := udpPingOnce(conn, targetIP, portNum, payloadLen, intervalMs)
		stats.update(elapsed, success)

		if success {
			output.WriteString(fmt.Sprintf("Reply from %s seq=%d time=%.2f ms\n", host, i, elapsed))
		} else {
			output.WriteString("Request timed out\n")
		}
		callback(output.String(), false, false)

		// Wait interval before next ping (except for last one)
		if i < pingCount-1 {
			time.Sleep(1 * time.Second)
		}
	}

	// Print statistics
	sent, responded, min, max, avg := stats.getStats()
	output.WriteString("\n--- ping statistics ---\n")

	if sent > 0 {
		lossPercent := float64(sent-responded) * 100.0 / float64(sent)
		output.WriteString(fmt.Sprintf("%d packets transmitted, %d received, %.2f%% packet loss\n", sent, responded, lossPercent))
	}

	if responded > 0 {
		output.WriteString(fmt.Sprintf("rtt min/avg/max = %.2f/%.2f/%.2f ms\n", min, avg, max))
	}

	callback(output.String(), false, true)
	return nil
}

// udpPingOnce performs a single UDP ping
func udpPingOnce(conn *net.UDPConn, targetIP net.IP, targetPort int, payloadLen int, intervalMs int) (float64, bool) {
	// Generate random payload
	payload := randomString(payloadLen)
	timeOfSend := time.Now()

	// Send UDP packet
	_, err := conn.Write([]byte(payload))
	if err != nil {
		return 0, false
	}

	// Set deadline for response
	deadline := timeOfSend.Add(time.Duration(intervalMs) * time.Millisecond)
	conn.SetDeadline(deadline)

	// Ensure deadline is cleared when function returns
	defer conn.SetDeadline(time.Time{})

	// Wait for response
	buf := make([]byte, 65536)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				break
			}
			return 0, false
		}

		receivedData := string(buf[:n])

		// Check if response matches our payload and source
		if receivedData == payload && addr.IP.Equal(targetIP) && addr.Port == targetPort {
			rtt := float64(time.Since(timeOfSend).Microseconds()) / 1000.0
			return rtt, true
		}
	}

	// Wait remaining time
	timeRemaining := time.Until(deadline)
	if timeRemaining > 0 {
		time.Sleep(timeRemaining)
	}

	return 0, false
}

// randomString generates a random string of specified length
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[rand.Intn(len(charset))]
	}
	return string(result)
}

// update updates statistics
func (s *udpStatistics) update(elapsed float64, success bool) {
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
func (s *udpStatistics) getStats() (sent, responded int64, min, max, avg float64) {
	s.Lock()
	defer s.Unlock()

	avg = 0.0
	if s.respondedCount > 0 {
		avg = s.totalTime / float64(s.respondedCount)
	}

	return s.sentCount, s.respondedCount, s.minTime, s.maxTime, avg
}

// parseUDPTarget parses the target string in format "host:port"
func parseUDPTarget(target string) (host, port string, err error) {
	target = strings.TrimSpace(target)

	// Check for IPv6 format [host]:port
	if strings.HasPrefix(target, "[") {
		idx := strings.LastIndex(target, "]:")
		if idx != -1 {
			host = target[1:idx]
			port = target[idx+2:]
			return host, port, nil
		}
		return "", "", fmt.Errorf("invalid IPv6 format")
	}

	// Check for host:port format
	idx := strings.LastIndex(target, ":")
	if idx == -1 {
		return "", "", fmt.Errorf("missing port number")
	}

	// Handle IPv6 without brackets (multiple colons)
	if strings.Count(target, ":") > 1 {
		return "", "", fmt.Errorf("IPv6 address must be enclosed in brackets [host]:port")
	}

	host = target[:idx]
	port = target[idx+1:]

	if host == "" || port == "" {
		return "", "", fmt.Errorf("empty host or port")
	}

	return host, port, nil
}
