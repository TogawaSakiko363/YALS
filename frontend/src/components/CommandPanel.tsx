import React, { useState, useEffect } from 'react';
import { Play, Terminal, Loader2 } from 'lucide-react';
import { CommandType } from '../types/yals';

interface CommandPanelProps {
  selectedAgent: string | null;
  isConnected: boolean;
  activeCommands: Set<string>;
  onExecuteCommand: (command: CommandType, target: string) => Promise<void>;
  latestOutput?: string | null;
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
  latestOutput,
  commands
}) => {
  const [selectedCommand, setSelectedCommand] = useState<CommandType>('ping');
  const [target, setTarget] = useState('');
  const [isExecuting, setIsExecuting] = useState(false);
  
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

        <div>
          <label className="block text-sm font-medium text-gray-700 mb-2">
            选择诊断命令
          </label>
          
          {!hasCommands && (
            <div className="text-center py-8 text-gray-500">
              <Terminal className="w-12 h-12 mx-auto mb-2 text-gray-300" />
              <p>暂无可用命令</p>
            </div>
          )}
          
          {hasCommands && (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-2">
              {commandOptions.map((cmd) => (
                <div
                  key={cmd.value}
                  onClick={() => setSelectedCommand(cmd.value)}
                  className={`p-3 rounded-md border cursor-pointer transition-all duration-200 ${
                    selectedCommand === cmd.value
                      ? 'border-blue-500 bg-blue-50'
                      : 'border-gray-200 hover:border-gray-300'
                  }`}
                >
                  <div className="flex items-center justify-between mb-1">
                    <h3 className="font-medium text-sm text-gray-900">{cmd.label}</h3>
                    {selectedCommand === cmd.value && (
                      <div className="w-2 h-2 bg-blue-500 rounded-full"></div>
                    )}
                  </div>
                  <p className="text-xs text-gray-600">{cmd.description}</p>
                </div>
              ))}
            </div>
          )}
          

        </div>


        <div>
          <label htmlFor="target" className="block text-sm font-medium text-gray-700 mb-1">
            目标地址
          </label>
          <input
            id="target"
            type="text"
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            onKeyPress={handleKeyPress}
            placeholder="例如：8.8.8.8 或 google.com"
            className="w-full px-3 py-2 text-sm border border-gray-300 rounded-md focus:ring-1 focus:ring-blue-500 focus:border-transparent transition-all duration-200"
            disabled={!isConnected || !selectedAgent}
          />
        </div>


        <div className="flex items-center justify-between">
          <div className="text-xs text-gray-600">
            {!isConnected ? (
              <span className="text-red-600">请先连接服务器</span>
            ) : !selectedAgent ? (
              <span className="text-yellow-600">请选择代理节点</span>
            ) : (
              <span>节点: <strong>{selectedAgent}</strong></span>
            )}
          </div>
          
          <button
            onClick={handleExecute}
            disabled={!canExecute || isCommandActive}
            className={`px-4 py-2 rounded-md text-sm font-medium transition-all duration-200 flex items-center gap-1.5 ${
              canExecute && !isCommandActive
                ? 'bg-blue-600 text-white hover:bg-blue-700 shadow-sm hover:shadow-md'
                : 'bg-gray-300 text-gray-500 cursor-not-allowed'
            }`}
          >
            {isExecuting || isCommandActive ? (
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
            ) : (
              <Play className="w-3.5 h-3.5" />
            )}
            {isExecuting || isCommandActive ? '执行中...' : '执行'}
          </button>
        </div>


        <div className="mt-6">
          <pre className="bg-slate-800 text-white p-4 rounded-lg text-xs whitespace-pre-wrap overflow-x-auto min-h-[120px] text-left">
            {latestOutput !== null ? (
              latestOutput || 'Command completed with no output'
            ) : (
              <span className="text-slate-400 italic">
                Select a command type and target address above, then click "Execute" to start testing
              </span>
            )}
          </pre>
        </div>
      </div>
    </div>
  );
};