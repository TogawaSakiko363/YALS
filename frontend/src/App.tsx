import { useState, useEffect, useCallback } from 'react';
import { useYalsClient } from './hooks/useYalsClient';
import { AgentSelector } from './components/AgentSelector';
import { CommandPanel } from './components/CommandPanel';
import { CommandType, IPVersion } from './types/yals';
import { Github } from 'lucide-react';
import { CustomConfig } from './hooks/useCustomConfig';

interface AppProps {
  config: CustomConfig;
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
    stopCommand
  } = useYalsClient();

  // Attempt auto-connection when component mounts
  useEffect(() => {
    if (!isConnected && !isConnecting) {
      connect();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const [latestOutput, setLatestOutput] = useState<string | null>(null);

  const handleExecuteCommand = useCallback(async (command: CommandType, target: string, ipVersion: IPVersion) => {
    try {
      setLatestOutput(null); // Clear previous output
      clearAllStreamingOutputs(); // Clear all streaming outputs to prevent stale data
      
      const { response } = await executeCommand(command, target, ipVersion);
      
      // Set final output from response
      setLatestOutput(response.output || '');
    } catch (error: any) {
      console.error('Command execution failed:', error);
      setLatestOutput(error.message || 'Command execution failed');
    }
  }, [executeCommand, clearAllStreamingOutputs]);

  const handleStopCommand = useCallback(() => {
    // Find the first active command and stop it
    if (activeCommands.size > 0) {
      const firstActiveCommand = Array.from(activeCommands)[0];
      
      // Get current output before stopping
      const currentOutput = streamingOutputs.get(firstActiveCommand) || '';
      
      // Stop the command
      stopCommand(firstActiveCommand);
      
      // Update latest output with stopped message
      if (currentOutput) {
        setLatestOutput(currentOutput + '\n\n*** Command Stopped ***');
      }
    }
  }, [activeCommands, streamingOutputs, stopCommand]);

  const handleClearOutput = useCallback(() => {
    setLatestOutput(null);
    clearAllStreamingOutputs();
  }, [clearAllStreamingOutputs]);

  return (
    <div className="app-container" style={{ backgroundColor: config.backgroundColor }}>
      {/* Header */}
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

      {/* Main Content - flex-grow to fill available space */}
      <main className="main-content">
        <div className="container">
          <div className="grid-container">
            {/* Left Column - Agent Selection */}
            <div className="agent-item-container">
              <AgentSelector
                groups={groups}
                selectedAgent={selectedAgent}
                onSelectAgent={setSelectedAgent}
              />
            </div>

            {/* Right Column - Command Panel*/}
            <div className="command-panel-container">
              <CommandPanel
                selectedAgent={selectedAgent}
                isConnected={isConnected}
                activeCommands={activeCommands}
                onExecuteCommand={handleExecuteCommand}
                onStopCommand={handleStopCommand}
                onClearOutput={handleClearOutput}
                latestOutput={latestOutput}
                streamingOutputs={streamingOutputs}
                commands={commands}
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
