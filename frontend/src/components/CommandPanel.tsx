import React, { useState, useEffect, useMemo, useCallback } from 'react';
import { Play, Loader2 } from 'lucide-react';
import { CommandType, CommandConfig, IPVersion } from '../types/yals';
import { AnsiTerminal } from './AnsiTerminal';

interface CommandPanelProps {
  selectedAgent: string | null;
  isConnected: boolean;
  activeCommands: Set<string>;
  onExecuteCommand: (command: CommandType, target: string, ipVersion: IPVersion) => Promise<void>;
  onStopCommand?: () => void;
  onClearOutput?: () => void;
  latestOutput?: string | null;
  streamingOutputs?: Map<string, string>;
  commands: CommandConfig[];
}

interface CommandOption {
  value: CommandType;
  label: string;
  description: string;
  ignore_target: boolean;
}

export const CommandPanel: React.FC<CommandPanelProps> = React.memo(({
  selectedAgent,
  isConnected,
  activeCommands,
  onExecuteCommand,
  onStopCommand,
  latestOutput,
  streamingOutputs,
  commands
}) => {
  const [selectedCommand, setSelectedCommand] = useState<CommandType>('ping');
  const [target, setTarget] = useState('');
  const [ipVersion, setIpVersion] = useState<IPVersion>('auto');
  const [queueLimitError, setQueueLimitError] = useState<string | null>(null);

  // Convert commands array to CommandOption array (maintains order) - memoized
  const commandOptions: CommandOption[] = useMemo(() => 
    (commands || []).map((config) => ({
      value: config.name as CommandType,
      label: config.name.toUpperCase(),
      description: config.description,
      ignore_target: config.ignore_target || false
    })), [commands]);

  useEffect(() => {
    if (commandOptions.length > 0 && (!selectedCommand || !commandOptions.find(cmd => cmd.value === selectedCommand))) {
      setSelectedCommand(commandOptions[0].value);
    }
  }, [commandOptions, selectedCommand]);

  const hasCommands = commandOptions.length > 0;

  const handleExecute = useCallback(async () => {
    const currentCommand = commandOptions.find(cmd => cmd.value === selectedCommand);
    const requiresTarget = !currentCommand?.ignore_target;
    
    if (requiresTarget && !target.trim()) return;
    if (!selectedAgent || !isConnected) return;

    setQueueLimitError(null); // Clear previous error
    
    try {
      await onExecuteCommand(selectedCommand, requiresTarget ? target.trim() : '', ipVersion);
    } catch (error: any) {
      console.error('Command execution failed:', error);
      // Check if it's a queue limit error
      if (error.message && error.message.includes('execution limit')) {
        setQueueLimitError(error.message);
      }
    }
  }, [commandOptions, selectedCommand, target, ipVersion, selectedAgent, isConnected, onExecuteCommand]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleExecute();
    }
  }, [handleExecute]);

  // Get current command configuration - memoized
  const currentCommand = useMemo(() => 
    commandOptions.find(cmd => cmd.value === selectedCommand), 
    [commandOptions, selectedCommand]
  );
  const requiresTarget = !currentCommand?.ignore_target;
  
  const commandId = useMemo(() => {
    const sessionId = sessionStorage.getItem('yals_session_id') || '';
    return `${selectedCommand}-${requiresTarget ? target.trim() : ''}-${selectedAgent}-${sessionId}`;
  }, [selectedCommand, requiresTarget, target, selectedAgent]);
  
  const isCommandActive = useMemo(() => 
    activeCommands.has(commandId),
    [activeCommands, commandId]
  );

  // Get display output - memoized to avoid recalculation
  const displayOutput = useMemo(() => {
    if (streamingOutputs && streamingOutputs.size > 0) {
      const outputs = Array.from(streamingOutputs.values());
      const streamingOutput = outputs[outputs.length - 1];
      if (streamingOutput) return streamingOutput;
    }
    if (latestOutput) return latestOutput;
    return null;
  }, [streamingOutputs, latestOutput]);

  const outputText = useMemo(() => {
    if (!displayOutput) {
      return 'Please select command type and target address above, then click "Run" to start testing';
    }
    return displayOutput.length > 0 ? displayOutput : 'Command execution completed with no output';
  }, [displayOutput]);

  return (
    <div className="command-panel-container">
      {/* Command Panel container */}
      <div className="command-test-container">

        <div className="space-y-2">
          {!hasCommands && (
            <div className="text-center py-6 text-gray-500">
              <p>No commands available</p>
            </div>
          )}

          {hasCommands && (
            <div className="space-y-2">
              {/* Desktop layout: horizontal arrangement */}
              <div className="command-actions-desktop">
                {/* Command selection and IP Version in a row */}
                <div className="command-select-row">
                  {/* Command selection dropdown */}
                  <div className="command-select-container">
                    <select
                      value={selectedCommand}
                      onChange={(e) => setSelectedCommand(e.target.value as CommandType)}
                      className="command-select"
                      disabled={!isConnected || !selectedAgent || isCommandActive}
                    >
                      {commandOptions.map((cmd) => (
                        <option key={cmd.value} value={cmd.value}>
                          {cmd.label}
                        </option>
                      ))}
                    </select>
                  </div>

                  {/* IP Version selector */}
                  <div className="command-select-container">
                    <select
                      value={ipVersion}
                      onChange={(e) => setIpVersion(e.target.value as IPVersion)}
                      className="command-select"
                      disabled={!isConnected || !selectedAgent || isCommandActive}
                    >
                      <option value="auto">Auto</option>
                      <option value="ipv4">IPv4</option>
                      <option value="ipv6">IPv6</option>
                    </select>
                  </div>
                </div>

                {/* Target input - takes remaining space */}
                <div className="command-target-container">
                  <input
                    id="target-desktop"
                    type="text"
                    value={requiresTarget ? target : ''}
                    onChange={(e) => requiresTarget && setTarget(e.target.value)}
                    onKeyDown={requiresTarget ? handleKeyDown : undefined}
                    placeholder={requiresTarget ? "Enter the target" : "No target required"}
                    className="command-target-input"
                    disabled={!requiresTarget || !isConnected || !selectedAgent || isCommandActive}
                  />
                </div>

                {/* Execute/Stop button */}
                <div className="command-button-container">
                  <button
                    onClick={() => {
                      if (isCommandActive) {
                        onStopCommand?.();
                      } else {
                        handleExecute();
                      }
                    }}
                    disabled={!isConnected || !selectedAgent || (requiresTarget && !target.trim())}
                    className={`command-button ${
                      isCommandActive ? 'danger' : 'primary'
                    }`}
                  >
                    {isCommandActive ? (
                      <Loader2 className="w-3.5 h-3.5 animate-spin" />
                    ) : (
                      <Play className="w-3.5 h-3.5" />
                    )}
                    {isCommandActive ? 'Stop' : 'Run'}
                  </button>
                </div>
              </div>
            </div>
          )}

          {/* Queue limit error message */}
          {queueLimitError && (
            <div className="command-status error">
              {queueLimitError}
            </div>
          )}
        </div>
      </div>

      {/* Display terminal container directly, remove outer white card container */}
      <div className="terminal-container">
        {/* Terminal Header with macOS style dots */}
        <div className="terminal-header">
          <div className="terminal-dots">
            <div className="terminal-dot red"></div>
            <div className="terminal-dot yellow"></div>
            <div className="terminal-dot green"></div>
          </div>
        </div>

        {/* Terminal Content with ANSI color support */}
        <AnsiTerminal content={outputText} className="terminal-content" />
      </div>
    </div>
  );
});