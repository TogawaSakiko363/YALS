import { useEffect, useState } from 'react';
import { Plus, Save, Trash2, Shield, Server, Settings, ChevronUp, ChevronDown, LogOut, Pencil, X, Activity, Home, Download, Copy, Check, Menu, Sun, Moon } from 'lucide-react';
import { useTheme } from '../hooks/useTheme';
import { useYalsClient } from '../hooks/useYalsClient';
import { AgentCommand, AgentConfigPayload, AgentConfigRecord, RuntimeSettings, ProbeTarget } from '../types/yals';
import { getErrorMessage } from '../utils/error';

const createEmptyAgent = (): AgentConfigPayload => ({
  token: '',
  name: '',
  group: '',
  details: {
    location: '',
    datacenter: '',
    test_ip: '',
    description: ''
  },
  commands: [
    {
      name: 'ping',
      template: 'ping -c 4',
      ignore_target: false,
      maxmium_queue: 0
    }
  ]
});

function FieldLabel({ children }: { children: React.ReactNode }) {
  return <label className="block text-sm font-medium u-text mb-1 text-left">{children}</label>;
}

function getCommandMode(command: AgentCommand): 'shell' | 'plugin' {
  return command.use_plugin ? 'plugin' : 'shell';
}

// validateAgentForm mirrors the server-side checks so the operator gets
// immediate, precise feedback before a save round-trip. Returns an error message
// or null when the form is valid.
function validateAgentForm(agent: AgentConfigPayload): string | null {
  if (!agent.name.trim()) return 'Agent name is required';
  if (agent.commands.length === 0) return 'At least one command is required';

  const seen = new Set<string>();
  for (let i = 0; i < agent.commands.length; i += 1) {
    const command = agent.commands[i];
    const mode = getCommandMode(command);
    // In plugin mode the command name is the plugin name (no separate name field).
    const name = (mode === 'plugin' ? (command.use_plugin || '') : command.name).trim();
    if (!name) return mode === 'plugin' ? `Command #${i + 1}: select a plugin` : `Command #${i + 1}: name is required`;
    if (seen.has(name)) return `Duplicate command name: ${name}`;
    seen.add(name);

    if (mode === 'shell') {
      const template = (command.template || '').trim();
      if (!template) return `Command "${name}": template is required`;
      if (command.ignore_target && template.includes('{target}')) {
        return `Command "${name}": template uses {target} but "Ignore Target Input" is enabled`;
      }
    }
  }
  return null;
}

// buildInstallCommand returns the one-line installer the operator runs on the
// agent host. It pulls install_agent.sh from the repo and runs it with this
// server's address (derived from the panel's own URL) plus the agent's uuid/token.
function buildInstallCommand(uuid: string, token: string): string {
  const host = window.location.hostname || 'example.com';
  const port = window.location.port || (window.location.protocol === 'https:' ? '443' : '80');
  return `curl -fsSL https://raw.githubusercontent.com/TogawaSakiko363/YALS/refs/heads/main/install_agent.sh | sudo bash -s -- --server-host ${host} --server-port ${port} --uuid ${uuid} --token ${token}`;
}

// copyToClipboard copies text using the async Clipboard API, falling back to a
// hidden textarea + execCommand for non-secure contexts / older browsers.
async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // fall through to the legacy path
  }
  try {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(ta);
    return ok;
  } catch {
    return false;
  }
}

