import React, { useState, useEffect } from 'react';
import { Play, Terminal, Loader2 } from 'lucide-react';
import { CommandType } from '../types/yals';

interface CommandPanelProps {
  selectedAgent: string | null;
  isConnected: boolean;
  activeCommands: Set<string>;
  onExecuteCommand: (command: CommandType, target: string) => Promise<void>;
  onStopCommand?: () => void;
  latestOutput?: string | null;
  streamingOutputs?: Map<string, string>;
  currentCommandId?: string | null;
  commands: Record<string, string>;
}

interface CommandOption {
  value: CommandType;
  label: string;
  description: string;
}

export const CommandPanel: React.FC<CommandPanelProps> = ({
  selectedAgent,
  isConnected,
  activeCommands,
  onExecuteCommand,
  onStopCommand,
  latestOutput,
  streamingOutputs,
  currentCommandId,
  commands
}) => {
  const [selectedCommand, setSelectedCommand] = useState<CommandType>('ping');
  const [target, setTarget] = useState('');
  const [isExecuting, setIsExecuting] = useState(false);

  // Convert commands map to CommandOption array
  const commandOptions: CommandOption[] = Object.entries(commands || {}).map(([key, description]) => ({
    value: key as CommandType,
    label: key.toUpperCase(),
    description: description
  }));

  useEffect(() => {
    if (commandOptions.length > 0 && !selectedCommand) {
      setSelectedCommand(commandOptions[0].value);
    }
  }, [commandOptions, selectedCommand]);

  const hasCommands = commandOptions.length > 0;

  const handleExecute = async () => {
    if (!target.trim() || !selectedAgent || !isConnected) return;

    setIsExecuting(true);
    try {
      await onExecuteCommand(selectedCommand, target.trim());
    } catch (error) {
      console.error('Command execution failed:', error);
    } finally {
      setIsExecuting(false);
    }
  };

  const handleKeyPress = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleExecute();
    }
  };

  const canExecute = isConnected && selectedAgent && target.trim() && !isExecuting;
  const commandId = `${selectedCommand}-${target.trim()}-${selectedAgent}`;
  const isCommandActive = activeCommands.has(commandId);

  return (
    <div className="command-panel-container">
      {/* Network test container */}
      <div className="command-test-container">
        <div className="panel-title">
          <Terminal className="panel-title-icon" />
          <h2 className="panel-title-text">Network Test</h2>
        </div>

        <div className="space-y-4">
          {!hasCommands && (
            <div className="text-center py-8 text-gray-500">
              <Terminal className="w-12 h-12 mx-auto mb-2 text-gray-300" />
              <p>No commands available</p>
            </div>
          )}

          {hasCommands && (
            <div className="space-y-3">
              {/* Large screen layout: horizontal arrangement */}
              <div className="command-actions-desktop">
                {/* Command selection dropdown */}
                <div className="command-select-container">
                  <label className="command-label">
                    Command Type
                  </label>
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

                {/* Target input - takes remaining space */}
                <div className="command-target-container">
                  <label htmlFor="target" className="command-label">
                    Target Address
                  </label>
                  <input
                    id="target"
                    type="text"
                    value={target}
                    onChange={(e) => setTarget(e.target.value)}
                    onKeyPress={handleKeyPress}
                    placeholder="Enter the target"
                    className="command-target-input"
                    disabled={!isConnected || !selectedAgent || isCommandActive}
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
                    disabled={(!canExecute && !isCommandActive) || !onStopCommand}
                    className={`command-button ${
                      isCommandActive ? 'danger' : canExecute ? 'primary' : ''
                    }`}
                  >
                    {isExecuting || isCommandActive ? (
                      <Loader2 className="w-3.5 h-3.5 animate-spin" />
                    ) : (
                      <Play className="w-3.5 h-3.5" />
                    )}
                    {isExecuting || isCommandActive ? 'Stop' : 'Run'}
                  </button>
                </div>
              </div>
              
              {/* Small screen layout: vertical arrangement */}
              <div className="command-actions-mobile">
                {/* Command selection dropdown */}
                <div>
                  <label className="command-label">
                    Command Type
                  </label>
                  <select
                    value={selectedCommand}
                    onChange={(e) => setSelectedCommand(e.target.value as CommandType)}
                    className="command-select w-full"
                    disabled={!isConnected || !selectedAgent || isCommandActive}
                  >
                    {commandOptions.map((cmd) => (
                      <option key={cmd.value} value={cmd.value}>
                        {cmd.label}
                      </option>
                    ))}
                  </select>
                </div>

                {/* Target input */}
                <div>
                  <label htmlFor="target" className="command-label">
                    Target Address
                  </label>
                  <input
                    id="target"
                    type="text"
                    value={target}
                    onChange={(e) => setTarget(e.target.value)}
                    onKeyPress={handleKeyPress}
                    placeholder="Enter the target"
                    className="command-target-input"
                    disabled={!isConnected || !selectedAgent || isCommandActive}
                  />
                </div>

                {/* Execute/Stop button */}
                <div>
                  <button
                    onClick={() => {
                      if (isCommandActive) {
                        onStopCommand?.();
                      } else {
                        handleExecute();
                      }
                    }}
                    disabled={(!canExecute && !isCommandActive) || !onStopCommand}
                    className={`command-button command-button-full-width ${
                      isCommandActive ? 'danger' : canExecute ? 'primary' : ''
                    }`}
                  >
                    {isExecuting || isCommandActive ? (
                      <Loader2 className="w-3.5 h-3.5 animate-spin" />
                    ) : (
                      <Play className="w-3.5 h-3.5" />
                    )}
                    {isExecuting || isCommandActive ? 'Stop' : 'Run'}
                  </button>
                </div>
              </div>
            </div>
          )}

          {/* Status indicator */}
          <div className="command-status">
            {!isConnected ? (
              <span className="command-status error">Please connect to server first</span>
            ) : !selectedAgent ? (
              <span className="command-status warning">Please select a node</span>
            ) : (
              <span>Node: <strong>{selectedAgent}</strong>   Command: {commandOptions.find(cmd => cmd.value === selectedCommand)?.description || 'Unknown'}</span>
            )}
          </div>
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
          <div className="terminal-title">
            <span className="terminal-title-text">Terminal</span>
          </div>
        </div>

        {/* Terminal Content */}
        <div className="terminal-content">
          {(() => {
            // Get current command output
            const streamingOutput = currentCommandId ? streamingOutputs?.get(currentCommandId) : undefined;
            const isStreaming = currentCommandId ? activeCommands.has(currentCommandId) : false;
            
            // Prioritize streaming output, show final output if no streaming output
            let displayOutput: string | null | undefined;
            if (isStreaming && streamingOutput !== undefined && streamingOutput !== '') {
              displayOutput = streamingOutput;
            } else if (latestOutput !== null && latestOutput !== undefined && latestOutput !== '') {
              displayOutput = latestOutput;
            } else if (streamingOutput !== undefined && streamingOutput !== '') {
              // When command completes but no final output, show streaming output content
              displayOutput = streamingOutput;
            } else {
              displayOutput = null;
            }
            
            if (displayOutput === null || displayOutput === undefined) {
              return 'Please select command type and target address above, then click "Run" to start testing';
            } else if (displayOutput && displayOutput.length > 0) {
              return displayOutput;
            } else {
              return 'Command execution completed with no output';
            }
          })()}
        </div>
      </div>
    </div>
  );
};