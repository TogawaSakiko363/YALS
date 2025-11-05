package agent

import (
	"YALS/internal/plugin"
	"bufio"
	"context"
	"fmt"
	"math"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MTRPlugin implements the MTR network diagnostic plugin
type MTRPlugin struct{}

// GetName returns the plugin name
func (p *MTRPlugin) GetName() string {
	return "mtr"
}

// GetDescription returns the plugin description (fallback, actual description comes from config)
func (p *MTRPlugin) GetDescription() string {
	return "MTR network diagnostic tool"
}

// MTRHop represents a single hop in the MTR trace
type MTRHop struct {
	TTL      int
	IP       string
	Hostname string
	Sent     int
	Received int
	Lost     int
	LossRate float64
	Times    []float64
	Last     float64
	Avg      float64
	Best     float64
	Worst    float64
	StdDev   float64
}

// MTRResult represents the complete MTR trace result
type MTRResult struct {
	Target string
	Hops   map[int]*MTRHop
	mutex  sync.RWMutex
}

// Execute runs the MTR command and returns formatted output
func (p *MTRPlugin) Execute(target string) (string, error) {
	// Sanitize target to prevent command injection
	target = plugin.SanitizeTarget(target)
	if target == "" {
		return "", fmt.Errorf("invalid target")
	}

	// Check if mtr command is available
	if !plugin.IsCommandAvailable("mtr") {
		return "", fmt.Errorf("mtr command not found on system")
	}

	// Execute mtr with raw output format
	cmd := exec.Command("mtr", "--raw", "-c", "4", target)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("mtr execution failed: %v", err)
	}

	// Parse the raw output
	result := p.parseRawOutput(string(output), target)

	// Format and return the result
	return p.formatMTRResult(result), nil
}

// ExecuteStreaming runs the MTR command with streaming output
func (p *MTRPlugin) ExecuteStreaming(target string, callback plugin.StreamingCallback) error {
	return p.runStreamingMTR(target, callback, context.Background())
}

// ExecuteStreamingWithID runs the MTR command with command ID for stop functionality
func (p *MTRPlugin) ExecuteStreamingWithID(target, commandID string, callback plugin.StreamingCallback) error {
	// Create a context that can be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create MTR command
	cmd := exec.CommandContext(ctx, "mtr", "--raw", "-c", "10", target)

	// Register command for stop functionality
	manager := plugin.GetManager()
	manager.RegisterActiveCommand(commandID, cmd)
	defer manager.UnregisterActiveCommand(commandID)

	return p.runStreamingMTRWithContext(ctx, cmd, target, callback)
}

// runStreamingMTR runs MTR with streaming output
func (p *MTRPlugin) runStreamingMTR(target string, callback plugin.StreamingCallback, ctx context.Context) error {
	// Sanitize target to prevent command injection
	target = plugin.SanitizeTarget(target)
	if target == "" {
		callback("Invalid target", true, true)
		return fmt.Errorf("invalid target")
	}

	// Check if mtr command is available
	if !plugin.IsCommandAvailable("mtr") {
		callback("MTR command not found on system", true, true)
		return fmt.Errorf("mtr command not found on system")
	}

	cmd := exec.CommandContext(ctx, "mtr", "--raw", "-c", "10", target)
	return p.runStreamingMTRWithContext(ctx, cmd, target, callback)
}

// runStreamingMTRWithContext runs MTR with context and streams results
func (p *MTRPlugin) runStreamingMTRWithContext(ctx context.Context, cmd *exec.Cmd, target string, callback plugin.StreamingCallback) error {
	// Get stdout pipe
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start mtr command: %v", err)
	}

	// Create result structure
	result := &MTRResult{
		Target: target,
		Hops:   make(map[int]*MTRHop),
	}

	// Read output line by line and parse
	scanner := bufio.NewScanner(stdout)
	lastUpdate := time.Now()

	go func() {
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
				line := scanner.Text()
				p.processRawLine(result, line)

				// Send updates every 2 seconds to avoid flooding
				now := time.Now()
				if now.Sub(lastUpdate) >= 2*time.Second {
					output := p.formatMTRResult(result)
					callback(output, false, false)
					lastUpdate = now
				}
			}
		}
	}()

	// Wait for command to complete or context to be cancelled
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		// Command was cancelled
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		callback("MTR execution cancelled", false, true)
		return nil
	case err := <-done:
		// Command completed
		if err != nil {
			return fmt.Errorf("mtr command failed: %v", err)
		}
		// Send final results
		output := p.formatMTRResult(result)
		callback(output, false, false)
		callback("", false, true)
		return nil
	}
}

