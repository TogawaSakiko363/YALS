import { useEffect, useState } from 'react';
import { Plus, Save, Trash2, Shield, Server, KeyRound, Settings, RefreshCw, ChevronUp, ChevronDown, LogOut, Pencil, X, Activity, Home } from 'lucide-react';
import { CustomConfig } from '../hooks/useCustomConfig';
import { useYalsClient } from '../hooks/useYalsClient';
import { AgentCommand, AgentConfigPayload, AgentConfigRecord, RuntimeSettings, ProbeTarget } from '../types/yals';
import { getErrorMessage } from '../utils/error';

interface ControlPanelProps {
  config: CustomConfig;
}

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
  return <label className="block text-sm font-medium text-gray-700 mb-1 text-left">{children}</label>;
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
    const name = command.name.trim();
    if (!name) return `Command #${i + 1}: name is required`;
    if (seen.has(name)) return `Duplicate command name: ${name}`;
    seen.add(name);

    if (getCommandMode(command) === 'shell') {
      const template = (command.template || '').trim();
      if (!template) return `Command "${name}": template is required`;
      if (command.ignore_target && template.includes('{target}')) {
        return `Command "${name}": template uses {target} but "Ignore Target Input" is enabled`;
      }
    } else if (!(command.use_plugin || '').trim()) {
      return `Command "${name}": select a plugin`;
    }
  }
  return null;
}

