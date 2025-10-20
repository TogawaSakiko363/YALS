import { useState, useEffect } from 'react';
import { useYalsClient } from './hooks/useYalsClient';
import { ConnectionStatus } from './components/ConnectionStatus';
import { AgentSelector } from './components/AgentSelector';
import { CommandPanel } from './components/CommandPanel';
import { CommandHistory } from './components/CommandHistory';
import { CommandType } from './types/yals';
import { Github } from 'lucide-react';
import { config } from './custom';

// 动态导入logo
const logo = new URL(config.logoPath, import.meta.url).href;

function App() {
  const {
    isConnected,
    isConnecting,
    groups,
    selectedAgent,
    commandHistory,
    activeCommands,
    streamingOutputs,
    appConfig,
    commands,
    connect,
    disconnect,
    executeCommand,
    setSelectedAgent,
    clearHistory,
    clearStreamingOutput,
    stopCommand
  } = useYalsClient();

  // 组件挂载时尝试自动连接
  useEffect(() => {
    if (!isConnected && !isConnecting) {
      connect();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const [latestOutput, setLatestOutput] = useState<string | null>(null);
  const [currentCommandId, setCurrentCommandId] = useState<string | null>(null);

  const handleExecuteCommand = async (command: CommandType, target: string) => {
    try {
      const commandId = `${command}-${target.trim()}-${selectedAgent}`;
      setCurrentCommandId(commandId);
      setLatestOutput(null); // 清空之前的输出
      
      const res = await executeCommand(command, target);
      // 设置最终输出
      setLatestOutput(res.output || '');
      setCurrentCommandId(null); // 命令完成，清空当前命令ID
      
      // 延迟清理流式输出，确保Terminal组件能够显示最终结果
      setTimeout(() => {
        clearStreamingOutput(commandId);
      }, 100);
    } catch (error: any) {
      console.error('命令执行失败:', error);
      setLatestOutput(error.message || '命令执行失败');
      setCurrentCommandId(null);
    }
  };

  const handleStopCommand = () => {
    if (currentCommandId) {
      stopCommand(currentCommandId);
      // 不要立即清理状态，等待后端返回停止确认
      // setCurrentCommandId(null);
      // setLatestOutput('命令已停止');
    }
  };

  return (
    <div className="app-container" style={{ backgroundColor: config.backgroundColor }}>
      {/* Header */}
      <header className="app-header">
        <div className="container max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 w-full">
          <div className="header-content">
            <div className="header-left">
              <div className="logo-container">
                <img src={logo} alt="Logo" className="logo-image" />
              </div>
              <div className="app-title">
                <h1 className="title-large">Looking Glass</h1>
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

      {/* Main Content - flex-grow to fill available space */}
      <main className="main-content">
        <div className="container max-w-7xl mx-auto px-3 sm:px-4 lg:px-6 py-4 sm:py-6 lg:py-6 w-full flex-1">
          <div className="grid-container grid grid-cols-1 lg:grid-cols-3 gap-4 sm:gap-6 lg:gap-6 h-full">
            {/* Left Column - Agent Selection */}
            <div className="lg:col-span-1">
              <AgentSelector
                groups={groups}
                selectedAgent={selectedAgent}
                onSelectAgent={setSelectedAgent}
              />
            </div>

            {/* Right Column - Command Panel and History */}
            <div className="lg:col-span-2 space-y-4 sm:space-y-6 lg:space-y-6 flex flex-col">
              <CommandPanel
                selectedAgent={selectedAgent}
                isConnected={isConnected}
                activeCommands={activeCommands}
                onExecuteCommand={handleExecuteCommand}
                onStopCommand={handleStopCommand}
                latestOutput={latestOutput}
                streamingOutputs={streamingOutputs}
                currentCommandId={currentCommandId}
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

      {/* Footer - auto sticks to bottom */}
      <footer className="app-footer">
        <div className="container max-w-7xl mx-auto px-3 sm:px-4 lg:px-6 py-3 sm:py-4 w-full">
          <div className="footer-content">
            <div className="footer-left">
              <a href="https://github.com/TogawaSakiko363/YALS" target="_blank" rel="noopener noreferrer" className="github-link flex items-center gap-0.5">
                Powered by YALS
                <Github className="w-4 h-4" />
              </a>
              <p className="version-info">
                Version {appConfig?.version || 'unknown'}
              </p>
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