// processRawLine processes a single line of MTR raw output
func (p *MTRPlugin) processRawLine(result *MTRResult, line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	parts := strings.Fields(line)
	if len(parts) < 2 {
		return
	}

	result.mutex.Lock()
	defer result.mutex.Unlock()

	// MTR raw output format based on actual output:
	// x <ttl> <sequence>               - send probe
	// h <ttl> <ip>                     - host discovery
	// p <ttl> <rtt_microseconds> <sequence> - ping response
	// d <ttl> <ip>                     - packet drop

	switch parts[0] {
	case "x": // Send probe: x <ttl> <sequence>
		if len(parts) >= 2 {
			ttl, err := strconv.Atoi(parts[1])
			if err != nil {
				return
			}

			if _, exists := result.Hops[ttl]; !exists {
				result.Hops[ttl] = &MTRHop{
					TTL:   ttl,
					Times: make([]float64, 0),
				}
			}
			result.Hops[ttl].Sent++
		}
	case "h": // Host line: h <ttl> <ip>
		if len(parts) >= 3 {
			ttl, err := strconv.Atoi(parts[1])
			if err != nil {
				return
			}
			ip := parts[2]

			if _, exists := result.Hops[ttl]; !exists {
				result.Hops[ttl] = &MTRHop{
					TTL:   ttl,
					Times: make([]float64, 0),
				}
			}
			result.Hops[ttl].IP = ip
			result.Hops[ttl].Hostname = ip
		}
	case "p": // Ping response: p <ttl> <rtt_microseconds> <sequence>
		if len(parts) >= 3 {
			ttl, err := strconv.Atoi(parts[1])
			if err != nil {
				return
			}
			rttMicros, err := strconv.ParseFloat(parts[2], 64)
			if err != nil {
				return
			}

			rttMs := rttMicros / 1000.0 // Convert microseconds to milliseconds

			if _, exists := result.Hops[ttl]; !exists {
				result.Hops[ttl] = &MTRHop{
					TTL:   ttl,
					Times: make([]float64, 0),
				}
			}

			hop := result.Hops[ttl]
			hop.Received++
			hop.Times = append(hop.Times, rttMs)
			hop.Last = rttMs
			p.updateHopStats(hop)
		}
	case "d": // Drop/timeout line: d <ttl> <ip>
		if len(parts) >= 3 {
			ttl, err := strconv.Atoi(parts[1])
			if err != nil {
				return
			}
			ip := parts[2]

			if _, exists := result.Hops[ttl]; !exists {
				result.Hops[ttl] = &MTRHop{
					TTL:   ttl,
					Times: make([]float64, 0),
				}
			}

			hop := result.Hops[ttl]
			if hop.IP == "" {
				hop.IP = ip
				hop.Hostname = ip
			}
			// 'd' lines in MTR raw output don't necessarily mean packet loss
			// They appear to be related to timeouts or other events
			// We'll calculate loss based on Sent vs Received counts
			p.updateHopStats(hop)
		}
	}
}

// updateHopStats updates statistics for a hop
func (p *MTRPlugin) updateHopStats(hop *MTRHop) {
	// Calculate loss rate: (Sent - Received) / Sent * 100
	if hop.Sent > 0 {
		hop.LossRate = float64(hop.Sent-hop.Received) / float64(hop.Sent) * 100
	}

	if len(hop.Times) == 0 {
		return
	}

	// Calculate average
	sum := 0.0
	for _, t := range hop.Times {
		sum += t
	}
	hop.Avg = sum / float64(len(hop.Times))

	// Find best and worst
	hop.Best = hop.Times[0]
	hop.Worst = hop.Times[0]
	for _, t := range hop.Times {
		if t < hop.Best {
			hop.Best = t
		}
		if t > hop.Worst {
			hop.Worst = t
		}
	}

	// Calculate standard deviation
	if len(hop.Times) > 1 {
		variance := 0.0
		for _, t := range hop.Times {
			variance += math.Pow(t-hop.Avg, 2)
		}
		variance /= float64(len(hop.Times) - 1)
		hop.StdDev = math.Sqrt(variance)
	}
}

// parseRawOutput parses MTR raw output for non-streaming execution
func (p *MTRPlugin) parseRawOutput(output, target string) *MTRResult {
	result := &MTRResult{
		Target: target,
		Hops:   make(map[int]*MTRHop),
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		p.processRawLine(result, line)
	}

	return result
}

// formatMTRResult formats the MTR result for display
func (p *MTRPlugin) formatMTRResult(result *MTRResult) string {
	if result == nil || len(result.Hops) == 0 {
		return "No route data available"
	}

	result.mutex.RLock()
	defer result.mutex.RUnlock()

	var output strings.Builder

	// Header
	output.WriteString("Hop  Host                       Loss%  Snt  Last   Avg    Best   Wrst   StDev\n")

	// Get sorted TTL list
	ttls := make([]int, 0, len(result.Hops))
	for ttl := range result.Hops {
		ttls = append(ttls, ttl)
	}
	sort.Ints(ttls)

	// Format each hop
	for _, ttl := range ttls {
		hop := result.Hops[ttl]

		// Hop number
		output.WriteString(fmt.Sprintf("%-4d ", ttl))

		// Hostname/IP (truncate if too long)
		hostname := hop.Hostname
		if hostname == "" {
			hostname = hop.IP
		}
		if hostname == "" {
			hostname = "(waiting for reply)"
		}

		if len(hostname) > 26 {
			hostname = hostname[:23] + "..."
		}
		output.WriteString(fmt.Sprintf("%-26s ", hostname))

		// If we have no sent packets for this hop, show waiting message
		if hop.Sent == 0 {
			output.WriteString("\n")
			continue
		}

		// Loss percentage
		output.WriteString(fmt.Sprintf("%5.1f%% ", hop.LossRate))

		// Sent count
		output.WriteString(fmt.Sprintf("%3d  ", hop.Sent))

		if hop.Received > 0 {
			// Last, Avg, Best, Worst, StdDev
			output.WriteString(fmt.Sprintf("%6.1f %6.1f %6.1f %6.1f %6.1f",
				hop.Last, hop.Avg, hop.Best, hop.Worst, hop.StdDev))
		} else {
			output.WriteString("   ???    ???    ???    ???    ???")
		}

		output.WriteString("\n")
	}

	return output.String()
}

// init function to auto-register the MTR plugin
func init() {
	plugin.RegisterAgentPlugin("mtr", func() plugin.Plugin {
		return &MTRPlugin{}
	})
}
