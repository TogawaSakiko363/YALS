package agent

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"YALS/internal/logger"
	"YALS/internal/proto"

	probing "github.com/prometheus-community/pro-bing"
)

const (
	probePingCount   = 4
	probePingTimeout = 5 * time.Second
	probeConcurrency = 20
)

// setProbeConfig stores a new probe configuration and wakes the probe loop so
// the new interval/targets take effect immediately.
func (c *Client) setProbeConfig(cfg proto.ProbeConfig) {
	c.probeMu.Lock()
	c.probeCfg = cfg
	c.probeMu.Unlock()
	select {
	case c.probeReconfig <- struct{}{}:
	default:
	}
}

func (c *Client) currentProbeConfig() proto.ProbeConfig {
	c.probeMu.Lock()
	defer c.probeMu.Unlock()
	return c.probeCfg
}

// runProbeLoop pings the configured targets every interval and reports the
// results on the stream until ctx is cancelled. A reconfiguration wakes it early.
func (c *Client) runProbeLoop(ctx context.Context, stream proto.AgentService_StreamCommandsClient) {
	for {
		cfg := c.currentProbeConfig()
		interval := cfg.IntervalSec
		if interval <= 0 {
			interval = 60
		}
		if len(cfg.Targets) > 0 {
			c.runProbeCycle(ctx, stream, cfg.Targets)
		}

		select {
		case <-ctx.Done():
			return
		case <-c.probeReconfig:
		case <-time.After(time.Duration(interval) * time.Second):
		}
	}
}

func (c *Client) runProbeCycle(ctx context.Context, stream proto.AgentService_StreamCommandsClient, targets []proto.ProbeTargetSpec) {
	results := make([]proto.ProbeResult, len(targets))
	sem := make(chan struct{}, probeConcurrency)
	var wg sync.WaitGroup

	for i, t := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, t proto.ProbeTargetSpec) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = probeOne(t)
		}(i, t)
	}
	wg.Wait()

	if ctx.Err() != nil {
		return
	}

	batch := proto.ProbeBatch{TS: time.Now().Unix(), Results: results}
	data, err := json.Marshal(batch)
	if err != nil {
		return
	}
	if err := c.streamSend(stream, &proto.CommandMessage{Type: "probe_report", Data: data}); err != nil {
		logger.Debugf("probe report send failed: %v", err)
	}
}

// probeOne measures a single target for one cycle, dispatching on its protocol.
// TCP measures connect (handshake) RTT; everything else (empty/ICMP/unknown)
// falls back to ICMP, so existing ICMP-only configs keep working unchanged.
func probeOne(t proto.ProbeTargetSpec) proto.ProbeResult {
	if strings.EqualFold(t.Protocol, "TCP") {
		return probeTCP(t)
	}
	return probeICMP(t)
}

// probeICMP ICMP-pings a single target and returns its cycle result. A failure to
// run (e.g. missing raw-socket privilege) yields zero received packets (100% loss).
func probeICMP(t proto.ProbeTargetSpec) proto.ProbeResult {
	res := proto.ProbeResult{Name: t.Name, Sent: probePingCount}

	pinger, err := probing.NewPinger(t.IP)
	if err != nil {
		return res
	}
	pinger.Count = probePingCount
	pinger.Timeout = probePingTimeout
	pinger.Interval = 300 * time.Millisecond
	pinger.SetPrivileged(true) // raw ICMP; the agent already needs privileges for mtr/traceroute

	if err := pinger.Run(); err != nil {
		return res
	}

	stats := pinger.Statistics()
	res.Sent = stats.PacketsSent
	res.Recv = stats.PacketsRecv
	if stats.PacketsRecv > 0 {
		res.LatencyMs = float64(stats.AvgRtt) / float64(time.Millisecond)
	}
	return res
}

// probeTCP measures TCP handshake RTT to IP:Port over probePingCount attempts.
// Each successful connect counts as a received "packet"; the average connect time
// is the reported latency. An unusable port yields 100% loss.
func probeTCP(t proto.ProbeTargetSpec) proto.ProbeResult {
	res := proto.ProbeResult{Name: t.Name, Sent: probePingCount}
	if t.Port <= 0 || t.Port > 65535 {
		return res // no valid port: report as fully lost rather than probing nothing
	}

	addr := net.JoinHostPort(t.IP, strconv.Itoa(t.Port))
	var total time.Duration
	for i := 0; i < probePingCount; i++ {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", addr, probePingTimeout)
		if err == nil {
			total += time.Since(start)
			res.Recv++
			_ = conn.Close()
		}
		if i < probePingCount-1 {
			time.Sleep(300 * time.Millisecond) // pace attempts, mirroring the ICMP interval
		}
	}

	if res.Recv > 0 {
		res.LatencyMs = float64(total) / float64(res.Recv) / float64(time.Millisecond)
	}
	return res
}
