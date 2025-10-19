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

  // 将commands map转换为CommandOption数组
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
      console.error('命令执行失败:', error);
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
      {/* 网络测试容器 */}
      <div className="command-test-container">
        <div className="panel-title">
          <Terminal className="panel-title-icon" />
          <h2 className="panel-title-text">网络测试</h2>
        </div>

        <div className="space-y-4">
          {!hasCommands && (
            <div className="text-center py-8 text-gray-500">
              <Terminal className="w-12 h-12 mx-auto mb-2 text-gray-300" />
              <p>暂无可用命令</p>
            </div>
          )}

          {hasCommands && (
            <div className="space-y-3">
              {/* 大屏幕布局：水平排列 */}
              <div className="command-actions-desktop">
                {/* 命令选择下拉菜单 */}
                <div className="command-select-container">
                  <label className="command-label">
                    命令类型
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

                {/* 目标输入 - 占据剩余空间 */}
                <div className="command-target-container">
                  <label htmlFor="target" className="command-label">
                    目标地址
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

                {/* 执行/停止按钮 */}
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
              
              {/* 小屏幕布局：垂直排列 */}
              <div className="command-actions-mobile">
                {/* 命令选择下拉菜单 */}
                <div>
                  <label className="command-label">
                    命令类型
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

                {/* 目标输入 */}
                <div>
                  <label htmlFor="target" className="command-label">
                    目标地址
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

                {/* 执行/停止按钮 */}
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

          {/* 状态提示 */}
          <div className="command-status">
            {!isConnected ? (
              <span className="command-status error">请先连接服务器</span>
            ) : !selectedAgent ? (
              <span className="command-status warning">请选择节点</span>
            ) : (
              <span>节点: <strong>{selectedAgent}</strong>   命令功能: {commandOptions.find(cmd => cmd.value === selectedCommand)?.description || '未知'}</span>
            )}
          </div>
        </div>
      </div>

      {/* 直接显示终端容器，移除外层的白色卡片容器 */}
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
            // 获取当前命令的输出
            const streamingOutput = currentCommandId ? streamingOutputs?.get(currentCommandId) : undefined;
            const isStreaming = currentCommandId ? activeCommands.has(currentCommandId) : false;
            
            // 优先显示流式输出，如果没有流式输出则显示最终输出
            let displayOutput: string | null | undefined;
            if (isStreaming && streamingOutput !== undefined && streamingOutput !== '') {
              displayOutput = streamingOutput;
            } else if (latestOutput !== null && latestOutput !== undefined && latestOutput !== '') {
              displayOutput = latestOutput;
            } else if (streamingOutput !== undefined && streamingOutput !== '') {
              // 命令完成但没有最终输出时，显示流式输出的内容
              displayOutput = streamingOutput;
            } else {
              displayOutput = null;
            }
            
            if (displayOutput === null || displayOutput === undefined) {
              return '请在上方选择命令类型和目标地址，然后点击"Run"开始测试';
            } else if (displayOutput && displayOutput.length > 0) {
              return displayOutput;
            } else {
              return '命令执行完成，无输出内容';
            }
          })()}
        </div>
      </div>
    </div>
  );
};