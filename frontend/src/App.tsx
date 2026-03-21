import { useMemo, useState } from 'react';
import { Github, Plus, Save, Trash2, Shield, Server, KeyRound } from 'lucide-react';
import { AgentSelector } from './components/AgentSelector';
import { CommandPanel } from './components/CommandPanel';
import { CustomConfig } from './hooks/useCustomConfig';
import { useYalsClient } from './hooks/useYalsClient';
import { AgentCommand, AgentConfigPayload, AgentConfigRecord, CommandType, IPVersion } from './types/yals';

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
      description: 'ping'
    }
  ]
});

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
    loginControl,
    listManagedAgents,
    saveManagedAgent,
    deleteManagedAgent
  } = useYalsClient();

  const isControlPage = window.location.pathname === '/control.html';
  const [latestOutput, setLatestOutput] = useState<string | null>(null);
  const [controlPassword, setControlPassword] = useState('');
  const [controlError, setControlError] = useState<string | null>(null);
  const [controlMessage, setControlMessage] = useState<string | null>(null);
  const [editingAgent, setEditingAgent] = useState<AgentConfigPayload>(createEmptyAgent());

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
        setControlError('加载 Agent 管理数据失败');
      });
    }
    return null;
  }, [isControlAuthenticated, isControlPage, listManagedAgents]);

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
    } catch (error: any) {
      setControlError(error.message || '控制台登录失败');
    }
  };

  const updateCommand = (index: number, key: keyof AgentCommand, value: string | boolean | number | undefined) => {
    setEditingAgent((prev) => {
      const commandsCopy = [...prev.commands];
      commandsCopy[index] = { ...commandsCopy[index], [key]: value };
      return { ...prev, commands: commandsCopy };
    });
  };

  const addCommand = () => {
    setEditingAgent((prev) => ({
      ...prev,
      commands: [...prev.commands, { name: '', description: '', template: '' }]
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
      commands: record.commands
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
      setControlMessage(`已保存 Agent，UUID: ${saved.uuid}，Token: ${saved.token}`);
    } catch (error: any) {
      setControlError(error.message || '保存 Agent 失败');
    }
  };

  const handleDeleteAgent = async (uuid?: string) => {
    if (!uuid) return;
    try {
      setControlError(null);
      await deleteManagedAgent(uuid);
      setControlMessage('已删除 Agent');
      setEditingAgent(createEmptyAgent());
    } catch (error: any) {
      setControlError(error.message || '删除 Agent 失败');
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
                  <h2 className="text-lg font-semibold text-gray-900">控制台登录</h2>
                </div>
                <p className="text-sm text-gray-600 mb-4">使用 [`config.yaml`](config.yaml) 中 [`server.password`](config.yaml:7) 的值进入控制台。</p>
                <input
                  type="password"
                  value={controlPassword}
                  onChange={(event) => setControlPassword(event.target.value)}
                  className="command-target-input mb-3"
                  placeholder="输入控制台密码"
                />
                {controlError && <div className="command-status error mb-3">{controlError}</div>}
                <button className="command-button primary" onClick={handleControlLogin}>
                  <Shield className="w-4 h-4" /> 登录
                </button>
              </div>
            ) : (
              <div className="grid-container" style={{ gridTemplateColumns: '1.15fr 1.85fr' }}>
                <div className="bg-white shadow-sm border border-gray-200 p-4 rounded-md">
                  <div className="flex items-center justify-between mb-4">
                    <div className="flex items-center gap-2">
                      <Server className="w-4 h-4 text-gray-600" />
                      <h2 className="text-base font-semibold text-gray-900">已登记 Agent</h2>
                    </div>
                    <button className="command-button primary" onClick={startCreateAgent}>
                      <Plus className="w-4 h-4" /> 新建
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
                    {managedAgents.length === 0 && <p className="text-sm text-gray-500">暂无已登记 Agent</p>}
                  </div>
                </div>

                <div className="bg-white shadow-sm border border-gray-200 p-4 rounded-md space-y-4">
                  <div className="flex items-center justify-between">
                    <h2 className="text-base font-semibold text-gray-900">Agent 配置</h2>
                    {editingAgent.uuid && <span className="text-xs text-gray-500">UUID: {editingAgent.uuid}</span>}
                  </div>

                  <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                    <input className="command-target-input" placeholder="名称" value={editingAgent.name} onChange={(e) => setEditingAgent({ ...editingAgent, name: e.target.value })} />
                    <input className="command-target-input" placeholder="分组" value={editingAgent.group} onChange={(e) => setEditingAgent({ ...editingAgent, group: e.target.value })} />
                    <input className="command-target-input" placeholder="位置" value={editingAgent.details.location} onChange={(e) => setEditingAgent({ ...editingAgent, details: { ...editingAgent.details, location: e.target.value } })} />
                    <input className="command-target-input" placeholder="机房" value={editingAgent.details.datacenter} onChange={(e) => setEditingAgent({ ...editingAgent, details: { ...editingAgent.details, datacenter: e.target.value } })} />
                    <input className="command-target-input" placeholder="测试 IP" value={editingAgent.details.test_ip} onChange={(e) => setEditingAgent({ ...editingAgent, details: { ...editingAgent.details, test_ip: e.target.value } })} />
                    <input className="command-target-input" placeholder="描述" value={editingAgent.details.description} onChange={(e) => setEditingAgent({ ...editingAgent, details: { ...editingAgent.details, description: e.target.value } })} />
                  </div>

                  <div className="space-y-3">
                    <div className="flex items-center justify-between">
                      <h3 className="text-sm font-semibold text-gray-900">命令模板 / 插件</h3>
                      <button className="command-button primary" onClick={addCommand}>
                        <Plus className="w-4 h-4" /> 添加命令
                      </button>
                    </div>
                    {editingAgent.commands.map((command, index) => (
                      <div key={`${command.name}-${index}`} className="border border-gray-200 rounded-md p-3 space-y-3">
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                          <input className="command-target-input" placeholder="命令名" value={command.name} onChange={(e) => updateCommand(index, 'name', e.target.value)} />
                          <input className="command-target-input" placeholder="描述" value={command.description || ''} onChange={(e) => updateCommand(index, 'description', e.target.value)} />
                          <input className="command-target-input" placeholder="模板，如 ping -c 4" value={command.template || ''} onChange={(e) => updateCommand(index, 'template', e.target.value)} />
                          <input className="command-target-input" placeholder="插件名，如 tcping" value={command.use_plugin || ''} onChange={(e) => updateCommand(index, 'use_plugin', e.target.value)} />
                        </div>
                        <div className="flex items-center justify-between">
                          <label className="text-sm text-gray-700 flex items-center gap-2">
                            <input type="checkbox" checked={command.ignore_target || false} onChange={(e) => updateCommand(index, 'ignore_target', e.target.checked)} />
                            忽略目标输入
                          </label>
                          <button className="command-button danger" onClick={() => removeCommand(index)}>
                            <Trash2 className="w-4 h-4" /> 删除命令
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>

                  <div className="rounded-md border border-gray-200 p-4 bg-gray-50">
                    <div className="flex items-center gap-2 mb-2 text-sm font-semibold text-gray-900">
                      <KeyRound className="w-4 h-4" /> Agent Token
                    </div>
                    <input
                      className="command-target-input"
                      placeholder="Agent Token"
                      value={editingAgent.token}
                      onChange={(e) => setEditingAgent({ ...editingAgent, token: e.target.value })}
                    />
                    <p className="text-xs text-gray-500 mt-2">UUID 用于映射识别，Token 用于 agent 连接时的令牌鉴权。</p>
                  </div>

                  {controlError && <div className="command-status error">{controlError}</div>}
                  {controlMessage && <div className="command-status success">{controlMessage}</div>}

                  <div className="flex flex-wrap gap-3">
                    <button className="command-button primary" onClick={handleSaveAgent}>
                      <Save className="w-4 h-4" /> 保存 Agent
                    </button>
                    {editingAgent.uuid && (
                      <button className="command-button danger" onClick={() => handleDeleteAgent(editingAgent.uuid)}>
                        <Trash2 className="w-4 h-4" /> 删除 Agent
                      </button>
                    )}
                  </div>

                  <div className="rounded-md bg-gray-50 border border-gray-200 p-4 text-sm text-gray-700 space-y-2">
                    <p className="font-semibold">安装命令示例</p>
                    <code>yals_agent -s {window.location.hostname || 'example.com'} -p {window.location.port || '443'} -u {editingAgent.uuid || '[保存后生成 UUID]'} -t {editingAgent.token || '[Token]'}</code>
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
