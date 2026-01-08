package dns

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// DNSServer represents a DNS server configuration
type DNSServer struct {
	Name     string
	Type     string // "dot", "doh", "tls"
	Address  string
	Port     int
	Latency  time.Duration
	LastTest time.Time
}

// DNSResolver manages DNS resolution with multiple servers
type DNSResolver struct {
	servers      []*DNSServer
	currentIndex int
	mutex        sync.RWMutex
	stopChan     chan struct{}
	testInterval time.Duration
}

var (
	globalResolver *DNSResolver
	resolverOnce   sync.Once
)

// GetResolver returns the global DNS resolver instance
func GetResolver() *DNSResolver {
	resolverOnce.Do(func() {
		globalResolver = NewDNSResolver()
		globalResolver.StartLatencyMonitoring()
	})
	return globalResolver
}

// NewDNSResolver creates a new DNS resolver with predefined servers
func NewDNSResolver() *DNSResolver {
	return &DNSResolver{
		servers: []*DNSServer{
			{
				Name:    "Aliyun DoH",
				Type:    "doh",
				Address: "https://223.5.5.5/dns-query",
				Port:    443,
			},
			{
				Name:    "Google DoT",
				Type:    "dot",
				Address: "8.8.8.8",
				Port:    853,
			},
			{
				Name:    "Cloudflare TLS",
				Type:    "tls",
				Address: "1.1.1.1",
				Port:    853,
			},
		},
		currentIndex: 0,
		stopChan:     make(chan struct{}),
		testInterval: 5 * time.Minute, // Test every 5 minutes
	}
}

// StartLatencyMonitoring starts periodic latency testing
func (r *DNSResolver) StartLatencyMonitoring() {
	// Initial test
	go r.testAllServers()

	// Periodic testing
	go func() {
		ticker := time.NewTicker(r.testInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				r.testAllServers()
			case <-r.stopChan:
				return
			}
		}
	}()
}

// Stop stops the latency monitoring
func (r *DNSResolver) Stop() {
	close(r.stopChan)
}

// testAllServers tests latency for all DNS servers
func (r *DNSResolver) testAllServers() {
	var wg sync.WaitGroup
	testDomain := "www.bing.com"

	for _, server := range r.servers {
		wg.Add(1)
		go func(srv *DNSServer) {
			defer wg.Done()

			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err := r.resolveWithServer(ctx, testDomain, srv)
			elapsed := time.Since(start)

			r.mutex.Lock()
			if err == nil {
				srv.Latency = elapsed
			} else {
				srv.Latency = time.Hour // Set high latency on failure
			}
			srv.LastTest = time.Now()
			r.mutex.Unlock()
		}(server)
	}

	wg.Wait()

	// Select the fastest server
	r.selectFastestServer()
}

// selectFastestServer selects the server with lowest latency
func (r *DNSResolver) selectFastestServer() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	fastestIndex := 0
	minLatency := r.servers[0].Latency

	for i, server := range r.servers {
		if server.Latency < minLatency {
			minLatency = server.Latency
			fastestIndex = i
		}
	}

	r.currentIndex = fastestIndex
}

// Resolve resolves a domain name to IP addresses using the fastest server
func (r *DNSResolver) Resolve(ctx context.Context, domain string) ([]net.IP, error) {
	r.mutex.RLock()
	currentServer := r.servers[r.currentIndex]
	r.mutex.RUnlock()

	// Try current fastest server
	ips, err := r.resolveWithServer(ctx, domain, currentServer)
	if err == nil {
		return ips, nil
	}

	// Fallback: try all servers
	for _, server := range r.servers {
		if server == currentServer {
			continue
		}
		ips, err := r.resolveWithServer(ctx, domain, server)
		if err == nil {
			return ips, nil
		}
	}

	// Final fallback: use system resolver
	return net.DefaultResolver.LookupIP(ctx, "ip", domain)
}

// resolveWithServer resolves using a specific DNS server
func (r *DNSResolver) resolveWithServer(ctx context.Context, domain string, server *DNSServer) ([]net.IP, error) {
	switch server.Type {
	case "dot":
		return r.resolveDoT(ctx, domain, server)
	case "doh":
		return r.resolveDoH(ctx, domain, server)
	case "tls":
		return r.resolveDoT(ctx, domain, server) // DoT and TLS use same method
	default:
		return nil, fmt.Errorf("unknown DNS server type: %s", server.Type)
	}
}

