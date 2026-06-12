import { useEffect, useState } from 'react';
import { Github, Settings } from 'lucide-react';
import { AgentSelector } from '../components/AgentSelector';
import { CommandPanel } from '../components/CommandPanel';
import { PageHeader } from '../components/PageHeader';
import { CustomConfig } from '../hooks/useCustomConfig';
import { useYalsClient } from '../hooks/useYalsClient';
import { CommandType, IPVersion } from '../types/yals';
import { getErrorMessage } from '../utils/error';

interface LookingGlassProps {
  config: CustomConfig;
}

// The public looking-glass home page: agent picker + command runner. It owns its
// own client connection, so it is the only place that opens the gRPC-web stream.
export function LookingGlass({ config }: LookingGlassProps) {
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

  const isCommandRunning = activeCommands.size > 0;
  const [latestOutput, setLatestOutput] = useState<string | null>(null);

  useEffect(() => {
    if (!isConnected && !isConnecting) {
      connect().catch(() => {
        // Connection errors are handled inside connect() (retry/backoff);
        // swallow here to avoid an unhandled promise rejection.
      });
    }
  }, [connect, isConnected, isConnecting]);

  const handleExecuteCommand = async (command: CommandType, target: string, ipVersion: IPVersion) => {
    try {
      setLatestOutput(null);
      clearAllStreamingOutputs();
      const { response } = await executeCommand(command, target, ipVersion);
      setLatestOutput(response.output || '');
    } catch (error: unknown) {
      console.error('Command execution failed:', error);
      setLatestOutput(getErrorMessage(error) || 'Command execution failed');
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

  return (
    <div className="app-container" style={{ backgroundColor: config.backgroundColor }}>
      <PageHeader config={config} active="home" />

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
              <a href="/control" title="Control panel" aria-label="Control panel" className="github-link flex items-center">
                <Settings className="w-4 h-4" />
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
