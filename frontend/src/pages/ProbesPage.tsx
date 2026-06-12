import { Fragment, useCallback, useEffect, useMemo, useState } from 'react';
import { CustomConfig } from '../hooks/useCustomConfig';
import { useYalsClient } from '../hooks/useYalsClient';
import { ProbeRow } from '../types/yals';
import { PageHeader } from '../components/PageHeader';
import { getErrorMessage } from '../utils/error';

interface ProbesPageProps {
  config: CustomConfig;
}

const WINDOWS = ['1h', '6h', '12h', '24h'];
const GROUPS = [
  { value: 'all', label: 'All' },
  { value: 'location', label: 'Location' },
  { value: 'isp', label: 'ISP' },
  { value: 'protocol', label: 'Protocol' }
];

function lossClass(loss: number): string {
  if (loss >= 50) return 'probe-loss bad';
  if (loss > 0) return 'probe-loss warn';
  return 'probe-loss ok';
}

export function ProbesPage({ config }: ProbesPageProps) {
  const { fetchProbes, fetchProbesMeta } = useYalsClient();
  const [agents, setAgents] = useState<string[]>([]);
  const [agent, setAgent] = useState('');
  const [group, setGroup] = useState('all');
  const [windowSel, setWindowSel] = useState('1h');
  const [rows, setRows] = useState<ProbeRow[]>([]);
  const [error, setError] = useState<string | null>(null);

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
    fetchProbes(agent, group, windowSel)
      .then((res) => {
        setRows(res.rows || []);
        setError(null);
      })
      .catch((e) => setError(getErrorMessage(e)));
  }, [fetchProbes, agent, group, windowSel]);

  useEffect(() => {
    if (!agent) return;
    load();
    const id = setInterval(load, 15000);
    return () => clearInterval(id);
  }, [load, agent]);

  const groupedRows = useMemo(() => {
    if (group === 'all') {
      return [{ key: '', rows }];
    }
    const map = new Map<string, ProbeRow[]>();
    for (const r of rows) {
      const key = (group === 'location' ? r.location : group === 'isp' ? r.isp : r.protocol) || '—';
      const list = map.get(key);
      if (list) {
        list.push(r);
      } else {
        map.set(key, [r]);
      }
    }
    return Array.from(map.entries())
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([key, groupRows]) => ({ key, rows: groupRows }));
  }, [rows, group]);

  const colCount = group === 'all' ? 6 : 4;

  return (
    <div className="app-container" style={{ backgroundColor: config.backgroundColor }}>
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
              <select value={group} onChange={(e) => setGroup(e.target.value)} className="command-select">
                {GROUPS.map((g) => (
                  <option key={g.value} value={g.value}>{g.label}</option>
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
                  {group === 'all' && (
                    <>
                      <th>Location</th>
                      <th>ISP</th>
                    </>
                  )}
                  <th>Latest</th>
                  <th>Avg</th>
                  <th>Loss</th>
                </tr>
              </thead>
              <tbody>
                {groupedRows.map((g) => (
                  <Fragment key={g.key || 'all'}>
                    {g.key && (
                      <tr className="probes-group-row">
                        <td colSpan={colCount}>{g.key}</td>
                      </tr>
                    )}
                    {g.rows.map((r) => (
                      <tr key={r.name}>
                        <td className="font-medium text-gray-900">{r.name}</td>
                        {group === 'all' && (
                          <>
                            <td>{r.location || '—'}</td>
                            <td>{r.isp || '—'}</td>
                          </>
                        )}
                        <td>{r.has_latest ? `${r.latest_ms.toFixed(1)} ms` : '—'}</td>
                        <td>{r.has_avg ? `${r.avg_ms.toFixed(1)} ms` : '—'}</td>
                        <td>{r.has_data ? <span className={lossClass(r.loss_pct)}>{r.loss_pct.toFixed(0)}%</span> : '—'}</td>
                      </tr>
                    ))}
                  </Fragment>
                ))}
                {rows.length === 0 && (
                  <tr>
                    <td colSpan={colCount} className="control-table-empty">No probe data yet for this agent.</td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      </main>
    </div>
  );
}
