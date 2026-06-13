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

function StatusCardSkeleton() {
  return (
    <div className="status-card status-card-skeleton" aria-hidden="true">
      <div className="skeleton-line" />
      <div className="skeleton-bar" />
      <div className="skeleton-bar" />
      <div className="skeleton-bar" />
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
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    const load = () => {
      fetchStatus()
        .then((data) => {
          if (!cancelled) {
            setItems(data);
            setError(null);
            setLoading(false);
          }
        })
        .catch((e) => {
          // Keep the last good data on a transient poll failure so the page
          // doesn't flicker back to an empty/error state mid-refresh.
          if (!cancelled) {
            setError(getErrorMessage(e));
            setLoading(false);
          }
        });
    };
    load();
    const id = setInterval(load, 5000);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [fetchStatus]);

  const showError = error && items.length === 0;
  const showEmpty = !loading && !error && items.length === 0;
  const showSkeletons = loading && items.length === 0;

  return (
    <div className="app-container">
      <PageHeader config={config} active="status" />
      <main className="main-content">
        <div className="container">
          {showError && <div className="command-status error">{error}</div>}
          {showEmpty && <p className="text-sm u-text-muted">No agents registered yet.</p>}
          <div className="status-grid">
            {showSkeletons
              ? Array.from({ length: 3 }).map((_, i) => <StatusCardSkeleton key={i} />)
              : items.map((item) => <StatusCard key={item.uuid} item={item} />)}
          </div>
        </div>
      </main>
      <PageFooter config={config} />
    </div>
  );
}
