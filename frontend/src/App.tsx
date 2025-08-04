import { useState, useEffect } from 'react';
import { useYalsClient } from './hooks/useYalsClient';
import { ConnectionStatus } from './components/ConnectionStatus';
import { AgentSelector } from './components/AgentSelector';
import { CommandPanel } from './components/CommandPanel';
import { CommandHistory } from './components/CommandHistory';
import { CommandType } from './types/yals';
import logo from './images/logo.png';

function App() {
  const {
    isConnected,
    isConnecting,
    groups,
    selectedAgent,
    commandHistory,
    activeCommands,
    appConfig,
    commands,
    connect,
    disconnect,
    executeCommand,
    setSelectedAgent,
    clearHistory
  } = useYalsClient();

  useEffect(() => {
    if (!isConnected && !isConnecting) {
      connect();
    }
  }, []);

  const [latestOutput, setLatestOutput] = useState<string | null>(null);

  const handleExecuteCommand = async (command: CommandType, target: string) => {
    try {
      const res = await executeCommand(command, target);
      setLatestOutput(res.output || '');
    } catch (error: any) {
      console.error('命令执行失败:', error);
      setLatestOutput(error.message || '命令执行失败');
    }
  };

  return (
    <div className="min-h-screen bg-gray-50 flex flex-col">
      <header className="bg-white shadow-sm border-b border-gray-200 flex-shrink-0">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 w-full">
          <div className="flex items-center justify-between h-16">
            <div className="flex items-center gap-3">
              <div className="flex items-center gap-2">
                <img src={logo} alt="Logo" className="w-30 h-12 object-contain" />
              </div>
              <div>
                <h1 className="text-l font-bold text-black-500">Looking Glass</h1>
              </div>
            </div>
            
            <ConnectionStatus
              isConnected={isConnected}
              isConnecting={isConnecting}
              onConnect={connect}
              onDisconnect={disconnect}
            />
          </div>
        </div>
      </header>

      <main className="flex-1 flex flex-col">
        <div className="max-w-7xl mx-auto px-3 sm:px-4 lg:px-6 py-4 sm:py-6 lg:py-6 w-full flex-1">
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-4 sm:gap-6 lg:gap-6 h-full">
            <div className="lg:col-span-1">
              <AgentSelector
                groups={groups}
                selectedAgent={selectedAgent}
                onSelectAgent={setSelectedAgent}
              />
            </div>

            <div className="lg:col-span-2 space-y-4 sm:space-y-6 lg:space-y-6 flex flex-col">
              <CommandPanel
                selectedAgent={selectedAgent}
                isConnected={isConnected}
                activeCommands={activeCommands}
                onExecuteCommand={handleExecuteCommand}
                latestOutput={latestOutput}
                commands={commands}
              />

              <CommandHistory
                history={commandHistory}
                activeCommands={activeCommands}
                onClearHistory={clearHistory}
              />
            </div>
          </div>
        </div>
      </main>

      <footer className="bg-white border-t border-gray-200 flex-shrink-0">
        <div className="max-w-7xl mx-auto px-3 sm:px-4 lg:px-6 py-3 sm:py-4 w-full">
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2">
            <div className="text-sm text-gray-600">
              <a href="https://github.com/TogawaSakiko363/YALS" target="_blank" rel="noopener noreferrer" className="text-xs sm:text-sm text-blue-600 hover:text-blue-800 hover:underline">Powered by YALS</a>
              <p className="text-xs text-gray-500 mt-0.5">
                Version {appConfig?.version || '1.0.0-basic'}
              </p>
            </div>
            <div className="text-xs sm:text-sm text-gray-500">
              <p>© Your Company Name here. All rights reserved.</p>
            </div>
          </div>
        </div>
      </footer>
    </div>
  );
}

export default App;
