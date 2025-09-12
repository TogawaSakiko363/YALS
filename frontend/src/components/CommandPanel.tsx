import React, { useState, useEffect } from 'react';
import { Play, Terminal, Square, Loader2 } from 'lucide-react';
import { CommandType } from '../types/yals';
import { Terminal as XTermComponent } from './Terminal';

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
    <div className="bg-white rounded-lg shadow-sm border border-gray-200 p-4 flex flex-col">
      <div className="flex items-center gap-2 mb-3">
        <Terminal className="w-4 h-4 text-gray-600" />
        <h2 className="text-base font-semibold text-gray-900">网络测试</h2>
      </div>

      <div className="space-y-4 flex-1 flex flex-col">
        {!hasCommands && (
          <div className="text-center py-8 text-gray-500">
            <Terminal className="w-12 h-12 mx-auto mb-2 text-gray-300" />
            <p>暂无可用命令</p>
          </div>
        )}

        {hasCommands && (
          <div className="flex items-end gap-2">
            {/* 命令选择下拉菜单 */}
            <div className="shrink-0">
              <label className="block text-sm font-medium text-gray-700 mb-1">
                命令类型
              </label>
              <select
                value={selectedCommand}
                onChange={(e) => setSelectedCommand(e.target.value as CommandType)}
                className="w-auto min-w-[80px] px-3 py-2 text-sm border border-gray-300 rounded-md focus:ring-1 focus:ring-blue-500 focus:border-transparent transition-all duration-200 bg-white"
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
            <div className="flex-grow min-w-[150px]">
              <label htmlFor="target" className="block text-sm font-medium text-gray-700 mb-1">
                目标地址
              </label>
              <input
                id="target"
                type="text"
                value={target}
                onChange={(e) => setTarget(e.target.value)}
                onKeyPress={handleKeyPress}
                placeholder="Enter the target"
                className="w-full px-3 py-2 text-sm border border-gray-300 rounded-md focus:ring-1 focus:ring-blue-500 focus:border-transparent transition-all duration-200"
                disabled={!isConnected || !selectedAgent || isCommandActive}
              />
            </div>

            {/* 执行和停止按钮 */}
            <div className="ml-1 flex gap-2">
              {/* Run按钮 */}
              <button
                onClick={handleExecute}
                disabled={!canExecute || isCommandActive}
                className={`px-4 py-2 rounded-md text-sm font-medium transition-all duration-200 flex items-center gap-1.5 ${canExecute && !isCommandActive
                  ? 'bg-blue-600 text-white hover:bg-blue-700 shadow-sm hover:shadow-md'
                  : 'bg-gray-300 text-gray-500 cursor-not-allowed'
                  }`}
                style={{ height: '36px' }}
              >
                {isExecuting || isCommandActive ? (
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <Play className="w-3.5 h-3.5" />
                )}
                Run
              </button>

              {/* Stop按钮 */}
              <button
                onClick={onStopCommand}
                disabled={!isCommandActive}
                className={`px-4 py-2 rounded-md text-sm font-medium transition-all duration-200 flex items-center gap-1.5 ${isCommandActive
                  ? 'bg-red-600 text-white hover:bg-red-700 shadow-sm hover:shadow-md'
                  : 'bg-gray-300 text-gray-500 cursor-not-allowed'
                  }`}
                style={{ height: '36px' }}
              >
                <Square className="w-3.5 h-3.5" />
                Stop
              </button>
            </div>
          </div>
        )}

        {/* 状态提示 */}
        <div className="text-xs text-gray-600 mt-1">
          {!isConnected ? (
            <span className="text-red-600">请先连接服务器</span>
          ) : !selectedAgent ? (
            <span className="text-yellow-600">请选择代理节点</span>
          ) : (
            <span>节点: <strong>{selectedAgent}</strong>   命令功能: {commandOptions.find(cmd => cmd.value === selectedCommand)?.description || '未知'}</span>
          )}
        </div>

        {/* 美化的终端输出区域 */}
        <div className="mt-6">
          <div className="bg-gray-800 rounded-lg shadow-lg overflow-hidden">
            {/* Terminal Header with macOS style dots */}
            <div className="bg-gray-700 px-4 py-3 flex items-center">
              <div className="flex space-x-2">
                <div className="w-3 h-3 bg-red-500 rounded-full"></div>
                <div className="w-3 h-3 bg-yellow-500 rounded-full"></div>
                <div className="w-3 h-3 bg-green-500 rounded-full"></div>
              </div>
              <div className="flex-1 text-center">
                <span className="text-gray-300 text-sm font-medium">Terminal</span>
              </div>
            </div>

            {/* Terminal Content */}
            <div className="p-4 min-h-[300px]">
              <XTermComponent
                output={latestOutput}
                streamingOutput={currentCommandId ? streamingOutputs?.get(currentCommandId) : undefined}
                isStreaming={currentCommandId ? activeCommands.has(currentCommandId) : false}
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};