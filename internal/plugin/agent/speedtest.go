package agent

import (
	"YALS/internal/plugin"
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// SpeedTestPlugin implements the speed test plugin with iperf3 and HTTP download
type SpeedTestPlugin struct {
	portManager *PortManager
	httpServer  *http.Server
	httpStarted bool
	httpMutex   sync.Mutex
	rateLimiter *RateLimiter
}

// PortManager manages iperf3 port allocation
type PortManager struct {
	basePort  int
	usedPorts map[int]bool
	mutex     sync.Mutex
}

// RateLimiter limits HTTP test requests per IP
type RateLimiter struct {
	requests map[string][]time.Time
	mutex    sync.RWMutex
}

var speedTestInstance *SpeedTestPlugin
var speedTestOnce sync.Once

func init() {
	speedTestOnce.Do(func() {
		speedTestInstance = &SpeedTestPlugin{
			portManager: &PortManager{
				basePort:  15201,
				usedPorts: make(map[int]bool),
			},
			rateLimiter: &RateLimiter{
				requests: make(map[string][]time.Time),
			},
		}
	})

	plugin.RegisterAgentPlugin("speedtest", func() plugin.Plugin {
		return speedTestInstance
	})
}

// GetName returns the plugin name
func (p *SpeedTestPlugin) GetName() string {
	return "speedtest"
}

// GetDescription returns the plugin description
func (p *SpeedTestPlugin) GetDescription() string {
	return "Speed test using iperf3 and HTTP download"
}

// GetIgnoreTarget returns whether this plugin ignores target parameter
func (p *SpeedTestPlugin) GetIgnoreTarget() bool {
	return true
}

// GetMaximumQueue returns the maximum queue size (0 = unlimited)
func (p *SpeedTestPlugin) GetMaximumQueue() int {
	return 0 // Allow unlimited concurrent tests
}

// Execute runs the speed test
func (p *SpeedTestPlugin) Execute(target string) (string, error) {
	var output string
	err := p.ExecuteStreaming(target, func(data string, isError bool, isComplete bool) {
		if !isError && !isComplete {
			output += data
		}
	})
	return output, err
}

// ExecuteStreaming runs the speed test with streaming output
func (p *SpeedTestPlugin) ExecuteStreaming(target string, callback plugin.StreamingCallback) error {
	return p.ExecuteStreamingWithID(target, "", callback)
}

// ExecuteStreamingWithID runs the speed test with command ID
func (p *SpeedTestPlugin) ExecuteStreamingWithID(target, commandID string, callback plugin.StreamingCallback) error {
	// Start HTTP server if not already started
	p.startHTTPServer()

	// Allocate port for iperf3
	port := p.portManager.AllocatePort()
	defer p.portManager.ReleasePort(port)

	// Get test IP from config
	testIP := p.getTestIP()

	// Send initial output
	output := fmt.Sprintf("iperf3 server will be available at %s:%d\n", testIP, port)
	output += fmt.Sprintf("iperf3 -c %s -p %d              # TCP download\n", testIP, port)
	output += fmt.Sprintf("iperf3 -c %s -p %d -R           # TCP upload\n", testIP, port)
	output += fmt.Sprintf("iperf3 -c %s -p %d -u -b 100M   # UDP test\n\n", testIP, port)
	output += "WARNING: iperf3 server will shutdown after 120 seconds if no connections\n\n"
	output += fmt.Sprintf("HTTP download test will be available at http://%s:8081/download\n\n", testIP)
	output += "NOTE: HTTP test will be limited to 128MB per download, and you can only perform 2 tests per IP within 10 minutes\n"

	callback(output, false, false)

	// Start iperf3 server with 120 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "iperf3", "-s", "-p", fmt.Sprintf("%d", port), "-1")

	// Register command for stop functionality
	if commandID != "" {
		manager := plugin.GetManager()
		manager.RegisterActiveCommand(commandID, cmd)
		defer manager.UnregisterActiveCommand(commandID)
	}

	// Run iperf3 server
	if err := cmd.Start(); err != nil {
		callback(fmt.Sprintf("Failed to start iperf3 server: %v\n", err), true, true)
		return err
	}

	// Wait for completion or timeout
	err := cmd.Wait()
	if err != nil && ctx.Err() != context.DeadlineExceeded {
		callback(fmt.Sprintf("iperf3 server error: %v\n", err), true, true)
		return err
	}

	callback("iperf3 server stopped\n", false, true)
	return nil
}

// AllocatePort allocates an available port
func (pm *PortManager) AllocatePort() int {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// Find first available port
	for port := pm.basePort; port < pm.basePort+100; port++ {
		if !pm.usedPorts[port] {
			pm.usedPorts[port] = true
			return port
		}
	}

	// Fallback to base port if all are used (shouldn't happen)
	return pm.basePort
}

// ReleasePort releases a port back to the pool
func (pm *PortManager) ReleasePort(port int) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	delete(pm.usedPorts, port)
}

// startHTTPServer starts the HTTP download server
func (p *SpeedTestPlugin) startHTTPServer() {
	p.httpMutex.Lock()
	defer p.httpMutex.Unlock()

	if p.httpStarted {
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/download", p.handleDownload)

	p.httpServer = &http.Server{
		Addr:    ":8081",
		Handler: mux,
	}

	go func() {
		if err := p.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log error but don't crash
			fmt.Printf("HTTP server error: %v\n", err)
		}
	}()

	p.httpStarted = true
}

// handleDownload handles HTTP download requests
func (p *SpeedTestPlugin) handleDownload(w http.ResponseWriter, r *http.Request) {
	// Get client IP
	clientIP := getClientIP(r)

	// Check rate limit
	if !p.rateLimiter.AllowRequest(clientIP) {
		http.Error(w, "Rate limit exceeded: maximum 2 tests per IP within 10 minutes", http.StatusTooManyRequests)
		return
	}

	// Set headers
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=speedtest.bin")
	w.Header().Set("Cache-Control", "no-cache")

	// Generate and send 128MB of random data
	const maxSize = 128 * 1024 * 1024 // 128MB
	const chunkSize = 1024 * 1024     // 1MB chunks

	buffer := make([]byte, chunkSize)
	sent := 0

	for sent < maxSize {
		// Generate random data
		rand.Read(buffer)

		// Write chunk
		n, err := w.Write(buffer)
		if err != nil {
			return
		}

		sent += n

		// Flush to client
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

// AllowRequest checks if a request from the given IP is allowed
func (rl *RateLimiter) AllowRequest(ip string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	cutoff := now.Add(-10 * time.Minute)

	// Clean old requests
	if requests, exists := rl.requests[ip]; exists {
		var validRequests []time.Time
		for _, t := range requests {
			if t.After(cutoff) {
				validRequests = append(validRequests, t)
			}
		}
		rl.requests[ip] = validRequests
	}

	// Check limit
	if len(rl.requests[ip]) >= 2 {
		return false
	}

	// Add new request
	rl.requests[ip] = append(rl.requests[ip], now)
	return true
}

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Use remote address
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

// getTestIP gets the test IP from agent configuration
func (p *SpeedTestPlugin) getTestIP() string {
	manager := plugin.GetManager()
	cfg := manager.GetConfig()

	if cfg != nil && cfg.Agent.Details.TestIP != "" {
		return cfg.Agent.Details.TestIP
	}

	// Fallback to localhost
	return "127.0.0.1"
}
