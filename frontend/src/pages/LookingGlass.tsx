import { useEffect, useState } from 'react';
import { AgentSelector } from '../components/AgentSelector';
import { CommandPanel } from '../components/CommandPanel';
import { PageHeader } from '../components/PageHeader';
import { PageFooter } from '../components/PageFooter';
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

      <PageFooter config={config} />
    </div>
  );
}
