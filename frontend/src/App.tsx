import { useMemo, useState } from 'react';
import { Github, Plus, Save, Trash2, Shield, Server, KeyRound, Settings, RefreshCw } from 'lucide-react';
import { AgentSelector } from './components/AgentSelector';
import { CommandPanel } from './components/CommandPanel';
import { CustomConfig } from './hooks/useCustomConfig';
import { useYalsClient } from './hooks/useYalsClient';
import { AgentCommand, AgentConfigPayload, AgentConfigRecord, CommandType, IPVersion, RuntimeSettings } from './types/yals';

interface AppProps {
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

function getCommandValue(command: AgentCommand): string {
  return command.use_plugin || command.template || '';
}

function App({ config }: AppProps) {
  const {
    isConnected,
    isConnecting,
    groups,
    selectedAgent,
    activeCommands,
    streamingOutputs,
    appConfig,
    commands,
    connect,
    executeCommand,
    setSelectedAgent,
    clearAllStreamingOutputs,
    stopCommand,
    isControlAuthenticated,
    managedAgents,
    runtimeSettings,
    loginControl,
    listManagedAgents,
    fetchRuntimeSettings,
    saveRuntimeSettings,
    saveManagedAgent,
    deleteManagedAgent
  } = useYalsClient();

  const isControlPage = window.location.pathname === '/control.html' || window.location.pathname === '/control';
  const isCommandRunning = activeCommands.size > 0;
  const [latestOutput, setLatestOutput] = useState<string | null>(null);
  const [controlPassword, setControlPassword] = useState('');
  const [controlError, setControlError] = useState<string | null>(null);
  const [controlMessage, setControlMessage] = useState<string | null>(null);
  const [editingAgent, setEditingAgent] = useState<AgentConfigPayload>(createEmptyAgent());
  const [editingRuntime, setEditingRuntime] = useState<RuntimeSettings>(runtimeSettings);

  useMemo(() => {
    if (!isControlPage && !isConnected && !isConnecting) {
      connect();
    }
    return null;
  }, [connect, isConnected, isConnecting, isControlPage]);

  useMemo(() => {
    if (isControlPage && isControlAuthenticated) {
      listManagedAgents().catch((error) => {
        console.error(error);
        setControlError('Failed to load agent records');
      });
      fetchRuntimeSettings()
        .then((settings) => setEditingRuntime(settings))
        .catch((error) => {
          console.error(error);
          setControlError('Failed to load runtime settings');
        });
    }
    return null;
  }, [fetchRuntimeSettings, isControlAuthenticated, isControlPage, listManagedAgents]);

  useMemo(() => {
    setEditingRuntime(runtimeSettings);
    return null;
  }, [runtimeSettings]);

  const generateRandomToken = () => {
    const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789';
    let result = '';
    for (let i = 0; i < 24; i += 1) {
      result += chars.charAt(Math.floor(Math.random() * chars.length));
    }
    setEditingAgent((prev) => ({ ...prev, token: result }));
  };

  const handleExecuteCommand = async (command: CommandType, target: string, ipVersion: IPVersion) => {
    try {
      setLatestOutput(null);
      clearAllStreamingOutputs();
      const { response } = await executeCommand(command, target, ipVersion);
      setLatestOutput(response.output || '');
    } catch (error: any) {
      console.error('Command execution failed:', error);
      setLatestOutput(error.message || 'Command execution failed');
    }
  };

  const handleStopCommand = () => {
    if (activeCommands.size > 0) {
      const firstActiveCommand = Array.from(activeCommands)[0];
      const currentOutput = streamingOutputs.get(firstActiveCommand) || '';
      stopCommand(firstActiveCommand);
      if (currentOutput) {
        setLatestOutput(`${currentOutput}\n\n*** Command Stopped ***`);
      }
    }
  };

  const handleControlLogin = async () => {
    try {
      setControlError(null);
      await loginControl(controlPassword);
      await listManagedAgents();
      const settings = await fetchRuntimeSettings();
      setEditingRuntime(settings);
    } catch (error: any) {
      setControlError(error.message || 'Control panel login failed');
    }
  };

  const handleSaveRuntime = async () => {
    try {
      setControlError(null);
      const saved = await saveRuntimeSettings(editingRuntime);
      setEditingRuntime(saved);
      setControlMessage('Runtime settings were saved and hot reloaded');
    } catch (error: any) {
      setControlError(error.message || 'Failed to save runtime settings');
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
      const currentValue = getCommandValue(current);
      const nextMode = getCommandMode(current) === 'shell' ? 'plugin' : 'shell';
      commandsCopy[index] = {
        ...current,
        template: nextMode === 'shell' ? currentValue : '',
        use_plugin: nextMode === 'plugin' ? currentValue : ''
      };
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

  const startCreateAgent = () => {
    setControlMessage(null);
    setControlError(null);
    setEditingAgent(createEmptyAgent());
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
  };

  const handleSaveAgent = async () => {
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
    } catch (error: any) {
      setControlError(error.message || 'Failed to save agent');
    }
  };

  const handleDeleteAgent = async (uuid?: string) => {
    if (!uuid) return;
    try {
      setControlError(null);
      await deleteManagedAgent(uuid);
      setControlMessage('Agent deleted');
      setEditingAgent(createEmptyAgent());
      await listManagedAgents();
    } catch (error: any) {
      setControlError(error.message || 'Failed to delete agent');
    }
  };

  if (isControlPage) {
    return (
      <div className="app-container" style={{ backgroundColor: config.backgroundColor }}>
        <header className="app-header">
          <div className="container max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 w-full">
            <div className="header-content">
              <div className="header-left">
                <div className="logo-container">
                  <img src={config.logoPath} alt="Logo" className="logo-image" />
                </div>
                <div className="app-title">
                  <h1 className="title-large">Looking Glass Control</h1>
                </div>
              </div>
            </div>
          </div>
        </header>

        <main className="main-content">
          <div className="container">
            {!isControlAuthenticated ? (
              <div className="bg-white shadow-sm border border-gray-200 rounded-md p-6 max-w-xl mx-auto">
                <div className="flex items-center gap-2 mb-4">
                  <Shield className="w-5 h-5 text-gray-700" />
                  <h2 className="text-lg font-semibold text-gray-900">Control Panel Login</h2>
                </div>
                <p className="text-sm text-gray-600 mb-4">Use the value from [`server.password`](config.yaml:7) in [`config.yaml`](config.yaml).</p>
                <FieldLabel>Control password</FieldLabel>
                <input
                  type="password"
                  value={controlPassword}
                  onChange={(event) => setControlPassword(event.target.value)}
                  className="command-target-input mb-3"
                  placeholder="Enter control password"
                />
                {controlError && <div className="command-status error mb-3">{controlError}</div>}
                <button className="command-button primary" onClick={handleControlLogin}>
                  <Shield className="w-4 h-4" /> Sign In
                </button>
              </div>
            ) : (
              <div className="space-y-4">
                <div className="bg-white shadow-sm border border-gray-200 p-4 rounded-md space-y-4">
                  <div className="flex items-center gap-2">
                    <Settings className="w-4 h-4 text-gray-600" />
                    <h2 className="text-base font-semibold text-gray-900">Runtime Settings</h2>
                  </div>
                  <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-3">
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
                  <label className="text-sm text-gray-700 flex items-center gap-2">
                    <input type="checkbox" checked={editingRuntime.rate_limit.enabled} onChange={(e) => setEditingRuntime({ ...editingRuntime, rate_limit: { ...editingRuntime.rate_limit, enabled: e.target.checked } })} />
                    Enable Rate Limiting
                  </label>
                  <div>
                    <button className="command-button primary" onClick={handleSaveRuntime}>
                      <Save className="w-4 h-4" /> Save Runtime Settings
                    </button>
                  </div>
                </div>

                <div className="grid-container" style={{ gridTemplateColumns: '1.15fr 1.85fr' }}>
                  <div className="bg-white shadow-sm border border-gray-200 p-4 rounded-md">
                    <div className="flex items-center justify-between mb-3">
                      <div className="flex items-center gap-2">
                        <Server className="w-4 h-4 text-gray-600" />
                        <h2 className="text-base font-semibold text-gray-900">Registered Agents</h2>
                      </div>
                      <button className="command-button primary" onClick={startCreateAgent}>
                        <Plus className="w-4 h-4" /> New
                      </button>
                    </div>
                    <div className="space-y-2">
                      {managedAgents.map((record) => (
                        <button
                          key={record.uuid}
                          type="button"
                          className={`agent-item-container online ${editingAgent.uuid === record.uuid ? 'selected' : ''}`}
                          onClick={() => startEditAgent(record)}
                        >
                          <div className="agent-item-header">
                            <div className="min-w-0 flex-1 text-left">
                              <h3 className="text-sm font-medium truncate text-gray-900">{record.name}</h3>
                              <p className="agent-details-text">{record.group} · UUID: {record.uuid}</p>
                            </div>
                          </div>
                        </button>
                      ))}
                      {managedAgents.length === 0 && <p className="text-sm text-gray-500">No registered agents</p>}
                    </div>
                  </div>

                  <div className="bg-white shadow-sm border border-gray-200 p-4 rounded-md space-y-4">
                    <div className="flex items-center justify-between">
                      <h2 className="text-base font-semibold text-gray-900">Agent Configuration</h2>
                      {editingAgent.uuid && <span className="text-xs text-gray-500">UUID: {editingAgent.uuid}</span>}
                    </div>

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
                        return (
                          <div key={index} className="border border-gray-200 rounded-md p-3 space-y-3">
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                              <div>
                                <FieldLabel>Command Name</FieldLabel>
                                <input className="command-target-input" placeholder="Command name" value={command.name} onChange={(e) => updateCommand(index, { name: e.target.value })} />
                              </div>
                              <div>
                                <FieldLabel>Template</FieldLabel>
                                <input
                                  className="command-target-input"
                                  placeholder={mode === 'shell' ? 'Template, for example ping -c 4' : 'Plugin name, for example tcping'}
                                  value={getCommandValue(command)}
                                  onChange={(e) => updateCommandSourceValue(index, e.target.value)}
                                />
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
                              </div>
                            </div>
                            <div className="flex items-center justify-between">
                              <label className="text-sm text-gray-700 flex items-center gap-2">
                                <input type="checkbox" checked={command.ignore_target || false} onChange={(e) => updateCommand(index, { ignore_target: e.target.checked })} />
                                Ignore Target Input
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
                    </div>
                  </div>
                </div>
              </div>
            )}
          </div>
        </main>

        <footer className="app-footer">
          <div className="container max-w-7xl mx-auto px-3 sm:px-4 lg:px-6 py-3 sm:py-4 w-full">
            <div className="footer-content">
              <div className="footer-left">
                <a href="https://github.com/TogawaSakiko363/YALS" target="_blank" rel="noopener noreferrer" className="github-link flex items-center gap-0.5">
                  Powered by YALS
                  <Github className="w-4 h-4" />
                </a>
              </div>
              <div className="footer-right">
                <p>{config.footerRightText}</p>
              </div>
            </div>
          </div>
        </footer>
      </div>
    );
  }

  return (
    <div className="app-container" style={{ backgroundColor: config.backgroundColor }}>
      <header className="app-header">
        <div className="container max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 w-full">
          <div className="header-content">
            <div className="header-left">
              <div className="logo-container">
                <img src={config.logoPath} alt="Logo" className="logo-image" />
              </div>
              <div className="app-title">
                <h1 className="title-large">Looking Glass</h1>
              </div>
            </div>
          </div>
        </div>
      </header>

      <main className="main-content">
        <div className="container">
          <div className="grid-container">
            <div className="agent-item-container">
              <AgentSelector
                groups={groups}
                selectedAgent={selectedAgent}
                onSelectAgent={setSelectedAgent}
                disabled={isCommandRunning}
              />
            </div>

            <div className="command-panel-container">
              <CommandPanel
                selectedAgent={selectedAgent}
                isConnected={isConnected}
                activeCommands={activeCommands}
                onExecuteCommand={handleExecuteCommand}
                onStopCommand={handleStopCommand}
                onClearOutput={() => {
                  setLatestOutput(null);
                  clearAllStreamingOutputs();
                }}
                latestOutput={latestOutput}
                streamingOutputs={streamingOutputs}
                commands={commands}
              />
            </div>
          </div>
        </div>
      </main>

      <footer className="app-footer">
        <div className="container max-w-7xl mx-auto px-3 sm:px-4 lg:px-6 py-3 sm:py-4 w-full">
          <div className="footer-content">
            <div className="footer-left">
              <a href="https://github.com/TogawaSakiko363/YALS" target="_blank" rel="noopener noreferrer" className="github-link flex items-center gap-0.5">
                Powered by YALS
                <Github className="w-4 h-4" />
              </a>
              <p className="version-info">Version {appConfig?.version || 'unknown'}</p>
            </div>
            <div className="footer-right">
              <p>{config.footerRightText}</p>
            </div>
          </div>
        </div>
      </footer>
    </div>
  );
}

export default App;