export function ControlPanel() {
  const {
    isControlAuthenticated,
    managedAgents,
    availablePlugins,
    nodeStatuses,
    runtimeSettings,
    loginControl,
    logoutControl,
    listManagedAgents,
    listPlugins,
    fetchAgentStatuses,
    fetchProbeTargets,
    saveProbeTargets,
    fetchRuntimeSettings,
    saveRuntimeSettings,
    saveManagedAgent,
    saveAgentOrder,
    deleteManagedAgent
  } = useYalsClient();

  const { resolved: themeResolved, toggle: toggleTheme } = useTheme();

  const [controlPassword, setControlPassword] = useState('');
  const [controlError, setControlError] = useState<string | null>(null);
  const [controlMessage, setControlMessage] = useState<string | null>(null);
  const [editingAgent, setEditingAgent] = useState<AgentConfigPayload>(createEmptyAgent());
  const [editingRuntime, setEditingRuntime] = useState<RuntimeSettings>(runtimeSettings);
  const [controlView, setControlView] = useState<'agents' | 'settings' | 'monitoring'>('agents');
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingTargets, setEditingTargets] = useState<ProbeTarget[]>([]);
  const [editingInterval, setEditingInterval] = useState(60);
  const [copiedUuid, setCopiedUuid] = useState<string | null>(null);
  const [savingAgent, setSavingAgent] = useState(false);
  // Local mirror of managedAgents for optimistic drag-reordering of the table.
  const [localAgents, setLocalAgents] = useState<AgentConfigRecord[]>([]);
  const [dragIndex, setDragIndex] = useState<number | null>(null);
  const [dragOverIndex, setDragOverIndex] = useState<number | null>(null);

  useEffect(() => {
    setLocalAgents(managedAgents);
  }, [managedAgents]);

  // Single place that loads control-plane data once the session is
  // authenticated. editingRuntime is kept in sync from runtimeSettings by the
  // effect below, so it is intentionally not set here.
  useEffect(() => {
    if (!isControlAuthenticated) return;
    listManagedAgents().catch((error) => {
      console.error(error);
      setControlError('Failed to load agent records');
    });
    listPlugins().catch((error) => {
      console.error(error);
      setControlError('Failed to load plugin list');
    });
    // Best-effort: live online status for the table. Failure is non-fatal.
    fetchAgentStatuses().catch((error) => console.error(error));
    fetchProbeTargets()
      .then((cfg) => {
        setEditingTargets(cfg.targets || []);
        setEditingInterval(cfg.interval_sec || 60);
      })
      .catch((error) => console.error(error));
    fetchRuntimeSettings().catch((error) => {
      console.error(error);
      setControlError('Failed to load runtime settings');
    });
  }, [fetchAgentStatuses, fetchProbeTargets, fetchRuntimeSettings, isControlAuthenticated, listManagedAgents, listPlugins]);

  useEffect(() => {
    setEditingRuntime(runtimeSettings);
  }, [runtimeSettings]);

  const handleCopyInstall = async (uuid: string, token: string) => {
    if (await copyToClipboard(buildInstallCommand(uuid, token))) {
      setCopiedUuid(uuid);
      window.setTimeout(() => setCopiedUuid((cur) => (cur === uuid ? null : cur)), 2000);
    }
  };

  const handleControlLogin = async () => {
    try {
      setControlError(null);
      // loginControl flips isControlAuthenticated; the load effect above then
      // fetches the agents and runtime settings, so we don't fetch them here.
      await loginControl(controlPassword);
    } catch (error: unknown) {
      setControlError(getErrorMessage(error) || 'Control panel login failed');
    }
  };

  const handleSaveRuntime = async () => {
    try {
      setControlError(null);
      const saved = await saveRuntimeSettings(editingRuntime);
      setEditingRuntime(saved);
      setControlMessage('Runtime settings saved. Rate-limit changes apply immediately; gRPC keepalive changes take effect after a server restart.');
    } catch (error: unknown) {
      setControlError(getErrorMessage(error) || 'Failed to save runtime settings');
    }
  };

  const addTarget = () => setEditingTargets((prev) => [...prev, { ip: '', name: '', location: '', isp: '', protocol: 'ICMP', port: 0 }]);
  const removeTarget = (index: number) => setEditingTargets((prev) => prev.filter((_, i) => i !== index));
  const updateTarget = (index: number, patch: Partial<ProbeTarget>) =>
    setEditingTargets((prev) => prev.map((t, i) => (i === index ? { ...t, ...patch } : t)));

  const handleSaveTargets = async () => {
    const names = new Set<string>();
    for (const t of editingTargets) {
      const name = t.name.trim();
      if (!name) {
        setControlMessage(null);
        setControlError('Every probe target needs a name');
        return;
      }
      if (names.has(name)) {
        setControlMessage(null);
        setControlError(`Duplicate target name: ${name}`);
        return;
      }
      names.add(name);
      if (!t.ip.trim()) {
        setControlMessage(null);
        setControlError(`Target "${name}": IP is required`);
        return;
      }
      if (t.protocol.toUpperCase() === 'TCP' && (!t.port || t.port < 1 || t.port > 65535)) {
        setControlMessage(null);
        setControlError(`Target "${name}": TCP requires a port between 1 and 65535`);
        return;
      }
    }
    try {
      setControlError(null);
      const saved = await saveProbeTargets({ interval_sec: editingInterval, targets: editingTargets });
      setEditingTargets(saved.targets || []);
      setEditingInterval(saved.interval_sec || 60);
      setControlMessage('Probe targets saved and pushed to agents');
    } catch (error: unknown) {
      setControlError(getErrorMessage(error) || 'Failed to save probe targets');
    }
  };

  const updateCommand = (index: number, patch: Partial<AgentCommand>) => {
    setEditingAgent((prev) => {
      const commandsCopy = [...prev.commands];
      commandsCopy[index] = { ...commandsCopy[index], ...patch };
      return { ...prev, commands: commandsCopy };
    });
  };

  const toggleCommandMode = (index: number) => {
    setEditingAgent((prev) => {
      const commandsCopy = [...prev.commands];
      const current = commandsCopy[index];
      const nextMode = getCommandMode(current) === 'shell' ? 'plugin' : 'shell';
      // A shell template and a plugin name are not interchangeable, so switching
      // modes does not carry the value over. Entering plugin mode preselects the
      // first plugin and uses its name as the command name (no separate name field).
      const firstPlugin = availablePlugins[0]?.name || '';
      commandsCopy[index] = nextMode === 'plugin'
        ? { ...current, template: '', use_plugin: firstPlugin, name: firstPlugin }
        : { ...current, template: '', use_plugin: '' };
      return { ...prev, commands: commandsCopy };
    });
  };

  const updateCommandSourceValue = (index: number, value: string) => {
    setEditingAgent((prev) => {
      const commandsCopy = [...prev.commands];
      const current = commandsCopy[index];
      const mode = getCommandMode(current);
      commandsCopy[index] = {
        ...current,
        template: mode === 'shell' ? value : '',
        use_plugin: mode === 'plugin' ? value : '',
        // In plugin mode the command name is the plugin name.
        name: mode === 'plugin' ? value : current.name
      };
      return { ...prev, commands: commandsCopy };
    });
  };

  const addCommand = () => {
    setEditingAgent((prev) => ({
      ...prev,
      commands: [...prev.commands, { name: '', template: '', ignore_target: false, maxmium_queue: 0 }]
    }));
  };

  const removeCommand = (index: number) => {
    setEditingAgent((prev) => ({
      ...prev,
      commands: prev.commands.filter((_, idx) => idx !== index)
    }));
  };

  const moveCommand = (index: number, direction: -1 | 1) => {
    setEditingAgent((prev) => {
      const target = index + direction;
      if (target < 0 || target >= prev.commands.length) return prev;
      const commandsCopy = [...prev.commands];
      [commandsCopy[index], commandsCopy[target]] = [commandsCopy[target], commandsCopy[index]];
      return { ...prev, commands: commandsCopy };
    });
  };

  const startCreateAgent = () => {
    setControlMessage(null);
    setControlError(null);
    setEditingAgent(createEmptyAgent());
    setDrawerOpen(true);
  };

  const startEditAgent = (record: AgentConfigRecord) => {
    setControlMessage(null);
    setControlError(null);
    setEditingAgent({
      uuid: record.uuid,
      token: record.token,
      name: record.name,
      group: record.group,
      details: record.details,
      commands: record.commands.map((command) => ({
        ...command,
        maxmium_queue: command.maxmium_queue ?? 0
      }))
    });
    setDrawerOpen(true);
  };

  const closeDrawer = () => {
    setDrawerOpen(false);
    setControlMessage(null);
    setControlError(null);
  };

  const handleSaveAgent = async () => {
    if (savingAgent) return; // guard against double-submit from rapid clicks
    // Plugin commands use the plugin name as their command name (no separate
    // name field), so normalize before validating/sending.
    const payload: AgentConfigPayload = {
      ...editingAgent,
      commands: editingAgent.commands.map((c) =>
        getCommandMode(c) === 'plugin' ? { ...c, name: (c.use_plugin || '').trim() } : c
      )
    };
    const validationError = validateAgentForm(payload);
    if (validationError) {
      setControlMessage(null);
      setControlError(validationError);
      return;
    }
    setSavingAgent(true);
    try {
      setControlError(null);
      const saved = await saveManagedAgent(payload);
      setEditingAgent({
        uuid: saved.uuid,
        token: saved.token,
        name: saved.name,
        group: saved.group,
        details: saved.details,
        commands: saved.commands
      });
      setControlMessage('Agent saved. Use the Install Command below to deploy it.');
    } catch (error: unknown) {
      setControlError(getErrorMessage(error) || 'Failed to save agent');
    } finally {
      setSavingAgent(false);
    }
  };

  // Drag-and-drop reorder of the agents table (grab the ☰ handle). On drop the new
  // order is applied optimistically and persisted; the Status page follows it.
  const commitAgentReorder = (targetIndex: number) => {
    setDragOverIndex(null);
    if (dragIndex === null || dragIndex === targetIndex) {
      setDragIndex(null);
      return;
    }
    const next = [...localAgents];
    const [moved] = next.splice(dragIndex, 1);
    next.splice(targetIndex, 0, moved);
    setLocalAgents(next);
    setDragIndex(null);
    saveAgentOrder(next.map((a) => a.uuid)).catch((error) => setControlError(getErrorMessage(error)));
  };

  const handleDeleteAgent = async (uuid?: string) => {
    if (!uuid) return;
    try {
      setControlError(null);
      await deleteManagedAgent(uuid); // already refreshes the agent list
      setEditingAgent(createEmptyAgent());
      setDrawerOpen(false);
    } catch (error: unknown) {
      setControlError(getErrorMessage(error) || 'Failed to delete agent');
    }
  };

  if (!isControlAuthenticated) {
    return (
      <div className="app-container">
        <main className="main-content">
          <div className="container">
            <div className="u-surface shadow-sm border u-border rounded-md p-6 max-w-xl mx-auto mt-10">
              <div className="flex items-center gap-2 mb-4">
                <Shield className="w-5 h-5 u-text" />
                <h2 className="text-lg font-semibold u-text">Control Panel Login</h2>
              </div>
              <FieldLabel>Control password</FieldLabel>
              <input
                type="password"
                value={controlPassword}
                onChange={(event) => setControlPassword(event.target.value)}
                onKeyDown={(event) => event.key === 'Enter' && handleControlLogin()}
                className="command-target-input mb-3"
                placeholder="Enter control password"
              />
              {controlError && <div className="command-status error mb-3">{controlError}</div>}
              <button className="command-button primary" onClick={handleControlLogin}>
                <Shield className="w-4 h-4" /> Sign In
              </button>
            </div>
          </div>
        </main>
      </div>
    );
  }

  return (
    <div className="control-layout">
        <aside className="control-sidebar">
          <div className="control-sidebar-brand">
            <Server className="w-5 h-5" /> YALS Control
          </div>
          <nav className="control-nav">
            <button type="button" className={`control-nav-item ${controlView === 'agents' ? 'active' : ''}`} onClick={() => setControlView('agents')}>
              <Server className="w-4 h-4" /> Agents
            </button>
            <button type="button" className={`control-nav-item ${controlView === 'monitoring' ? 'active' : ''}`} onClick={() => setControlView('monitoring')}>
              <Activity className="w-4 h-4" /> Monitoring
            </button>
            <button type="button" className={`control-nav-item ${controlView === 'settings' ? 'active' : ''}`} onClick={() => setControlView('settings')}>
              <Settings className="w-4 h-4" /> Settings
            </button>
          </nav>
          <div className="control-sidebar-footer">
            <button type="button" className="control-nav-item" onClick={toggleTheme}>
              {themeResolved === 'dark' ? <><Sun className="w-4 h-4" /> Light mode</> : <><Moon className="w-4 h-4" /> Dark mode</>}
            </button>
            <a href="/" className="control-nav-item">
              <Home className="w-4 h-4" /> Looking Glass
            </a>
            <button type="button" className="control-nav-item" onClick={logoutControl}>
              <LogOut className="w-4 h-4" /> Logout
            </button>
          </div>
        </aside>

        <div className="control-content">
          <div className="control-topbar">
            <h2>{controlView === 'agents' ? 'Agents' : controlView === 'monitoring' ? 'Monitoring' : 'Runtime Settings'}</h2>
            {controlView === 'agents' && (
              <button className="command-button primary" onClick={startCreateAgent}>
                <Plus className="w-4 h-4" /> New Agent
              </button>
            )}
            {controlView === 'monitoring' && (
              <button className="command-button primary" onClick={addTarget}>
                <Plus className="w-4 h-4" /> Add Target
              </button>
            )}
          </div>

          <div className="control-body">
            {controlError && !drawerOpen && (
              <div className="command-status error" style={{ marginBottom: '1rem' }}>{controlError}</div>
            )}

            {controlView === 'agents' ? (
              <div className="control-table-wrap">
                <table className="control-table">
                  <thead>
                    <tr>
                      <th>Name</th>
                      <th>Group</th>
                      <th>Status</th>
                      <th>Commands</th>
                      <th>Updated</th>
                      <th aria-label="Actions"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {localAgents.map((record, index) => {
                      const online = nodeStatuses.get(record.name);
                      return (
                        <tr
                          key={record.uuid}
                          className={dragOverIndex === index ? 'control-row-dragover' : ''}
                          onDragOver={(e) => {
                            if (dragIndex === null) return;
                            e.preventDefault();
                            if (dragOverIndex !== index) setDragOverIndex(index);
                          }}
                          onDrop={(e) => { e.preventDefault(); commitAgentReorder(index); }}
                        >
                          <td className="font-medium u-text">{record.name}</td>
                          <td>{record.group}</td>
                          <td>
                            {online === undefined ? (
                              <span className="u-text-faint">—</span>
                            ) : (
                              <span className={`status-dot ${online ? 'online' : 'offline'}`}>{online ? 'Online' : 'Offline'}</span>
                            )}
                          </td>
                          <td>{record.commands.length}</td>
                          <td className="u-text-muted">{record.updated_at ? new Date(record.updated_at).toLocaleString() : '—'}</td>
                          <td>
                            <div className="control-row-actions">
                              <button
                                type="button"
                                className="control-drag-handle"
                                title="Drag to reorder"
                                aria-label="Drag to reorder"
                                draggable
                                onDragStart={(e) => { setDragIndex(index); e.dataTransfer.effectAllowed = 'move'; }}
                                onDragEnd={() => { setDragIndex(null); setDragOverIndex(null); }}
                              >
                                <Menu className="w-3.5 h-3.5" />
                              </button>
                              <button type="button" className="control-icon-button" onClick={() => handleCopyInstall(record.uuid, record.token)} title="Copy install command">
                                {copiedUuid === record.uuid
                                  ? <><Check className="w-3.5 h-3.5" /> Copied</>
                                  : <><Download className="w-3.5 h-3.5" /> Install</>}
                              </button>
                              <button type="button" className="control-icon-button" onClick={() => startEditAgent(record)}>
                                <Pencil className="w-3.5 h-3.5" /> Edit
                              </button>
                              <button type="button" className="control-icon-button danger" onClick={() => handleDeleteAgent(record.uuid)}>
                                <Trash2 className="w-3.5 h-3.5" /> Delete
                              </button>
                            </div>
                          </td>
                        </tr>
                      );
                    })}
                    {localAgents.length === 0 && (
                      <tr>
                        <td colSpan={6} className="control-table-empty">No registered agents yet. Click “New Agent” to add one.</td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            ) : controlView === 'monitoring' ? (
              <div className="space-y-4">
                <div className="u-surface shadow-sm border u-border p-4 rounded-md max-w-xs">
                  <FieldLabel>Probe interval (seconds)</FieldLabel>
                  <input
                    className="command-target-input"
                    type="number"
                    min="5"
                    value={editingInterval}
                    onChange={(e) => setEditingInterval(Number(e.target.value) || 60)}
                  />
                  <p className="text-xs u-text-muted mt-1">How often each agent probes every target (ICMP ping or TCP connect).</p>
                </div>

                <div className="control-table-wrap">
                  <table className="control-table">
                    <thead>
                      <tr>
                        <th>Name</th>
                        <th>IP</th>
                        <th>Location</th>
                        <th>ISP</th>
                        <th>Protocol</th>
                        <th>Port</th>
                        <th aria-label="Actions"></th>
                      </tr>
                    </thead>
                    <tbody>
                      {editingTargets.map((t, index) => {
                        const isTCP = t.protocol.toUpperCase() === 'TCP';
                        return (
                        <tr key={index}>
                          <td><input className="command-target-input" placeholder="Name" value={t.name} onChange={(e) => updateTarget(index, { name: e.target.value })} /></td>
                          <td><input className="command-target-input" placeholder="IP" value={t.ip} onChange={(e) => updateTarget(index, { ip: e.target.value })} /></td>
                          <td><input className="command-target-input" placeholder="Location" value={t.location} onChange={(e) => updateTarget(index, { location: e.target.value })} /></td>
                          <td><input className="command-target-input" placeholder="ISP" value={t.isp} onChange={(e) => updateTarget(index, { isp: e.target.value })} /></td>
                          <td>
                            <select
                              className="command-select w-full"
                              value={isTCP ? 'TCP' : 'ICMP'}
                              onChange={(e) => updateTarget(index, e.target.value === 'TCP' && (!t.port || t.port < 1)
                                ? { protocol: 'TCP', port: 443 }
                                : { protocol: e.target.value })}
                            >
                              <option value="ICMP">ICMP</option>
                              <option value="TCP">TCP</option>
                            </select>
                          </td>
                          <td>
                            <input
                              className="command-target-input"
                              type="number"
                              min="1"
                              max="65535"
                              placeholder="Port"
                              disabled={!isTCP}
                              value={isTCP ? (t.port || '') : ''}
                              onChange={(e) => updateTarget(index, { port: Number(e.target.value) || 0 })}
                            />
                          </td>
                          <td>
                            <div className="control-row-actions">
                              <button type="button" className="control-icon-button danger" onClick={() => removeTarget(index)}>
                                <Trash2 className="w-3.5 h-3.5" /> Remove
                              </button>
                            </div>
                          </td>
                        </tr>
                        );
                      })}
                      {editingTargets.length === 0 && (
                        <tr>
                          <td colSpan={7} className="control-table-empty">No targets yet. Click “Add Target” to add one.</td>
                        </tr>
                      )}
                    </tbody>
                  </table>
                </div>

                {controlError && <div className="command-status error">{controlError}</div>}
                {controlMessage && <div className="command-status success">{controlMessage}</div>}

                <div>
                  <button className="command-button primary" onClick={handleSaveTargets}>
                    <Save className="w-4 h-4" /> Save &amp; Push to Agents
                  </button>
                </div>
              </div>
            ) : (
              <div className="u-surface shadow-sm border u-border p-4 rounded-md space-y-4 max-w-3xl">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  <div>
                    <FieldLabel>Server Keepalive Ping Interval</FieldLabel>
                    <input className="command-target-input" type="number" placeholder="30" value={editingRuntime.grpc.ping_interval} onChange={(e) => setEditingRuntime({ ...editingRuntime, grpc: { ...editingRuntime.grpc, ping_interval: Number(e.target.value) } })} />
                  </div>
                  <div>
                    <FieldLabel>Server Keepalive Timeout</FieldLabel>
                    <input className="command-target-input" type="number" placeholder="60" value={editingRuntime.grpc.pong_wait} onChange={(e) => setEditingRuntime({ ...editingRuntime, grpc: { ...editingRuntime.grpc, pong_wait: Number(e.target.value) } })} />
                  </div>
                  <div>
                    <FieldLabel>Rate Limit Max Commands</FieldLabel>
                    <input className="command-target-input" type="number" placeholder="10" value={editingRuntime.rate_limit.max_commands} onChange={(e) => setEditingRuntime({ ...editingRuntime, rate_limit: { ...editingRuntime.rate_limit, max_commands: Number(e.target.value) } })} />
                  </div>
                  <div>
                    <FieldLabel>Rate Limit Window Seconds</FieldLabel>
                    <input className="command-target-input" type="number" placeholder="60" value={editingRuntime.rate_limit.time_window} onChange={(e) => setEditingRuntime({ ...editingRuntime, rate_limit: { ...editingRuntime.rate_limit, time_window: Number(e.target.value) } })} />
                  </div>
                </div>
                <p className="text-xs u-text-muted">
                  Rate-limit changes apply immediately. gRPC keepalive changes are saved but only take effect after a server restart.
                </p>
                <label className="text-sm u-text flex items-center gap-2">
                  <input type="checkbox" checked={editingRuntime.rate_limit.enabled} onChange={(e) => setEditingRuntime({ ...editingRuntime, rate_limit: { ...editingRuntime.rate_limit, enabled: e.target.checked } })} />
                  Enable Rate Limiting
                </label>
                {controlMessage && <div className="command-status success">{controlMessage}</div>}
                <div>
                  <button className="command-button primary" onClick={handleSaveRuntime}>
                    <Save className="w-4 h-4" /> Save Runtime Settings
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>

        {drawerOpen && (
          <>
            <div className="control-drawer-backdrop" onClick={closeDrawer} />
            <div className="control-drawer">
              <div className="control-drawer-header">
                <h3>{editingAgent.uuid ? 'Edit Agent' : 'New Agent'}</h3>
                <button type="button" className="control-drawer-close" onClick={closeDrawer} aria-label="Close">
                  <X className="w-5 h-5" />
                </button>
              </div>
              <div className="control-drawer-body">

                    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                      <div>
                        <FieldLabel>Agent Name</FieldLabel>
                        <input className="command-target-input" placeholder="Agent name" value={editingAgent.name} onChange={(e) => setEditingAgent((prev) => ({ ...prev, name: e.target.value }))} />
                      </div>
                      <div>
                        <FieldLabel>Group Name</FieldLabel>
                        <input className="command-target-input" placeholder="Group" value={editingAgent.group} onChange={(e) => setEditingAgent((prev) => ({ ...prev, group: e.target.value }))} />
                      </div>
                      <div>
                        <FieldLabel>Location</FieldLabel>
                        <input className="command-target-input" placeholder="Location" value={editingAgent.details.location} onChange={(e) => setEditingAgent((prev) => ({ ...prev, details: { ...prev.details, location: e.target.value } }))} />
                      </div>
                      <div>
                        <FieldLabel>Datacenter</FieldLabel>
                        <input className="command-target-input" placeholder="Datacenter" value={editingAgent.details.datacenter} onChange={(e) => setEditingAgent((prev) => ({ ...prev, details: { ...prev.details, datacenter: e.target.value } }))} />
                      </div>
                      <div>
                        <FieldLabel>Test IP</FieldLabel>
                        <input className="command-target-input" placeholder="Test IP" value={editingAgent.details.test_ip} onChange={(e) => setEditingAgent((prev) => ({ ...prev, details: { ...prev.details, test_ip: e.target.value } }))} />
                      </div>
                      <div>
                        <FieldLabel>Description</FieldLabel>
                        <input className="command-target-input" placeholder="Description" value={editingAgent.details.description} onChange={(e) => setEditingAgent((prev) => ({ ...prev, details: { ...prev.details, description: e.target.value } }))} />
                      </div>
                    </div>

                    <div className="space-y-2">
                      <div className="flex items-center justify-between">
                        <h3 className="text-sm font-semibold u-text">Command Templates & Plugins</h3>
                        <button className="command-button primary" onClick={addCommand}>
                          <Plus className="w-4 h-4" /> Add Command
                        </button>
                      </div>
                      {editingAgent.commands.map((command, index) => {
                        const mode = getCommandMode(command);
                        const queueEnabled = (command.maxmium_queue ?? 0) > 0;
                        const selectedPlugin = mode === 'plugin'
                          ? availablePlugins.find((p) => p.name === command.use_plugin)
                          : undefined;
                        const ignoreTargetForced = mode === 'plugin' && selectedPlugin?.ignore_target_overridden === true;
                        const queueForced = mode === 'plugin' && selectedPlugin?.maximum_queue_overridden === true;
                        const ignoreTargetChecked = ignoreTargetForced ? selectedPlugin!.ignore_target : (command.ignore_target || false);
                        return (
                          <div key={index} className="command-edit-row">
                            <div className="command-edit-reorder">
                              <button type="button" className="command-edit-move" disabled={index === 0} onClick={() => moveCommand(index, -1)} title="Move up">
                                <ChevronUp className="w-3.5 h-3.5" />
                              </button>
                              <button type="button" className="command-edit-move" disabled={index === editingAgent.commands.length - 1} onClick={() => moveCommand(index, 1)} title="Move down">
                                <ChevronDown className="w-3.5 h-3.5" />
                              </button>
                            </div>
                            <button type="button" className="command-edit-mode" onClick={() => toggleCommandMode(index)} title="Toggle shell / plugin mode">
                              {mode === 'shell' ? 'Shell' : 'Plugin'}
                            </button>
                            {mode === 'shell' ? (
                              <>
                                <input className="command-target-input command-edit-name" placeholder="Name" value={command.name} onChange={(e) => updateCommand(index, { name: e.target.value })} />
                                <input className="command-target-input command-edit-source" placeholder="Template, e.g. ping -c 4 {target}" value={command.template || ''} onChange={(e) => updateCommandSourceValue(index, e.target.value)} />
                              </>
                            ) : (
                              <select className="command-select command-edit-source" value={command.use_plugin || ''} title={selectedPlugin?.description || 'Select a plugin'} onChange={(e) => updateCommandSourceValue(index, e.target.value)}>
                                <option value="" disabled>Select a plugin</option>
                                {availablePlugins.map((p) => (
                                  <option key={p.name} value={p.name}>{p.name}</option>
                                ))}
                              </select>
                            )}
                            {queueForced ? (
                              <span className="command-edit-queue-forced" title="Concurrency set by plugin">
                                {selectedPlugin!.maximum_queue > 0 ? `q:${selectedPlugin!.maximum_queue}` : 'q:∞'}
                              </span>
                            ) : (
                              <label className="command-edit-queue" title="Max concurrent runs">
                                <input type="checkbox" checked={queueEnabled} onChange={(e) => updateCommand(index, { maxmium_queue: e.target.checked ? Math.max(command.maxmium_queue ?? 0, 1) : 0 })} />
                                <input className="command-target-input command-edit-queue-num" type="number" min="1" placeholder="Concurrency" disabled={!queueEnabled} value={queueEnabled ? String(command.maxmium_queue ?? 1) : ''} onChange={(e) => updateCommand(index, { maxmium_queue: Number(e.target.value) || 1 })} />
                              </label>
                            )}
                            <label className="command-edit-ignore" title={`Ignore target input${ignoreTargetForced ? ' (set by plugin)' : ''}`}>
                              <input type="checkbox" checked={ignoreTargetChecked} disabled={ignoreTargetForced} onChange={(e) => updateCommand(index, { ignore_target: e.target.checked })} />
                              Ignore target
                            </label>
                            <button type="button" className="control-icon-button danger command-edit-remove" onClick={() => removeCommand(index)} title="Remove command">
                              <Trash2 className="w-3.5 h-3.5" />
                            </button>
                          </div>
                        );
                      })}
                    </div>

                    {controlError && <div className="command-status error">{controlError}</div>}
                    {controlMessage && <div className="command-status success">{controlMessage}</div>}

                    <div className="flex flex-wrap gap-3">
                      <button className="command-button primary" onClick={handleSaveAgent} disabled={savingAgent}>
                        <Save className="w-4 h-4" /> {savingAgent ? 'Saving…' : (editingAgent.uuid ? 'Save Agent' : 'Create Agent')}
                      </button>
                      {editingAgent.uuid && (
                        <button className="command-button danger" onClick={() => handleDeleteAgent(editingAgent.uuid)}>
                          <Trash2 className="w-4 h-4" /> Delete Agent
                        </button>
                      )}
                    </div>

                    {editingAgent.uuid && editingAgent.token && (
                      <div className="rounded-md u-bg-subtle border u-border p-4 text-sm u-text space-y-2">
                        <div className="flex items-center justify-between gap-2">
                          <p className="font-semibold">Install Command</p>
                          <button type="button" className="control-icon-button" onClick={() => handleCopyInstall(editingAgent.uuid!, editingAgent.token)} title="Copy install command">
                            {copiedUuid === editingAgent.uuid
                              ? <><Check className="w-3.5 h-3.5" /> Copied</>
                              : <><Copy className="w-3.5 h-3.5" /> Copy</>}
                          </button>
                        </div>
                        <code className="block break-all">{buildInstallCommand(editingAgent.uuid, editingAgent.token)}</code>
                        <p className="text-xs u-text-muted">
                          Run this on the agent host. It pulls and builds the agent, then registers it as a systemd service. The agent trusts the server's built-in certificate directly, and also accepts a real CA-trusted certificate when the server is reached through a TLS-terminating reverse proxy / CDN.
                        </p>
                      </div>
                    )}
              </div>
            </div>
          </>
        )}
      </div>
    );
}
