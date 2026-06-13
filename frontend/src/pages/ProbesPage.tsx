import { Fragment, useCallback, useEffect, useMemo, useState } from 'react';
import { ChevronDown } from 'lucide-react';
import { CustomConfig } from '../hooks/useCustomConfig';
import { useYalsClient } from '../hooks/useYalsClient';
import { ProbeRow, ProbeSeriesPoint } from '../types/yals';
import { PageHeader } from '../components/PageHeader';
import { PageFooter } from '../components/PageFooter';
import { LatencyChart } from '../components/LatencyChart';
import { getErrorMessage } from '../utils/error';

interface ProbesPageProps {
  config: CustomConfig;
}

const WINDOWS = ['1h', '6h', '12h', '24h'];

type SortKey = 'latest' | 'avg' | 'worst' | 'jitter' | 'loss';

// metricValue returns the sortable numeric value of a column for a row, or null
// when that row has no data for it (null rows always sort to the bottom).
function metricValue(r: ProbeRow, key: SortKey): number | null {
  switch (key) {
    case 'latest': return r.has_latest ? r.latest_ms : null;
    case 'avg': return r.has_avg ? r.avg_ms : null;
    case 'worst': return r.has_worst ? r.worst_ms : null;
    case 'jitter': return r.has_jitter ? r.jitter_ms : null;
    case 'loss': return r.has_data ? r.loss_pct : null;
  }
}

function lossClass(loss: number): string {
  if (loss >= 50) return 'probe-loss bad';
  if (loss > 0) return 'probe-loss warn';
  return 'probe-loss ok';
}

// Distinct, sorted, non-empty values of one field across the rows — the option
// set for that field's filter dropdown.
function distinct(rows: ProbeRow[], pick: (r: ProbeRow) => string): string[] {
  const set = new Set<string>();
  for (const r of rows) {
    const v = pick(r).trim();
    if (v) set.add(v);
  }
  return Array.from(set).sort((a, b) => a.localeCompare(b));
}

