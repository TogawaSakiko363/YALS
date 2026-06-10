package agent

import (
	"YALS/internal/plugin"
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"os/exec"
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

const (
	// httpDownloadPort is the fixed port for the shared HTTP download test
	// server. A single server is started lazily and reused across every
	// speed-test invocation.
	httpDownloadPort = 8081

	// maxDownloadBytes caps a single HTTP download test payload; downloadChunkSize
	// is the per-write chunk size for the streamed response.
	maxDownloadBytes  = 128 * 1024 * 1024
	downloadChunkSize = 1024 * 1024

	// rateLimitWindow and maxRequestsPerWindow bound HTTP download tests per IP.
	rateLimitWindow      = 10 * time.Minute
	maxRequestsPerWindow = 2
)

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
	// Start (or reuse) the shared HTTP download server. A bind failure is
	// reported to the user but does not abort the iperf3 test, which is
	// independent; the next invocation will retry the bind.
	httpErr := p.startHTTPServer()

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
	if httpErr != nil {
		output += fmt.Sprintf("NOTE: HTTP download test is currently unavailable: %v\n", httpErr)
	} else {
		output += fmt.Sprintf("HTTP download test will be available at http://%s:%d/download\n\n", testIP, httpDownloadPort)
		output += fmt.Sprintf("NOTE: HTTP test will be limited to %dMB per download, and you can only perform %d tests per IP within %d minutes\n",
			maxDownloadBytes/(1024*1024), maxRequestsPerWindow, int(rateLimitWindow.Minutes()))
	}

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

// startHTTPServer starts the shared HTTP download server if it is not already
// running. The listener is bound synchronously so that:
//   - a bind failure is surfaced to the caller instead of being swallowed inside
//     a goroutine;
//   - the "server is available" message is only emitted once the port is
//     actually accepting connections (no optimistic readiness);
//   - a failed attempt leaves httpStarted false, so the next invocation retries
//     instead of being permanently broken.
//
// On success the single server is reused for the lifetime of the agent process.
func (p *SpeedTestPlugin) startHTTPServer() error {
	p.httpMutex.Lock()
	defer p.httpMutex.Unlock()

	if p.httpStarted {
		return nil
	}

	addr := fmt.Sprintf(":%d", httpDownloadPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start HTTP download server on %s: %w", addr, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/download", p.handleDownload)
	p.httpServer = &http.Server{Handler: mux}

	go func() {
		if err := p.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			// Log error but don't crash; the listener is already bound so this
			// only fires on an unexpected runtime failure of an active server.
			fmt.Printf("HTTP server error: %v\n", err)
		}
	}()

	p.httpStarted = true
	return nil
}

// downloadPayload is a single random buffer reused as the speed-test payload
// across all requests. It is read-only after initialization, so concurrent
// downloads can share it without locking.
var (
	downloadPayload     []byte
	downloadPayloadOnce sync.Once
)

// getDownloadPayload lazily generates the shared download payload exactly once.
// A single CSPRNG fill keeps each 1MB block incompressible; reusing that block
// across chunks is safe against gzip (its 32KB window cannot span the 1MB repeat
// period), so throughput measurements stay accurate while the per-chunk CSPRNG
// cost — which previously dominated download CPU — is eliminated.
func getDownloadPayload() []byte {
	downloadPayloadOnce.Do(func() {
		downloadPayload = make([]byte, downloadChunkSize)
		if _, err := rand.Read(downloadPayload); err != nil {
			// Extremely unlikely; fall back to a non-zero pattern so the payload
			// is never all-zero (which would be trivially compressible).
			for i := range downloadPayload {
				downloadPayload[i] = byte(i*31 + 7)
			}
		}
	})
	return downloadPayload
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

	// Stream a reusable, pre-generated incompressible payload. Reusing one buffer
	// avoids re-running the CSPRNG for every chunk of every request.
	payload := getDownloadPayload()
	sent := 0

	for sent < maxDownloadBytes {
		n, err := w.Write(payload)
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
	cutoff := now.Add(-rateLimitWindow)

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
	if len(rl.requests[ip]) >= maxRequestsPerWindow {
		return false
	}

	// Add new request
	rl.requests[ip] = append(rl.requests[ip], now)
	return true
}

// getClientIP extracts the client IP from the request. The speed-test HTTP
// server is connected to directly by clients, so the proxy headers
// (X-Forwarded-For / X-Real-IP) are attacker-controlled and must not be trusted
// for rate-limiting decisions — only the real connection address is reliable.
func getClientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
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
