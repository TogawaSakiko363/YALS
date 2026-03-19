package dns

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// IPVersion represents the IP version preference
type IPVersion string

const (
	IPVersionAuto IPVersion = "auto" // Prefer IPv4, fallback to IPv6
	IPVersionIPv4 IPVersion = "ipv4" // Force IPv4 only
	IPVersionIPv6 IPVersion = "ipv6" // Force IPv6 only
)

const (
	dohServerURL = "https://dns.google/resolve"
	queryTimeout = 1 * time.Second
)

var httpClient = &http.Client{
	Timeout: 3 * time.Second,
	Transport: &http.Transport{
		DisableKeepAlives:   false,
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
	},
}

// Resolve resolves a domain name to IP addresses using Google DoH
func Resolve(ctx context.Context, domain string) ([]net.IP, error) {
	return ResolveWithVersion(ctx, domain, IPVersionAuto)
}

// ResolveWithVersion resolves a domain name with specific IP version preference
func ResolveWithVersion(ctx context.Context, domain string, version IPVersion) ([]net.IP, error) {
	// Create a context with timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, queryTimeout)
		defer cancel()
	}

	switch version {
	case IPVersionIPv4:
		// Query only A record (IPv4)
		return queryDoH(ctx, domain, "A")

	case IPVersionIPv6:
		// Query only AAAA record (IPv6)
		return queryDoH(ctx, domain, "AAAA")

	case IPVersionAuto:
		// Query both, prefer IPv4
		type queryResult struct {
			ips []net.IP
			err error
		}

		resultChan := make(chan queryResult, 2)

		// Query A record (IPv4)
		go func() {
			ips, err := queryDoH(ctx, domain, "A")
			resultChan <- queryResult{ips: ips, err: err}
		}()

		// Query AAAA record (IPv6)
		go func() {
			ips, err := queryDoH(ctx, domain, "AAAA")
			resultChan <- queryResult{ips: ips, err: err}
		}()

		// Collect results, prefer IPv4
		var ipv4IPs []net.IP
		var ipv6IPs []net.IP
		var lastErr error

		for range 2 {
			select {
			case res := <-resultChan:
				if res.err == nil && len(res.ips) > 0 {
					// Check if it's IPv4 or IPv6
					if res.ips[0].To4() != nil {
						ipv4IPs = res.ips
					} else {
						ipv6IPs = res.ips
					}
				} else {
					lastErr = res.err
				}
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		// Prefer IPv4, fallback to IPv6
		if len(ipv4IPs) > 0 {
			return ipv4IPs, nil
		}
		if len(ipv6IPs) > 0 {
			return ipv6IPs, nil
		}

		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("no IP addresses found in DoH response")

	default:
		return nil, fmt.Errorf("unknown IP version: %s", version)
	}
}

// queryDoH performs a single DoH query for a specific record type
func queryDoH(ctx context.Context, domain, recordType string) ([]net.IP, error) {
	// Build DoH request URL
	url := fmt.Sprintf("%s?name=%s&type=%s", dohServerURL, domain, recordType)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/dns-json")

	resp, err := httpClient.Do(req)
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
