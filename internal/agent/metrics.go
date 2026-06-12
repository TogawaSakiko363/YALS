package agent

import (
	"context"
	"encoding/json"
	"runtime"
	"time"

	"YALS/internal/logger"
	"YALS/internal/proto"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	psnet "github.com/shirou/gopsutil/v4/net"
)

const metricsInterval = 5 * time.Second

func rootDiskPath() string {
	if runtime.GOOS == "windows" {
		return "C:\\"
	}
	return "/"
}

// runMetricsReporter periodically collects host resource usage and reports it on
// the stream until ctx is cancelled (the connection ended). Bandwidth is derived
// from net-counter deltas; totals are cumulative bytes since this reporter started.
func (c *Client) runMetricsReporter(ctx context.Context, stream proto.AgentService_StreamCommandsClient) {
	// Prime the CPU delta and capture the net baseline for cumulative totals.
	_, _ = cpu.Percent(0, false)
	var lastUp, lastDown, baseUp, baseDown uint64
	if counters, err := psnet.IOCounters(false); err == nil && len(counters) > 0 {
		lastUp, lastDown = counters[0].BytesSent, counters[0].BytesRecv
		baseUp, baseDown = lastUp, lastDown
	}
	lastSample := time.Now()

	ticker := time.NewTicker(metricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		var m proto.SystemMetrics
		if pct, err := cpu.Percent(0, false); err == nil && len(pct) > 0 {
			m.CPUPercent = pct[0]
		}
		if vm, err := mem.VirtualMemory(); err == nil {
			m.MemUsed, m.MemTotal = vm.Used, vm.Total
		}
		if du, err := disk.Usage(rootDiskPath()); err == nil {
			m.DiskUsed, m.DiskTotal = du.Used, du.Total
		}
		if up, err := host.Uptime(); err == nil {
			m.UptimeSec = up
		}

		now := time.Now()
		if counters, err := psnet.IOCounters(false); err == nil && len(counters) > 0 {
			curUp, curDown := counters[0].BytesSent, counters[0].BytesRecv
			if dt := now.Sub(lastSample).Seconds(); dt > 0 {
				if curUp >= lastUp {
					m.NetUpRate = float64(curUp-lastUp) / dt
				}
				if curDown >= lastDown {
					m.NetDownRate = float64(curDown-lastDown) / dt
				}
			}
			if curUp >= baseUp {
				m.NetUpTotal = curUp - baseUp
			}
			if curDown >= baseDown {
				m.NetDownTotal = curDown - baseDown
			}
			lastUp, lastDown, lastSample = curUp, curDown, now
		}

		data, err := json.Marshal(m)
		if err != nil {
			continue
		}
		if err := c.streamSend(stream, &proto.CommandMessage{Type: "metrics_report", Data: data}); err != nil {
			logger.Debugf("metrics report send failed: %v", err)
			return
		}
	}
}
