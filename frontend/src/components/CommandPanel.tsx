import React, { useState, useMemo, useCallback } from 'react';
import { Play, Loader2 } from 'lucide-react';
import { CommandType, CommandConfig, IPVersion } from '../types/yals';
import { AnsiTerminal } from './AnsiTerminal';
import { getErrorMessage } from '../utils/error';

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
      ignore_target: config.ignore_target || false
    })), [commands]);

  // Derive the effective command instead of "fixing up" selectedCommand inside
  // an effect: when the available commands change (e.g. switching agent) and the
  // current selection is no longer valid, fall back to the first option. The
  // selectedCommand state still holds the user's explicit choice.
  const effectiveCommand = useMemo<CommandType | undefined>(() => {
    if (commandOptions.some(cmd => cmd.value === selectedCommand)) {
      return selectedCommand;
    }
    return commandOptions[0]?.value;
  }, [commandOptions, selectedCommand]);

  const hasCommands = commandOptions.length > 0;

  const handleExecute = useCallback(async () => {
    if (!effectiveCommand) return;
    const currentCommand = commandOptions.find(cmd => cmd.value === effectiveCommand);
    const requiresTarget = !currentCommand?.ignore_target;

    if (requiresTarget && !target.trim()) return;
    if (!selectedAgent || !isConnected) return;

    setQueueLimitError(null); // Clear previous error

    try {
      await onExecuteCommand(effectiveCommand, requiresTarget ? target.trim() : '', ipVersion);
    } catch (error: unknown) {
      console.error('Command execution failed:', error);
      // Check if it's a queue limit error
      const message = getErrorMessage(error);
      if (message.includes('execution limit')) {
        setQueueLimitError(message);
      }
    }
  }, [commandOptions, effectiveCommand, target, ipVersion, selectedAgent, isConnected, onExecuteCommand]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleExecute();
    }
  }, [handleExecute]);

  // Get current command configuration - memoized
  const currentCommand = useMemo(() =>
    commandOptions.find(cmd => cmd.value === effectiveCommand),
    [commandOptions, effectiveCommand]
  );
  const requiresTarget = !currentCommand?.ignore_target;

  const commandId = useMemo(() => {
    const sessionId = sessionStorage.getItem('yals_session_id') || '';
    return `${effectiveCommand ?? ''}-${requiresTarget ? target.trim() : ''}-${selectedAgent}-${sessionId}`;
  }, [effectiveCommand, requiresTarget, target, selectedAgent]);
  
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
            <div className="text-center py-6 u-text-muted">
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
                      value={effectiveCommand ?? ''}
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