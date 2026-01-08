package dns

import (
	"context"
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
	Type     string // "doh" only
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

// NewDNSResolver creates a new DNS resolver with predefined DoH servers
func NewDNSResolver() *DNSResolver {
	return &DNSResolver{
		servers: []*DNSServer{
			{
				Name:    "Alibaba",
				Type:    "doh",
				Address: "https://223.5.5.5/resolve",
				Port:    443,
			},
			{
				Name:    "Google",
				Type:    "doh",
				Address: "https://8.8.8.8/resolve",
				Port:    443,
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
	testDomain := "www.google.com"

	for _, server := range r.servers {
		wg.Add(1)
		go func(srv *DNSServer) {
			defer wg.Done()

			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			_, err := r.resolveWithServer(ctx, testDomain, srv)
			elapsed := time.Since(start)

			r.mutex.Lock()
			if err == nil {
				srv.Latency = elapsed
			} else {
				srv.Latency = 10 * time.Second // Set high latency on failure
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
	// Create a context with timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	r.mutex.RLock()
	currentServer := r.servers[r.currentIndex]
	r.mutex.RUnlock()

	// Try current fastest server first
	ips, err := r.resolveWithServer(ctx, domain, currentServer)
	if err == nil && len(ips) > 0 {
		return ips, nil
	}

	// Fallback: try all other servers in parallel
	type result struct {
		ips []net.IP
		err error
	}

	resultChan := make(chan result, len(r.servers))

	for _, server := range r.servers {
		if server == currentServer {
			continue
		}

		go func(srv *DNSServer) {
			ips, err := r.resolveWithServer(ctx, domain, srv)
			resultChan <- result{ips: ips, err: err}
		}(server)
	}

	// Wait for first successful result or all failures
	for i := 0; i < len(r.servers)-1; i++ {
		select {
		case res := <-resultChan:
			if res.err == nil && len(res.ips) > 0 {
				return res.ips, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Final fallback: use system resolver
	return net.DefaultResolver.LookupIP(ctx, "ip", domain)
}

// resolveWithServer resolves using a specific DNS server
func (r *DNSResolver) resolveWithServer(ctx context.Context, domain string, server *DNSServer) ([]net.IP, error) {
	if server.Type == "doh" {
		return r.resolveDoH(ctx, domain, server)
	}
	return nil, fmt.Errorf("unknown DNS server type: %s", server.Type)
}

// resolveDoH resolves using DNS over HTTPS (queries both A and AAAA records)
func (r *DNSResolver) resolveDoH(ctx context.Context, domain string, server *DNSServer) ([]net.IP, error) {
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives:   false,
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// Query both A (IPv4) and AAAA (IPv6) records in parallel
	type queryResult struct {
		ips []net.IP
		err error
	}

	resultChan := make(chan queryResult, 2)

	// Query A record (IPv4)
	go func() {
		ips, err := r.queryDoH(ctx, client, server.Address, domain, "A")
		resultChan <- queryResult{ips: ips, err: err}
	}()

	// Query AAAA record (IPv6)
	go func() {
		ips, err := r.queryDoH(ctx, client, server.Address, domain, "AAAA")
		resultChan <- queryResult{ips: ips, err: err}
	}()

	// Collect results from both queries
	var allIPs []net.IP
	var lastErr error

	for i := 0; i < 2; i++ {
		select {
		case res := <-resultChan:
			if res.err == nil {
				allIPs = append(allIPs, res.ips...)
			} else {
				lastErr = res.err
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if len(allIPs) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("no IP addresses found in DoH response")
	}

	return allIPs, nil
}

// queryDoH performs a single DoH query for a specific record type
func (r *DNSResolver) queryDoH(ctx context.Context, client *http.Client, serverAddr, domain, recordType string) ([]net.IP, error) {
	// Build DoH request URL
	url := fmt.Sprintf("%s?name=%s&type=%s", serverAddr, domain, recordType)

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
			Type int    `json:"type"`
		} `json:"Answer"`
	}

	if err := json.Unmarshal(body, &dohResp); err != nil {
		return nil, fmt.Errorf("failed to parse DoH response: %v", err)
	}

	var ips []net.IP
	for _, answer := range dohResp.Answer {
		// Type 1 = A record (IPv4), Type 28 = AAAA record (IPv6)
		if (recordType == "A" && answer.Type == 1) || (recordType == "AAAA" && answer.Type == 28) {
			if ip := net.ParseIP(answer.Data); ip != nil {
				ips = append(ips, ip)
			}
		}
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