// resolveDoT resolves using DNS over TLS
func (r *DNSResolver) resolveDoT(ctx context.Context, domain string, server *DNSServer) ([]net.IP, error) {
	// Create TLS connection with context
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
	}

	// Use context deadline if available
	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		timeout := time.Until(deadline)
		if timeout > 0 && timeout < dialer.Timeout {
			dialer.Timeout = timeout
		}
	}

	conn, err := tls.DialWithDialer(dialer, "tcp", fmt.Sprintf("%s:%d", server.Address, server.Port), &tls.Config{
		ServerName: server.Address,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to DoT server: %v", err)
	}
	defer conn.Close()

	// Build DNS query (simplified - A record query)
	query := buildDNSQuery(domain)

	// Send query with deadline
	queryDeadline := time.Now().Add(5 * time.Second)
	if hasDeadline && deadline.Before(queryDeadline) {
		queryDeadline = deadline
	}

	if err := conn.SetDeadline(queryDeadline); err != nil {
		return nil, err
	}

	if _, err := conn.Write(query); err != nil {
		return nil, fmt.Errorf("failed to send DNS query: %v", err)
	}

	// Read response
	response := make([]byte, 512)
	n, err := conn.Read(response)
	if err != nil {
		return nil, fmt.Errorf("failed to read DNS response: %v", err)
	}

	// Parse response
	return parseDNSResponse(response[:n])
}

// resolveDoH resolves using DNS over HTTPS
func (r *DNSResolver) resolveDoH(ctx context.Context, domain string, server *DNSServer) ([]net.IP, error) {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Build DoH request URL
	url := fmt.Sprintf("%s?name=%s&type=A", server.Address, domain)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/dns-json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query DoH server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH server returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse JSON response
	var dohResp struct {
		Answer []struct {
			Data string `json:"data"`
		} `json:"Answer"`
	}

	if err := json.Unmarshal(body, &dohResp); err != nil {
		return nil, fmt.Errorf("failed to parse DoH response: %v", err)
	}

	var ips []net.IP
	for _, answer := range dohResp.Answer {
		if ip := net.ParseIP(answer.Data); ip != nil {
			ips = append(ips, ip)
		}
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses found in DoH response")
	}

	return ips, nil
}

// buildDNSQuery builds a simple DNS A record query
func buildDNSQuery(domain string) []byte {
	// DNS query format (simplified)
	// This is a basic implementation - for production use a proper DNS library
	query := []byte{
		0x00, 0x00, // Length (will be set later)
		0x00, 0x01, // Transaction ID
		0x01, 0x00, // Flags: standard query
		0x00, 0x01, // Questions: 1
		0x00, 0x00, // Answer RRs: 0
		0x00, 0x00, // Authority RRs: 0
		0x00, 0x00, // Additional RRs: 0
	}

	// Add domain name
	labels := []byte{}
	for _, label := range []byte(domain) {
		if label == '.' {
			continue
		}
		labels = append(labels, label)
	}

	// Encode domain name (simplified)
	parts := []string{}
	currentPart := ""
	for _, c := range domain {
		if c == '.' {
			if currentPart != "" {
				parts = append(parts, currentPart)
				currentPart = ""
			}
		} else {
			currentPart += string(c)
		}
	}
	if currentPart != "" {
		parts = append(parts, currentPart)
	}

	for _, part := range parts {
		query = append(query, byte(len(part)))
		query = append(query, []byte(part)...)
	}
	query = append(query, 0x00) // End of domain name

	// Query type (A record) and class (IN)
	query = append(query, 0x00, 0x01, 0x00, 0x01)

	// Set length
	length := len(query) - 2
	query[0] = byte(length >> 8)
	query[1] = byte(length & 0xFF)

	return query
}

// parseDNSResponse parses a DNS response (simplified)
func parseDNSResponse(response []byte) ([]net.IP, error) {
	if len(response) < 12 {
		return nil, fmt.Errorf("response too short")
	}

	// Skip header and question section (simplified parsing)
	// For production, use a proper DNS library like github.com/miekg/dns

	var ips []net.IP

	// Try to extract IP addresses from response
	// This is a very simplified parser
	for i := 12; i < len(response)-4; i++ {
		// Look for A record (type 1) with 4-byte data
		if i+6 < len(response) {
			if response[i] == 0x00 && response[i+1] == 0x01 { // Type A
				if i+10 < len(response) {
					dataLen := int(response[i+8])<<8 | int(response[i+9])
					if dataLen == 4 && i+10+dataLen <= len(response) {
						ip := net.IPv4(response[i+10], response[i+11], response[i+12], response[i+13])
						ips = append(ips, ip)
					}
				}
			}
		}
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses found in response")
	}

	return ips, nil
}

// GetCurrentServer returns information about the currently selected server
func (r *DNSResolver) GetCurrentServer() *DNSServer {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.servers[r.currentIndex]
}

// GetAllServers returns information about all servers
func (r *DNSResolver) GetAllServers() []*DNSServer {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	servers := make([]*DNSServer, len(r.servers))
	copy(servers, r.servers)
	return servers
}