export function ProbesPage({ config }: ProbesPageProps) {
  const { fetchProbes, fetchProbeSeries, fetchProbesMeta } = useYalsClient();
  const [agents, setAgents] = useState<string[]>([]);
  const [agent, setAgent] = useState('');
  const [windowSel, setWindowSel] = useState('1h');
  const [rows, setRows] = useState<ProbeRow[]>([]);
  const [error, setError] = useState<string | null>(null);

  // Per-row expandable latency chart: which targets are open, and their lazily
  // loaded series (keyed by target name).
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [series, setSeries] = useState<Record<string, ProbeSeriesPoint[]>>({});

  // Independent filters: each narrows the table by one label dimension; "all"
  // disables that dimension. They compose with AND.
  const [locationFilter, setLocationFilter] = useState('all');
  const [ispFilter, setIspFilter] = useState('all');
  const [protocolFilter, setProtocolFilter] = useState('all');

  // Click-to-sort on the metric columns. null = default (targets.yaml) order.
  const [sortKey, setSortKey] = useState<SortKey | null>(null);
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc');

  const toggleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      setSortKey(key);
      setSortDir('desc');
    }
  };

  useEffect(() => {
    fetchProbesMeta()
      .then((meta) => {
        setAgents(meta.agents);
        setAgent((cur) => cur || meta.agents[0] || '');
      })
      .catch((e) => setError(getErrorMessage(e)));
  }, [fetchProbesMeta]);

  const load = useCallback(() => {
    if (!agent) return;
    fetchProbes(agent, windowSel)
      .then((res) => {
        setRows(res.rows || []);
        setError(null);
      })
      .catch((e) => setError(getErrorMessage(e)));
  }, [fetchProbes, agent, windowSel]);

  useEffect(() => {
    if (!agent) return;
    load();
    const id = setInterval(load, 15000);
    return () => clearInterval(id);
  }, [load, agent]);

  const toggleExpand = (name: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  };

  // Refresh the series for every expanded row. Re-runs (and refetches) whenever a
  // row is opened or the agent/window changes, and ticks every 15s while open so
  // the charts stay live alongside the table.
  const refreshSeries = useCallback(() => {
    if (!agent || expanded.size === 0) return;
    for (const name of expanded) {
      fetchProbeSeries(agent, name, windowSel)
        .then((res) => setSeries((prev) => ({ ...prev, [name]: res.points || [] })))
        .catch(() => {
          // Keep the previous series rather than blanking the chart on a blip.
        });
    }
  }, [agent, windowSel, expanded, fetchProbeSeries]);

  useEffect(() => {
    if (expanded.size === 0) return;
    refreshSeries();
    const id = setInterval(refreshSeries, 15000);
    return () => clearInterval(id);
  }, [refreshSeries, expanded]);

  // Target labels are the same for every agent (one shared targets.yaml), so the
  // option sets are stable as the selected agent changes.
  const locations = useMemo(() => distinct(rows, (r) => r.location), [rows]);
  const isps = useMemo(() => distinct(rows, (r) => r.isp), [rows]);
  const protocols = useMemo(() => distinct(rows, (r) => r.protocol), [rows]);

  const visibleRows = useMemo(
    () =>
      rows.filter(
        (r) =>
          (locationFilter === 'all' || r.location === locationFilter) &&
          (ispFilter === 'all' || r.isp === ispFilter) &&
          (protocolFilter === 'all' || r.protocol === protocolFilter)
      ),
    [rows, locationFilter, ispFilter, protocolFilter]
  );

  const sortedRows = useMemo(() => {
    if (!sortKey) return visibleRows;
    const dir = sortDir === 'asc' ? 1 : -1;
    return [...visibleRows].sort((a, b) => {
      const va = metricValue(a, sortKey);
      const vb = metricValue(b, sortKey);
      if (va === null && vb === null) return 0;
      if (va === null) return 1; // no-data rows always last
      if (vb === null) return -1;
      return (va - vb) * dir;
    });
  }, [visibleRows, sortKey, sortDir]);

  return (
    <div className="app-container">
      <PageHeader config={config} active="probes" />
      <main className="main-content">
        <div className="container">
          <div className="probes-toolbar">
            <div className="probes-toolbar-left">
              <select value={agent} onChange={(e) => setAgent(e.target.value)} className="command-select">
                {agents.length === 0 && <option value="">No agents</option>}
                {agents.map((a) => (
                  <option key={a} value={a}>{a}</option>
                ))}
              </select>
              <select value={locationFilter} onChange={(e) => setLocationFilter(e.target.value)} className="command-select">
                <option value="all">All Locations</option>
                {locations.map((v) => (
                  <option key={v} value={v}>{v}</option>
                ))}
              </select>
              <select value={ispFilter} onChange={(e) => setIspFilter(e.target.value)} className="command-select">
                <option value="all">All ISPs</option>
                {isps.map((v) => (
                  <option key={v} value={v}>{v}</option>
                ))}
              </select>
              <select value={protocolFilter} onChange={(e) => setProtocolFilter(e.target.value)} className="command-select">
                <option value="all">All Protocols</option>
                {protocols.map((v) => (
                  <option key={v} value={v}>{v}</option>
                ))}
              </select>
            </div>
            <div className="probes-windows">
              {WINDOWS.map((w) => (
                <button key={w} type="button" className={`probes-window ${windowSel === w ? 'active' : ''}`} onClick={() => setWindowSel(w)}>
                  {w}
                </button>
              ))}
            </div>
          </div>

          {error && <div className="command-status error">{error}</div>}

          <div className="control-table-wrap">
            <table className="control-table">
              <thead>
                <tr>
                  <th>Target</th>
                  <th>Location</th>
                  <th>ISP</th>
                  <th>Protocol</th>
                  {([['latest', 'Latest'], ['avg', 'Avg'], ['worst', 'Worst'], ['jitter', 'Jitter'], ['loss', 'Loss']] as [SortKey, string][]).map(([key, label]) => (
                    <th key={key} className="probes-th-sortable" onClick={() => toggleSort(key)} title={`Sort by ${label}`}>
                      {label}{sortKey === key ? (sortDir === 'asc' ? ' ▲' : ' ▼') : ''}
                    </th>
                  ))}
                  <th aria-label="Expand"></th>
                </tr>
              </thead>
              <tbody>
                {sortedRows.map((r) => {
                  const isOpen = expanded.has(r.name);
                  return (
                    <Fragment key={r.name}>
                      <tr>
                        <td className="font-medium u-text">{r.name}</td>
                        <td>{r.location || '—'}</td>
                        <td>{r.isp || '—'}</td>
                        <td>{r.protocol === 'TCP' ? `TCP:${r.port}` : (r.protocol || '—')}</td>
                        <td>{r.has_latest ? `${r.latest_ms.toFixed(1)} ms` : '—'}</td>
                        <td>{r.has_avg ? `${r.avg_ms.toFixed(1)} ms` : '—'}</td>
                        <td>{r.has_worst ? `${r.worst_ms.toFixed(1)} ms` : '—'}</td>
                        <td>{r.has_jitter ? `${r.jitter_ms.toFixed(1)} ms` : '—'}</td>
                        <td>{r.has_data ? <span className={lossClass(r.loss_pct)}>{r.loss_pct.toFixed(0)}%</span> : '—'}</td>
                        <td>
                          <button
                            type="button"
                            className="probes-expand-button"
                            onClick={() => toggleExpand(r.name)}
                            aria-label={isOpen ? 'Hide latency chart' : 'Show latency chart'}
                            aria-expanded={isOpen}
                          >
                            <ChevronDown className={`probes-expand-icon w-4 h-4 ${isOpen ? 'open' : ''}`} />
                          </button>
                        </td>
                      </tr>
                      {isOpen && (
                        <tr>
                          <td colSpan={10} className="probes-chart-cell">
                            {series[r.name]
                              ? <LatencyChart points={series[r.name]} name={r.name} />
                              : <div className="latency-chart-empty">Loading…</div>}
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  );
                })}
                {sortedRows.length === 0 && (
                  <tr>
                    <td colSpan={10} className="control-table-empty">
                      {rows.length === 0 ? 'No probe data yet for this agent.' : 'No targets match the selected filters.'}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      </main>
      <PageFooter config={config} />
    </div>
  );
}
