import { useEffect, useState } from 'react';
import { CustomConfig } from '../hooks/useCustomConfig';
import { useYalsClient } from '../hooks/useYalsClient';
import { StatusItem } from '../types/yals';
import { PageHeader } from '../components/PageHeader';
import { PageFooter } from '../components/PageFooter';
import { formatBytes, formatBps, formatPct, formatUptime } from '../utils/format';
import { getErrorMessage } from '../utils/error';

interface StatusPageProps {
  config: CustomConfig;
}

function MetricBar({ label, value, used, total }: { label: string; value: number; used?: number; total?: number }) {
  return (
    <div className="status-metric">
      <div className="status-metric-head">
        <span>{label}</span>
        <span>{formatPct(value)}</span>
      </div>
      <div className="status-bar">
        <div className="status-bar-fill" style={{ width: `${Math.min(Math.max(value, 0), 100)}%` }} />
      </div>
      {used !== undefined && total !== undefined && (
        <div className="status-metric-sub">{formatBytes(used)} / {formatBytes(total)}</div>
      )}
    </div>
  );
}

function StatusCard({ item }: { item: StatusItem }) {
  const m = item.metrics;
  const memPct = m && m.mem_total > 0 ? (m.mem_used / m.mem_total) * 100 : 0;
  const diskPct = m && m.disk_total > 0 ? (m.disk_used / m.disk_total) * 100 : 0;

  return (
    <div className={`status-card ${item.online ? '' : 'is-offline'}`}>
      <div className="status-card-head">
        <span className={`status-dot ${item.online ? 'online' : 'offline'}`}>{item.name}</span>
        {item.group && <span className="status-card-group">{item.group}</span>}
      </div>

      {!item.online || !m ? (
        <p className="status-card-empty">{item.online ? 'Awaiting metrics…' : 'Offline'}</p>
      ) : (
        <>
          <MetricBar label="CPU" value={m.cpu_percent} />
          <MetricBar label="Memory" value={memPct} used={m.mem_used} total={m.mem_total} />
          <MetricBar label="Disk" value={diskPct} used={m.disk_used} total={m.disk_total} />
          <div className="status-net">
            <span title="Upload bandwidth">↑ {formatBps(m.net_up_rate)}</span>
            <span title="Download bandwidth">↓ {formatBps(m.net_down_rate)}</span>
          </div>
          <div className="status-net-total">
            <span title="Total uploaded">↑ {formatBytes(m.net_up_total)}</span>
            <span title="Total downloaded">↓ {formatBytes(m.net_down_total)}</span>
            <span className="status-uptime" title="Uptime">{formatUptime(m.uptime_sec)}</span>
          </div>
        </>
      )}
    </div>
  );
}

export function StatusPage({ config }: StatusPageProps) {
  const { fetchStatus } = useYalsClient();
  const [items, setItems] = useState<StatusItem[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const load = () => {
      fetchStatus()
        .then((data) => {
          if (!cancelled) {
            setItems(data);
            setError(null);
          }
        })
        .catch((e) => {
          if (!cancelled) setError(getErrorMessage(e));
        });
    };
    load();
    const id = setInterval(load, 5000);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [fetchStatus]);

  return (
    <div className="app-container" style={{ backgroundColor: config.backgroundColor }}>
      <PageHeader config={config} active="status" />
      <main className="main-content">
        <div className="container">
          {error && <div className="command-status error">{error}</div>}
          {items.length === 0 && !error && <p className="text-sm text-gray-500">No agents registered yet.</p>}
          <div className="status-grid">
            {items.map((item) => (
              <StatusCard key={item.uuid} item={item} />
            ))}
          </div>
        </div>
      </main>
      <PageFooter config={config} />
    </div>
  );
}
