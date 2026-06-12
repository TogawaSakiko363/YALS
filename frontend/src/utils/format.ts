// Human-readable formatting helpers for the monitoring pages.

export function formatBytes(bytes: number): string {
  if (!bytes || bytes < 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let value = bytes;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return `${value.toFixed(value >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}

// formatBps takes bytes/sec and renders bits/sec (the bandwidth convention).
export function formatBps(bytesPerSec: number): string {
  let bits = (bytesPerSec || 0) * 8;
  const units = ['bps', 'Kbps', 'Mbps', 'Gbps'];
  let i = 0;
  while (bits >= 1000 && i < units.length - 1) {
    bits /= 1000;
    i++;
  }
  return `${bits.toFixed(bits >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}

export function formatPct(n: number): string {
  return `${Math.round(n || 0)}%`;
}

export function formatUptime(sec: number): string {
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}