export function ControlPanel({ config }: ControlPanelProps) {
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
    deleteManagedAgent
  } = useYalsClient();

  const [controlPassword, setControlPassword] = useState('');
  const [controlError, setControlError] = useState<string | null>(null);
  const [controlMessage, setControlMessage] = useState<string | null>(null);
  const [editingAgent, setEditingAgent] = useState<AgentConfigPayload>(createEmptyAgent());
  const [editingRuntime, setEditingRuntime] = useState<RuntimeSettings>(runtimeSettings);
  const [controlView, setControlView] = useState<'agents' | 'settings' | 'monitoring'>('agents');
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editingTargets, setEditingTargets] = useState<ProbeTarget[]>([]);
  const [editingInterval, setEditingInterval] = useState(60);

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

  const generateRandomToken = () => {
    // This token becomes the agent's authentication secret, so it must be
    // generated with a CSPRNG rather than the predictable Math.random().
    const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789';
    const randomValues = new Uint32Array(24);
    crypto.getRandomValues(randomValues);
    let result = '';
    for (let i = 0; i < 24; i += 1) {
      result += chars.charAt(randomValues[i] % chars.length);
    }
    setEditingAgent((prev) => ({ ...prev, token: result }));
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
      // first available plugin (the mode is inferred from use_plugin being set).
      commandsCopy[index] = nextMode === 'plugin'
        ? { ...current, template: '', use_plugin: availablePlugins[0]?.name || '' }
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
        use_plugin: mode === 'plugin' ? value : ''
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
    const validationError = validateAgentForm(editingAgent);
    if (validationError) {
      setControlMessage(null);
      setControlError(validationError);
      return;
    }
    try {
      setControlError(null);
      const saved = await saveManagedAgent(editingAgent);
      setEditingAgent({
        uuid: saved.uuid,
        token: saved.token,
        name: saved.name,
        group: saved.group,
        details: saved.details,
        commands: saved.commands
      });
      setControlMessage(`Agent saved. UUID: ${saved.uuid}, Token: ${saved.token}`);
    } catch (error: unknown) {
      setControlError(getErrorMessage(error) || 'Failed to save agent');
    }
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
      <div className="app-container" style={{ backgroundColor: config.backgroundColor }}>
        <main className="main-content">
          <div className="container">
            <div className="bg-white shadow-sm border border-gray-200 rounded-md p-6 max-w-xl mx-auto mt-10">
              <div className="flex items-center gap-2 mb-4">
                <Shield className="w-5 h-5 text-gray-700" />
                <h2 className="text-lg font-semibold text-gray-900">Control Panel Login</h2>
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
                    {managedAgents.map((record) => {
                      const online = nodeStatuses.get(record.name);
                      return (
                        <tr key={record.uuid}>
                          <td className="font-medium text-gray-900">{record.name}</td>
                          <td>{record.group}</td>
                          <td>
                            {online === undefined ? (
                              <span className="text-gray-400">—</span>
                            ) : (
                              <span className={`status-dot ${online ? 'online' : 'offline'}`}>{online ? 'Online' : 'Offline'}</span>
                            )}
                          </td>
                          <td>{record.commands.length}</td>
                          <td className="text-gray-500">{record.updated_at ? new Date(record.updated_at).toLocaleString() : '—'}</td>
                          <td>
                            <div className="control-row-actions">
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
                    {managedAgents.length === 0 && (
                      <tr>
                        <td colSpan={6} className="control-table-empty">No registered agents yet. Click “New Agent” to add one.</td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            ) : controlView === 'monitoring' ? (
              <div className="space-y-4">
                <div className="bg-white shadow-sm border border-gray-200 p-4 rounded-md max-w-xs">
                  <FieldLabel>Probe interval (seconds)</FieldLabel>
                  <input
                    className="command-target-input"
                    type="number"
                    min="5"
                    value={editingInterval}
                    onChange={(e) => setEditingInterval(Number(e.target.value) || 60)}
                  />
                  <p className="text-xs text-gray-500 mt-1">How often each agent probes every target (ICMP ping or TCP connect).</p>
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
              <div className="bg-white shadow-sm border border-gray-200 p-4 rounded-md space-y-4 max-w-3xl">
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
                <p className="text-xs text-gray-500">
                  Rate-limit changes apply immediately. gRPC keepalive changes are saved but only take effect after a server restart.
                </p>
                <label className="text-sm text-gray-700 flex items-center gap-2">
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

                    <div className="space-y-3">
                      <div className="flex items-center justify-between">
                        <h3 className="text-sm font-semibold text-gray-900">Command Templates & Plugins</h3>
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
                          <div key={index} className="border border-gray-200 rounded-md p-3 space-y-3">
                            <div className="flex items-center justify-between">
                              <span className="text-xs font-medium text-gray-500">Command #{index + 1}</span>
                              <div className="flex items-center gap-1">
                                <button
                                  type="button"
                                  className="p-1 text-gray-500 hover:text-gray-900 disabled:opacity-30 disabled:cursor-not-allowed"
                                  disabled={index === 0}
                                  onClick={() => moveCommand(index, -1)}
                                  title="Move up"
                                >
                                  <ChevronUp className="w-4 h-4" />
                                </button>
                                <button
                                  type="button"
                                  className="p-1 text-gray-500 hover:text-gray-900 disabled:opacity-30 disabled:cursor-not-allowed"
                                  disabled={index === editingAgent.commands.length - 1}
                                  onClick={() => moveCommand(index, 1)}
                                  title="Move down"
                                >
                                  <ChevronDown className="w-4 h-4" />
                                </button>
                              </div>
                            </div>
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                              <div>
                                <FieldLabel>Command Name</FieldLabel>
                                <input className="command-target-input" placeholder="Command name" value={command.name} onChange={(e) => updateCommand(index, { name: e.target.value })} />
                              </div>
                              <div>
                                <FieldLabel>{mode === 'shell' ? 'Template' : 'Plugin'}</FieldLabel>
                                {mode === 'shell' ? (
                                  <input
                                    className="command-target-input"
                                    placeholder="e.g. ping -c 4 {target}"
                                    value={command.template || ''}
                                    onChange={(e) => updateCommandSourceValue(index, e.target.value)}
                                  />
                                ) : (
                                  <select
                                    className="command-select w-full"
                                    value={command.use_plugin || ''}
                                    onChange={(e) => updateCommandSourceValue(index, e.target.value)}
                                  >
                                    <option value="" disabled>Select a plugin</option>
                                    {availablePlugins.map((p) => (
                                      <option key={p.name} value={p.name}>{p.name}</option>
                                    ))}
                                  </select>
                                )}
                                {mode === 'shell' && (
                                  <p className="text-xs text-gray-500 mt-1">
                                    Use <code>{'{target}'}</code> to place the target; if omitted it is appended at the end.
                                  </p>
                                )}
                                {mode === 'plugin' && selectedPlugin?.description && (
                                  <p className="text-xs text-gray-500 mt-1">{selectedPlugin.description}</p>
                                )}
                              </div>
                              <div>
                                <FieldLabel>Mode</FieldLabel>
                                <button
                                  type="button"
                                  className="command-button primary"
                                  onClick={() => toggleCommandMode(index)}
                                >
                                  {mode === 'shell' ? 'Mode: Shell' : 'Mode: Plugin'}
                                </button>
                              </div>
                              <div>
                                <FieldLabel>Maximum Queue</FieldLabel>
                                {queueForced ? (
                                  <p className="text-sm text-gray-600">
                                    {selectedPlugin!.maximum_queue > 0
                                      ? `${selectedPlugin!.maximum_queue} (set by plugin)`
                                      : 'Unlimited (set by plugin)'}
                                  </p>
                                ) : (
                                  <div className="flex items-center gap-2">
                                    <label className="text-sm text-gray-700 flex items-center gap-2 whitespace-nowrap">
                                      <input
                                        type="checkbox"
                                        checked={queueEnabled}
                                        onChange={(e) => updateCommand(index, { maxmium_queue: e.target.checked ? Math.max(command.maxmium_queue ?? 0, 1) : 0 })}
                                      />
                                      Enabled
                                    </label>
                                    <input
                                      className="command-target-input"
                                      type="number"
                                      min="1"
                                      placeholder="Concurrency"
                                      disabled={!queueEnabled}
                                      value={queueEnabled ? String(command.maxmium_queue ?? 1) : ''}
                                      onChange={(e) => updateCommand(index, { maxmium_queue: Number(e.target.value) || 1 })}
                                    />
                                  </div>
                                )}
                              </div>
                            </div>
                            <div className="flex items-center justify-between">
                              <label className="text-sm text-gray-700 flex items-center gap-2">
                                <input
                                  type="checkbox"
                                  checked={ignoreTargetChecked}
                                  disabled={ignoreTargetForced}
                                  onChange={(e) => updateCommand(index, { ignore_target: e.target.checked })}
                                />
                                Ignore Target Input{ignoreTargetForced ? ' (set by plugin)' : ''}
                              </label>
                              <button className="command-button danger" onClick={() => removeCommand(index)}>
                                <Trash2 className="w-4 h-4" /> Remove Command
                              </button>
                            </div>
                          </div>
                        );
                      })}
                    </div>

                    <div className="rounded-md border border-gray-200 p-4 bg-gray-50">
                      <div className="flex items-center gap-2 mb-2 text-sm font-semibold text-gray-900">
                        <KeyRound className="w-4 h-4" /> Agent Token
                      </div>
                      <FieldLabel>Authentication Token</FieldLabel>
                      <div className="flex gap-2 items-center">
                        <input
                          className="command-target-input"
                          placeholder="Authentication token"
                          value={editingAgent.token}
                          onChange={(e) => setEditingAgent((prev) => ({ ...prev, token: e.target.value }))}
                        />
                        <button className="command-button primary" type="button" onClick={generateRandomToken}>
                          <RefreshCw className="w-4 h-4" /> Generate 24-Character Token
                        </button>
                      </div>
                      <p className="text-xs text-gray-500 mt-2">UUID is used for mapping and identification. Token is used for agent authentication.</p>
                    </div>

                    {controlError && <div className="command-status error">{controlError}</div>}
                    {controlMessage && <div className="command-status success">{controlMessage}</div>}

                    <div className="flex flex-wrap gap-3">
                      <button className="command-button primary" onClick={handleSaveAgent}>
                        <Save className="w-4 h-4" /> Save Agent
                      </button>
                      {editingAgent.uuid && (
                        <button className="command-button danger" onClick={() => handleDeleteAgent(editingAgent.uuid)}>
                          <Trash2 className="w-4 h-4" /> Delete Agent
                        </button>
                      )}
                    </div>

                    <div className="rounded-md bg-gray-50 border border-gray-200 p-4 text-sm text-gray-700 space-y-2">
                      <p className="font-semibold">Install Command Example</p>
                      <code>yals_agent -s {window.location.hostname || 'example.com'} -p {window.location.port || '443'} -u {editingAgent.uuid || '[UUID after save]'} -t {editingAgent.token || '[Token]'}</code>
                      <p className="text-xs text-gray-500">
                        The agent verifies the server using the built-in certificate that both ship with — no extra TLS parameters are needed.
                      </p>
                    </div>
              </div>
            </div>
          </>
        )}
      </div>
    );
}
